package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed scripts/live-source.py
var liveSourceExporter string

const liveNativeLimit = int64(8 << 30)

var liveJobIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)
var liveAccountPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,252}$`)

func validLiveAccount(platform, account string) bool {
	if platform == "litecp" { return regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`).MatchString(account) }
	return liveAccountPattern.MatchString(account)
}
var liveJobMu sync.Mutex

// Serialize state writes with cancellation so a stale worker cannot revive a
// cancelled job. The marker also survives restarts and capture-worker processes.
var liveStateMu sync.Mutex
var liveRunning = map[string]bool{}
var liveCancels = map[string]context.CancelFunc{}

type LiveSource struct {
	AuthMethod  string `json:"authMethod,omitempty"`
	Platform    string `json:"platform"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Fingerprint string `json:"fingerprint"`
}
type LiveAccount struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
}
type liveSourceFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type liveExportResult struct {
	Accounts     []LiveAccount   `json:"accounts"`
	Native       *liveSourceFile `json:"native"`
	Portable     *liveSourceFile `json:"portable"`
	SourceExport string          `json:"sourceExport"`
	Warnings     []string        `json:"warnings"`
	Error        string          `json:"error"`
}
type LiveMigrationJob struct {
	CPanelWebsiteID      string                     `json:"cpanelWebsiteId,omitempty"`
	ArtifactsCleaned     bool                       `json:"artifactsCleaned,omitempty"`
	AutoRestore          bool                       `json:"autoRestore,omitempty"`
	Progress             *migrationTransferProgress `json:"progress,omitempty"`
	ID                   string                     `json:"id"`
	Source               LiveSource                 `json:"source"`
	Accounts             []LiveAccount              `json:"accounts"`
	Account              string                     `json:"account"`
	Status               string                     `json:"status"`
	Stage                string                     `json:"stage,omitempty"`
	Message              string                     `json:"message"`
	SourceExport         string                     `json:"-"`
	SourceExportRetained bool                       `json:"sourceExportRetained,omitempty"`
	NativeBytes          int64                      `json:"nativeBytes"`
	NativeSHA256         string                     `json:"nativeSha256,omitempty"`
	NativeReady          bool                       `json:"nativeReady"`
	CredentialsRetained  bool                       `json:"credentialsRetained"`
	Preview              *MigrationPreview          `json:"preview,omitempty"`
	Warnings             []string                   `json:"warnings"`
	CreatedAt            time.Time                  `json:"createdAt"`
	UpdatedAt            time.Time                  `json:"updatedAt"`
	StageStartedAt       time.Time                  `json:"stageStartedAt,omitempty"`
	CompletedAt          time.Time                  `json:"completedAt,omitempty"`
}

func setLiveJobStage(job *LiveMigrationJob, stage, status, message string) {
	if job.Stage != stage {
		job.Stage = stage
		job.StageStartedAt = time.Now().UTC()
	}
	job.Status = status
	job.Message = message
	if (status == "restored" || status == "restored-review") || status == "failed" || status == "restore-review" || status == "interrupted" || status == "cancelled" {
		job.CompletedAt = time.Now().UTC()
	}
}

func liveJobCancellable(job LiveMigrationJob) bool {
	switch job.Status {
	case "queued", "connecting", "running", "review", "awaiting-overwrite", "restoring", "restore-review", "captured", "interrupted", "restored-review":
		return true
	}
	return false
}

func migrationCleanupEligible(status string) bool {
	return (status == "restored" || status == "restored-review") || status == "restore-review" || status == "captured" || status == "failed" || status == "cancelled" || status == "interrupted"
}

func (a *App) liveMigrationCancelled(id string) bool {
	root, err := a.liveJobRoot(id)
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(root, "cancel-requested"))
	return err == nil
}

func (a *App) cancelLiveMigration(id string) (LiveMigrationJob, error) {
	liveStateMu.Lock()
	job, err := a.loadLiveJob(id)
	if err != nil {
		liveStateMu.Unlock()
		return job, err
	}
	if job.Status == "cancelled" {
		liveStateMu.Unlock()
		return job, nil
	}
	if !liveJobCancellable(job) {
		liveStateMu.Unlock()
		return job, errors.New("this migration is completed or is applying its final changes and cannot be cancelled")
	}
	root, err := a.liveJobRoot(job.ID)
	if err != nil {
		liveStateMu.Unlock()
		return job, err
	}
	if err = os.WriteFile(filepath.Join(root, "cancel-requested"), []byte(time.Now().UTC().Format(time.RFC3339)), 0600); err != nil {
		liveStateMu.Unlock()
		return job, errors.New("could not record the cancellation request")
	}
	job.AutoRestore = false
	job.Progress = nil
	setLiveJobStage(&job, job.Stage, "cancelled", "Migration cancelled. Captured files and source credentials were retained and were not deleted.")
	err = a.saveLiveJobLocked(&job)
	liveStateMu.Unlock()
	liveJobMu.Lock()
	if cancel := liveCancels[job.ID]; cancel != nil {
		cancel()
	}
	liveJobMu.Unlock()
	// Persist cancellation immediately; stopping a systemd worker may take time.
	if !a.system.demo {
		go func() {
			if liveCaptureActive(job.ID) {
				_, _ = runCommand(context.Background(), 15*time.Second, "systemctl", "stop", liveCaptureUnit(job.ID))
			}
		}()
	}
	if err != nil {
		return job, errors.New("migration stopped but its cancelled status could not be saved")
	}
	return job, nil
}

func (a *App) liveMigrationCancellation(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Action  string `json:"action"`
		ID      string `json:"id"`
		Confirm bool   `json:"confirm"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if !input.Confirm {
		writeError(w, 400, "Confirm migration cancellation")
		return
	}
	if input.Action == "cancel" {
		job, err := a.cancelLiveMigration(input.ID)
		if err != nil {
			writeError(w, 409, err.Error())
			return
		}
		writeJSON(w, 200, job)
		return
	}
	if input.Action != "cancel-all" {
		writeError(w, 400, "Select a cancellation action")
		return
	}
	a.cancelPendingLiveMigrations(w)
}

