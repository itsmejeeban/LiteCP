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
	"strconv"
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
	scheme := "http"
	if site.SSL {
		scheme = "https"
	}
	url := scheme + "://" + site.Domain
	payload, _ := json.Marshal(map[string]string{"root": site.Root, "url": url, "domain": site.Domain, "user": input.WordPressUsername, "password": input.WordPressPassword, "email": input.WordPressEmail})
	script := `$v=json_decode(stream_get_contents(STDIN),true);if(!$v)exit(1);define('WP_INSTALLING',true);$_SERVER['HTTP_HOST']=$v['domain'];$_SERVER['SERVER_NAME']=$v['domain'];$_SERVER['REQUEST_URI']='/';require $v['root'].'/wp-load.php';require_once ABSPATH.'wp-admin/includes/upgrade.php';add_filter('pre_wp_mail','__return_false');if(is_blog_installed())exit(2);$r=wp_install($v['domain'],$v['user'],$v['email'],true,'',$v['password']);if(is_wp_error($r)||empty($r['user_id']))exit(3);update_option('siteurl',$v['url']);update_option('home',$v['url']);$u=wp_authenticate($v['user'],$v['password']);if(is_wp_error($u)||!user_can($u,'manage_options'))exit(4);echo 'LITECP_WORDPRESS_INSTALLED';`
	output, err := runCommandInput(ctx, 3*time.Minute, string(payload), "setpriv", "--reuid="+strconv.Itoa(uid), "--regid="+strconv.Itoa(gid), "--clear-groups", s.phpBinary(site.PHPVersion), "-r", script)
	if err != nil || !strings.Contains(output, "LITECP_WORDPRESS_INSTALLED") {
		return errors.New("WordPress setup did not complete; website files and database were retained for review")
	}
	return nil
}
