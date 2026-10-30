package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (a *App) backupCounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "Use GET.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	local, remote := 0, 0
	seen := map[string]bool{}
	countFile := func(path string) {
		path = filepath.Clean(path)
		if seen[path] {
			return
		}
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			seen[path] = true
			local++
		}
	}
	for _, backup := range a.store.PublicState().Backups {
		if validScheduledBackupPath(backup.Path) {
			countFile(backup.Path)
		}
		// A confirmed remote upload is independent of local retention. The remote
		// storage itself is not contacted by dashboard refreshes.
		if backup.Status == "complete" && backup.RemotePath != "" {
			remote++
		}
	}
	for _, site := range a.store.Sites() {
		root := a.system.websiteBackupsRoot(site)
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if _, err := loadWebsiteZIPBackup(root, entry.Name(), site.ID); err == nil {
				countFile(filepath.Join(root, entry.Name(), "website.zip"))
			}
		}
	}
	entries, _ := os.ReadDir(snapshotRoot)
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".zip") {
			countFile(filepath.Join(snapshotRoot, entry.Name()))
		}
	}
	writeJSON(w, 200, map[string]int{"local": local, "remote": remote})
}