func (a *App) cancelPendingLiveMigrations(w http.ResponseWriter) {
	entries, err := os.ReadDir(filepath.Join(a.system.dataDir, "live-migrations"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		writeError(w, 500, "Cannot read live migration jobs")
		return
	}
	cancelled := 0
	failures := []string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		job, loadErr := a.loadLiveJob(entry.Name())
		if loadErr == nil && liveJobCancellable(job) {
			if _, cancelErr := a.cancelLiveMigration(job.ID); cancelErr == nil {
				cancelled++
			} else {
				failures = append(failures, job.ID+": "+cancelErr.Error())
			}
		}
	}
	writeJSON(w, 200, map[string]any{"cancelled": cancelled, "errors": failures})
}

func validateLiveSource(source LiveSource) error {
	if source.AuthMethod != "" && source.AuthMethod != "key" && source.AuthMethod != "password" {
		return errors.New("select SSH key or root password authentication")
	}
	if source.Platform != "cpanel" && source.Platform != "plesk" && source.Platform != "litecp" {
		return errors.New("select cPanel, Plesk or LiteCP")
	}
	if source.Port < 1 || source.Port > 65535 {
		return errors.New("SSH port must be between 1 and 65535")
	}
	if ip := net.ParseIP(source.Host); ip != nil {
		if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
			return errors.New("select the source hosting server, not a local or link-local address")
		}
	} else if !domainPattern.MatchString(source.Host) || len(source.Host) > 253 {
		return errors.New("enter a valid source IP or hostname")
	}
	value, err := base64.RawStdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(source.Fingerprint, "SHA256:"), "="))
	if !strings.HasPrefix(source.Fingerprint, "SHA256:") || err != nil || len(value) != 32 {
		return errors.New("enter the independently verified SHA256 SSH host-key fingerprint")
	}
	return nil
}

func pinnedKnownHost(scan, fingerprint string) (string, error) {
	for _, line := range strings.Split(scan, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		key, err := base64.StdEncoding.DecodeString(fields[2])
		if err != nil {
			continue
		}
		digest := sha256.Sum256(key)
		if "SHA256:"+base64.RawStdEncoding.EncodeToString(digest[:]) == strings.TrimSuffix(fingerprint, "=") {
			return line + "\n", nil
		}
	}
	return "", errors.New("source SSH host-key fingerprint does not match; connection rejected")
}

func (a *App) liveJobRoot(id string) (string, error) {
	if !liveJobIDPattern.MatchString(id) {
		return "", errors.New("invalid live migration ID")
	}
	return filepath.Join(a.system.dataDir, "live-migrations", id), nil
}
func (a *App) saveLiveJob(job *LiveMigrationJob) error {
	liveStateMu.Lock()
	defer liveStateMu.Unlock()
	return a.saveLiveJobLocked(job)
}

