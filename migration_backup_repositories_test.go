package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRestorePreservesGeneratedBackupRepositories(t *testing.T) {
	home, stage := t.TempDir(), t.TempDir()
	for _, name := range []string{"manual", "scheduled"} {
		dir := filepath.Join(home, "backups", name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "original.zip"), []byte("verified backup"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := preserveRestoreBackupRepositories(home, stage); err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{home, stage} {
		for _, name := range []string{"manual", "scheduled"} {
			value, err := os.ReadFile(filepath.Join(base, "backups", name, "original.zip"))
			if err != nil || string(value) != "verified backup" {
				t.Fatal("backup lost or changed", err)
			}
		}
	}
	if err := preserveRestoreBackupRepositories(home, stage); err == nil {
		t.Fatal("existing stage repository overwritten")
	}
}

func TestRestoreRejectsBackupRepositorySymlinks(t *testing.T) {
	home, stage := t.TempDir(), t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(home, "backups")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := preserveRestoreBackupRepositories(home, stage); err == nil {
		t.Fatal("symlink followed")
	}
}
