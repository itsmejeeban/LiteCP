package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type overviewJob struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Message string `json:"message"`
	Stage   string `json:"stage"`
	Percent *int64 `json:"percent,omitempty"`
	Route   string `json:"route"`
}

func (a *App) overviewJobs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	items := []overviewJob{}
	state := a.store.PublicState()
	for _, b := range state.Backups {
		if backupInProgress(b.Status) {
			items = append(items, overviewJob{ID: "backup-" + b.ID, Title: "Website backup: " + b.Domain, Stage: b.Status, Route: "backups/history"})
		}
	}
	// Only in-process manual backup workers are live; stale job files are excluded.
	websiteBackupWorkers.Lock()
	keys := []string{}
	for key, active := range websiteBackupWorkers.active {
		if active {
			keys = append(keys, key)
		}
	}
	websiteBackupWorkers.Unlock()
	for _, dir := range keys {
		var job websiteBackupJob
		data, err := os.ReadFile(filepath.Join(dir, "job.json"))
		if err != nil || json.Unmarshal(data, &job) != nil || job.Status != "running" {
			continue
		}
		domain := job.SiteID
		if site := a.store.SiteByID(job.SiteID); site != nil {
			domain = site.Domain
		}
		items = append(items, overviewJob{ID: "zip-" + job.ID, Title: "Website backup: " + domain, Message: job.Message, Stage: job.Status, Route: "website-backups"})
	}
	entries, _ := os.ReadDir(filepath.Join(a.system.dataDir, "live-migrations"))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		job, err := a.loadLiveJob(entry.Name())
		if err != nil || (job.Status != "running" && job.Status != "queued" && job.Status != "restoring" && job.Status != "cancelling") {
			continue
		}
		title := "Migration: " + job.Source.Platform
		route := "migrations/jobs"
		if job.Source.Platform == "backup" {
			title = "Website restore"
			route = "backups/restore"
		}
		for _, account := range job.Accounts {
			if account.ID == job.Account && account.Domain != "" {
				title += " — " + account.Domain
				break
			}
		}
		a.attachMigrationProgress(&job)
		item := overviewJob{ID: "migration-" + job.ID, Title: title, Message: job.Message, Stage: job.Stage, Route: route}
		if job.Stage == "transfer" && job.Progress != nil {
			value := job.Progress.Percent
			if value < 0 {
				value = 0
			}
			if value > 99 {
				value = 99
			}
			item.Percent = &value
			item.Stage = "Transfer"
		}
		items = append(items, item)
	}
	for _, source := range []struct{ file, title, route string }{
		{filepath.Join(snapshotRoot, "status.json"), "System snapshot", "backups/system-snapshot"},
		{filepath.Join(systemUpdateRoot, "status.json"), "System update", "system-updates"},
	} {
		var status struct{ State, Action, Message string }
		data, err := os.ReadFile(source.file)
		if err != nil || json.Unmarshal(data, &status) != nil || (status.State != "running" && status.State != "queued") {
			continue
		}
		units := []string{"litecp-system-snapshot.service", "litecp-system-snapshot.timer"}
		if source.title == "System update" {
			units = []string{"litecp-system-update-job.service", "litecp-system-update.service"}
		}
		active := false
		for _, unit := range units {
			out, e := runCommand(r.Context(), 2*time.Second, "systemctl", "is-active", unit)
			if e == nil && (strings.TrimSpace(out) == "active" || strings.TrimSpace(out) == "activating") {
				active = true
				break
			}
		}
		if !active {
			continue
		}
		items = append(items, overviewJob{ID: source.title, Title: source.title + " — " + status.Action, Stage: status.State, Message: status.Message, Route: source.route})
	}
	if cfg, err := a.readSnapshotSettings(); err == nil && cfg.Pending != "" {
		var status struct{ State string }
		data, _ := os.ReadFile(filepath.Join(snapshotRoot, "status.json"))
		_ = json.Unmarshal(data, &status)
		if status.State == "complete" {
			items = append(items, overviewJob{ID: "snapshot-upload", Title: "System snapshot upload", Stage: "uploading", Message: cfg.UploadStatus, Route: "backups/system-snapshot"})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	writeJSON(w, 200, map[string]any{"items": items})
}
