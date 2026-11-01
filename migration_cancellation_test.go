package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func cancellationFixture(t *testing.T, app *App, id, status string) LiveMigrationJob {
	t.Helper()
	root, err := app.liveJobRoot(id)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"native-backup", "password.enc"} {
		if err = os.WriteFile(filepath.Join(root, name), []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	job := LiveMigrationJob{ID: id, Status: status, Stage: "restore", AutoRestore: true, CredentialsRetained: true}
	if err = app.saveLiveJob(&job); err != nil {
		t.Fatal(err)
	}
	return job
}

func TestMigrationCancelAllIncludesPendingReview(t *testing.T) {
	app := &App{system: &System{dataDir: t.TempDir(), demo: true}, sessions: map[string]session{"test": {Expires: time.Now().Add(time.Hour)}}}
	statuses := []string{"queued", "connecting", "running", "review", "awaiting-overwrite", "restoring", "restore-review", "captured", "interrupted", "restored-review", "committing", "failed", "connected"}
	for i, status := range statuses {
		cancellationFixture(t, app, fmt.Sprintf("cancel_fixture_%03d", i), status)
	}
	ctx, cancel := context.WithCancel(context.Background())
	liveJobMu.Lock()
	liveCancels["cancel_fixture_002"] = cancel
	liveJobMu.Unlock()
	defer func() { liveJobMu.Lock(); delete(liveCancels, "cancel_fixture_002"); liveJobMu.Unlock() }()
	// Cancellation must bypass a restore's mutation lock, rather than wait for it.
	app.mutationMu.Lock()
	defer app.mutationMu.Unlock()
	request := httptest.NewRequest(http.MethodPost, "/api/live-migrations/cancel", strings.NewReader(`{"action":"cancel-all","confirm":true}`))
	request.AddCookie(&http.Cookie{Name: "litecp_session", Value: "test"})
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { app.withAuth(app.liveMigrationCancellation)(response, request); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Cancel All waited for the running restore")
	}
	if response.Code != 200 {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	var result struct {
		Cancelled int      `json:"cancelled"`
		Errors    []string `json:"errors"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Cancelled != 9 || len(result.Errors) != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("running worker context was not cancelled")
	}
	for i, status := range statuses {
		id := fmt.Sprintf("cancel_fixture_%03d", i)
		job, err := app.loadLiveJob(id)
		if err != nil {
			t.Fatal(err)
		}
		if i < 9 {
			if job.Status != "cancelled" || job.AutoRestore || job.CompletedAt.IsZero() {
				t.Fatalf("%s not cancelled: %+v", status, job)
			}
		} else if job.Status != status {
			t.Fatalf("completed/non-pending %s was changed", status)
		}
		root, _ := app.liveJobRoot(id)
		for _, name := range []string{"native-backup", "password.enc"} {
			content, err := os.ReadFile(filepath.Join(root, name))
			if err != nil || string(content) != "preserve" {
				t.Fatalf("cancellation changed retained %s", name)
			}
		}
	}
}

func TestMigrationCancellationSurvivesLateWorkerAndRestart(t *testing.T) {
	data := t.TempDir()
	app := &App{system: &System{dataDir: data, demo: true}}
	stale := cancellationFixture(t, app, "cancel_persist_123456", "restore-review")
	if _, err := app.cancelLiveMigration(stale.ID); err != nil {
		t.Fatal(err)
	}
	stale.Status = "restored-review"
	stale.AutoRestore = true
	if err := app.saveLiveJob(&stale); err != nil {
		t.Fatal(err)
	}
	if stale.Status != "cancelled" || stale.AutoRestore {
		t.Fatal("late worker revived cancelled job")
	}
	restarted := &App{system: &System{dataDir: data, demo: true}}
	job, err := restarted.loadLiveJob(stale.ID)
	if err != nil || job.Status != "cancelled" || job.AutoRestore {
		t.Fatalf("restart lost cancellation: %+v %v", job, err)
	}
	if _, err = restarted.cancelLiveMigration(stale.ID); err != nil {
		t.Fatal("repeat cancellation must be idempotent:", err)
	}
	// A separate capture process may finish writing stale status.json. The
	// durable marker remains authoritative when that state is read.
	stale.Status = "review"
	stale.AutoRestore = true
	content, _ := json.Marshal(stale)
	root, _ := app.liveJobRoot(stale.ID)
	if err = os.WriteFile(filepath.Join(root, "status.json"), content, 0600); err != nil {
		t.Fatal(err)
	}
	job, err = restarted.loadLiveJob(stale.ID)
	if err != nil || job.Status != "cancelled" || job.AutoRestore {
		t.Fatal("cross-process stale write revived job")
	}
}

func TestMigrationCancellationRequiresAuthenticationAndConfirmation(t *testing.T) {
	app := &App{system: &System{dataDir: t.TempDir(), demo: true}, sessions: map[string]session{"test": {Expires: time.Now().Add(time.Hour)}}}
	cancellationFixture(t, app, "cancel_access_123456", "restore-review")
	for _, authenticated := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodPost, "/api/live-migrations/cancel", strings.NewReader(`{"action":"cancel-all"}`))
		if authenticated {
			request.AddCookie(&http.Cookie{Name: "litecp_session", Value: "test"})
		}
		response := httptest.NewRecorder()
		app.withAuth(app.liveMigrationCancellation)(response, request)
		expected := 401
		if authenticated {
			expected = 400
		}
		if response.Code != expected {
			t.Fatalf("unexpected response %d", response.Code)
		}
	}
	job, _ := app.loadLiveJob("cancel_access_123456")
	if job.Status != "restore-review" {
		t.Fatal("unconfirmed cancellation changed job")
	}
	cancellationFixture(t, app, "cancel_commit_123456", "committing")
	if _, err := app.cancelLiveMigration("cancel_commit_123456"); err == nil {
		t.Fatal("atomic commit must not be interrupted")
	}
}
