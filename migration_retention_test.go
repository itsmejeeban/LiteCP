package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationRetentionIgnoresIncompleteStages(t *testing.T) {
	app := &App{system: &System{dataDir: t.TempDir()}}
	count, err := app.retainedMigrationStages()
	if err != nil || count != 0 {
		t.Fatalf("empty retention: count=%d err=%v", count, err)
	}
	parent := filepath.Join(app.system.dataDir, "migrations")
	for _, id := range []string{"failed_one", "failed_two", "failed_three"} {
		if err := os.MkdirAll(filepath.Join(parent, id, "files"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(parent, "failed_two", "manifest.json"), []byte("invalid manifest"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "failed_three", "manifest.json"), []byte(`{"id":"another_stage"}`), 0600); err != nil {
		t.Fatal(err)
	}
	count, err = app.retainedMigrationStages()
	if err != nil || count != 0 {
		t.Fatalf("partial stages consumed retention: count=%d err=%v", count, err)
	}
	response := httptest.NewRecorder()
	app.migrationUpload(response, httptest.NewRequest("POST", "/api/migrations", nil))
	if response.Code != 400 {
		t.Fatalf("partial stages blocked new upload before validation: %d", response.Code)
	}
	for _, id := range []string{"ready_one", "ready_two", "ready_three"} {
		root := filepath.Join(parent, id)
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		content, _ := json.Marshal(MigrationPreview{ID: id})
		if err := os.WriteFile(filepath.Join(root, "manifest.json"), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	count, err = app.retainedMigrationStages()
	if err != nil || count != 3 {
		t.Fatalf("completed stages not counted: count=%d err=%v", count, err)
	}
	response = httptest.NewRecorder()
	app.migrationUpload(response, httptest.NewRequest("POST", "/api/migrations", nil))
	if response.Code != 409 {
		t.Fatalf("completed retention limit not enforced: %d", response.Code)
	}
}

func TestMigrationStageRejectsSymlinkDirectory(t *testing.T) {
	app := &App{system: &System{dataDir: t.TempDir()}}
	parent := filepath.Join(app.system.dataDir, "migrations")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "manifest.json"), []byte(`{"id":"linked_stage"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(parent, "linked_stage")); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	if _, _, err := app.loadMigration("linked_stage"); err == nil {
		t.Fatal("linked migration directory accepted")
	}
	if count, err := app.retainedMigrationStages(); err != nil || count != 0 {
		t.Fatalf("linked stage consumed retention: count=%d err=%v", count, err)
	}
}
