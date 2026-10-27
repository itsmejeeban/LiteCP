package main

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed scripts/update-panel.sh
var panelUpdateScript string

func (a *App) panelUpdateStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]string{"state": "idle"}
	if data, err := os.ReadFile(filepath.Join(a.system.dataDir, "panel-update.json")); err == nil {
		_ = json.Unmarshal(data, &status)
	}
	if status["state"] == "running" && !a.system.demo {
		active, _ := runCommand(r.Context(), 3*time.Second, "systemctl", "is-active", "litecp-panel-update.service")
		if strings.TrimSpace(active) != "active" && strings.TrimSpace(active) != "activating" {
			status["state"] = "failed"
			status["message"] = "The update job stopped. Review journalctl -u litecp-panel-update before retrying."
		}
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *App) startPanelUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Version string `json:"version"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Choose an available LiteCP release.")
		return
	}
	if a.system.demo {
		writeError(w, http.StatusBadRequest, "Updates are unavailable in demo mode.")
		return
	}
	release := checkRelease(version)
	if !release.Checked || !release.Available || body.Version != release.Latest {
		writeError(w, http.StatusConflict, "The selected release is no longer available. Check for updates again.")
		return
	}
	// A fixed transient unit prevents concurrent updates and survives panel restart.
	_, err := runCommand(r.Context(), 10*time.Second, "systemd-run", "--quiet", "--unit=litecp-panel-update", "--collect", "--property=RuntimeMaxSec=1800", "--property=UMask=0077", "--setenv=GOMAXPROCS=1", "--", "/bin/bash", "-c", panelUpdateScript, "litecp-update", release.Latest, a.system.dataDir)
	if err != nil {
		writeError(w, http.StatusConflict, "Could not start the update. Another update may already be running; check its status.")
		return
	}
	a.store.AddActivity("LiteCP update started", "Installing "+release.Latest, "info")
	writeJSON(w, http.StatusAccepted, map[string]string{"state": "running", "version": release.Latest})
}
