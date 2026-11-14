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
	StartedAt      time.Time `json:"startedAt,omitempty"`
	ElapsedSeconds int64     `json:"elapsedSeconds,omitempty"`
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Message        string    `json:"message"`
	Stage          string    `json:"stage"`
	Percent        *int64    `json:"percent,omitempty"`
	Route          string    `json:"route"`
}

func (a *App) overviewJobs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	items := []overviewJob{}
	state := a.store.PublicState()
	for _, b := range state.Backups {
		if backupInProgress(b.Status) {
			items = append(items, overviewJob{ID: "backup-" + b.ID, Title: "Website backup: " + b.Domain, Stage: b.Status, Route: "backups/history", StartedAt: b.CreatedAt})
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
		items = append(items, overviewJob{ID: "zip-" + job.ID, Title: "Website backup: " + domain, Message: job.Message, Stage: job.Status, Route: "website-backups", StartedAt: job.StartedAt})
	}
	for _, status := range a.migrationStatuses() {
		if !overviewMigrationActive(status.Status) {
			continue
		}
		job, err := a.loadLiveJob(status.ID)
		if err != nil || !overviewMigrationActive(job.Status) {
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
		item := overviewJob{ID: "migration-" + job.ID, Title: title, Message: job.Message, Stage: job.Stage, Route: route, StartedAt: job.CreatedAt}
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
	for _, source := range []struct{ file, unit, title, route string }{{filepath.Join(a.system.dataDir, "panel-update.json"), "litecp-panel-update.service", "Panel update", "settings/update"}, {filepath.Join(a.system.dataDir, "server-scan", "status.json"), "litecp-server-malware-scan.service", "Malware scan", "security"}} {
		var job struct {
			State, Message string
			StartedAt      time.Time
		}
		b, e := os.ReadFile(source.file)
		if e != nil || json.Unmarshal(b, &job) != nil || job.State != "running" {
			continue
		}
		out, e := runCommand(r.Context(), 2*time.Second, "systemctl", "is-active", source.unit)
		if e == nil && strings.TrimSpace(out) == "active" {
			items = append(items, overviewJob{ID: source.unit, Title: source.title, Message: job.Message, Stage: job.State, Route: source.route, StartedAt: job.StartedAt})
		}
	}
	operationsMu.Lock()
	policy, readErr := a.readOperations()
	operationsMu.Unlock()
	if readErr == nil {
		for id, v := range policy.Verification {
			if v.Status == "running" && time.Since(v.CheckedAt) < 31*time.Minute {
				items = append(items, overviewJob{ID: "verify-" + id, Title: "Backup verification", Message: "Read-only archive integrity check", Stage: "verifying", Route: "operations", StartedAt: v.CheckedAt})
			}
		}
	}
	for i := range items {
		if !items[i].StartedAt.IsZero() {
			items[i].ElapsedSeconds = int64(time.Since(items[i].StartedAt).Seconds())
			if items[i].ElapsedSeconds < 0 {
				items[i].ElapsedSeconds = 0
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	writeJSON(w, 200, map[string]any{"items": items})
}

func overviewMigrationActive(status string) bool {
	return status == "running" || status == "queued" || status == "restoring" || status == "cancelling"
}
