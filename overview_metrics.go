package main

import (
	"net/http"
	"sync"
	"time"
)

// Cache expensive runtime/service discovery across dashboard tabs. Numeric
// usage stays fresh without repeating every service probe on each request.
var overviewSnapshotCache struct {
	sync.Mutex
	system   *System
	at       time.Time
	snapshot SystemSnapshot
}

func (a *App) overviewMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		writeError(w, 405, "Use GET")
		return
	}
	if r.URL.Query().Get("backups") == "1" {
		if validExternalAPIGrant(r) {
			writeError(w, 403, "Backup history requires a panel session.")
			return
		}
		backups := []Backup{}
		if operator := a.requestOperator(r); operator == nil || operator.Role == "backups" {
			a.store.mu.RLock()
			backups = append(backups, a.store.cfg.Backups...)
			a.store.mu.RUnlock()
		}
		writeJSON(w, 200, map[string]any{"backups": backups})
		return
	}
	overviewSnapshotCache.Lock()
	snapshot, checked := overviewSnapshotCache.snapshot, overviewSnapshotCache.at
	if overviewSnapshotCache.system != a.system || time.Since(overviewSnapshotCache.at) >= 30*time.Second {
		snapshot, checked = a.system.Snapshot(r.Context(), a.store.Sites()), time.Now().UTC()
		if r.Context().Err() == nil {
			overviewSnapshotCache.system = a.system
			overviewSnapshotCache.at = checked
			overviewSnapshotCache.snapshot = snapshot
		}
	}
	overviewSnapshotCache.Unlock()
	if !a.store.Settings().EmailHostingEnabled {
		snapshot.Services = append([]ServiceStatus{}, snapshot.Services...)
		for i := range snapshot.Services {
			if snapshot.Services[i].Key == "mail" {
				snapshot.Services[i].State = "disabled"
			}
		}
	}
	usageMonitor.RLock()
	point := usageMonitor.latest
	usageMonitor.RUnlock()
	if !point.Time.IsZero() && time.Since(point.Time) < time.Minute {
		snapshot.CPUPercent = point.CPU
	}
	snapshot.Memory = memoryUsage()
	snapshot.Disk = diskUsage(r.Context())
	snapshot.UptimeSeconds, snapshot.LoadAverage = uptimeAndLoad()
	writeJSON(w, 200, map[string]any{
		"system": map[string]any{
			"cpuPercent": snapshot.CPUPercent, "memory": snapshot.Memory, "disk": snapshot.Disk,
			"uptimeSeconds": snapshot.UptimeSeconds, "loadAverage": snapshot.LoadAverage, "services": snapshot.Services,
		},
		"servicesCheckedAt": checked,
	})
}
