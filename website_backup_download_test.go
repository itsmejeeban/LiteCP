package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWebsiteBackupDownloadRequiresCompleteManifest(t *testing.T) {
	for _, test := range []struct {
		name string
		manifest bool
		owner string
		sizeDelta int64
		want int
	}{
		{name: "in_progress", want: 404},
		{name: "different_website", manifest: true, owner: "another_site", want: 404},
		{name: "truncated_archive", manifest: true, owner: "backup_site", sizeDelta: 1, want: 404},
		{name: "completed", manifest: true, owner: "backup_site", want: 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := OpenStore(t.TempDir())
			if err != nil { t.Fatal(err) }
			site := Site{ID: "backup_site", Domain: "backup.example.com"}
			store.cfg.Sites = []Site{site}
			app := &App{store: store, system: &System{dataDir: t.TempDir()}}
			id := "abcdefghijklmnop1234"
			root := filepath.Join(app.system.dataDir, "website-backups", safeSiteID(site.ID), id)
			if err := os.MkdirAll(root, 0700); err != nil { t.Fatal(err) }
			archive := []byte("fixture archive")
			if err := os.WriteFile(filepath.Join(root, "website.zip"), archive, 0600); err != nil { t.Fatal(err) }
			if test.manifest {
				content, _ := json.Marshal(websiteZIPBackup{ID: id, SiteID: test.owner, Size: int64(len(archive)) + test.sizeDelta})
				if err := os.WriteFile(filepath.Join(root, "manifest.json"), content, 0600); err != nil { t.Fatal(err) }
			}
			response := httptest.NewRecorder()
			app.websiteBackups(response, httptest.NewRequest("GET", "/api/website-backups?siteId="+site.ID+"&id="+id, nil))
			if response.Code != test.want { t.Fatalf("download status=%d want=%d", response.Code, test.want) }
			response = httptest.NewRecorder()
			app.websiteBackups(response, httptest.NewRequest("GET", "/api/website-backups?siteId="+site.ID, nil))
			var result struct{ Items []websiteZIPBackup `json:"items"` }
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil { t.Fatal(err) }
			if (len(result.Items) == 1) != (test.want == 200) { t.Fatalf("incomplete/misowned listing: %+v", result.Items) }
		})
	}
}
