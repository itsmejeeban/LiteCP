package main

import (
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
)

// The client supplies a recorded ID, never a filesystem path.
func (a *App) backupHistoryItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodDelete && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, DELETE, POST")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	var selected *Backup
	for _, backup := range a.store.PublicState().Backups {
		if backup.ID == r.URL.Query().Get("id") {
			copy := backup
			selected = &copy
			break
		}
	}
	if selected == nil {
		writeError(w, http.StatusNotFound, "Backup not found.")
		return
	}
	if backupInProgress(selected.Status) {
		writeError(w, http.StatusConflict, "Backup is still running. Wait until it finishes.")
		return
	}
	if r.Method == http.MethodPost {
		preview, err := a.prepareHistoryBackup(r.Context(), *selected)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, preview)
		return
	}
	resolved, err := filepath.EvalSymlinks(selected.Path)
	missing := errors.Is(err, os.ErrNotExist)
	if !validScheduledBackupPath(selected.Path) || (!missing && (err != nil || filepath.Clean(resolved) != filepath.Clean(selected.Path))) {
		writeError(w, http.StatusBadRequest, "Backup path could not be safely verified.")
		return
	}
	if r.Method == http.MethodDelete && r.URL.Query().Get("remote") == "true" {
		if selected.RemotePath == "" {
			writeError(w, 400, "No remote copy is recorded.")
			return
		}
		if err := a.system.historyRemoteOperation(r.Context(), a.store.BackupDestinationByID(selected.DestinationID), *selected, "remove", ""); err != nil {
			writeError(w, 502, err.Error())
			return
		}
	}
	if r.Method == http.MethodDelete {
		if err := a.system.RemoveBackupFile(selected.Path); err != nil {
			writeError(w, http.StatusInternalServerError, "Could not remove the local backup.")
			return
		}
		if err := a.store.RemoveBackups(map[string]bool{selected.ID: true}); err != nil {
			writeError(w, http.StatusInternalServerError, "Archive removed, but history could not be updated. Retry removing this entry.")
			return
		}
		detail := "backup removed; remote copy retained"
		if r.URL.Query().Get("remote") == "true" {
			detail = "local and remote backup removed"
		}
		a.store.AddActivity("Backup removed", selected.Domain+" "+detail, "warning")
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	localPath := selected.Path
	if missing && selected.RemotePath != "" {
		localPath, err = a.historyLocalCopy(r.Context(), *selected)
		if err != nil {
			writeError(w, 502, err.Error())
			return
		}
		defer os.Remove(localPath)
	}
	file, err := os.Open(localPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "Local backup file is unavailable.")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusBadRequest, "Backup is not a regular archive.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(selected.Path)}))
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}
