package main

import (
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

func TestNginxConfigProtectsWHMCSFiles(t *testing.T) {
	system := &System{os: OSInfo{ID: "ubuntu"}}
	config := system.nginxConfig(Site{Domain: "billing.example.com", Root: "/var/www/litecp/billing.example.com/public"})
	for _, required := range []string{"server_name billing.example.com", "location = /configuration.php", "location ^~ /crons/", "php8.3-fpm.sock"} {
		if !strings.Contains(config, required) {
			t.Errorf("nginx config is missing %q", required)
		}
	}
}
