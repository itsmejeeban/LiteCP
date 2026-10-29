package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveSourceValidationAndPinnedIdentity(t *testing.T) {
	key := []byte("test SSH host public-key bytes")
	digest := sha256.Sum256(key)
	fingerprint := "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:])
	source := LiveSource{Platform: "cpanel", Host: "source.example.com", Port: 22, Fingerprint: fingerprint}
	if err := validateLiveSource(source); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"127.0.0.1", "169.254.169.254", "-oProxyCommand=evil", "host;touch /tmp/bad", "source.example.com\n"} {
		bad := source
		bad.Host = host
		if validateLiveSource(bad) == nil {
			t.Errorf("unsafe host accepted: %q", host)
		}
	}
	scan := "source.example.com ssh-ed25519 " + base64.StdEncoding.EncodeToString(key) + "\n"
	if _, err := pinnedKnownHost(scan, fingerprint); err != nil {
		t.Fatal(err)
	}
	if _, err := pinnedKnownHost(scan, "SHA256:"+base64.RawStdEncoding.EncodeToString(make([]byte, 32))); err == nil {
		t.Fatal("mismatched host key was accepted")
	}
	args := strings.Join(liveSSHArgs(source, "/private/job"), " ")
	for _, required := range []string{"StrictHostKeyChecking=yes", "BatchMode=yes", "IdentityAgent=none", "ForwardAgent=no", "/private/job/known_hosts", "root@source.example.com"} {
		if !strings.Contains(args, required) {
			t.Errorf("missing SSH protection %s", required)
		}
	}
}

func TestLiveSourceArchiveValidation(t *testing.T) {
	id := "abcdefghijklmnop1234"
	file := liveSourceFile{Path: "/root/litecp-export-" + id + "/cpmove-user.tar.gz", Size: 1024, SHA256: strings.Repeat("a", 64)}
	if err := validateLiveFile(&file, id); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/etc/shadow", "/root/litecp-export-" + id + "/../secret", "/root/litecp-export-" + id + "/x' ;id"} {
		bad := file
		bad.Path = path
		if validateLiveFile(&bad, id) == nil {
			t.Errorf("unsafe remote file accepted: %s", path)
		}
	}
	file.Size = liveNativeLimit + 1
	if validateLiveFile(&file, id) == nil {
		t.Fatal("oversized archive accepted")
	}
}

func TestLiveMigrationEndpointsRequireAuthentication(t *testing.T) {
	app := &App{}
	for _, path := range []string{"/api/live-migrations", "/api/live-migrations/download?id=abcdefghijklmnop"} {
		response := httptest.NewRecorder()
		app.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != 401 {
			t.Fatalf("%s returned %d", path, response.Code)
		}
	}
}

func TestLiveMigrationInterruptedJobAndNoSecrets(t *testing.T) {
	app := &App{system: &System{dataDir: t.TempDir()}, sessions: map[string]session{"test": {Expires: time.Now().Add(time.Hour)}}}
	id := "abcdefghijklmnop1234"
	root, _ := app.liveJobRoot(id)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	job := LiveMigrationJob{ID: id, Stage: "capture", Status: "running", SourceExport: "/root/litecp-export-secret/source", SourceExportRetained: true, Accounts: []LiveAccount{}, Warnings: []string{}, CredentialsRetained: true}
	if err := app.saveLiveJob(&job); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/live-migrations?id="+id, nil)
	request.AddCookie(&http.Cookie{Name: "litecp_session", Value: "test"})
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)
	var result LiveMigrationJob
	if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Status != "interrupted" {
		t.Fatal("interrupted job was not identified")
	}
	if strings.Contains(response.Body.String(), "privateKey") || strings.Contains(response.Body.String(), filepath.Join(root, "identity")) || strings.Contains(response.Body.String(), job.SourceExport) {
		t.Fatal("credential or internal path material leaked into public job state")
	}
	if !result.SourceExportRetained {
		t.Fatal("safe retained-export indicator missing")
	}
}

