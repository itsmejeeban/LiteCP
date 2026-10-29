package main

import (
	"path/filepath"
	"testing"
)

func TestManagedDiskUsageRootNamesExcludesRetainedWebsites(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "home", "litecp")
	sites := []Site{
		{Domain: "active.example", Root: filepath.Join(base, "active.example", "public_html")},
		{Domain: "renamed.example", Root: filepath.Join(base, "original.example", "public_html")},
		{Domain: "legacy.example", Root: filepath.Join(string(filepath.Separator), "var", "www", "legacy.example")},
	}

	got := managedDiskUsageRootNames(base, sites)
	if _, ok := got["active.example"]; !ok {
		t.Fatal("active website directory was excluded")
	}
	if _, ok := got["original.example"]; !ok {
		t.Fatal("renamed website's saved storage directory was excluded")
	}
	if _, ok := got["terminated.example"]; ok {
		t.Fatal("unmanaged retained directory was included")
	}
	if _, ok := got["legacy.example"]; ok {
		t.Fatal("directory outside the managed storage base was included")
	}
}
