package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// These workers run outside the panel process. Their durable status files let
// notification checks continue after the browser closes or the panel restarts.
func (a *App) collectOperationsJobAlerts(ctx context.Context) []operationsAlert {
	if a.system.demo {
		return nil
	}
	items := []operationsAlert{}
	for _, job := range []struct {
		kind, path string
		units      []string
	}{
		{"snapshot", filepath.Join(snapshotRoot, "status.json"), []string{"litecp-system-snapshot.service", "litecp-system-snapshot.timer"}},
		{"system-update", filepath.Join(systemUpdateRoot, "status.json"), []string{"litecp-system-update-job.service", "litecp-system-update.service"}},
		{"panel-update", filepath.Join(a.system.dataDir, "panel-update.json"), []string{"litecp-panel-update.service"}},
		{"malware-scan", filepath.Join(a.system.dataDir, "server-scan", "status.json"), []string{"litecp-server-malware-scan.service"}},
		{"selinux-repair", filepath.Join(a.system.dataDir, "selinux-repair", "status.json"), []string{selinuxRepairUnit}},
	} {
		file, err := os.Open(job.path)
		if err != nil {
			continue
		}
		info, statErr := file.Stat()
		content, readErr := io.ReadAll(io.LimitReader(file, 1<<20))
		file.Close()
		if statErr != nil || readErr != nil {
			continue
		}
		var status struct {
			State  string `json:"state"`
			Action string `json:"action"`
		}
		if json.Unmarshal(content, &status) != nil {
			continue
		}
		if (status.State == "running" || status.State == "queued") && time.Since(info.ModTime()) > 2*time.Minute {
			if job.kind == "malware-scan" {
				// Scheduled scans run from cron, outside the interactive scan
				// service. The worker's advisory lock covers both entry points.
				lock := filepath.Join(a.system.dataDir, "server-scan", "lock")
				if _, err := os.Stat(lock); err == nil {
					if _, err := runCommand(ctx, 2*time.Second, "flock", "--nonblock", lock, "/usr/bin/true"); err == nil {
						status.State = "interrupted"
					}
				}
			} else {
				// A queued snapshot also owns a timer. Check every owner before
				// reporting interruption, including scheduled system updates.
				inactive := true
				for _, unit := range job.units {
					output, err := runCommand(ctx, 2*time.Second, "systemctl", "is-active", unit)
					state := strings.TrimSpace(output)
					if state == "active" || state == "activating" {
						inactive = false
						break
					}
					if err == nil || (state != "inactive" && state != "failed" && state != "unknown") {
						inactive = false
					}
				}
				if inactive {
					status.State = "interrupted"
				}
			}
		}
		if alert, ok := operationsJobStatusAlert(job.kind, status.State, status.Action); ok {
			alert.ID = fmt.Sprintf("job-%s-%d", job.kind, info.ModTime().UnixNano())
			items = append(items, alert)
		}
	}
	if alert, ok := a.operationsWebmailSetupAlert(ctx); ok {
		items = append(items, alert)
	}
	for _, site := range a.store.Sites() {
		root := a.system.websiteBackupsRoot(site)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return items
			}
			if !entry.IsDir() || !liveJobIDPattern.MatchString(entry.Name()) {
				continue
			}
			var job websiteBackupJob
			data, err := os.ReadFile(filepath.Join(root, entry.Name(), "job.json"))
			if err != nil || json.Unmarshal(data, &job) != nil || job.SiteID != site.ID || job.ID != entry.Name() || job.Status != "running" {
				continue
			}
			websiteBackupWorkers.Lock()
			active := websiteBackupWorkers.active[filepath.Join(root, job.ID)]
			websiteBackupWorkers.Unlock()
			if !active {
				items = append(items, operationsAlert{
					ID: "interrupted-backup-" + job.ID, Category: "backup",
					Title: "Website backup was interrupted", Detail: site.Domain + ": review the partial backup and create a new backup.",
					Severity: "warning", Route: "website-backups",
				})
			}
		}
	}
	return items
}

func (a *App) operationsWebmailSetupAlert(ctx context.Context) (operationsAlert, bool) {
	base := filepath.Join(a.system.dataDir, "webmail-setup")
	latest, err := os.ReadFile(filepath.Join(base, "latest"))
	name := strings.TrimSpace(string(latest))
	if err != nil || filepath.Base(name) != name || !strings.HasPrefix(name, "attempt-") {
		return operationsAlert{}, false
	}
	directory := filepath.Join(base, name)
	result, err := os.ReadFile(filepath.Join(directory, "result"))
	if err == nil && strings.TrimSpace(string(result)) == "0" {
		return operationsAlert{}, false
	}
	if err != nil {
		info, statErr := os.Stat(directory)
		if statErr != nil || time.Since(info.ModTime()) < 2*time.Minute {
			return operationsAlert{}, false
		}
		output, _ := runCommand(ctx, 2*time.Second, "systemctl", "is-active", "litecp-webmail-setup.service")
		state := strings.TrimSpace(output)
		if state != "inactive" && state != "failed" && state != "unknown" {
			return operationsAlert{}, false
		}
	}
	return operationsAlert{
		ID: "webmail-setup-" + name, Category: "mail",
		Title: "Webmail setup did not complete", Detail: "Review the saved webmail setup status and installer log before retrying.",
		Severity: "warning", Route: "email/configure-webmail",
	}, true
}

func operationsJobStatusAlert(kind, state, action string) (operationsAlert, bool) {
	alert := operationsAlert{Category: kind, Severity: "warning"}
	if kind == "malware-scan" && state == "infected" {
		alert.Category = "malware-detected"
		alert.Title = "Server malware scan found potential threats"
		alert.Detail = "Review affected files and detection details in Malware Scan."
		alert.Severity = "critical"
		alert.Route = "security"
		return alert, true
	}
	if state != "failed" && state != "error" && state != "interrupted" {
		return operationsAlert{}, false
	}
	switch kind {
	case "selinux-repair":
		alert.Category = "security"
		alert.Title = "SELinux repair did not complete"
		alert.Detail = "Review the repair log and rollback status in Security Dashboard."
		alert.Route = "security-dashboard"
	case "snapshot":
		alert.Title = "System snapshot failed"
		if action == "restore" {
			alert.Title = "System snapshot restoration failed"
		}
		alert.Detail = "Review the snapshot status and retained recovery files."
		alert.Route = "backups/system-snapshot"
	case "system-update":
		alert.Title = "System update operation failed"
		alert.Detail = "Review package update status and service logs before retrying."
		alert.Route = "system-updates"
	case "panel-update":
		alert.Title = "LiteCP update failed"
		alert.Detail = "Review the update status and retained restore point before retrying."
		alert.Route = "settings/update"
	case "malware-scan":
		alert.Title = "Server malware scan did not complete"
		alert.Detail = "Review scanner status and signatures, then retry the scan."
		alert.Route = "security"
	default:
		return operationsAlert{}, false
	}
	return alert, true
}
