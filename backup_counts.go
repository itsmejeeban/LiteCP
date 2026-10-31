package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) backupCounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "Use GET.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	local, remote := 0, 0
	var latestLocal, latestRemote time.Time
	updateLatest := func(latest *time.Time, created time.Time) {
		if created.After(*latest) {
			*latest = created
		}
	}
	seen := map[string]bool{}
	countFile := func(path string, created time.Time) {
		path = filepath.Clean(path)
		if seen[path] {
			return
		}
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			seen[path] = true
			local++
			if created.IsZero() {
				created = info.ModTime()
			}
			updateLatest(&latestLocal, created)
		}
	}
	for _, backup := range a.store.PublicState().Backups {
		if validScheduledBackupPath(backup.Path) {
			countFile(backup.Path, backup.CreatedAt)
		}
		// A confirmed remote upload is independent of local retention. The remote
		// storage itself is not contacted by dashboard refreshes.
		if backup.Status == "complete" && backup.RemotePath != "" {
			remote++
			updateLatest(&latestRemote, backup.CreatedAt)
		}
	}
	for _, site := range a.store.Sites() {
		root := a.system.websiteBackupsRoot(site)
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if item, err := loadWebsiteZIPBackup(root, entry.Name(), site.ID); err == nil {
				if !item.RemoteOnly {
					countFile(filepath.Join(root, entry.Name(), "website.zip"), item.CreatedAt)
				}
				if item.RemotePath != "" {
					remote++
					updateLatest(&latestRemote, item.CreatedAt)
				}
			}
		}
	}
	entries, _ := os.ReadDir(snapshotRoot)
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".zip") {
			countFile(filepath.Join(snapshotRoot, entry.Name()), time.Time{})
		}
	}
	var localCreated, remoteCreated *time.Time
	if !latestLocal.IsZero() {
		localCreated = &latestLocal
	}
	if !latestRemote.IsZero() {
		remoteCreated = &latestRemote
	}
	writeJSON(w, 200, map[string]any{"local": local, "remote": remote, "lastLocalCreatedAt": localCreated, "lastRemoteCreatedAt": remoteCreated})
}
