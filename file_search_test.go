package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSearchManagedFilesScopesAndHiddenEntries(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"docs", "docs/nested", ".private"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0750); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"index.php", "docs/index.html", "docs/nested/INDEX.txt", ".private/index.ini"} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte("test"), 0640); err != nil {
			t.Fatal(err)
		}
	}
	system := &System{}
	site := Site{ID: "search-site", Domain: "example.test", Root: root}

	all, err := system.SearchManagedFiles(site, "", "index", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Entries) != 3 {
		t.Fatalf("expected 3 visible matches, got %#v", all.Entries)
	}
	docs, err := system.SearchManagedFiles(site, "docs", "index", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs.Entries) != 2 || docs.Root != "docs" {
		t.Fatalf("unexpected scoped search: %#v", docs)
	}
	withHidden, err := system.SearchManagedFiles(site, "", "index", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(withHidden.Entries) != 4 {
		t.Fatalf("expected hidden match, got %#v", withHidden.Entries)
	}
}
