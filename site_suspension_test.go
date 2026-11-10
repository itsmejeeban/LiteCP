package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSuspensionConfigRoundTrip(t *testing.T){ original:="server {\n listen 80;\n}\nserver {\n listen 443 ssl;\n ssl_certificate /cert;\n}\n"; blocked:=suspensionConfig(original,true);if strings.Count(blocked,"return 503")!=2{t.Fatal("both listeners must be suspended")};if suspensionConfig(blocked,true)!=blocked{t.Fatal("not idempotent")};if suspensionConfig(blocked,false)!=original{t.Fatal("unsuspension changed original configuration")}}

func TestSuspensionHTTPOnlyTLSIsolation(t *testing.T){
 original:="server {\n listen 80;\n server_name example.com;\n}\n"
 site:=Site{Domain:"example.com"}
 blocked:=suspensionSiteConfig(original,site,true)
 if !strings.Contains(blocked,"ssl_reject_handshake on")||!strings.Contains(blocked,"server_name example.com;"){t.Fatal("missing domain TLS isolation")}
 if suspensionSiteConfig(blocked,site,true)!=blocked{t.Fatal("guard not idempotent")}
 if suspensionSiteConfig(blocked,site,false)!=original{t.Fatal("guard not removed on unsuspend")}
 tls:=original+"server {\n listen 443 ssl;\n ssl_certificate /cert;\n}\n"
 if strings.Contains(suspensionSiteConfig(tls,site,true),"ssl_reject_handshake"){t.Fatal("valid TLS website must retain suspension page")}
}

func TestSuspensionPreservesSSLChallengeLocation(t *testing.T) {
	site := Site{Domain: "example.com"}
	original, err := webrootSSLConfig("server {\n listen 80;\n server_name example.com;\n}\n", site, true)
	if err != nil {
		t.Fatal(err)
	}
	blocked := suspensionSiteConfig(original, site, true)
	if strings.Count(blocked, "location ^~ /.well-known/acme-challenge/") != 1 ||
		!strings.Contains(blocked, "auth_basic off; allow all;") ||
		!strings.Contains(blocked, "return 503") {
		t.Fatal("suspension must reuse the SSL challenge location and block normal requests")
	}
	if suspensionSiteConfig(blocked, site, true) != blocked ||
		suspensionSiteConfig(blocked, site, false) != original {
		t.Fatal("suspension must round-trip the SSL config without changing its original rules")
	}
}

func TestSuspensionChecksACMELocationPerServer(t *testing.T) {
	existing := "server { location \"/.well-known/acme-challenge/\" { return 404; } }\n"
	other := "server {\n # location ^~ /.well-known/acme-challenge/ { }\n listen 8080;\n}\n"
	blocked := suspensionConfig(existing+other, true)
	if strings.Count(blocked, suspensionACME) != 1 || !strings.Contains(blocked, "return 404; } }\nserver {"+suspensionStart+suspensionACME) {
		t.Fatal("only the server without an active challenge location needs a fallback")
	}
	legacy := strings.Replace(existing, "server {", "server {"+suspensionStart+suspensionACME+suspensionResponse, 1)
	if suspensionConfig(legacy, false) != existing {
		t.Fatal("unsuspending an older config must remove its temporary challenge location")
	}
}

func TestSuspensionNginxParser(t *testing.T) {
	nginx, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("Nginx is not installed")
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "nginx.conf")
	original := "server {\n listen 127.0.0.1:18080;\n location ^~ /.well-known/acme-challenge/ { auth_basic off; allow all; try_files $uri =404; }\n}\n"
	content := "pid nginx.pid;\nerror_log stderr;\nevents {}\nhttp {\n access_log off;\n" + suspensionConfig(original, true) + "}\n"
	if err := os.WriteFile(config, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(nginx, "-t", "-p", dir, "-c", config).CombinedOutput(); err != nil {
		t.Fatalf("Nginx rejected the suspension config: %v\n%s", err, output)
	}
}
