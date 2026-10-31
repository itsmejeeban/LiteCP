package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWebsiteBackupsMovedOutsideRemovedHome(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "website")
	source := filepath.Join(home, "backups")
	target := filepath.Join(root, "retained", "backups")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "backup.zip"), []byte("preserve backup bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := moveWebsiteBackupsToSafety(source, target); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "backup.zip"))
	if err != nil || string(data) != "preserve backup bytes" {
		t.Fatal("backup was not preserved", err)
	}
	if _, err = os.Stat(source); !os.IsNotExist(err) {
		t.Fatal("backup source was not moved")
	}
}
func TestWebsiteBackupsDoNotReplaceRetainedFolder(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "retained")
	for _, dir := range []string{source, target} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := moveWebsiteBackupsToSafety(source, target); err == nil {
		t.Fatal("existing retained folder accepted")
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal("source backup folder changed")
	}
}
func TestRetainedScheduledBackupPaths(t *testing.T) {
	if !validScheduledBackupPath("/var/backups/litecp/retained-websites/site-123/backups/scheduled/backup.tar.gz") {
		t.Fatal("retained path rejected")
	}
	if validScheduledBackupPath("/var/backups/litecp/retained-websites/site-123/public_html/backup.tar.gz") {
		t.Fatal("unmanaged path accepted")
	}
}
