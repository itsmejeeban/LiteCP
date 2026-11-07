package main

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func fixtureBackupFreeBytes(context.Context, string) (int64, error) {
	return 2 << 30, nil
}

func TestWebsiteBackupIncludesNonPublicDirectories(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"public_html/index.php", "logs/access.log", "tmp/upload.part", "private/.env", ".trash/old/payload", "backups/manual/old.zip", "backups/scheduled/old.zip"} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	destination := filepath.Join(t.TempDir(), "home.zip")
	if err := zipWebsiteTree(context.Background(), root, destination, "home", map[string]bool{"public_html": true, "backups/manual": true, "backups/scheduled": true}); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	names := map[string]bool{}
	for _, file := range archive.File {
		names[file.Name] = true
	}
	for _, name := range []string{"home/logs/access.log", "home/tmp/upload.part", "home/private/.env", "home/.trash/old/payload"} {
		if !names[name] {
			t.Errorf("missing %s", name)
		}
	}
	for _, name := range []string{"home/public_html/index.php", "home/backups/manual/old.zip", "home/backups/scheduled/old.zip"} {
		if names[name] {
			t.Errorf("included recursive or duplicated path %s", name)
		}
	}
}

func TestManualBackupInspectsHomeDirectories(t *testing.T) {
	root := t.TempDir()
	public := filepath.Join(root, "public_html.zip")
	databases := filepath.Join(root, "databases.zip")
	home := filepath.Join(root, "home_directories.zip")
	manualZIPFixture(t, public, map[string]string{"public_html/index.php": "public"})
	manualZIPFixture(t, databases, map[string]string{"README.txt": "no database"})
	manualZIPFixture(t, home, map[string]string{"home/private/.env": "private", "home/logs/access.log": "log"})
	archive := filepath.Join(root, "website.zip")
	if err := zipBackupParts(archive, map[string]string{"public_html.zip": public, "databases.zip": databases, "home_directories.zip": home}); err != nil {
		t.Fatal(err)
	}
	stage := t.TempDir()
	preview, err := inspectManualBackup(context.Background(), archive, stage, fixtureBackupFreeBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Roots) != 2 {
		t.Fatalf("roots: %v", preview.Roots)
	}
	value, err := os.ReadFile(filepath.Join(stage, "files", "home", "private", ".env"))
	if err != nil || string(value) != "private" {
		t.Fatal("private file was omitted", err)
	}
}
