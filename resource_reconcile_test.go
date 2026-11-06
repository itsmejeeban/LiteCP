package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResourceFrontendReconcilePreservesProxyAndTLS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontend.conf")
	content := "# LiteCP enforced website resource boundary\nserver {\n listen 443 ssl;\n ssl_certificate /retained/cert.pem;\n ssl_certificate_key /retained/key.pem;\n location / { proxy_pass http://unix:/private/http.sock:; }\n}\n"
	policy := WebsiteResourceLimits{Enabled: true}
	site := Site{ID: "reconcile", Domain: "audit.example.com", ResourceLimits: &policy}
	// Include the normal gate so reconciliation is byte-for-byte idempotent.
	content = injectCaptchaConfig(content, site)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := (&System{}).reconcileSiteNginx(site, path); err != nil {
		t.Fatal("resource frontend incorrectly required FastCGI", err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != content {
		t.Fatal("proxy frontend or TLS changed", err)
	}
}
