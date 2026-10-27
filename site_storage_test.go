package main

import (
	"strings"
	"testing"
)

func TestWebsiteHomeBoundaries(t *testing.T) {
	site := Site{Domain: "renamed.example.com", Root: "/home/litecp/original.example.com/public_html"}
	if siteHome(site) != "/home/litecp/original.example.com" || managedSiteRoot(site) != siteHome(site) || sitePublicPath(site) != "public_html" {
		t.Fatal("renames must retain their saved home")
	}
	if defaultSiteRoot("example.com") != "/home/litecp/example.com/public_html" {
		t.Fatal("wrong default document root")
	}
	for _, root := range []string{"/home/other/public_html", "/home/litecp/../other/public_html", "/var/www/litecp/example.com/public"} {
		legacy := Site{Root: root}
		if siteHome(legacy) != "" || managedSiteRoot(legacy) != root {
			t.Fatalf("unexpected home boundary: %s", root)
		}
	}
	for _, path := range []string{"public_html", "backups", ".trash", ".trash/item/payload", "etc", "tmp", "logs", "whmcsdata"} {
		if !protectedSitePath(site, path) {
			t.Fatalf("unprotected structure: %s", path)
		}
	}
	if protectedSitePath(site, "public_html/index.php") {
		t.Fatal("website files must remain editable")
	}
	if !validScheduledBackupPath("/home/litecp/example.com/backups/scheduled/site.tar.gz") || validScheduledBackupPath("/home/other/backups/scheduled/site.tar.gz") || validScheduledBackupPath("/home/litecp/example.com/public_html/site.tar.gz") {
		t.Fatal("invalid scheduled backup boundary")
	}
}

func TestWebsiteHomeNginxPublicBoundary(t *testing.T) {
	system := NewSystem(t.TempDir())
	site := Site{ID: "home-test", Domain: "example.com", Root: defaultSiteRoot("example.com"), AppType: "regular", PHPVersion: "8.3", RuntimeUID: 1500, RuntimeGID: 1500}
	config := system.nginxConfig(site)
	if !strings.Contains(config, "root /home/litecp/example.com/public_html;") || strings.Contains(config, "root /home/litecp/example.com;") {
		t.Fatal("Nginx must only serve public_html")
	}
	if !strings.Contains(config, "/home/litecp/example.com/logs/access.log") {
		t.Fatal("logs must be private siblings")
	}
	pool := system.phpPoolConfig(site)
	if !strings.Contains(pool, "/home/litecp/example.com/tmp") {
		t.Fatal("PHP temporary files must be outside public_html")
	}
}