func (a *App) saveLiveJobLocked(job *LiveMigrationJob) error {
	root, err := a.liveJobRoot(job.ID)
	if err != nil {
		return err
	}
	if a.liveMigrationCancelled(job.ID) {
		job.AutoRestore = false
		job.Progress = nil
		setLiveJobStage(job, job.Stage, "cancelled", "Migration cancelled. Captured files and source credentials were retained and were not deleted.")
	}
	job.UpdatedAt = time.Now().UTC()
	content, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(root, "status.json"), content, 0600)
}
func (a *App) loadLiveJob(id string) (LiveMigrationJob, error) {
	var job LiveMigrationJob
	root, err := a.liveJobRoot(id)
	if err != nil {
		return job, err
	}
	content, err := os.ReadFile(filepath.Join(root, "status.json"))
	if err != nil {
		return job, errors.New("live migration job not found")
	}
	if json.Unmarshal(content, &job) != nil || job.ID != id {
		return job, errors.New("invalid live migration state")
	}
	if a.liveMigrationCancelled(id) {
		job.Status = "cancelled"
		job.AutoRestore = false
		job.Progress = nil
		job.Message = "Migration cancelled. Captured files and source credentials were retained and were not deleted."
	}
	return job, nil
}
func liveSSHArgs(source LiveSource, root string) []string {
	args := []string{"-F", "/dev/null", "-p", strconv.Itoa(source.Port), "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "GlobalKnownHostsFile=/dev/null", "-o", "UserKnownHostsFile=" + filepath.Join(root, "known_hosts"), "-o", "IdentitiesOnly=yes", "-o", "IdentityAgent=none", "-o", "ForwardAgent=no", "-o", "ClearAllForwardings=yes", "-o", "RequestTTY=no", "-o", "PasswordAuthentication=no", "-o", "KbdInteractiveAuthentication=no", "-o", "ConnectTimeout=15", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "-i", filepath.Join(root, "identity"), "root@" + source.Host}
	if source.AuthMethod == "password" {
		for i := range args {
			switch args[i] {
			case "BatchMode=yes":
				args[i] = "BatchMode=no"
			case "PasswordAuthentication=no":
				args[i] = "PasswordAuthentication=yes"
			}
		}
		args = append([]string{"-o", "PreferredAuthentications=password", "-o", "PubkeyAuthentication=no", "-o", "NumberOfPasswordPrompts=1"}, args...)
	}
	return args
}
func (a *App) liveRemote(ctx context.Context, source LiveSource, root, mode, account, id string) (liveExportResult, error) {
	var result liveExportResult
	request, _ := json.Marshal(map[string]string{"platform": source.Platform, "mode": mode, "account": account, "job": id})
	// The shell command contains only a base64 JSON token and fixed executable.
	command := "LITECP_EXPORT_REQUEST=" + base64.StdEncoding.EncodeToString(request) + " python3 -"
	args := append(liveSSHArgs(source, root), command)
	cmd, cleanup, err := a.liveSSHCommand(ctx, source, root, args)
	if err != nil {
		return result, err
	}
	defer cleanup()
	cmd.Stdin = strings.NewReader(liveSourceExporter)
	var stdout, stderr limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if json.Unmarshal(stdout.Bytes(), &result) != nil {
		return result, errors.New("source SSH/export command failed; verify the root key, source platform and Python 3 availability")
	}
	if result.Error != "" {
		return result, errors.New(result.Error)
	}
	if err != nil {
		return result, errors.New("source SSH/export command did not complete successfully")
	}
	return result, nil
}

func (a *App) liveMigrations(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if id := r.URL.Query().Get("id"); id != "" {
			job, err := a.loadLiveJob(id)
			if err != nil {
				writeError(w, 404, err.Error())
				return
			}
			liveJobMu.Lock()
			running := liveRunning[id]
			liveJobMu.Unlock()
			running = running || liveCaptureActive(id)
			if job.Status == "running" && !running {
				setLiveJobStage(&job, "capture", "interrupted", "The capture worker stopped before completion. Retained files need review; no destination was activated.")
				_ = a.saveLiveJob(&job)
			}
			a.attachMigrationProgress(&job)
			writeJSON(w, 200, job)
			return
		}
		items := []LiveMigrationJob{}
		entries, err := os.ReadDir(filepath.Join(a.system.dataDir, "live-migrations"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			writeError(w, 500, "Cannot read live migration jobs")
			return
		}
		for _, entry := range entries {
			if entry.IsDir() {
				if job, err := a.loadLiveJob(entry.Name()); err == nil {
					if job.Status == "running" && !liveCaptureActive(job.ID) {
						setLiveJobStage(&job, "capture", "interrupted", "The capture worker stopped before completion. Retained files need review; no destination was activated.")
						_ = a.saveLiveJob(&job)
					}
					a.attachMigrationProgress(&job)
					items = append(items, job)
				}
			}
		}
		sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
		writeJSON(w, 200, map[string]any{"items": items})
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "Use GET or POST.")
		return
	}
	if a.system.demo {
		writeError(w, 400, "Live migration requires the installed Linux server.")
		return
	}
	var input struct {
		Action     string     `json:"action"`
		ID         string     `json:"id"`
		Source     LiveSource `json:"source"`
		PrivateKey string     `json:"privateKey"`
		Password   string     `json:"password"`
		Account    string     `json:"account"`
		Accounts   []string   `json:"accounts"`
		Confirm    bool       `json:"confirm"`
	}
	if err := decodeJSONWithLimit(r, &input, 80<<10); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if input.Action == "queue" {
		a.queueLiveAccounts(w, r, input.ID, input.Accounts, input.Confirm)
		return
	}
	if input.Action == "cleanup" {
		if !input.Confirm {
			writeError(w, 400, "Confirm deletion of finished migration temporary files")
			return
		}
		// withAuth already holds mutationMu for this POST. Taking it again
		// deadlocks before any files are removed.
		result, err := a.cleanMigrationTemporaryFiles()
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, result)
		return
	}
	if input.Action == "cancel" {
		if !input.Confirm {
			writeError(w, 400, "Confirm migration cancellation")
			return
		}
		job, err := a.cancelLiveMigration(input.ID)
		if err != nil {
			writeError(w, 409, err.Error())
			return
		}
		writeJSON(w, 200, job)
		return
	}
	if input.Action == "cancel-all" {
		if !input.Confirm {
			writeError(w, 400, "Confirm cancellation of all pending migrations")
			return
		}
		a.cancelPendingLiveMigrations(w)
		return
	}
	if input.Action == "fingerprint" {
		source := input.Source
		if source.AuthMethod != "password" {
			writeError(w, 400, "Automatic host-key discovery is for password authentication")
			return
		}
		source.Fingerprint = "SHA256:" + base64.RawStdEncoding.EncodeToString(make([]byte, 32))
		if err := validateLiveSource(source); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		scan, err := runCommand(r.Context(), 15*time.Second, "ssh-keyscan", "-T", "10", "-p", strconv.Itoa(source.Port), "-t", "ed25519,ecdsa,rsa", source.Host)
		if err != nil {
			writeError(w, 502, "Could not retrieve the SSH host key; verify the source address and SSH port")
			return
		}
		fingerprint, err := discoveredLiveFingerprint(scan)
		if err != nil {
			writeError(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"fingerprint": fingerprint})
		return
	}
	if input.Action == "connect" {
		if err := validateLiveSource(input.Source); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if input.Source.AuthMethod == "password" {
			if !validLiveRootPassword(input.Password) {
				writeError(w, 400, "Enter a root password of 1–1024 characters without line breaks.")
				return
			}
			if _, err := exec.LookPath("sshpass"); err != nil {
				writeError(w, 503, "Password migration requires sshpass; update the LiteCP installer dependencies.")
				return
			}
		} else if len(input.PrivateKey) < 100 || len(input.PrivateKey) > 64<<10 || !strings.Contains(input.PrivateKey, "PRIVATE KEY") {
			writeError(w, 400, "Provide an unencrypted root SSH private key.")
			return
		}
		// Discovery only creates small private connection metadata. Retained backups
		// must not block reconnecting; archive capacity is checked at capture time.
		if _, err := migrationFreeBytes(r.Context(), a.system.dataDir); err != nil {
			writeError(w, 503, "Cannot verify migration storage availability.")
			return
		}
		id, err := randomToken(18)
		if err != nil {
			writeError(w, 500, "Could not allocate job")
			return
		}
		id = safeSiteID(id)
		root, _ := a.liveJobRoot(id)
		if err := os.MkdirAll(root, 0700); err != nil {
			writeError(w, 500, "Cannot create private migration directory")
			return
		}
		job := LiveMigrationJob{ID: id, Source: input.Source, Stage: "connection", Status: "connecting", Message: "Verifying SSH host identity", Accounts: []LiveAccount{}, Warnings: []string{}, CredentialsRetained: true, CreatedAt: time.Now().UTC(), StageStartedAt: time.Now().UTC()}
		if input.Source.AuthMethod == "password" {
			cipher, err := a.system.EncryptSecret(input.Password)
			if err != nil {
				writeError(w, 500, "Cannot protect source password")
				return
			}
			if err = os.WriteFile(filepath.Join(root, "password.enc"), []byte(cipher), 0600); err != nil {
				writeError(w, 500, "Cannot store source credential")
				return
			}
		} else {
			if err := os.WriteFile(filepath.Join(root, "identity"), []byte(input.PrivateKey+"\n"), 0600); err != nil {
				writeError(w, 500, "Cannot store root-only source credential")
				return
			}
		}
		_ = a.saveLiveJob(&job)
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		if input.Source.AuthMethod != "password" {
			if _, err := runCommand(ctx, 5*time.Second, "ssh-keygen", "-y", "-P", "", "-f", filepath.Join(root, "identity")); err != nil {
				job.Status = "failed"
				job.Message = "The SSH key is invalid or needs a passphrase. Use a dedicated migration key without a passphrase."
				_ = a.saveLiveJob(&job)
				writeError(w, 400, job.Message)
				return
			}
		}
		scan, err := runCommand(ctx, 15*time.Second, "ssh-keyscan", "-T", "10", "-p", strconv.Itoa(input.Source.Port), "-t", "ed25519,ecdsa,rsa", input.Source.Host)
		if err == nil {
			var known string
			known, err = pinnedKnownHost(scan, input.Source.Fingerprint)
			if err == nil {
				err = os.WriteFile(filepath.Join(root, "known_hosts"), []byte(known), 0600)
			}
		}
		if err == nil {
			var result liveExportResult
			result, err = a.liveRemote(ctx, input.Source, root, "discover", "", id)
			if err == nil {
				job.Accounts = result.Accounts
			}
		}
		if err != nil {
			job.Status = "failed"
			job.Message = err.Error()
			_ = a.saveLiveJob(&job)
			writeError(w, 502, job.Message)
			return
		}
		setLiveJobStage(&job, "selection", "connected", "Host key verified. Select one or more source websites.")
		if err := a.saveLiveJob(&job); err != nil {
			writeError(w, 500, "Could not save discovery state")
			return
		}
		writeJSON(w, 201, job)
		return
	}
	if input.Action == "retry" {
		original, err := a.loadLiveJob(input.ID)
		if err != nil {
			writeError(w, 404, err.Error())
			return
		}
		if !input.Confirm || !original.CredentialsRetained || !migrationCleanupEligible(original.Status) || liveCaptureActive(original.ID) {
			writeError(w, 409, "Retry requires retained credentials and a stopped, incomplete capture.")
			return
		}
		if _, err := selectedMigrationAccounts(original, []string{original.Account}); err != nil {
			writeError(w, 409, "Reconnect and select the source website before retrying capture.")
			return
		}
		// Repeated clicks must reuse an active retry or a verified backup waiting
		// for overwrite consent, rather than capture the same website again.
		entries, err := os.ReadDir(filepath.Join(a.system.dataDir, "live-migrations"))
		if err != nil {
			writeError(w, 500, "Cannot inspect existing migration retries")
			return
		}
		var replacement *LiveMigrationJob
		for _, entry := range entries {
			existing, err := a.loadLiveJob(entry.Name())
			if err == nil && existing.ID != original.ID && existing.Source == original.Source && existing.Account == original.Account &&
				(existing.Status == "queued" || existing.Status == "running" || existing.Status == "restoring" ||
					(existing.Status == "awaiting-overwrite" && existing.CreatedAt.After(original.CreatedAt) && !existing.ArtifactsCleaned && existing.NativeReady && existing.Preview != nil)) {
				if replacement == nil || existing.CreatedAt.After(replacement.CreatedAt) {
					replacement = &existing
				}
			}
		}
		if replacement != nil {
			writeJSON(w, 202, replacement)
			return
		}
		id, err := randomToken(18)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		id = safeSiteID(id)
		oldRoot, _ := a.liveJobRoot(original.ID)
		newRoot, _ := a.liveJobRoot(id)
		if err = os.MkdirAll(newRoot, 0700); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		names := []string{"known_hosts", "identity"}
		if original.Source.AuthMethod == "password" {
			names = []string{"known_hosts", "password.enc"}
		}
		for _, name := range names {
			content, e := os.ReadFile(filepath.Join(oldRoot, name))
			if e != nil {
				writeError(w, 409, "Source credentials unavailable; reconnect the source.")
				return
			}
			if e = os.WriteFile(filepath.Join(newRoot, name), content, 0600); e != nil {
				writeError(w, 500, e.Error())
				return
			}
		}
		original.ID = id
		original.CPanelWebsiteID = ""
		original.ArtifactsCleaned = false
		original.NativeReady = false
		original.NativeBytes = 0
		original.NativeSHA256 = ""
		original.SourceExport = ""
		original.SourceExportRetained = false
		original.Warnings = nil
		original.Preview = nil
		original.Progress = nil
		original.Stage = "queued"
		original.CompletedAt = time.Time{}
		original.Status = "queued"
		original.AutoRestore = true
		original.Message = "Capture retry queued for the selected website. Previous capture files were retained."
		original.CreatedAt = time.Now().UTC()
		original.StageStartedAt = original.CreatedAt
		if err = a.saveLiveJob(&original); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, 202, original)
		return
	}
	if input.Action == "capture" {
		job, err := a.loadLiveJob(input.ID)
		if err != nil {
			writeError(w, 404, err.Error())
			return
		}
		if job.Status != "connected" || !job.CredentialsRetained || !input.Confirm {
			writeError(w, 409, "Review and confirm native source backup creation before capture; source credentials must still be available.")
			return
		}
		found := false
		for _, account := range job.Accounts {
			if account.ID == input.Account {
				found = true
			}
		}
		if !found || !validLiveAccount(job.Source.Platform, input.Account) {
			writeError(w, 400, "Select a discovered source account")
			return
		}
		entries, _ := os.ReadDir(filepath.Join(a.system.dataDir, "live-migrations"))
		for _, entry := range entries {
			if liveCaptureActive(entry.Name()) {
				writeError(w, 409, "Another capture worker is already running.")
				return
			}
		}
		liveJobMu.Lock()
		if len(liveRunning) > 0 {
			liveJobMu.Unlock()
			writeError(w, 409, "Another live capture is already running.")
			return
		}
		liveRunning[job.ID] = true
		liveJobMu.Unlock()
		job.AutoRestore = true
		job.Account = input.Account
		setLiveJobStage(&job, "capture", "running", "Creating source native backup. This can take time; no target website is being changed.")
		if err := a.saveLiveJob(&job); err != nil {
			liveJobMu.Lock()
			delete(liveRunning, job.ID)
			liveJobMu.Unlock()
			writeError(w, 500, "Could not save capture job")
			return
		}
		if job.Status == "cancelled" || a.liveMigrationCancelled(job.ID) {
			liveJobMu.Lock()
			delete(liveRunning, job.ID)
			liveJobMu.Unlock()
			writeJSON(w, 200, job)
			return
		}
		_, startErr := runCommand(r.Context(), 15*time.Second, "systemd-run", "--unit="+liveCaptureUnit(job.ID), "--collect", "--property=RuntimeMaxSec=7500", "--property=UMask=0077", "--setenv=LITECP_DATA_DIR="+a.system.dataDir, "/usr/local/bin/litecp", "capture-worker", job.ID)
		liveJobMu.Lock()
		delete(liveRunning, job.ID)
		liveJobMu.Unlock()
		if startErr != nil {
			job.Status = "failed"
			job.Message = "Cannot start independent capture worker: " + startErr.Error()
			_ = a.saveLiveJob(&job)
			writeError(w, 502, job.Message)
			return
		}
		writeJSON(w, 202, job)
		return
	}
	if input.Action == "forget" {
		job, err := a.loadLiveJob(input.ID)
		if err != nil {
			writeError(w, 404, err.Error())
			return
		}
		liveJobMu.Lock()
		running := liveRunning[job.ID]
		liveJobMu.Unlock()
		running = running || liveCaptureActive(job.ID)
		if running || job.Status == "queued" || !input.Confirm {
			writeError(w, 409, "Confirm credential removal after the capture has stopped.")
			return
		}
		root, _ := a.liveJobRoot(job.ID)
		for _, name := range []string{"identity", "known_hosts", "password.enc"} {
			if err := os.Remove(filepath.Join(root, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				writeError(w, 500, "Could not remove source credentials")
				return
			}
		}
		job.CredentialsRetained = false
		if err := a.saveLiveJob(&job); err != nil {
			writeError(w, 500, "Credential removal completed but job state could not be saved")
			return
		}
		writeJSON(w, 200, job)
		return
	}
	writeError(w, 400, "Unknown live migration action")
}

