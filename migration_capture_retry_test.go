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

func TestCaptureRetryQueuesSelectedWebsiteAndPreservesFailedJob(t *testing.T) {
	app := &App{system: &System{dataDir: t.TempDir()}}
	original := LiveMigrationJob{
		ID: "abcdefghijklmnop1234", Status: "failed", Stage: "capture",
		Source:  LiveSource{Platform: "plesk", Host: "source.example.test", Port: 22, AuthMethod: "password"},
		Account: "api.example.test", Accounts: []LiveAccount{{ID: "api.example.test", Domain: "api.example.test"}},
		CredentialsRetained: true, NativeReady: true, NativeBytes: 100,
		SourceExport: "/root/retained", SourceExportRetained: true,
	}
	root, _ := app.liveJobRoot(original.ID)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"password.enc", "known_hosts", "native-backup"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("retained-"+name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.saveLiveJob(&original); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "status.json"))
	var retry LiveMigrationJob
	for i := 0; i < 2; i++ {
		response := httptest.NewRecorder()
		app.liveMigrations(response, httptest.NewRequest(http.MethodPost, "/api/live-migrations", strings.NewReader(`{"action":"retry","id":"`+original.ID+`","confirm":true}`)))
		if response.Code != http.StatusAccepted {
			t.Fatalf("retry not queued: %d %s", response.Code, response.Body.String())
		}
		var got LiveMigrationJob
		if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Status != "queued" || got.Stage != "queued" || !got.AutoRestore || got.Account != original.Account || got.NativeReady || got.NativeBytes != 0 || got.SourceExportRetained {
			t.Fatalf("retry cannot proceed automatically or inherited stale artifacts: %+v", got)
		}
		if i == 0 {
			retry = got
		} else if got.ID != retry.ID {
			t.Fatal("repeated retry created a duplicate migration")
		}
	}
	after, _ := os.ReadFile(filepath.Join(root, "status.json"))
	if string(before) != string(after) {
		t.Fatal("retry changed the original job")
	}
	newRoot, _ := app.liveJobRoot(retry.ID)
	for _, name := range []string{"password.enc", "known_hosts"} {
		value, err := os.ReadFile(filepath.Join(newRoot, name))
		if err != nil || string(value) != "retained-"+name {
			t.Fatal("retry credentials were not copied", err)
		}
	}
	value, err := os.ReadFile(filepath.Join(root, "native-backup"))
	if err != nil || string(value) != "retained-native-backup" {
		t.Fatal("old backup was not preserved", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(root))
	if len(entries) != 2 {
		t.Fatal("unexpected retry directories", len(entries))
	}
}

func TestCaptureRetryRequiresSelectedDiscoveredWebsite(t *testing.T) {
	app := &App{system: &System{dataDir: t.TempDir()}}
	job := LiveMigrationJob{ID: "abcdefghijklmnop1234", Status: "failed", Account: "unknown.example.test", CredentialsRetained: true, Accounts: []LiveAccount{{ID: "api.example.test"}}}
	root, _ := app.liveJobRoot(job.ID)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := app.saveLiveJob(&job); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	app.liveMigrations(response, httptest.NewRequest(http.MethodPost, "/api/live-migrations", strings.NewReader(`{"action":"retry","id":"`+job.ID+`","confirm":true}`)))
	if response.Code != http.StatusConflict {
		t.Fatalf("undiscovered website accepted: %d %s", response.Code, response.Body.String())
	}
	entries, _ := os.ReadDir(filepath.Dir(root))
	if len(entries) != 1 {
		t.Fatal("invalid retry created a job")
	}
}

func TestCaptureRetryReusesVerifiedBackupAwaitingOverwrite(t *testing.T) {
	app := &App{system: &System{dataDir: t.TempDir()}}
	failed := LiveMigrationJob{
		ID: "abcdefghijklmnop1234", Status: "failed", Account: "api.example.test",
		Source:   LiveSource{Platform: "plesk", Host: "source.example.test", Port: 22},
		Accounts: []LiveAccount{{ID: "api.example.test"}}, CredentialsRetained: true,
	}
	oldest := failed
	oldest.ID = "abcdefghijklmnop2345"
	oldest.Status, oldest.NativeReady = "awaiting-overwrite", true
	oldest.Preview = &MigrationPreview{}
	oldest.CreatedAt = time.Now().Add(-time.Hour)
	latest := oldest
	latest.ID = "abcdefghijklmnop3456"
	latest.CreatedAt = time.Now()
	for _, job := range []LiveMigrationJob{failed, oldest, latest} {
		root, _ := app.liveJobRoot(job.ID)
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := app.saveLiveJob(&job); err != nil {
			t.Fatal(err)
		}
	}
	response := httptest.NewRecorder()
	app.liveMigrations(response, httptest.NewRequest(http.MethodPost, "/api/live-migrations", strings.NewReader(`{"action":"retry","id":"`+failed.ID+`","confirm":true}`)))
	var result LiveMigrationJob
	if response.Code != http.StatusAccepted || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.ID != latest.ID || result.Status != "awaiting-overwrite" {
		t.Fatalf("verified overwrite-waiting job was not reused: %d %s", response.Code, response.Body.String())
	}
	root, _ := app.liveJobRoot(failed.ID)
	entries, _ := os.ReadDir(filepath.Dir(root))
	if len(entries) != 3 {
		t.Fatal("retry created a duplicate capture", len(entries))
	}
	if result.AutoRestore {
		t.Fatal("retry changed the overwrite consent state")
	}
}
