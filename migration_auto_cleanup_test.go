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
			portable := filepath.Join(root, "portable-backup")
			for _, dir := range []string{root, stage} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "fixture"), []byte("data"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, path := range []string{archive, portable} {
				if err := os.WriteFile(path, []byte("archive"), 0600); err != nil {
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
			for _, path := range []string{archive, portable, stage} {
				_, err := os.Stat(path)
				if status == "restored" {
					if !os.IsNotExist(err) || !job.ArtifactsCleaned {
						t.Fatalf("successful artifact not cleaned: %s", path)
					}
				} else if err != nil || job.ArtifactsCleaned {
					t.Fatalf("unfinished artifact removed: %s", path)
				}
			}
			if _, err := os.Stat(history); err != nil {
				t.Fatal("history lost", err)
			}
		})
	}
}
