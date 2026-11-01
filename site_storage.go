package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const siteStorageBase = "/home/litecp"

func defaultSiteRoot(domain string) string {
	return filepath.Join(siteStorageBase, domain, "public_html")
}

// Derive the account boundary from the saved path, not the current domain: domain
// edits may need to relocate a home saved under an earlier domain.
func siteHome(site Site) string {
	root := filepath.Clean(site.Root)
	home := filepath.Dir(root)
	if filepath.Base(root) == "public_html" && filepath.Dir(home) == filepath.Clean(siteStorageBase) && domainPattern.MatchString(filepath.Base(home)) {
		return home
	}
	return ""
}
func managedSiteRoot(site Site) string {
	if home := siteHome(site); home != "" {
		return home
	}
	return site.Root
}
func sitePublicPath(site Site) string {
	if siteHome(site) != "" {
		return "public_html"
	}
	return ""
}

// Structural folders cannot be renamed, removed or chmodded through file actions.
func protectedSitePath(site Site, relative string) bool {
	if siteHome(site) == "" {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(relative))
	for _, name := range []string{".", "public_html", "backups", "backups/manual", "backups/scheduled", ".trash", "tmp", "logs", "ssl", "etc"} {
		if clean == name {
			return true
		}
	}
	return strings.HasPrefix(clean, ".trash/")
}
func (s *System) websiteBackupsRoot(site Site) string {
	if home := siteHome(site); home != "" {
		return filepath.Join(home, "backups", "manual")
	}
	return filepath.Join(s.dataDir, "website-backups", safeSiteID(site.ID))
}
func siteScheduledBackupRoot(site Site) string {
	if home := siteHome(site); home != "" {
		return filepath.Join(home, "backups", "scheduled")
	}
	return "/var/backups/litecp"
}

// The parent is root-owned so a compromised website cannot rename its sibling
// backup, certificate, or configuration directories. Runtime-writable private
// paths get the isolated website identity; backup metadata stays root-only.
func (s *System) ensureSiteHome(site Site) error {
	home := siteHome(site)
	if home == "" {
		return nil
	}
	for _, path := range []string{"/home", siteStorageBase, home} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			if err = os.Mkdir(path, 0755); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe website home: %s", path)
		}
	}
	if err := os.Chown(home, 0, 0); err != nil {
		return err
	}
	if err := os.Chmod(home, 0755); err != nil {
		return err
	}
	for _, name := range []string{"backups", "backups/manual", "backups/scheduled", ".trash", "tmp", "logs", "ssl", "etc", "public_ftp"} {
		target := filepath.Join(home, name)
		info, err := os.Lstat(target)
		if errors.Is(err, os.ErrNotExist) {
			if err = os.Mkdir(target, 0700); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe private website directory: %s", name)
		}
		uid, gid := 0, 0
		if name == "tmp" || name == "public_ftp" {
			uid = site.RuntimeUID
			gid = site.RuntimeGID
		}
		if err = os.Chown(target, uid, gid); err != nil {
			return err
		}
		if err = os.Chmod(target, 0700); err != nil {
			return err
		}
	}
	return nil
}

func (s *System) labelSiteHome(ctx context.Context, site Site) error {
	home := siteHome(site)
	if home == "" {
		return nil
	}
	if _, err := exec.LookPath("selinuxenabled"); err != nil {
		return nil
	}
	if err := exec.CommandContext(ctx, "selinuxenabled").Run(); err != nil {
		return nil
	}
	// Persistent patterns are installed by the installer outside the restricted
	// panel service. New homes only need those labels applied to their files.
	_, err := runCommand(ctx, 30*time.Second, "restorecon", "-R", home)
	return err
}

func siteLogDirectives(site Site) string {
	if home := siteHome(site); home != "" {
		return fmt.Sprintf("    access_log %s/access.log;\n    error_log %s/error.log;\n", filepath.Join(home, "logs"), filepath.Join(home, "logs")) + trafficLogDirective(site)
	}
	return ""
}

func validScheduledBackupPath(path string) bool {
	path = filepath.Clean(path)
	if !strings.HasSuffix(path, ".tar.gz") {
		return false
	}
	if filepath.Dir(path) == filepath.Clean("/var/backups/litecp") {
		return true
	}
	directory := filepath.Dir(path)
	if filepath.Base(directory) == "scheduled" && filepath.Base(filepath.Dir(directory)) == "backups" && filepath.Dir(filepath.Dir(filepath.Dir(directory))) == filepath.Clean(retainedWebsiteBackupRoot) && retainedWebsiteIDPattern.MatchString(filepath.Base(filepath.Dir(filepath.Dir(directory)))) {
		return true
	}
	if filepath.Base(directory) != "scheduled" || filepath.Base(filepath.Dir(directory)) != "backups" {
		return false
	}
	home := filepath.Dir(filepath.Dir(directory))
	return filepath.Dir(home) == filepath.Clean(siteStorageBase) && domainPattern.MatchString(filepath.Base(home))
}
