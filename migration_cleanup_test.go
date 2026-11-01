package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMigrationCleanupProtectsUnfinishedJobs(t *testing.T) {
	for _, status := range []string{"queued", "connecting", "connected", "running", "restoring", "review", "awaiting-overwrite", "unknown"} {
		if migrationCleanupEligible(status) {
			t.Fatalf("cleanup must preserve %s", status)
		}
	}
	for _, status := range []string{"restored-review", "restore-review", "captured", "failed", "cancelled", "interrupted"} {
		if !migrationCleanupEligible(status) {
			t.Fatalf("cleanup should allow finished job %s", status)
		}
	}
}

func TestMigrationCleanupAuthenticatedRequestDoesNotDeadlock(t *testing.T) {
	app := &App{system: &System{dataDir: t.TempDir()}, sessions: map[string]session{"test": {Expires: time.Now().Add(time.Hour)}}}
	request := httptest.NewRequest(http.MethodPost, "/api/live-migrations", strings.NewReader(`{"action":"cleanup","confirm":true}`))
	request.AddCookie(&http.Cookie{Name: "litecp_session", Value: "test"})
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { app.withAuth(app.liveMigrations)(response, request); close(done) }()
	select {
	case <-done:
		if response.Code != 200 {
			t.Fatalf("cleanup failed: %d %s", response.Code, response.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("authenticated cleanup deadlocked on the mutation lock")
	}
}

func TestMigrationCleanupRequiresAuthenticationAndConfirmation(t *testing.T) {
	app := &App{system: &System{dataDir: t.TempDir()}, sessions: map[string]session{"test": {Expires: time.Now().Add(time.Hour)}}}
	for _, authenticated := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodPost, "/api/live-migrations", strings.NewReader(`{"action":"cleanup"}`))
		if authenticated {
			request.AddCookie(&http.Cookie{Name: "litecp_session", Value: "test"})
		}
		response := httptest.NewRecorder()
		app.withAuth(app.liveMigrations)(response, request)
		expected := 401
		if authenticated {
			expected = 400
		}
		if response.Code != expected {
			t.Fatalf("unexpected access result: %d", response.Code)
		}
	}
}

func TestMigrationCleanupRemovesOnlyStoppedTemporaryData(t *testing.T) {
	app := &App{system: &System{dataDir: t.TempDir()}}
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	jobID := "cleanup_stopped_123456"
	activeID := "cleanup_running_123456"
	live := filepath.Join(app.system.dataDir, "live-migrations")
	stages := filepath.Join(app.system.dataDir, "migrations")
	for id, status := range map[string]string{jobID: "captured", activeID: "running"} {
		data, _ := json.Marshal(LiveMigrationJob{ID: id, Status: status, NativeReady: true})
		write(filepath.Join(live, id, "status.json"), string(data))
		write(filepath.Join(live, id, "native-backup"), "native")
		write(filepath.Join(live, id, "portable-backup"), "portable")
		write(filepath.Join(live, id, "password.enc"), "encrypted")
		write(filepath.Join(stages, id, "files", "index.php"), "staged")
	}
	// A failed restage and its retained previous tree have generated names.
	for _, suffix := range []string{"12345", "12345-previous"} {
		write(filepath.Join(stages, ".restage-"+jobID+"-"+suffix, "files", "index.php"), "old staging")
	}
	uploadID := "uploaded_stage_123456"
	data, _ := json.Marshal(MigrationPreview{ID: uploadID})
	write(filepath.Join(stages, uploadID, "manifest.json"), string(data))
	write(filepath.Join(stages, uploadID, "files", "index.php"), "upload")
	write(filepath.Join(stages, "unrecognized", "important.txt"), "keep")
	write(filepath.Join(app.system.dataDir, "website", "index.php"), "live website")
	result, err := app.cleanMigrationTemporaryFiles()
	if err != nil {
		t.Fatal(err)
	}
	if result.Stages != 3 || result.RemovedPaths != 6 {
		t.Fatalf("unexpected cleanup result: %+v", result)
	}
	for _, path := range []string{filepath.Join(live, jobID, "native-backup"), filepath.Join(live, jobID, "portable-backup"), filepath.Join(stages, jobID), filepath.Join(stages, uploadID), filepath.Join(stages, ".restage-"+jobID+"-12345"), filepath.Join(stages, ".restage-"+jobID+"-12345-previous")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("temporary data remains: %s (%v)", path, err)
		}
	}
	for _, path := range []string{filepath.Join(live, jobID, "status.json"), filepath.Join(live, jobID, "password.enc"), filepath.Join(live, activeID, "native-backup"), filepath.Join(stages, activeID, "files", "index.php"), filepath.Join(stages, "unrecognized", "important.txt"), filepath.Join(app.system.dataDir, "website", "index.php")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("protected data lost: %s (%v)", path, err)
		}
	}
	second, err := app.cleanMigrationTemporaryFiles()
	if err != nil || second.RemovedPaths != 0 {
		t.Fatalf("repeat cleanup is not idempotent: %+v (%v)", second, err)
	}
}
