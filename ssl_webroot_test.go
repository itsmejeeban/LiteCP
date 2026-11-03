package main

import (
	"strings"
	"testing"
)

func TestWebrootSSLConfigPreservesWebsiteRules(t *testing.T) {
	site := Site{Domain: "example.com"}
	original := "server {\n listen 80;\n server_name example.com;\n root /home/litecp/example.com/public_html;\n modsecurity on;\n location / { try_files $uri /index.php; }\n}\n"
	challenge, err := webrootSSLConfig(original, site, false)
	if err != nil {
		t.Fatal(err)
	}
	active, err := webrootSSLConfig(challenge, site, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"modsecurity on;", "root /home/litecp/example.com/public_html;", "listen 443 ssl;", "/etc/letsencrypt/live/example.com/fullchain.pem", "location ^~ /.well-known/acme-challenge/"} {
		if !strings.Contains(active, part) {
			t.Fatal("lost rule:", part)
		}
	}
	again, err := webrootSSLConfig(active, site, true)
	if err != nil || again != active {
		t.Fatal("SSL application must be idempotent", err)
	}
	if strings.Contains(challenge, "listen 443 ssl") {
		t.Fatal("TLS enabled before issuance")
	}
}
func TestWebrootSSLConfigRejectsAmbiguousConfiguration(t *testing.T) {
	if _, err := webrootSSLConfig("server { }\nserver { }", Site{Domain: "example.com"}, true); err == nil {
		t.Fatal("ambiguous configuration accepted")
	}
}
