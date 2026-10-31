package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type restorableBackup struct {
	ID            string    `json:"id"`
	SiteID        string    `json:"siteId"`
	Domain        string    `json:"domain"`
	Name          string    `json:"name"`
	Kind          string    `json:"kind"`
	Size          int64     `json:"size"`
	CreatedAt     time.Time `json:"createdAt"`
	Local         bool      `json:"local"`
	Remote        bool      `json:"remote"`
	Destination   string    `json:"destination"`
	WebsiteExists bool      `json:"websiteExists"`
}

func availableHistoryLocal(b Backup) bool {
	if !validScheduledBackupPath(b.Path) {
		return false
	}
	resolved, err := filepath.EvalSymlinks(b.Path)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(b.Path) {
		return false
	}
	info, err := os.Lstat(b.Path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0 && info.Size() == b.Size
}
func (a *App) backupRestoreInventory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	state := a.store.PublicState()
	items := []restorableBackup{}
	warnings := []string{}
	sites := append([]Site{}, state.Sites...)
	if entries, err := os.ReadDir(retainedWebsiteBackupRoot); err == nil {
		for _, entry := range entries {
			if site, e := retainedWebsite(entry.Name()); e == nil {
				sites = append(sites, site)
			}
		}
	}
	seenManual := map[string]bool{}
	for _, site := range sites {
		root := a.system.websiteBackupsRoot(site)
		if _, err := os.Stat(filepath.Join(retainedWebsiteDirectory(site.ID), "backups", "manual")); err == nil {
			root = filepath.Join(retainedWebsiteDirectory(site.ID), "backups", "manual")
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				warnings = append(warnings, "Cannot read local website backups for "+site.Domain)
			}
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			b, err := loadWebsiteZIPBackup(root, entry.Name(), site.ID)
			if err != nil {
				continue
			}
			if seenManual[b.ID] {
				continue
			}
			seenManual[b.ID] = true
			items = append(items, restorableBackup{ID: b.ID, SiteID: b.SiteID, Domain: site.Domain, Name: site.Domain + "-" + b.ID + ".zip", Kind: "website-zip", Size: b.Size, CreatedAt: b.CreatedAt, Local: !b.RemoteOnly, Remote: b.RemotePath != "", Destination: b.DestinationID, WebsiteExists: a.store.SiteByDomain(site.Domain) != nil})
		}
	}
	var mu sync.Mutex
	queue := make(chan Backup)
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for b := range queue {
				if backupInProgress(b.Status) || b.Size <= 0 {
					continue
				}
				item := restorableBackup{ID: b.ID, SiteID: b.SiteID, Domain: b.Domain, Name: filepath.Base(b.Path), Kind: "history", Size: b.Size, CreatedAt: b.CreatedAt, Local: availableHistoryLocal(b), Destination: b.DestinationName, WebsiteExists: a.store.SiteByID(b.SiteID) != nil}
				var warning string
				if b.RemotePath != "" {
					if destination := a.store.BackupDestinationByID(b.DestinationID); destination != nil {
						if err := a.system.historyRemoteOperation(ctx, destination, b, "stat", ""); err == nil {
							item.Remote = true
						} else {
							warning = "Remote backup unavailable for " + b.Domain + " (" + b.DestinationName + "): " + err.Error()
						}
					} else {
						warning = "Remote destination is no longer attached for " + b.Domain
					}
				}
				mu.Lock()
				if item.Local || item.Remote {
					items = append(items, item)
				}
				if warning != "" {
					warnings = append(warnings, warning)
				}
				mu.Unlock()
			}
		}()
	}
	for _, b := range state.Backups {
		select {
		case queue <- b:
		case <-ctx.Done():
		}
	}
	close(queue)
	workers.Wait()
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	writeJSON(w, 200, map[string]any{"items": items, "warnings": warnings, "checkedAt": time.Now().UTC()})
}