func validateLiveFile(file *liveSourceFile, jobID string) error {
	if file == nil || file.Size <= 0 || file.Size > liveNativeLimit {
		return errors.New("source archive is absent, empty, or exceeds the 8 GiB direct-transfer limit")
	}
	prefix := "/root/litecp-export-" + jobID + "/"
	if !strings.HasPrefix(file.Path, prefix) || filepath.Clean(file.Path) != file.Path || strings.ContainsAny(file.Path, "'\"\r\n\x00") {
		return errors.New("source returned an unexpected archive path")
	}
	digest, err := hex.DecodeString(file.SHA256)
	if err != nil || len(digest) != 32 {
		return errors.New("source archive checksum is invalid")
	}
	return nil
}
func (a *App) transferLiveFile(ctx context.Context, source LiveSource, root, destination string, file *liveSourceFile) error {
	descriptor, _ := json.Marshal(migrationTransferProgress{File: filepath.Base(destination), Total: file.Size})
	if err := writeAtomic(filepath.Join(root, "transfer.json"), descriptor, 0600); err != nil {
		return err
	}
	available, err := migrationFreeBytes(ctx, root)
	if err != nil {
		return err
	}
	if available < file.Size*2+(1<<30) {
		return errors.New("not enough local disk space to retain and inspect the source archive safely")
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer output.Close()
	cmd, cleanup, err := a.liveSSHCommand(ctx, source, root, append(liveSSHArgs(source, root), "cat -- '"+file.Path+"'"))
	if err != nil {
		return err
	}
	defer cleanup()
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(output, hash), io.LimitReader(pipe, file.Size+1))
	if copyErr != nil || written != file.Size {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return errors.New("archive transfer was interrupted or its size changed; retained partial data was not imported")
	}
	if err := cmd.Wait(); err != nil {
		return errors.New("SSH archive stream failed; retained data was not imported")
	}
	if hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return errors.New("archive SHA256 verification failed; data was not imported")
	}
	return output.Sync()
}
func (a *App) captureLiveMigration(job LiveMigrationJob) {
	defer func() { liveJobMu.Lock(); delete(liveRunning, job.ID); liveJobMu.Unlock() }()
	defer func() {
		if recover() != nil {
			job.Status = "failed"
			job.Message = "Capture stopped unexpectedly. Retained files and source export need review; no destination was activated."
			_ = a.saveLiveJob(&job)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	root, _ := a.liveJobRoot(job.ID)
	cancelled := func() bool {
		_, err := os.Stat(filepath.Join(root, "cancel-requested"))
		return err == nil
	}
	fail := func(err error) {
		if cancelled() {
			setLiveJobStage(&job, job.Stage, "cancelled", "Migration cancelled. Captured files and source credentials were retained and were not deleted.")
		} else {
			setLiveJobStage(&job, job.Stage, "failed", err.Error())
		}
		_ = a.saveLiveJob(&job)
	}
	if cancelled() {
		fail(context.Canceled)
		return
	}
	result, err := a.liveRemote(ctx, job.Source, root, "capture", job.Account, job.ID)
	if err != nil {
		fail(err)
		return
	}
	job.SourceExport = result.SourceExport
	job.SourceExportRetained = result.SourceExport != ""
	job.Warnings = result.Warnings
	if cancelled() {
		fail(context.Canceled)
		return
	}
	if err := validateLiveFile(result.Native, job.ID); err != nil {
		fail(err)
		return
	}
	setLiveJobStage(&job, "transfer", "running", "Source backup finished. Transferring and verifying the native archive.")
	_ = a.saveLiveJob(&job)
	if err := a.transferLiveFile(ctx, job.Source, root, filepath.Join(root, "native-backup"), result.Native); err != nil {
		fail(err)
		return
	}
	if cancelled() {
		fail(context.Canceled)
		return
	}
	job.NativeReady = true
	job.NativeBytes = result.Native.Size
	job.NativeSHA256 = result.Native.SHA256
	setLiveJobStage(&job, "verify", "running", "Native backup checksum verified. Transferring the compatible website archive.")
	_ = a.saveLiveJob(&job)
	archive := filepath.Join(root, "native-backup")
	{
		if result.Portable == nil {
			setLiveJobStage(&job, "preflight", "captured", "Native backup verified. This source layout needs manual compatibility review before website import.")
			_ = a.saveLiveJob(&job)
			return
		}
		if err := validateLiveFile(result.Portable, job.ID); err != nil {
			fail(err)
			return
		}
		archive = filepath.Join(root, "portable-backup")
		if err := a.transferLiveFile(ctx, job.Source, root, archive, result.Portable); err != nil {
			fail(err)
			return
		}
		if cancelled() {
			fail(context.Canceled)
			return
		}
	}
	if job.Source.Platform == "cpanel" {
		if _, err := a.liveRemote(ctx, job.Source, root, "cleanup", job.Account, job.ID); err != nil {
			fail(errors.New("source backup was copied and verified, but LiteCP could not remove the temporary cPanel archive: " + err.Error()))
			return
		}
		job.SourceExport = ""
		job.SourceExportRetained = false
		_ = a.saveLiveJob(&job)
	}
	stage := filepath.Join(a.system.dataDir, "migrations", job.ID)
	required, sizeErr := migrationArchiveBytes(ctx, archive)
	if sizeErr != nil {
		setLiveJobStage(&job, "preflight", "captured", "Native backup verified. Cannot estimate website extraction: "+sizeErr.Error())
		_ = a.saveLiveJob(&job)
		return
	}
	if available, err := migrationFreeBytes(ctx, root); err != nil || available < required+(1<<30) {
		setLiveJobStage(&job, "preflight", "captured", "Native backup verified. Insufficient disk space for this archive plus a 1 GiB safety reserve; no target data was changed.")
		_ = a.saveLiveJob(&job)
		return
	}
	if err := os.MkdirAll(filepath.Join(stage, "files"), 0700); err != nil {
		fail(err)
		return
	}
	setLiveJobStage(&job, "extract", "running", "Archive verified. Inspecting and staging website files and database dumps.")
	_ = a.saveLiveJob(&job)
	preview, err := extractMigrationWithLimit(ctx, archive, filepath.Join(stage, "files"), job.Source.Platform, liveMigrationExtractLimit)
	if err != nil {
		setLiveJobStage(&job, "preflight", "captured", "Native backup verified; website staging could not complete: "+err.Error())
		_ = a.saveLiveJob(&job)
		return
	}
	preview.ID = job.ID
	content, _ := json.Marshal(preview)
	if err := writeAtomic(filepath.Join(stage, "manifest.json"), content, 0600); err != nil {
		fail(err)
		return
	}
	job.Preview = &preview
	setLiveJobStage(&job, "preflight", "review", "Backup verified and staged. LiteCP will automatically restore it after compatibility checks.")
	_ = a.saveLiveJob(&job)
	// Worker status is stored separately; the HTTP process owns config/activity writes.
}

func migrationFreeBytes(ctx context.Context, path string) (int64, error) {
	space, err := runCommand(ctx, 5*time.Second, "df", "-Pk", path)
	if err != nil {
		return 0, errors.New("cannot verify free migration disk space")
	}
	lines := strings.Split(strings.TrimSpace(space), "\n")
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 6 {
		return 0, errors.New("cannot parse free migration disk space")
	}
	available, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil || available < 0 {
		return 0, errors.New("invalid migration disk-space result")
	}
	return available * 1024, nil
}

func (a *App) migrationDatabaseImport(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID, SQL, DatabaseID, Password string
		Confirm                       bool
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if !input.Confirm {
		writeError(w, 400, "Confirm restore into an empty managed database; a failed import resets only that empty target.")
		return
	}
	preview, root, err := a.loadMigration(input.ID)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	if !containsString(preview.Databases, input.SQL) {
		writeError(w, 400, "Select a SQL dump from the inspected migration")
		return
	}
	database := a.store.DatabaseByID(input.DatabaseID)
	if database == nil {
		writeError(w, 404, "Managed target database not found")
		return
	}
	if err := a.system.importScopedSQL(r.Context(), filepath.Join(root, "files", filepath.FromSlash(input.SQL)), *database, input.Password); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	a.store.AddActivity("Migration database restored", database.Name+": from inspected SQL; application credentials still need review", "success")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) liveMigrationDownload(w http.ResponseWriter, r *http.Request) {
	job, err := a.loadLiveJob(r.URL.Query().Get("id"))
	if err != nil || !job.NativeReady {
		writeError(w, 404, "Verified native backup not found")
		return
	}
	root, _ := a.liveJobRoot(job.ID)
	file, err := os.Open(filepath.Join(root, "native-backup"))
	if err != nil {
		writeError(w, 404, "Native backup file is unavailable")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, 400, "Invalid backup file")
		return
	}
	extension := ".tar"
	if job.Source.Platform == "cpanel" {
		extension = ".tar.gz"
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s-native%s"`, job.Source.Platform, job.ID, extension))
	http.ServeContent(w, r, "native"+extension, info.ModTime(), file)
}

func validLiveRootPassword(password string) bool {
	return len(password) > 0 && len(password) <= 1024 && !strings.ContainsAny(password, "\r\n\x00")
}

// Pass passwords through an inherited pipe, never command arguments or env.
func (a *App) liveSSHCommand(ctx context.Context, source LiveSource, root string, args []string) (*exec.Cmd, func(), error) {
	if source.AuthMethod != "password" {
		return exec.CommandContext(ctx, "ssh", args...), func() {}, nil
	}
	cipher, err := os.ReadFile(filepath.Join(root, "password.enc"))
	if err != nil {
		return nil, nil, errors.New("source password is unavailable")
	}
	password, err := a.system.DecryptSecret(string(cipher))
	if err != nil {
		return nil, nil, errors.New("source password could not be decrypted")
	}
	if !validLiveRootPassword(password) {
		return nil, nil, errors.New("invalid stored source credential")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	if _, err = writer.Write([]byte(password + "\n")); err != nil {
		reader.Close()
		writer.Close()
		return nil, nil, err
	}
	writer.Close()
	cmd := exec.CommandContext(ctx, "sshpass", append([]string{"-d", "3", "ssh"}, args...)...)
	cmd.ExtraFiles = []*os.File{reader}
	return cmd, func() { reader.Close() }, nil
}

func discoveredLiveFingerprint(scan string) (string, error) {
	for _, algorithm := range []string{"ssh-ed25519", "ecdsa-sha2-nistp256", "ssh-rsa"} {
		for _, line := range strings.Split(scan, "\n") {
			fields := strings.Fields(line)
			if len(fields) != 3 || fields[1] != algorithm {
				continue
			}
			key, err := base64.StdEncoding.DecodeString(fields[2])
			if err != nil || len(key) == 0 {
				continue
			}
			hash := sha256.Sum256(key)
			return "SHA256:" + base64.RawStdEncoding.EncodeToString(hash[:]), nil
		}
	}
	return "", errors.New("source did not return a supported SSH host key")
}
