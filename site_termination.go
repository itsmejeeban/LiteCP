package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// Permanent removal is reachable only after an exact domain confirmation.
func (a *App) terminateWebsite(ctx context.Context, site Site) error {
	home := siteHome(site)
	if home == "" || filepath.Dir(home) != filepath.Clean(siteStorageBase) {
		return errors.New("permanent removal requires a managed /home/litecp/<domain> directory; legacy files must be reviewed separately")
	}
	info, err := os.Lstat(home)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return errors.New("unsafe website home; no files removed")
	}
	resolvedBase, err := filepath.EvalSymlinks(siteStorageBase)
	if err != nil {
		return err
	}
	if err == nil && info != nil {
		resolved, err := filepath.EvalSymlinks(home)
		if err != nil || filepath.Dir(resolved) != resolvedBase {
			return errors.New("website home resolves outside the storage boundary")
		}
	}
	state := a.store.PublicState()
	for _, backup := range state.Backups {
		if backup.SiteID == site.ID && backupInProgress(backup.Status) {
			return errors.New("wait for the website backup to finish before removing this website")
		}
	}

	for _, other := range state.Sites {
		if other.ID != site.ID && managedSiteRoot(other) == home {
			return errors.New("another website shares this directory")
		}
	}
	databases := []ManagedDatabase{}
	for _, database := range state.Databases {
		if database.SiteID != site.ID {
			continue
		}
		for _, other := range state.Databases {
			if other.SiteID != site.ID && (other.Name == database.Name || other.Username == database.Username) {
				return errors.New("a database or user is shared with another website; review it before removal")
			}
		}
		for _, other := range state.Sites {
			if other.ID != site.ID && other.Database == database.Name {
				return errors.New("another website uses this database")
			}
		}
		databases = append(databases, database)
	}
	if site.Database != "" {
		found := false
		for _, database := range databases {
			if database.Name == site.Database {
				found = true
			}
		}
		if !found {
			return errors.New("website database ownership cannot be verified; no data removed")
		}
	}
	root := a.system.websiteBackupsRoot(site)
	websiteBackupWorkers.Lock()
	defer websiteBackupWorkers.Unlock()
	for key := range websiteBackupWorkers.active {
		if filepath.Dir(key) == root {
			return errors.New("wait for the website backup to finish before removing this website")
		}
	}
	accounts, err := a.system.ftpAccounts()
	if err != nil {
		return err
	}
	if err := a.retainWebsiteBackups(site); err != nil {
		return err
	}
	rollbackDNS, err := a.prepareWebsiteDNSChange(ctx, site, nil)
	if err != nil {
		return err
	}
	defer func() {
		if a.store.SiteByID(site.ID) != nil {
			if e := rollbackDNS(nil); e != nil {
				log.Printf("DNS rollback for website %s failed: %v", site.ID, e)
			}
		}
	}()
	// Validate everything above before starting irreversible work. Keep the site
	// registered on any failure, so remaining data can be reviewed and retried.
	if err := a.system.RemoveSiteConfig(ctx, site); err != nil {
		return err
	}
	for _, account := range append([]FTPAccount{}, accounts...) {
		if account.SiteID != site.ID {
			continue
		}
		next := []FTPAccount{}
		for _, item := range accounts {
			if item.ID != account.ID {
				next = append(next, item)
			}
		}
		if err := a.system.updateFTP(ctx, site, next, account, "", "userdel"); err != nil {
			return fmt.Errorf("hosting stopped; remove FTP login: %w", err)
		}
		accounts = next
	}
	for _, database := range databases {
		if err := a.system.DropManagedDatabase(ctx, database); err != nil {
			return fmt.Errorf("hosting stopped; database removal failed: %w", err)
		}
		if err := a.store.RemoveDatabase(database.ID); err != nil {
			return err
		}
	}
	if !a.system.demo {
		if err := a.system.saveWebsiteCron(site, []WebsiteCronJob{}); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(home); err != nil {
		return fmt.Errorf("hosting stopped; website directory removal failed: %w", err)
	}
	if err := a.store.RemoveSite(site.ID); err != nil {
		return err
	}
	a.store.AddActivity("Website permanently removed", site.Domain+": website files, linked managed databases and FTP logins removed; local and remote backups retained", "warning")
	return nil
}
