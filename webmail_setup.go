package main

import (
	"context"
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed scripts/install-features.sh
var webmailFeatureInstaller string

//go:embed scripts/install-webmail-signon.sh
var webmailSignonInstaller string

type webmailSetupInput struct {
	Hostname    string `json:"hostname"`
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"privateKey"`
	Port        int    `json:"port"`
}

func validateWebmailSetup(input webmailSetupInput) error {
	if !domainPattern.MatchString(input.Hostname) || !strings.Contains(input.Hostname, ".") {
		return fmt.Errorf("Enter a public mail hostname, such as mail.example.com.")
	}
	if input.Port < 1024 || input.Port > 65535 {
		return fmt.Errorf("Webmail port must be between 1024 and 65535.")
	}
	for _, path := range []string{input.Certificate, input.PrivateKey} {
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n\t ;{}'\"\\") {
			return fmt.Errorf("Enter absolute certificate paths without spaces or configuration delimiters.")
		}
	}
	return nil
}

func (a *App) configureWebmail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	baseDirectory := filepath.Join(a.system.dataDir, "webmail-setup")
	directory := ""
	if latest, err := os.ReadFile(filepath.Join(baseDirectory, "latest")); err == nil {
		name := strings.TrimSpace(string(latest))
		if filepath.Base(name) == name && strings.HasPrefix(name, "attempt-") {
			directory = filepath.Join(baseDirectory, name)
		}
	}
	status := func() string {
		if directory == "" {
			return "not-started"
		}
		value, err := os.ReadFile(filepath.Join(directory, "result"))
		if err == nil {
			if strings.TrimSpace(string(value)) == "0" {
				return "complete"
			}
			return "failed"
		}
		if _, err := os.Stat(directory); err == nil {
			active, _ := runCommand(r.Context(), 5*time.Second, "systemctl", "is-active", "litecp-webmail-setup.service")
			if strings.TrimSpace(active) == "active" || strings.TrimSpace(active) == "activating" {
				return "running"
			}
			return "interrupted"
		}
		return "not-started"
	}
	if r.Method == http.MethodGet {
		host := a.system.mailHostname(r.Context())
		writeJSON(w, 200, map[string]any{"status": status(), "configured": a.system.WebmailAvailable(), "url": a.system.WebmailURL(), "hostname": host, "port": 8444, "certificate": "/etc/letsencrypt/live/" + host + "/fullchain.pem", "privateKey": "/etc/letsencrypt/live/" + host + "/privkey.pem", "logPath": filepath.Join(directory, "setup.log")})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		writeError(w, 405, "Method not allowed.")
		return
	}
	if a.system.demo {
		writeError(w, 400, "Webmail installation is unavailable in preview mode.")
		return
	}
	if a.system.WebmailAvailable() {
		writeError(w, 409, "Webmail is already configured. Existing configuration was preserved.")
		return
	}
	if !a.store.Settings().EmailHostingEnabled {
		writeError(w, 400, "Enable email hosting before configuring webmail.")
		return
	}
	if status() == "running" {
		writeError(w, 409, "Webmail setup is already running.")
		return
	}
	var input webmailSetupInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	input.Hostname = strings.ToLower(strings.TrimSpace(input.Hostname))
	if err := validateWebmailSetup(input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	for _, path := range []string{input.Certificate, input.PrivateKey} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			writeError(w, 400, "Certificate or private-key file is unavailable. Install a matching trusted certificate first.")
			return
		}
	}
	// Check the hostname and expiry before allowing package/service changes.
	if _, err := runCommand(r.Context(), 10*time.Second, "openssl", "x509", "-in", input.Certificate, "-noout", "-checkhost", input.Hostname, "-checkend", "0"); err != nil {
		writeError(w, 400, "Certificate must be valid and match the mail hostname.")
		return
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(input.Port)))
	if err != nil {
		writeError(w, 400, "This webmail port is already in use or unavailable. Choose another port.")
		return
	}
	listener.Close()
	if err := os.MkdirAll(baseDirectory, 0700); err != nil {
		writeError(w, 500, "Cannot prepare webmail setup.")
		return
	}
	directory, err = os.MkdirTemp(baseDirectory, "attempt-")
	if err != nil {
		writeError(w, 500, "Cannot prepare a new setup attempt.")
		return
	}
	if err := os.WriteFile(filepath.Join(baseDirectory, "latest"), []byte(filepath.Base(directory)), 0600); err != nil {
		writeError(w, 500, "Cannot record setup status.")
		return
	}
	for name, content := range map[string]string{"install-features.sh": webmailFeatureInstaller, "install-webmail-signon.sh": webmailSignonInstaller} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(strings.ReplaceAll(content, "\r\n", "\n")), 0700); err != nil {
			writeError(w, 500, "Cannot prepare webmail installer.")
			return
		}
	}
	// A system service keeps installation alive when the browser closes and runs
	// outside the web service's read-only filesystem sandbox. Inputs are arguments.
	wrapper := `set -e; umask 077; cd "$1"; trap 'result=$?; printf "%s\n" "$result" > result' EXIT; export LITECP_MAIL_HOSTNAME="$2" LITECP_MAIL_CERT="$3" LITECP_MAIL_KEY="$4" LITECP_WEBMAIL_PORT="$5"; /bin/bash ./install-features.sh mail > setup.log 2>&1`
	_, err = runCommand(context.Background(), 15*time.Second, "systemd-run", "--quiet", "--collect", "--unit=litecp-webmail-setup", "--property=RuntimeMaxSec=3600", "--property=UMask=0077", "--", "/bin/bash", "-c", wrapper, "litecp-webmail-setup", directory, input.Hostname, input.Certificate, input.PrivateKey, strconv.Itoa(input.Port))
	if err != nil {
		writeError(w, 502, "Webmail setup could not start. Existing mailboxes were preserved; review server service logs.")
		return
	}
	a.store.AddActivity("Webmail setup started", input.Hostname+" background configuration requested", "info")
	writeJSON(w, 202, map[string]string{"status": "running"})
}
