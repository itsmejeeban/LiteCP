package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type DNSRecord struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (s *System) WebmailAvailable() bool {
	if s.demo {
		return true
	}
	_, err := os.Stat("/etc/litecp/mail/webmail-url")
	return err == nil
}

func (s *System) WebmailURL() string {
	if s.demo {
		return "/webmail/"
	}
	content, err := os.ReadFile("/etc/litecp/mail/webmail-url")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

func (s *System) mailHostname(ctx context.Context) string {
	if !s.demo {
		if output, err := runCommand(ctx, 5*time.Second, "postconf", "-h", "myhostname"); err == nil {
			hostname := strings.TrimSpace(output)
			if domainPattern.MatchString(hostname) {
				return hostname
			}
		}
	}
	hostname, _ := os.Hostname()
	return hostname
}

func (s *System) dkimKeyPath(domain string) string {
	if s.demo {
		return filepath.Join(s.dataDir, "dkim", domain, "default.private")
	}
	return filepath.Join("/etc/litecp/mail/dkim", domain, "default.private")
}

func (s *System) ensureDKIMKey(domain string) (string, error) {
	if err := validateMailDomain(domain); err != nil {
		return "", err
	}
	path := s.dkimKeyPath(domain)
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key, keyErr := rsa.GenerateKey(rand.Reader, 2048)
		if keyErr != nil {
			return "", keyErr
		}
		content = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
			return "", err
		}
		if err := writeAtomic(path, content, 0600); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	block, _ := pem.Decode(content)
	if block == nil {
		return "", errors.New("DKIM private key is invalid")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return "", err
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	return "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(public), nil
}

func (s *System) syncDKIM(ctx context.Context, domains []MailDomain) error {
	if s.demo {
		return nil
	}
	// Older installs remain usable until the optional DKIM package is installed.
	if !unitFileExists("opendkim") {
		return nil
	}
	var keys, signing strings.Builder
	for _, domain := range domains {
		if _, err := s.ensureDKIMKey(domain.Domain); err != nil {
			return err
		}
		name := "default._domainkey." + domain.Domain
		keys.WriteString(fmt.Sprintf("%s %s:default:%s\n", name, domain.Domain, s.dkimKeyPath(domain.Domain)))
		signing.WriteString(fmt.Sprintf("*@%s %s\n", domain.Domain, name))
	}
	for name, data := range map[string]string{"KeyTable": keys.String(), "SigningTable": signing.String()} {
		if err := writeAtomic(filepath.Join("/etc/litecp/mail", name), []byte(data), 0644); err != nil {
			return err
		}
	}
	// This subtree is writable in the panel's service sandbox. Run directly:
	// passing service pipe descriptors through a nested transient unit can fail.
	if _, err := os.Stat("/etc/litecp/mail/dkim"); err == nil {
		if _, err := runCommand(ctx, 15*time.Second, "chown", "-R", "opendkim:opendkim", "/etc/litecp/mail/dkim"); err != nil {
			return err
		}
	}
	if s.mailHostingDisabled() {
		return nil
	}
	_, err := runCommand(ctx, 30*time.Second, "systemctl", "reload-or-restart", "opendkim")
	return err
}

func (s *System) mailHostingDisabled() bool {
	_, err := os.Stat("/etc/litecp/mail/disabled")
	return err == nil
}

func (s *System) SetMailHosting(ctx context.Context, enabled bool) error {
	if s.demo {
		return nil
	}
	if len(s.mailUnits()) < 2 {
		return errors.New("Postfix and Dovecot are not installed")
	}
	units := s.mailUnits()
	for _, unit := range []string{"opendkim", "rspamd"} {
		if unitFileExists(unit) {
			units = append(units, unit)
		}
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	args := append([]string{action, "--now"}, units...)
	previousDisabled := s.mailHostingDisabled()
	rollbackServices := func() error {
		rollback := "enable"
		if previousDisabled {
			rollback = "disable"
		}
		_, rollbackErr := runHostCommand(context.Background(), 60*time.Second, "systemctl", append([]string{rollback, "--now"}, units...)...)
		return rollbackErr
	}
	if _, err := runHostCommand(ctx, 60*time.Second, "systemctl", args...); err != nil {
		rollbackErr := rollbackServices()
		return fmt.Errorf("mail service change failed: %v (rollback: %v)", err, rollbackErr)
	}
	var markerErr error
	if enabled {
		if err := os.Remove("/etc/litecp/mail/disabled"); err != nil && !errors.Is(err, os.ErrNotExist) {
			markerErr = err
		}
	} else {
		markerErr = writeAtomic("/etc/litecp/mail/disabled", []byte("Email hosting disabled by LiteCP\n"), 0644)
	}
	if markerErr != nil {
		rollbackErr := rollbackServices()
		return fmt.Errorf("mail services changed but the persistent state marker could not be updated: %v (rollback: %v)", markerErr, rollbackErr)
	}
	return nil
}

func (a *App) toggleMailHosting(w http.ResponseWriter, r *http.Request) {
	var input struct{ Enabled bool }
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	previous := a.store.Settings().EmailHostingEnabled
	if err := a.system.SetMailHosting(r.Context(), input.Enabled); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	if err := a.store.UpdateSettings(func(value *Settings) { value.EmailHostingEnabled = input.Enabled }); err != nil {
		if rollbackErr := a.system.SetMailHosting(context.Background(), previous); rollbackErr != nil {
			writeError(w, 500, "Panel state could not be saved, and mail-service rollback failed: "+rollbackErr.Error())
			return
		}
		writeError(w, 500, "Mail hosting change was rolled back because state could not be saved.")
		return
	}
	label := "disabled"
	if input.Enabled {
		label = "enabled"
	}
	a.store.AddActivity("Email hosting "+label, "Mailboxes and stored messages were preserved", "warning")
	writeJSON(w, 200, map[string]bool{"enabled": input.Enabled})
}

func (a *App) mailDNS(w http.ResponseWriter, r *http.Request) {
	domain := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("domain")))
	if !a.store.HasMailDomain(domain) {
		writeError(w, 404, "Mail domain not found.")
		return
	}
	host := a.system.mailHostname(r.Context())
	records := []DNSRecord{
		{Type: "A", Name: host, Value: primaryIP()},
		{Type: "MX", Name: domain, Value: "10 " + host + "."},
		{Type: "TXT", Name: domain, Value: "v=spf1 mx -all"},
		{Type: "TXT", Name: "_dmarc." + domain, Value: "v=DMARC1; p=none; rua=mailto:postmaster@" + domain + "; adkim=r; aspf=r"},
	}
	// GET does not generate private keys or mutate mail configuration.
	dkim := ""
	if content, err := os.ReadFile(a.system.dkimKeyPath(domain)); err == nil {
		if block, _ := pem.Decode(content); block != nil {
			if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
				if public, err := x509.MarshalPKIXPublicKey(&key.PublicKey); err == nil {
					dkim = "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(public)
				}
			}
		}
	}
	if dkim != "" {
		records = append(records, DNSRecord{Type: "TXT", Name: "default._domainkey." + domain, Value: dkim})
	}
	writeJSON(w, 200, map[string]any{"records": records, "dkimReady": dkim != "" && (a.system.demo || unitFileExists("opendkim")), "hostname": host, "webmailUrl": a.system.WebmailURL(), "note": "Merge SPF with any other senders; do not publish two SPF records. Create postmaster before enabling DMARC reports. Set the IP's PTR/reverse DNS to the mail hostname with your VPS provider. Use trusted TLS and verify forward/reverse DNS, reputation and outbound TCP 25. Start DMARC in monitoring mode; move to quarantine/reject after checking reports. Inbox delivery is not guaranteed."})
}
