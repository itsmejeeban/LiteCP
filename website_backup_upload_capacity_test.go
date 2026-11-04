package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebsiteBackupUploadRefusesFullStageQueueBeforeSaving(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	site := Site{ID: "site", Domain: "qa.test", Root: filepath.Join(t.TempDir(), "public_html")}
	store.cfg.Sites = []Site{site}
	system := &System{dataDir: t.TempDir()}
	app := &App{store: store, system: system}
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("stage%d", i)
		root := filepath.Join(system.dataDir, "migrations", id)
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(MigrationPreview{ID: id})
		if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/website-backups/upload?siteId=site", strings.NewReader("body must not be consumed or saved"))
	app.websiteBackupUpload(response, request)
	if response.Code != 409 || !strings.Contains(response.Body.String(), "No new archive was saved") {
		t.Fatal(response.Code, response.Body.String())
	}
	if _, err := os.Stat(system.websiteBackupsRoot(site)); !os.IsNotExist(err) {
		t.Fatal("upload storage was created despite full inspection queue", err)
	}
}
