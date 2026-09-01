package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := hashPassword("a-correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword(hash, "a-correct-horse-battery-staple") {
		t.Fatal("valid password was rejected")
	}
	if verifyPassword(hash, "a-different-password") {
		t.Fatal("invalid password was accepted")
	}
}

func TestDomainValidation(t *testing.T) {
	valid := []string{"billing.example.com", "clients.hosting-test.co.uk", "xn--bcher-kva.example"}
	invalid := []string{"example", "https://example.com", "../../etc/passwd", "example.com;reboot", "-bad.example.com"}
	for _, domain := range valid {
		if !domainPattern.MatchString(domain) {
			t.Errorf("valid domain rejected: %s", domain)
		}
	}
	for _, domain := range invalid {
		if domainPattern.MatchString(domain) {
			t.Errorf("invalid domain accepted: %s", domain)
		}
	}
}

func TestStorePersistsConfiguration(t *testing.T) {
	directory := t.TempDir()
	store, err := OpenStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize("admin", "a-correct-horse-battery-staple"); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.CheckPassword("admin", "a-correct-horse-battery-staple") {
		t.Fatal("persisted administrator credentials could not be verified")
	}
}

func TestFreshPublicStateUsesEmptyArrays(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize("admin", "a-correct-horse-battery-staple"); err != nil {
		t.Fatal(err)
	}
	state := store.PublicState()
	if state.Sites == nil || state.Backups == nil || state.Activities == nil {
		t.Fatal("fresh public state must use JSON arrays instead of null collections")
	}
}

func TestOpenStoreMigratesLegacySiteDefaults(t *testing.T) {
	directory := t.TempDir()
	legacy := Config{Sites: []Site{{ID: "legacy", Domain: "billing.example.com", Root: "/var/www/example", PHPVersion: ""}}}
	content, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "config.json"), content, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	site := store.SiteByID("legacy")
	if site == nil || site.AppType != "whmcs" || site.PHPVersion != "8.3" {
		t.Fatalf("legacy site was not migrated: %#v", site)
	}
}

func TestNginxConfigProtectsWHMCSFiles(t *testing.T) {
	system := &System{os: OSInfo{ID: "ubuntu"}}
	config := system.nginxConfig(Site{Domain: "billing.example.com", Root: "/var/www/litecp/billing.example.com/public", AppType: "whmcs", PHPVersion: "8.1"})
	for _, required := range []string{"server_name billing.example.com", "location = /configuration.php", "location ^~ /crons/", "php8.1-fpm.sock"} {
		if !strings.Contains(config, required) {
			t.Errorf("nginx config is missing %q", required)
		}
	}
}

func TestNginxConfigProtectsWordPressFiles(t *testing.T) {
	system := &System{os: OSInfo{ID: "almalinux"}}
	config := system.nginxConfig(Site{Domain: "blog.example.com", Root: "/var/www/litecp/blog.example.com/public", AppType: "wordpress", PHPVersion: "8.4"})
	for _, required := range []string{"server_name blog.example.com", "location = /wp-config.php", "wp-content/uploads/.*\\.php", "/var/opt/remi/php84/run/php-fpm/www.sock"} {
		if !strings.Contains(config, required) {
			t.Errorf("nginx config is missing %q", required)
		}
	}
	if strings.Contains(config, "location = /configuration.php") {
		t.Fatal("WordPress vhost contains WHMCS-only rules")
	}
	uploadRule := strings.Index(config, "wp-content/uploads")
	genericPHP := strings.Index(config, "location ~ \\.php$")
	if uploadRule < 0 || genericPHP < 0 || uploadRule > genericPHP {
		t.Fatal("WordPress upload protection must be evaluated before the generic PHP handler")
	}
}

func TestApplicationPHPCompatibility(t *testing.T) {
	valid := [][2]string{{"whmcs", "7.3"}, {"whmcs", "8.3"}, {"wordpress", "7.4"}, {"wordpress", "8.4"}}
	for _, pair := range valid {
		if err := validateSiteOptions(pair[0], pair[1]); err != nil {
			t.Errorf("valid combination %v rejected: %v", pair, err)
		}
	}
	invalid := [][2]string{{"whmcs", "8.4"}, {"wordpress", "7.3"}, {"unknown", "8.3"}, {"whmcs", "8.0"}}
	for _, pair := range invalid {
		if err := validateSiteOptions(pair[0], pair[1]); err == nil {
			t.Errorf("invalid combination accepted: %v", pair)
		}
	}
}

func TestReplaceFastCGISocketPreservesTLSConfig(t *testing.T) {
	original := "listen 443 ssl;\n    fastcgi_pass unix:/run/php/php8.3-fpm.sock;\nssl_certificate /etc/letsencrypt/live/example/fullchain.pem;\n"
	updated, err := replaceFastCGISocket(original, "/run/php/php8.2-fpm.sock")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated, "fastcgi_pass unix:/run/php/php8.2-fpm.sock;") || !strings.Contains(updated, "ssl_certificate /etc/letsencrypt") {
		t.Fatalf("unexpected updated config: %s", updated)
	}
}
