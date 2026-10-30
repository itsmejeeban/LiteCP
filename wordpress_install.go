package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func validateWordPressInstall(input CreateSiteInput) error {
	if input.AppType != "wordpress" || input.WordPressMode != "install" {
		return nil
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.@-]{1,60}$`).MatchString(input.WordPressUsername) {
		return errors.New("enter a valid WordPress administrator username (1–60 letters, numbers, _, ., @ or -)")
	}
	if len(input.WordPressPassword) < 12 || len(input.WordPressPassword) > 128 || strings.TrimSpace(input.WordPressPassword) != input.WordPressPassword || strings.ContainsAny(input.WordPressPassword, "\r\n\x00") {
		return errors.New("WordPress administrator password must contain 12–128 characters without surrounding spaces")
	}
	address, err := mail.ParseAddress(input.WordPressEmail)
	if err != nil || address.Address != input.WordPressEmail {
		return errors.New("enter a valid WordPress administrator email")
	}
	return nil
}
func phpWordPressString(value string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `'`, `\'`) + "'"
}

func (s *System) installWordPress(ctx context.Context, site Site, db DBCredentials, input CreateSiteInput) error {
	if s.demo {
		return errors.New("WordPress installation requires an installed Linux server")
	}
	if err := validateWordPressInstall(input); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 3 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 4 || r.URL.Scheme != "https" || (r.URL.Host != "wordpress.org" && r.URL.Host != "downloads.wordpress.org") {
			return errors.New("unexpected WordPress download redirect")
		}
		return nil
	}}
	request, err := http.NewRequestWithContext(ctx, "GET", "https://wordpress.org/latest.zip", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("could not download WordPress from wordpress.org")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("WordPress download returned HTTP %d", response.StatusCode)
	}
	// No credentials are written to this archive. Keep failed installation data
	// available for diagnosis instead of silently deleting website files.
	directory := filepath.Join(s.dataDir, "wordpress-downloads")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, "core-*.zip")
	if err != nil {
		return err
	}
	written, err := io.Copy(file, io.LimitReader(response.Body, (100<<20)+1))
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if written > 100<<20 {
		return errors.New("WordPress download exceeds 100 MiB")
	}
	archive, err := zip.OpenReader(file.Name())
	if err != nil {
		return errors.New("WordPress download is not a valid archive")
	}
	defer archive.Close()
	uid, gid, err := s.siteFileOwnership(site)
	if err != nil {
		return err
	}
	if len(archive.File) > 50000 {
		return errors.New("WordPress archive has too many files")
	}
	var total uint64
	for _, member := range archive.File {
		if !strings.HasPrefix(member.Name, "wordpress/") {
			return errors.New("unexpected WordPress archive layout")
		}
		relative := strings.TrimSuffix(strings.TrimPrefix(member.Name, "wordpress/"), "/")
		if relative == "" {
			continue
		}
		if _, err := safeArchiveName(relative); err != nil {
			return err
		}
		if !member.FileInfo().IsDir() && !member.Mode().IsRegular() {
			return errors.New("unsupported WordPress archive entry")
		}
		total += member.UncompressedSize64
		if total > 512<<20 {
			return errors.New("WordPress archive expands beyond 512 MiB")
		}
		if _, _, err := resolveManagedPath(site.Root, relative, true); err != nil {
			return err
		}
		if _, err := os.Lstat(filepath.Join(site.Root, filepath.FromSlash(relative))); err == nil {
			return fmt.Errorf("WordPress destination already exists: %s", relative)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	for _, member := range archive.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		relative := strings.TrimSuffix(strings.TrimPrefix(member.Name, "wordpress/"), "/")
		if relative == "" {
			continue
		}
		if member.FileInfo().IsDir() {
			if err := secureEnsureManagedDirectory(site.Root, relative, 0750, uid, gid); err != nil {
				return err
			}
			continue
		}
		if err := secureEnsureManagedDirectory(site.Root, filepath.ToSlash(filepath.Dir(relative)), 0750, uid, gid); err != nil {
			return err
		}
		source, err := member.Open()
		if err != nil {
			return err
		}
		_, err = secureWriteManagedNoReplace(site.Root, relative, source, int64(member.UncompressedSize64), 0640, uid, gid)
		source.Close()
		if err != nil {
			return err
		}
	}
	config := "<?php\n"
	for key, value := range map[string]string{"DB_NAME": db.Name, "DB_USER": db.Username, "DB_PASSWORD": db.Password, "DB_HOST": db.Host, "DB_CHARSET": "utf8mb4", "DB_COLLATE": ""} {
		config += "define(" + phpWordPressString(key) + ", " + phpWordPressString(value) + ");\n"
	}
	for _, key := range []string{"AUTH_KEY", "SECURE_AUTH_KEY", "LOGGED_IN_KEY", "NONCE_KEY", "AUTH_SALT", "SECURE_AUTH_SALT", "LOGGED_IN_SALT", "NONCE_SALT"} {
		salt, err := randomToken(48)
		if err != nil {
			return err
		}
		config += "define(" + phpWordPressString(key) + ", " + phpWordPressString(salt) + ");\n"
	}
	if input.EnableCron {
		config += "define('DISABLE_WP_CRON', true);\n"
	}
	config += "$table_prefix = 'wp_';\ndefine('WP_DEBUG', false);\nif (!defined('ABSPATH')) define('ABSPATH', __DIR__ . '/');\nrequire_once ABSPATH . 'wp-settings.php';\n"
	if _, err := secureWriteManagedNoReplace(site.Root, "wp-config.php", strings.NewReader(config), int64(len(config)), 0640, uid, gid); err != nil {
		return err
	}
	return s.finishWordPressSetup(ctx, site, input)
}

