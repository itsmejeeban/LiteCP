package main

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//go:embed scripts/system-update.py
var systemUpdateHelper string

const systemUpdateRoot = "/var/lib/litecp/system-updates"

func (a *App) systemUpdates(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if a.system.demo {
		writeError(w, 400, "System updates require the installed Linux server.")
		return
	}
	if r.Method == http.MethodGet {
		result := map[string]any{"inventory": nil, "status": map[string]string{"state": "idle", "message": "Check updates to compare installed and available versions."}, "schedule": map[string]string{"frequency": "disabled", "time": "03:00"}}
		for key, file := range map[string]string{"inventory": "inventory.json", "status": "status.json", "schedule": "schedule.json"} {
			var value any
			if data, err := os.ReadFile(filepath.Join(systemUpdateRoot, file)); err == nil && json.Unmarshal(data, &value) == nil {
				result[key] = value
			}
		}
		if status, ok := result["status"].(map[string]any); ok && status["state"] == "running" {
			manual, _ := runCommand(r.Context(), 5*time.Second, "systemctl", "is-active", "litecp-system-update-job.service")
			auto, _ := runCommand(r.Context(), 5*time.Second, "systemctl", "is-active", "litecp-system-update.service")
			if strings.TrimSpace(manual) != "active" && strings.TrimSpace(manual) != "activating" && strings.TrimSpace(auto) != "active" && strings.TrimSpace(auto) != "activating" {
				status["state"] = "interrupted"
				status["message"] = "The update worker stopped. Check package manager logs before retrying."
			}
		}
		result["litecpVersion"] = version
		writeJSON(w, 200, result)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "Use GET or POST.")
		return
	}
	var input struct {
		Action    string   `json:"action"`
		Packages  []string `json:"packages"`
		Frequency string   `json:"frequency"`
		Time      string   `json:"time"`
		Confirm   bool     `json:"confirm"`
	}
	if err := decodeJSONWithLimit(r, &input, 64<<10); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if input.Action != "check" && input.Action != "update" && input.Action != "schedule" {
		writeError(w, 400, "Choose a valid update action.")
		return
	}
	if input.Action != "check" && !input.Confirm {
		writeError(w, 400, "Confirm the update or schedule change.")
		return
	}
	if len(input.Packages) > 512 {
		writeError(w, 400, "Select at most 512 packages per update.")
		return
	}
	for _, name := range input.Packages {
		if len(name) > 200 || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9+_.:-]*$`).MatchString(name) {
			writeError(w, 400, "Invalid package name.")
			return
		}
	}
	if input.Action == "schedule" && ((input.Frequency != "disabled" && input.Frequency != "daily" && input.Frequency != "weekly") || !regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`).MatchString(input.Time)) {
		writeError(w, 400, "Choose daily, weekly or disabled, and a valid time.")
		return
	}
	for _, unit := range []string{"litecp-system-update-job.service", "litecp-system-update.service"} {
		active, _ := runCommand(r.Context(), 5*time.Second, "systemctl", "is-active", unit)
		if strings.TrimSpace(active) == "active" || strings.TrimSpace(active) == "activating" {
			writeError(w, 409, "A system update operation is already running.")
			return
		}
	}
	if err := os.MkdirAll(systemUpdateRoot, 0700); err != nil {
		writeError(w, 500, "Cannot prepare system update storage.")
		return
	}
	if err := writeAtomic(filepath.Join(systemUpdateRoot, "system-update.py"), []byte(systemUpdateHelper), 0700); err != nil {
		writeError(w, 500, "Cannot prepare the system update helper.")
		return
	}
	request, err := os.CreateTemp(systemUpdateRoot, "request-*.json")
	if err != nil {
		writeError(w, 500, "Cannot save the update request.")
		return
	}
	err = json.NewEncoder(request).Encode(input)
	request.Close()
	if err != nil {
		writeError(w, 500, "Cannot save the update request.")
		return
	}
	_, err = runCommand(r.Context(), 15*time.Second, "systemd-run", "--quiet", "--collect", "--unit=litecp-system-update-job", "--property=RuntimeMaxSec=7200", "--property=UMask=0077", "--", "/usr/bin/python3", filepath.Join(systemUpdateRoot, "system-update.py"), request.Name())
	if err != nil {
		writeError(w, 502, "Cannot start the system update worker. Review system service logs.")
		return
	}
	a.store.AddActivity("System update requested", input.Action+" operation started", "info")
	writeJSON(w, 202, map[string]string{"state": "running", "message": "Operation started in the background. You may leave this page."})
}
