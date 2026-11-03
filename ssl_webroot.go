package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var sslServerStart = regexp.MustCompile(`(?m)^\s*server\s*\{`)

func webrootSSLConfig(content string, site Site, activate bool) (string, error) {
	starts := sslServerStart.FindAllStringIndex(content, -1)
	if len(starts) == 0 || (len(starts) != 1 && !strings.Contains(content, "ssl_certificate ")) {
		return "", errors.New("SSL setup requires one managed server block; review existing Nginx configuration before retrying")
	}
	if !strings.Contains(content, "/.well-known/acme-challenge/") {
		at := starts[0][1]
		content = content[:at] + "\n    location ^~ /.well-known/acme-challenge/ { auth_basic off; allow all; try_files $uri =404; }\n" + content[at:]
	}
	if !activate {
		return content, nil
	}
	cert := filepath.ToSlash(filepath.Join("/etc/letsencrypt/live", site.Domain, "fullchain.pem"))
	key := filepath.ToSlash(filepath.Join("/etc/letsencrypt/live", site.Domain, "privkey.pem"))
	if strings.Contains(content, "ssl_certificate ") {
		// Keep existing TLS routing and renew only this domain's certificate.
		if !strings.Contains(content, cert) || !strings.Contains(content, key) {
			return "", errors.New("existing TLS configuration uses different certificate paths; review before replacing")
		}
		return content, nil
	}
	at := sslServerStart.FindStringIndex(content)[1]
	tls := fmt.Sprintf("\n    listen 443 ssl;\n    listen [::]:443 ssl;\n    ssl_certificate %s;\n    ssl_certificate_key %s;\n    if ($scheme = http) { return 301 https://$host$request_uri; }\n", cert, key)
	return content[:at] + tls + content[at:], nil
}

func (s *System) enableWebrootSSL(ctx context.Context, site Site, email string, renew bool) error {
	configPath, _ := s.nginxPaths(site.Domain)
	original, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	challenge, err := webrootSSLConfig(string(original), site, false)
	if err != nil {
		return err
	}
	apply := func(content string) error {
		if err := writeAtomic(configPath, []byte(content), 0644); err != nil {
			return err
		}
		if _, err := runCommand(ctx, 15*time.Second, "nginx", "-t"); err != nil {
			return err
		}
		_, err := runCommand(ctx, 15*time.Second, "systemctl", "reload", "nginx")
		return err
	}
	rollback := func(cause error) error {
		if err := writeAtomic(configPath, original, 0644); err != nil {
			return fmt.Errorf("SSL setup failed: %v; configuration rollback failed: %w", cause, err)
		}
		recovery, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := runCommand(recovery, 10*time.Second, "nginx", "-t"); err != nil {
			return fmt.Errorf("SSL setup failed: %v; rollback validation failed: %w", cause, err)
		}
		if _, err := runCommand(recovery, 15*time.Second, "systemctl", "reload", "nginx"); err != nil {
			return fmt.Errorf("SSL setup failed: %v; rollback reload failed: %w", cause, err)
		}
		return cause
	}
	if err := apply(challenge); err != nil {
		return rollback(err)
	}
	args := []string{"certonly", "--webroot", "--webroot-path", site.Root, "--cert-name", site.Domain, "--domain", site.Domain, "--non-interactive", "--agree-tos", "--email", email, "--deploy-hook", "systemctl reload nginx"}
	if renew {
		args = append(args, "--force-renewal")
	}
	if _, err := runCommand(ctx, 180*time.Second, "certbot", args...); err != nil {
		return rollback(err)
	}
	active, err := webrootSSLConfig(challenge, site, true)
	if err != nil {
		return rollback(err)
	}
	if err := apply(active); err != nil {
		return rollback(err)
	}
	return nil
}