const wordPressSetupScript = `
$litecpInput = json_decode(stream_get_contents(STDIN), true);
if (!is_array($litecpInput)) { fwrite(STDERR, "Invalid setup input"); exit(1); }
if (!chdir($litecpInput['root'])) { fwrite(STDERR, "Cannot enter WordPress directory"); exit(1); }
define('WP_INSTALLING', true);
$_SERVER['HTTP_HOST'] = $litecpInput['domain'];
$_SERVER['SERVER_NAME'] = $litecpInput['domain'];
$_SERVER['REQUEST_URI'] = '/wp-admin/install.php';
$_SERVER['SCRIPT_NAME'] = '/wp-admin/install.php';
$_SERVER['PHP_SELF'] = '/wp-admin/install.php';
$_SERVER['SERVER_PROTOCOL'] = 'HTTP/1.1';
$_SERVER['REQUEST_METHOD'] = 'GET';
$_SERVER['HTTPS'] = strpos($litecpInput['url'], 'https://') === 0 ? 'on' : '';
if (!extension_loaded('mysqli')) { fwrite(STDERR, "Missing mysqli extension"); exit(1); }
fwrite(STDERR, "LITECP_STAGE:bootstrap\n");
require $litecpInput['root'].'/wp-load.php';
require_once ABSPATH.'wp-admin/includes/upgrade.php';
require_once ABSPATH.'wp-admin/includes/translation-install.php';
add_filter('pre_wp_mail', '__return_false');
if (is_blog_installed()) {
    fwrite(STDERR, "WordPress is already installed; no existing account changed"); exit(2);
}
fwrite(STDERR, "LITECP_STAGE:database\n");
$litecpResult = wp_install($litecpInput['domain'], $litecpInput['user'], $litecpInput['email'], true, '', $litecpInput['password'], 'en_US');
if (is_wp_error($litecpResult) || empty($litecpResult['user_id'])) { fwrite(STDERR, "Administrator creation failed"); exit(3); }
update_option('siteurl', $litecpInput['url']);
update_option('home', $litecpInput['url']);
$litecpAdmin = wp_authenticate($litecpInput['user'], $litecpInput['password']);
if (is_wp_error($litecpAdmin) || !user_can($litecpAdmin, 'manage_options')) { fwrite(STDERR, "Administrator verification failed"); exit(4); }
echo 'LITECP_WORDPRESS_INSTALLED';
`

func (s *System) finishWordPressSetup(ctx context.Context, site Site, input CreateSiteInput) error {
	if err := validateWordPressInstall(input); err != nil {
		return err
	}
	uid, gid, err := s.siteFileOwnership(site)
	if err != nil {
		return err
	}
	for _, name := range []string{"wp-config.php", "wp-load.php", "wp-admin/includes/upgrade.php"} {
		file, err := secureOpenManaged(site.Root, name, os.O_RDONLY, 0)
		if err != nil {
			return errors.New("WordPress core files or wp-config.php are missing; no existing data changed")
		}
		file.Close()
	}
	scheme := "http"
	if site.SSL {
		scheme = "https"
	}
	payload, _ := json.Marshal(map[string]string{"root": site.Root, "url": scheme + "://" + site.Domain, "domain": site.Domain, "user": input.WordPressUsername, "password": input.WordPressPassword, "email": input.WordPressEmail})
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	command, err := wordPressSetupCommand(ctx, site.Root, managedSiteRoot(site), s.phpBinary(site.PHPVersion), uid, gid, payload)
	if err != nil {
		return err
	}
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "LITECP_WORDPRESS_INSTALLED") {
		if ctx.Err() != nil {
			return errors.New("WordPress setup timed out; files and database retained")
		}
		diagnostic := strings.TrimSpace(string(output))
		for _, secret := range []string{input.WordPressPassword, input.WordPressEmail, input.WordPressUsername} {
			if secret != "" {
				diagnostic = strings.ReplaceAll(diagnostic, secret, "[redacted]")
			}
		}
		diagnostic = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(diagnostic, "")
		if len(diagnostic) > 900 {
			diagnostic = diagnostic[len(diagnostic)-900:]
		}
		if diagnostic == "" {
			diagnostic = "PHP exited without completing installation"
			if err != nil {
				diagnostic = err.Error()
			}
		}
		return fmt.Errorf("WordPress setup failed: %s. Files and database retained", diagnostic)
	}
	return nil
}

func (a *App) completeWordPressSetup(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, "Invalid setup request")
		return
	}
	site := a.store.SiteByID(input.ID)
	if site == nil || site.AppType != "wordpress" {
		writeError(w, 404, "WordPress website not found")
		return
	}
	setup := CreateSiteInput{AppType: "wordpress", WordPressMode: "install", WordPressUsername: input.Username, WordPressPassword: input.Password, WordPressEmail: input.Email}
	if a.system.demo {
		writeError(w, 400, "WordPress setup requires the installed server")
		return
	}
	if err := a.system.finishWordPressSetup(r.Context(), *site, setup); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	scheme := "http"
	if site.SSL {
		scheme = "https"
	}
	a.store.AddActivity("WordPress installed", site.Domain+": administrator account verified", "success")
	writeJSON(w, 200, map[string]string{"adminURL": scheme + "://" + site.Domain + "/wp-admin/"})
}
