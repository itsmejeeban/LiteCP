package main

import "testing"

func TestWebsiteDatabaseSizesOwnershipAndDeduplication(t *testing.T) {
	site := Site{ID: "one", Database: "primary"}
	databases := []ManagedDatabase{{Name: "primary", SiteID: "one"}, {Name: "extra", SiteID: "one"}, {Name: "other", SiteID: "two"}}
	size, ok := sumWebsiteDatabaseSizes(site, databases, map[string]int64{"primary": 100, "extra": 50, "other": 900})
	if !ok || size != 150 {
		t.Fatalf("got %d, %v", size, ok)
	}
	if _, ok := sumWebsiteDatabaseSizes(site, databases, map[string]int64{"primary": 100}); ok {
		t.Fatal("missing schema reported as known")
	}
	if size, ok := sumWebsiteDatabaseSizes(Site{ID: "empty"}, databases, nil); !ok || size != 0 {
		t.Fatal("website without databases must be zero")
	}
}
