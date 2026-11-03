package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSuccessfulMigrationAutoCleanup(t *testing.T) {
	for _, status := range []string{"restored", "restored-review", "cancelled", "failed"} {
		t.Run(status, func(t *testing.T) {
			app := &App{system: &System{dataDir: t.TempDir()}}
			job := LiveMigrationJob{ID: "cleanup_success_fixture", Status: status, NativeReady: true}
			root, _ := app.liveJobRoot(job.ID)
			archive := filepath.Join(root, "native-backup")
			stage := filepath.Join(app.system.dataDir, "migrations", job.ID)
			for _, dir := range []string{archive, stage} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "fixture"), []byte("data"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			history := filepath.Join(root, "job.json")
			if err := os.WriteFile(history, []byte("history"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := app.cleanSuccessfulMigration(&job); err != nil {
				t.Fatal(err)
			}
			_, err := os.Stat(archive)
			if status == "restored" {
				if !os.IsNotExist(err) || !job.ArtifactsCleaned {
					t.Fatal("successful artifacts not cleaned")
				}
			} else if err != nil || job.ArtifactsCleaned {
				t.Fatal("unfinished artifacts removed")
			}
			if _, err := os.Stat(history); err != nil {
				t.Fatal("history lost", err)
			}
		})
	}
}
