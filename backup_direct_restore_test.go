package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupRestoreCreatesJobDirectoryBeforeStatus(t *testing.T) {
	dir := t.TempDir()
	a := &App{system: &System{dataDir: dir}}
	job := LiveMigrationJob{ID: "restore-job-test", Source: LiveSource{Platform: "backup"}, Status: "running", Stage: "verify"}
	if err := a.initializeBackupRestoreJob(&job); err != nil {
		t.Fatal(err)
	}
	saved, err := a.loadLiveJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Source.Platform != "backup" || saved.Status != "running" {
		t.Fatalf("unexpected saved job: %+v", saved)
	}
	if _, err := os.Stat(filepath.Join(dir, "live-migrations", job.ID, "status.json")); err != nil {
		t.Fatal(err)
	}
}

func TestBackupRestoreRejectsInvalidJobDirectory(t *testing.T) {
	a := &App{system: &System{dataDir: t.TempDir()}}
	if err := a.initializeBackupRestoreJob(&LiveMigrationJob{ID: "../outside"}); err == nil {
		t.Fatal("invalid job ID accepted")
	}
}