func TestLiveJobStageLifecycle(t *testing.T) {
	job := LiveMigrationJob{}
	setLiveJobStage(&job, "capture", "running", "Creating backup")
	if job.Stage != "capture" || job.Status != "running" || job.StageStartedAt.IsZero() || !job.CompletedAt.IsZero() {
		t.Fatal("running stage state is incomplete")
	}
	started := job.StageStartedAt
	setLiveJobStage(&job, "capture", "running", "Still creating backup")
	if !job.StageStartedAt.Equal(started) {
		t.Fatal("stage start changed without a stage transition")
	}
	setLiveJobStage(&job, "result", "restored-review", "Restore finished")
	if job.Stage != "result" || job.CompletedAt.IsZero() {
		t.Fatal("terminal stage was not completed")
	}
}

func TestLiveMigrationListReconcilesOrphanedJob(t *testing.T) {
	app := &App{system: &System{dataDir: t.TempDir()}, sessions: map[string]session{"test": {Expires: time.Now().Add(time.Hour)}}}
	id := "abcdefghijklmnop1234"
	root, _ := app.liveJobRoot(id)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	job := LiveMigrationJob{ID: id, Stage: "capture", Status: "running", CreatedAt: time.Now().UTC()}
	if err := app.saveLiveJob(&job); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/live-migrations", nil)
	request.AddCookie(&http.Cookie{Name: "litecp_session", Value: "test"})
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)
	var result struct {
		Items []LiveMigrationJob `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].Status != "interrupted" || result.Items[0].CompletedAt.IsZero() {
		t.Fatal("orphaned list job was not reconciled")
	}
}

func TestLivePasswordCredentialPipe(t *testing.T) {
	app := &App{system: &System{dataDir: t.TempDir()}}
	root := t.TempDir()
	password := "root-secret-fixture-28"
	encrypted, err := app.system.EncryptSecret(password)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, password) {
		t.Fatal("password not encrypted")
	}
	if err = os.WriteFile(filepath.Join(root, "password.enc"), []byte(encrypted), 0600); err != nil {
		t.Fatal(err)
	}
	source := LiveSource{AuthMethod: "password", Host: "source.example.com", Port: 22}
	args := liveSSHArgs(source, root)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "StrictHostKeyChecking=yes") || !strings.Contains(joined, "PasswordAuthentication=yes") || !strings.Contains(joined, "PubkeyAuthentication=no") {
		t.Fatal("password authentication protection missing")
	}
	command, cleanup, err := app.liveSSHCommand(context.Background(), source, root, args)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if strings.Contains(strings.Join(command.Args, " "), password) || len(command.Env) > 0 {
		t.Fatal("password exposed in command arguments/environment")
	}
	content, err := io.ReadAll(command.ExtraFiles[0])
	if err != nil || string(content) != password+"\n" {
		t.Fatal("credential pipe incorrect")
	}
	for _, invalid := range []string{"", "secret\nline", "secret\x00line", strings.Repeat("a", 1025)} {
		if validLiveRootPassword(invalid) {
			t.Fatal("invalid password accepted")
		}
	}
}

func TestLiveAutomaticFingerprintPrefersEd25519(t *testing.T) {
	key := []byte("fixture-ed25519")
	digest := sha256.Sum256(key)
	scan := "host ssh-rsa " + base64.StdEncoding.EncodeToString([]byte("rsa")) + "\n" + "host ssh-ed25519 " + base64.StdEncoding.EncodeToString(key) + "\n"
	fingerprint, err := discoveredLiveFingerprint(scan)
	if err != nil || fingerprint != "SHA256:"+base64.RawStdEncoding.EncodeToString(digest[:]) {
		t.Fatal("automatic host identity incorrect")
	}
	if _, err = discoveredLiveFingerprint("# no host key"); err == nil {
		t.Fatal("missing host key accepted")
	}
}
