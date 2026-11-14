package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrationStatusIndexSeesReplacementAndCancellation(t *testing.T) {
	a := &App{system: &System{dataDir: t.TempDir()}}
	job := LiveMigrationJob{ID: "migration-12345678", Status: "running", MigrationBatch: "batch"}
	root, err := a.liveJobRoot(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "status.json")
	// A worker can replace the file with the same size and modification time.
	// The cache must observe the replacement even with these attributes unchanged.
	at := time.Now().Truncate(time.Second)
	write := func(status string) {
		t.Helper()
		if err := writeAtomic(path, []byte(`{"id":"`+job.ID+`","status":"`+status+`","migrationBatch":"batch"}`), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	check := func(status string) {
		t.Helper()
		statuses := a.migrationStatuses()
		if len(statuses) != 1 || statuses[0].Status != status || statuses[0].MigrationBatch != job.MigrationBatch {
			t.Fatalf("expected %s, got %+v", status, statuses)
		}
	}
	write("running")
	check("running")
	write("errored")
	check("errored")
	if err := os.WriteFile(filepath.Join(root, "cancel-requested"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	check("cancelled")
}
