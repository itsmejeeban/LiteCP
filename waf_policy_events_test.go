package main

import (
	"path/filepath"
	"testing"
)

func TestWAFRateSourcesIncludeManagedWebsiteLogs(t *testing.T) {
	sites := []Site{{ID: "test", Domain: "example.test", Root: defaultSiteRoot("example.test")}, {Domain: "legacy.test", Root: "/srv/legacy"}}
	sources := wafRateLogSources(sites)
	if len(sources) != 2 || sources[0].path != "/var/log/nginx/error.log" || sources[1].path != filepath.Join(siteHome(sites[0]), "logs", "error.log") || sources[1].website != "example.test" {
		t.Fatalf("unexpected sources: %+v", sources)
	}
	for len(sites) < 30 {
		sites = append(sites, sites[0])
	}
	if len(wafRateLogSources(sites)) > 21 {
		t.Fatal("unbounded website log scan")
	}
}
