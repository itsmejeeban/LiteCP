package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const snapshotRoot = "/var/backups/litecp/system-snapshots"

func snapshotPath(name string) (string, error) {
	if name == "" || filepath.Base(name) != name || !strings.HasSuffix(name, ".zip") || strings.ContainsAny(name, "\\\x00\r\n") {
		return "", fmt.Errorf("Select a ZIP filename from the snapshot folder.")
	}
	return filepath.Join(snapshotRoot, name), nil
}
func (a *App) snapshotCommand(ctx context.Context, action, path string) (json.RawMessage, error) {
	output, err := a.system.applicationHostCommand(ctx, 40*time.Minute, "/usr/bin/python3", "/usr/local/lib/litecp/system-snapshot.py", action, path)
	if err != nil {
		return nil, fmt.Errorf("Snapshot operation stopped. %s", strings.TrimSpace(output))
	}
	return json.RawMessage(strings.TrimSpace(output)), nil
}
func (a *App) systemSnapshots(w http.ResponseWriter, r *http.Request) {
	if a.system.demo {
		writeError(w, 400, "System snapshots require an installed server.")
		return
	}
	if err := os.MkdirAll(snapshotRoot, 0700); err != nil {
		writeError(w, 500, "Cannot access snapshot storage.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("status") == "1" {
			content, err := os.ReadFile(filepath.Join(snapshotRoot, "status.json"))
			if err != nil {
				writeJSON(w, 200, map[string]string{"state": "idle"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(content)
			return
		}
		if name := r.URL.Query().Get("download"); name != "" {
			path, err := snapshotPath(name)
			if err != nil {
				writeError(w, 400, err.Error())
				return
			}
			f, err := secureOpenManaged(snapshotRoot, filepath.Base(path), os.O_RDONLY, 0)
			if err != nil {
				writeError(w, 404, "Snapshot not found.")
				return
			}
			defer f.Close()
			info, _ := f.Stat()
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(path)))
			http.ServeContent(w, r, name, info.ModTime(), f)
			return
		}
		entries, _ := os.ReadDir(snapshotRoot)
		items := []map[string]any{}
		for _, e := range entries {
			if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".zip") {
				info, err := e.Info()
				if err == nil {
					items = append(items, map[string]any{"name": e.Name(), "size": info.Size(), "createdAt": info.ModTime()})
				}
			}
		}
		writeJSON(w, 200, map[string]any{"items": items, "folder": snapshotRoot})
	case http.MethodPost:
		name := "litecp-system-" + time.Now().UTC().Format("20060102-150405") + ".zip"
		path, _ := snapshotPath(name)
		if err := a.startSnapshotJob(r.Context(), "create", path, ""); err != nil {
			writeError(w, 502, err.Error())
			return
		}
		a.store.AddActivity("System snapshot requested", name, "info")
		writeJSON(w, 202, map[string]string{"name": name, "message": "Snapshot started. Hosted services pause briefly while data is captured."})
	default:
		writeError(w, 405, "Use GET or POST.")
	}
}
func (a *App) uploadSystemSnapshot(w http.ResponseWriter, r *http.Request) {
	if a.system.demo {
		writeError(w, 400, "Server required.")
		return
	}
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, 400, "Choose a snapshot ZIP.")
		return
	}
	part, err := reader.NextPart()
	if err != nil || !strings.HasSuffix(strings.ToLower(part.FileName()), ".zip") {
		writeError(w, 400, "Choose a snapshot ZIP.")
		return
	}
	defer part.Close()
	os.MkdirAll(snapshotRoot, 0700)
	id, _ := randomToken(12)
	name := "uploaded-" + safeSiteID(id) + ".zip"
	path, _ := snapshotPath(name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		writeError(w, 500, "Cannot store upload.")
		return
	}
	_, err = io.Copy(f, part)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		writeError(w, 500, "Upload incomplete.")
		return
	}
	writeJSON(w, 201, map[string]string{"name": name})
}
func (a *App) inspectSystemSnapshot(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if decodeJSON(r, &input) != nil {
		writeError(w, 400, "Select a snapshot.")
		return
	}
	path, err := snapshotPath(input.Name)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	result, err := a.snapshotCommand(r.Context(), "inspect", path)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, result)
}
func (a *App) restoreSystemSnapshot(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name         string `json:"name"`
		Confirmation string `json:"confirmation"`
	}
	if decodeJSON(r, &input) != nil || input.Confirmation != "RESTORE ENTIRE LITECP" {
		writeError(w, 400, "Type RESTORE ENTIRE LITECP to confirm replacement.")
		return
	}
	path, err := snapshotPath(input.Name)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if _, err = a.snapshotCommand(r.Context(), "inspect", path); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	err = a.startSnapshotJob(r.Context(), "restore", path, "RESTORE ENTIRE LITECP")
	if err != nil {
		writeError(w, 502, "Cannot start snapshot restore.")
		return
	}
	writeJSON(w, 202, map[string]string{"message": "Restore scheduled. The panel will disconnect. Sign in with the snapshot administrator credentials after completion."})
}

func (a *App) startSnapshotJob(ctx context.Context, action, path, confirmation string) error {
	args := []string{"--unit=litecp-system-snapshot", "--collect", "--on-active=3s", "--property=UMask=0077", "/usr/bin/python3", "/usr/local/lib/litecp/system-snapshot.py", action, path}
	if confirmation != "" {
		args = append(args, "--confirm", confirmation)
	}
	if err := writeAtomic(filepath.Join(snapshotRoot, "status.json"), []byte(`{"state":"queued"}`), 0600); err != nil {
		return err
	}
	_, err := runCommand(ctx, 10*time.Second, "systemd-run", args...)
	if err != nil {
		return fmt.Errorf("Another snapshot operation may already be running; wait for it to finish.")
	}
	return nil
}
