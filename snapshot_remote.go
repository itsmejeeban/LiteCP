package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Snapshot storage is deliberately independent of website backup destinations.
type snapshotSettings struct {
	Destination  BackupDestination `json:"destination"`
	Cron         string            `json:"cron"`
	Enabled      bool              `json:"enabled"`
	LastRun      time.Time         `json:"lastRun"`
	Pending      string            `json:"pending,omitempty"`
	UploadStatus string            `json:"uploadStatus"`
}

var snapshotSettingsMu sync.Mutex

func (a *App) readSnapshotSettings() (snapshotSettings, error) {
	var cfg snapshotSettings
	b, err := os.ReadFile(filepath.Join(a.system.dataDir, "snapshot-settings.json"))
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(b, &cfg)
	return cfg, err
}
func (a *App) writeSnapshotSettings(cfg snapshotSettings) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(a.system.dataDir, "snapshot-settings.json"), b, 0600)
}
func (a *App) snapshotSettingsAPI(w http.ResponseWriter, r *http.Request) {
	snapshotSettingsMu.Lock()
	defer snapshotSettingsMu.Unlock()
	cfg, err := a.readSnapshotSettings()
	if err != nil {
		writeError(w, 500, "Cannot read snapshot settings")
		return
	}
	if r.Method == http.MethodGet {
		cfg.Destination.HasPassword = cfg.Destination.PasswordCipher != ""
		cfg.Destination.PasswordCipher = ""
		writeJSON(w, 200, cfg)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "Use GET or POST")
		return
	}
	var input struct {
		Destination BackupDestination `json:"destination"`
		Password    string            `json:"password"`
		Cron        string            `json:"cron"`
		Enabled     bool              `json:"enabled"`
	}
	if decodeJSON(r, &input) != nil {
		writeError(w, 400, "Invalid settings")
		return
	}
	if input.Enabled {
		if err = validateCronSchedule(input.Cron); err != nil {
			writeError(w, 400, err.Error())
			return
		}
	}
	d := input.Destination
	if strings.ContainsAny(d.Host+d.Username+d.RemotePath+d.HostKeySHA256, "\x00\r\n") {
		writeError(w, 400, "Invalid remote settings")
		return
	}
	d.PasswordCipher = cfg.Destination.PasswordCipher
	if input.Password != "" {
		if len(input.Password) > 256 || strings.ContainsAny(input.Password, "\r\n\x00") {
			writeError(w, 400, "Invalid password")
			return
		}
		d.PasswordCipher, err = a.system.EncryptSecret(input.Password)
		if err != nil {
			writeError(w, 500, "Cannot encrypt password")
			return
		}
	} else if d.Host != cfg.Destination.Host || d.Username != cfg.Destination.Username || d.Port != cfg.Destination.Port || d.Protocol != cfg.Destination.Protocol {
		d.PasswordCipher = ""
	}
	if d.Host != "" {
		if err = validateBackupDestination(d, true); err != nil {
			writeError(w, 400, err.Error())
			return
		}
	}
	if cfg.Pending != "" {
		writeError(w, 409, "Wait for the current snapshot upload before changing settings")
		return
	}
	cfg.Destination = d
	cfg.Cron = input.Cron
	cfg.Enabled = input.Enabled
	if err = a.writeSnapshotSettings(cfg); err != nil {
		writeError(w, 500, "Cannot save snapshot settings")
		return
	}
	writeJSON(w, 200, map[string]bool{"saved": true})
}
func (a *App) snapshotRemoteNames(ctx context.Context, d BackupDestination) ([]string, error) {
	if err := validateBackupDestination(d, true); err != nil {
		return nil, err
	}
	password, err := a.system.DecryptSecret(d.PasswordCipher)
	if err != nil {
		return nil, fmt.Errorf("Cannot read snapshot credentials")
	}
	var output string
	if d.Protocol == "sftp" {
		output, err = a.system.backupSFTPOutput(ctx, d, password, "ls -1 "+sftpBatchQuote(path.Clean(d.RemotePath)+"/")+"\n")
	} else {
		remote := (&url.URL{Scheme: d.Protocol, Host: net.JoinHostPort(d.Host, strconv.Itoa(d.Port)), Path: path.Clean(d.RemotePath) + "/"}).String()
		lines := []string{"fail", "silent", "show-error", "list-only", "connect-timeout = 20", "max-time = 60", `user = "` + curlConfigEscape(d.Username+":"+password) + `"`, `url = "` + curlConfigEscape(remote) + `"`}
		if d.Protocol == "ftps" {
			lines = append(lines, "ssl-reqd")
		}
		output, err = runCommandInput(ctx, 70*time.Second, strings.Join(lines, "\n")+"\n", "curl", "--disable", "--config", "-")
	}
	if err != nil {
		return nil, fmt.Errorf("Remote snapshot scan failed: %s", strings.ReplaceAll(err.Error(), password, "[redacted]"))
	}
	return snapshotNamesFromListing(output), nil
}

