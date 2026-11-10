package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

//go:embed scripts/panel-restore-points.py
var panelRestorePointsScript string

var panelRestorePointID = regexp.MustCompile(`^update-[a-zA-Z0-9]{8}$`)

func (a *App) panelRestorePoints(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.runPanelRestorePoints(w, r, "list")
	case http.MethodPut:
		var input struct {
			Limit *int `json:"limit"`
		}
		if err := decodeJSONWithLimit(r, &input, 1024); err != nil || input.Limit == nil || *input.Limit < 0 || *input.Limit > 10 {
			writeError(w, http.StatusBadRequest, "Enter a maximum of 0 to 10 restore points.")
			return
		}
		a.runPanelRestorePoints(w, r, "set-limit", strconv.Itoa(*input.Limit))
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeError(w, http.StatusMethodNotAllowed, "Use GET or PUT.")
	}
}

func (a *App) deletePanelRestorePoint(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/updates/restore-points/")
	if !panelRestorePointID.MatchString(id) {
		writeError(w, http.StatusBadRequest, "Choose a valid restore point.")
		return
	}
	a.runPanelRestorePoints(w, r, "delete", id)
}

func (a *App) runPanelRestorePoints(w http.ResponseWriter, r *http.Request, operation string, args ...string) {
	if a.system.demo {
		writeError(w, http.StatusConflict, "Restore points require an installed LiteCP server.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	command := append([]string{"-I", "-c", panelRestorePointsScript, operation}, args...)
	result, err := exec.CommandContext(ctx, "/usr/bin/python3", command...).Output()
	if err != nil {
		var failure *exec.ExitError
		if errors.As(err, &failure) {
			switch failure.ExitCode() {
			case 3:
				message := strings.TrimSpace(string(failure.Stderr))
				if message == "" || len(message) > 512 {
					message = "Restore point storage is busy. Refresh and try again."
				}
				writeError(w, http.StatusConflict, message)
				return
			case 4:
				writeError(w, http.StatusNotFound, "Restore point was not found. Refresh the list.")
				return
			}
			log.Printf("Panel restore points %s: %.512q", operation, failure.Stderr)
		} else {
			log.Printf("Panel restore points %s: %v", operation, err)
		}
		writeError(w, http.StatusInternalServerError, "Could not complete the restore point operation. Refresh the list before retrying.")
		return
	}
	if !json.Valid(result) {
		writeError(w, http.StatusInternalServerError, "Invalid restore point response.")
		return
	}
	if operation == "delete" {
		_ = a.store.AddActivity("Panel restore point deleted", args[0], "success")
	} else if operation == "set-limit" {
		_ = a.store.AddActivity("Panel restore point limit saved", "Maximum verified restore points: "+args[0], "success")
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, json.RawMessage(result))
}
