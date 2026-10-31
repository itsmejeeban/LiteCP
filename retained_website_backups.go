package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const retainedWebsiteBackupRoot = "/var/backups/litecp/retained-websites"

var retainedWebsiteIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func retainedWebsiteDirectory(id string) string {
	return filepath.Join(retainedWebsiteBackupRoot, id)
}
func (a *App) retainWebsiteBackups(site Site) error {
	if !retainedWebsiteIDPattern.MatchString(site.ID) {
		return fmt.Errorf("invalid website ID; removal stopped")
	}
	directory := retainedWebsiteDirectory(site.ID)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(site)
	if err != nil {
		return err
	}
	if err = writeAtomic(filepath.Join(directory, "site.json"), data, 0600); err != nil {
		return err
	}
	old := filepath.Join(siteHome(site), "backups")
	target := filepath.Join(directory, "backups")
	if _, err = os.Lstat(old); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err = moveWebsiteBackupsToSafety(old, target); err != nil {
		return err
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	original := append([]Backup{}, a.store.cfg.Backups...)
	for i := range a.store.cfg.Backups {
		b := &a.store.cfg.Backups[i]
		if b.SiteID == site.ID && strings.HasPrefix(b.Path, old+string(filepath.Separator)) {
			b.Path = target + strings.TrimPrefix(b.Path, old)
		}
	}
	if err = a.store.saveLocked(); err != nil {
		a.store.cfg.Backups = original
		rollback := os.Rename(target, old)
		return fmt.Errorf("cannot save retained backup paths: %v (rollback: %v)", err, rollback)
	}
	return nil
}
func moveWebsiteBackupsToSafety(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("unsafe backup folder; website removal stopped")
	}
	if _, err = os.Lstat(target); !os.IsNotExist(err) {
		return fmt.Errorf("retained backup folder already exists; website removal stopped")
	}
	if err = os.Rename(source, target); err != nil {
		return fmt.Errorf("cannot preserve local backups; website removal stopped: %w", err)
	}
	return nil
}
func retainedWebsite(id string) (Site, error) {
	var site Site
	if !retainedWebsiteIDPattern.MatchString(id) {
		return site, fmt.Errorf("invalid backup website ID")
	}
	data, err := os.ReadFile(filepath.Join(retainedWebsiteDirectory(id), "site.json"))
	if err != nil {
		return site, err
	}
	err = json.Unmarshal(data, &site)
	if err != nil || site.ID != id || !domainPattern.MatchString(site.Domain) {
		return Site{}, fmt.Errorf("invalid retained website metadata")
	}
	return site, nil
}
