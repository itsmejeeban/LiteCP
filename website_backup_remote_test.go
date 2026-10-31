package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoteWebsiteZIPRecordSurvivesWithoutLocalArchive(t *testing.T) {
	root := t.TempDir()
	id := "backup_1234567890123456"
	dir := filepath.Join(root, id)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	item := websiteZIPBackup{ID: id, SiteID: "website123", Domain: "example.com", Size: 42, RemoteOnly: true, RemotePath: "sftp://backup.example.com/backups/example.zip", DestinationID: "destination123"}
	data, _ := json.Marshal(item)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := loadWebsiteZIPBackup(root, id, item.SiteID)
	if err != nil || got.RemotePath != item.RemotePath {
		t.Fatalf("remote record unavailable: %v", err)
	}
	if _, err = loadWebsiteZIPBackup(root, id, "another-website"); err == nil {
		t.Fatal("accepted another website's backup")
	}
	item.RemoteOnly = false
	data, _ = json.Marshal(item)
	if err = os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = loadWebsiteZIPBackup(root, id, item.SiteID); err == nil {
		t.Fatal("accepted incomplete local backup")
	}
}