func snapshotNamesFromListing(output string) []string {
	names := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		name := path.Base(strings.TrimSpace(line))
		if strings.HasPrefix(strings.TrimSpace(line), "sftp>") {
			continue
		}
		if _, e := snapshotPath(name); e == nil && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
func (a *App) snapshotRemoteAPI(w http.ResponseWriter, r *http.Request) {
	snapshotSettingsMu.Lock()
	cfg, err := a.readSnapshotSettings()
	snapshotSettingsMu.Unlock()
	if err != nil {
		writeError(w, 500, "Cannot read snapshot settings")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Minute)
	defer cancel()
	if r.Method == http.MethodGet {
		names, e := a.snapshotRemoteNames(ctx, cfg.Destination)
		if e != nil {
			writeError(w, 502, e.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"items": names})
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "Use GET or POST")
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if decodeJSON(r, &input) != nil {
		writeError(w, 400, "Choose a snapshot")
		return
	}
	target, err := snapshotPath(input.Name)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if _, err = os.Lstat(target); err == nil {
		writeError(w, 409, "A local snapshot already has this name. Use its Inspect & restore action.")
		return
	} else if !os.IsNotExist(err) {
		writeError(w, 500, "Cannot check local snapshot")
		return
	}
	if err = os.MkdirAll(snapshotRoot, 0700); err != nil {
		writeError(w, 500, "Cannot access snapshot folder")
		return
	}
	// Download to a unique name; failed downloads never replace an existing archive.
	token, err := randomToken(10)
	if err != nil {
		writeError(w, 500, "Cannot allocate download")
		return
	}
	local := filepath.Join(snapshotRoot, "remote-"+safeSiteID(token)+".zip")
	remote := (&url.URL{Scheme: cfg.Destination.Protocol, Host: net.JoinHostPort(cfg.Destination.Host, strconv.Itoa(cfg.Destination.Port)), Path: path.Join(cfg.Destination.RemotePath, input.Name)}).String()
	b := Backup{Path: input.Name, RemotePath: remote, Size: 1 << 40}
	if err = a.system.historyRemoteOperation(ctx, &cfg.Destination, b, "download", local); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	if _, err = a.snapshotCommand(ctx, "inspect", local); err != nil {
		writeError(w, 400, "Downloaded snapshot did not pass validation; retained for review")
		return
	}
	writeJSON(w, 200, map[string]string{"name": filepath.Base(local)})
}
func (a *App) startSnapshotAutomation() {
	if a.system.demo {
		return
	}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.tickSnapshotAutomation()
		}
	}()
}
func (a *App) tickSnapshotAutomation() {
	snapshotSettingsMu.Lock()
	defer snapshotSettingsMu.Unlock()
	cfg, err := a.readSnapshotSettings()
	if err != nil {
		return
	}
	if cfg.Pending != "" {
		var status struct {
			State  string `json:"state"`
			Action string `json:"action"`
			Result struct {
				Path string `json:"path"`
			} `json:"result"`
		}
		content, e := os.ReadFile(filepath.Join(snapshotRoot, "status.json"))
		if e != nil || json.Unmarshal(content, &status) != nil {
			return
		}
		if status.State == "failed" {
			cfg.Pending = ""
			cfg.UploadStatus = "Snapshot failed; review snapshot status"
			_ = a.writeSnapshotSettings(cfg)
			return
		}
		if status.State != "complete" || status.Action != "create" || filepath.Base(status.Result.Path) != cfg.Pending {
			return
		}
		local, e := snapshotPath(cfg.Pending)
		if e != nil {
			return
		}
		if _, e = os.Stat(local); e != nil {
			return
		}
		cfg.UploadStatus = "Uploading snapshot"
		if a.writeSnapshotSettings(cfg) != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
		snapshotSettingsMu.Unlock()
		_, e = a.system.UploadBackup(ctx, cfg.Destination, local)
		snapshotSettingsMu.Lock()
		cancel()
		if e != nil {
			cfg.UploadStatus = "Upload failed; local snapshot retained: " + e.Error()
		} else {
			cfg.UploadStatus = "Uploaded successfully; local snapshot retained"
		}
		cfg.Pending = ""
		_ = a.writeSnapshotSettings(cfg)
		return
	}
	now := time.Now()
	if loc, e := time.LoadLocation(a.store.Settings().Timezone); e == nil {
		now = now.In(loc)
	}
	if !cfg.Enabled || cfg.LastRun.Unix()/60 == now.Unix()/60 || !backupScheduleDue(cfg.Cron, now) {
		return
	}
	cfg.LastRun = now
	if a.writeSnapshotSettings(cfg) != nil {
		return
	}
	name := "litecp-system-" + time.Now().UTC().Format("20060102-150405") + ".zip"
	local, _ := snapshotPath(name)
	err = a.startSnapshotJob(context.Background(), "create", local, "")
	if err != nil {
		cfg.UploadStatus = err.Error()
	} else if cfg.Destination.Host != "" {
		cfg.Pending = name
		cfg.UploadStatus = "Waiting for snapshot creation"
	} else {
		cfg.UploadStatus = "Local snapshot scheduled"
	}
	_ = a.writeSnapshotSettings(cfg)
}
