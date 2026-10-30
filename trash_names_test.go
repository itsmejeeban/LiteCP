package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTrashDuplicateNamesRemainIdentifiable(t *testing.T) {
	system, site := featureSite(t)
	for i, id := range []string{"first", "second", "third"} {
		root := filepath.Join(system.siteTrashRoot(site), id)
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		entry := TrashEntry{ID: id, SiteID: site.ID, OriginalPath: "public_html/report.txt", Name: "report.txt", Type: "file", DeletedAt: time.Unix(int64(i), 0)}
		data, _ := json.Marshal(entry)
		if err := os.WriteFile(filepath.Join(root, "metadata.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "payload"), []byte(id), 0600); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := system.ListTrash(site)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"report (v3).txt", "report (v2).txt", "report.txt"}
	if len(entries) != len(want) {
		t.Fatalf("entries: %v", entries)
	}
	for i, entry := range entries {
		if entry.Name != want[i] || entry.OriginalPath != "public_html/report.txt" {
			t.Fatalf("entry: %+v", entry)
		}
	}
}
