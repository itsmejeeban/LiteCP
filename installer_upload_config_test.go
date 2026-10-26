package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerPreservesUnlimitedFileUploadLocation(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil { t.Skip("bash is required for the isolated installer configuration check") }
	installer, err := os.ReadFile("scripts/install.sh")
	if err != nil { t.Fatal(err) }
	text := strings.ReplaceAll(string(installer), "\r\n", "\n")
	start := strings.Index(text, "# Let the panel reject unauthenticated uploads")
	end := strings.Index(text, `info "Configuring lightweight Postfix and Dovecot email hosting"`)
	if start < 0 || end <= start { t.Fatal("installer proxy configuration section not found") }
	scriptDirectory, err := filepath.Abs("scripts")
	if err != nil { t.Fatal(err) }
	for _, test := range []struct{name, upload string}{
		{name: "legacy_without_upload_location"},
		{name: "existing_upload_location", upload: `    location = /api/files/upload {
        client_max_body_size 0;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
        proxy_pass http://127.0.0.1:9090;
        proxy_set_header X-Custom preserved;
    }
`},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "panel.conf")
			fixture := "server {\n    ssl_certificate /trusted/cert.pem;\n" + test.upload + `    location / {
        proxy_pass http://127.0.0.1:9090;
        proxy_request_buffering off;
        client_max_body_size 512m;
        proxy_read_timeout 120s;
        proxy_send_timeout 120s;
    }
}
`
			if err := os.WriteFile(config, []byte(fixture), 0600); err != nil { t.Fatal(err) }
			var previous string
			for pass := 0; pass < 2; pass++ {
				command := exec.Command(bash, "-e", "-c", text[start:end])
				command.Env = append(os.Environ(), "PANEL_CONFIG="+config, "SCRIPT_DIR="+scriptDirectory)
				if output, err := command.CombinedOutput(); err != nil { t.Fatalf("configuration failed: %v: %s", err, output) }
				content, err := os.ReadFile(config)
				if err != nil { t.Fatal(err) }
				result := string(content)
				location := strings.Index(result, "location = /api/files/upload {")
				if location < 0 || strings.Count(result, "location = /api/files/upload {") != 1 { t.Fatal("dedicated upload location missing or duplicated") }
				uploadEnd := strings.Index(result[location:], "}")
				if uploadEnd < 0 { t.Fatal("upload location not closed") }
				upload := result[location:location+uploadEnd]
				for _, directive := range []string{"client_max_body_size 0;", "client_body_timeout 86400s;", "proxy_read_timeout 86400s;", "proxy_send_timeout 86400s;", "proxy_request_buffering off;", "proxy_http_version 1.1;"} {
					if !strings.Contains(upload, directive) { t.Fatalf("upload lost %s: %s", directive, upload) }
				}
				root := result[strings.Index(result, "location / {"):]
				if !strings.Contains(root, "client_max_body_size 2100m;") || !strings.Contains(root, "proxy_read_timeout 2700s;") || !strings.Contains(root, "proxy_send_timeout 2700s;") { t.Fatal("other endpoints lost their finite limits") }
				if !strings.Contains(result, "ssl_certificate /trusted/cert.pem;") { t.Fatal("trusted certificate changed") }
				if test.upload != "" && !strings.Contains(upload, "proxy_set_header X-Custom preserved;") { t.Fatal("custom upload proxy setting changed") }
				if pass > 0 && previous != result { t.Fatal("proxy update is not idempotent") }
				previous = result
			}
		})
	}
}
