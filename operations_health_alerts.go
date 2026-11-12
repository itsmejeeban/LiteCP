package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func readOperationsResourceHistory(siteID string) []any {
	items := []any{}
	file, err := os.Open(filepath.Join(resourceDirectory(siteID), "history.jsonl"))
	if err != nil {
		return items
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return items
	}
	const tailBytes = 128 << 10
	if info.Size() > tailBytes {
		if _, err := file.Seek(-tailBytes, io.SeekEnd); err != nil {
			return items
		}
	}
	scanner := bufio.NewScanner(io.LimitReader(file, tailBytes))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	if info.Size() > tailBytes {
		// The seek may land inside a JSON record. Discard the partial line.
		scanner.Scan()
	}
	for scanner.Scan() {
		var row map[string]any
		if json.Unmarshal(scanner.Bytes(), &row) == nil {
			items = append(items, row)
		}
		if len(items) > 200 {
			items = items[1:]
		}
	}
	return items
}

// Collect persisted resource events as well as current state so a recovery
// between monitor cycles does not hide a CPU, memory, disk or process breach.
func operationsResourceAlerts(site Site, status resourceRuntimeStatus, history []any, now time.Time) []operationsAlert {
	if !resourceEnabled(site) {
		return nil
	}
	items := []operationsAlert{}
	if status.State == "Error" || status.Error != "" || status.Reason == "Monitor error" {
		items = append(items, operationsAlert{ID: "resource-runtime-" + site.ID, Category: "resource-enforcement", Title: "Website resource enforcement needs attention", Detail: site.Domain + " has an enabled policy whose runtime could not be verified. Review Resource Limit for its current status and recovery details.", Severity: "critical", Route: "website-resource-limits"})
	}
	lastBlock, lastProcess := time.Time{}, time.Time{}
	blockReason := ""
	for _, value := range history {
		row, ok := value.(map[string]any)
		if !ok {
			continue
		}
		timestamp, ok := row["at"].(string)
		if !ok {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, timestamp)
		if err != nil || at.After(now) || now.Sub(at) > 24*time.Hour {
			continue
		}
		switch row["event"] {
		case "block":
			if at.After(lastBlock) {
				lastBlock = at
				blockReason, _ = row["reason"].(string)
			}
		case "process limit reached":
			if at.After(lastProcess) {
				lastProcess = at
			}
		}
	}
	if status.State == "Resource blocked" && status.Reason != "Monitor error" {
		items = append(items, operationsAlert{ID: "resource-" + site.ID, Category: "resource-limit", Title: "Website resource limit reached", Detail: site.Domain + " is blocked after reaching its " + operationsResourceReason(status.Reason) + " limit. Check the usage history and recovery status.", Severity: "warning", Route: "website-resource-limits"})
	} else if !lastBlock.IsZero() {
		items = append(items, operationsAlert{ID: "resource-" + site.ID, Category: "resource-limit", Title: "Website resource limit was reached", Detail: fmt.Sprintf("%s reached its %s limit at %s. Review Resource Limit for its current recovery status.", site.Domain, operationsResourceReason(blockReason), lastBlock.UTC().Format(time.RFC3339)), Severity: "warning", Route: "website-resource-limits"})
	}
	if !lastProcess.IsZero() {
		items = append(items, operationsAlert{ID: "resource-process-" + site.ID, Category: "resource-limit", Title: "Website process limit reached", Detail: fmt.Sprintf("%s was denied additional processes at %s. Review its workload and configured process limit.", site.Domain, lastProcess.UTC().Format(time.RFC3339)), Severity: "warning", Route: "website-resource-limits"})
	}
	return items
}

func operationsResourceReason(reason string) string {
	switch reason {
	case "CPU", "Memory", "Disk":
		return strings.ToLower(reason)
	default:
		return "resource"
	}
}

func operationsIntegrityAlerts(state integrityState, readError error) []operationsAlert {
	if readError != nil {
		return []operationsAlert{{ID: "integrity-check", Category: "file-integrity", Title: "File integrity monitoring needs attention", Detail: "The saved integrity-monitor state could not be read. Review File Integrity and the service logs.", Severity: "critical", Route: "file-integrity"}}
	}
	if !state.Enabled {
		return nil
	}
	items := []operationsAlert{}
	if state.Error != "" {
		items = append(items, operationsAlert{ID: "integrity-check", Category: "file-integrity", Title: "File integrity check failed", Detail: "The last integrity check did not complete. Review File Integrity for its result and retry after correcting the cause.", Severity: "critical", Route: "file-integrity"})
	}
	if len(state.Changes) > 0 {
		items = append(items, operationsAlert{ID: "integrity-changes", Category: "file-integrity", Title: "Managed file integrity changes detected", Detail: fmt.Sprintf("%d managed files or directories differ from the trusted baseline. Review the changes in File Integrity.", len(state.Changes)), Severity: "warning", Route: "file-integrity"})
	}
	return items
}

func (a *App) collectOperationsHealthAlerts(ctx context.Context) []operationsAlert {
	items := []operationsAlert{}
	state, err := a.integrityRead()
	items = append(items, operationsIntegrityAlerts(state, err)...)
	if a.system.demo {
		return items
	}
	units := map[string]string{}
	for _, site := range a.store.Sites() {
		resourceStatus := resourceRuntimeStatus{}
		if resourceEnabled(site) {
			resourceStatus = resourceStatusForSite(site)
			items = append(items, operationsResourceAlerts(site, resourceStatus, readOperationsResourceHistory(site.ID), time.Now().UTC())...)
		}
		if !site.Suspended && !resourceEnabled(site) && site.Application == nil {
			units[a.system.phpUnitForVersion(site.PHPVersion)] = "Website PHP runtime"
		}
		if site.Cron && !site.Suspended {
			cronUnit := "crond"
			if a.system.os.ID == "ubuntu" || a.system.os.ID == "debian" {
				cronUnit = "cron"
			}
			units[cronUnit] = "Website scheduler"
		}
		if redisEnabled(site) && !site.Suspended && (!resourceEnabled(site) || resourceStatus.State == "Active") {
			units[redisUnit(site.ID)] = "Website Redis cache for " + site.Domain
		}
	}
	// Optional services are monitored only when systemd says they are enabled.
	// An installed service may intentionally be disabled by the administrator.
	for _, unit := range []string{"cron", "crond", "fail2ban", "litecp-ftps", "vsftpd", "pure-ftpd", "proftpd", "litecp-dns", "opendkim", "rspamd", "litecp-clamav-update.timer"} {
		if !unitFileExists(unit) || (unit == "opendkim" || unit == "rspamd") && !a.store.Settings().EmailHostingEnabled {
			continue
		}
		enabled, err := runCommand(ctx, 2*time.Second, "systemctl", "is-enabled", unit)
		if err == nil && (strings.TrimSpace(enabled) == "enabled" || strings.TrimSpace(enabled) == "enabled-runtime") {
			units[unit] = unit
		}
	}
	for unit, label := range units {
		if ctx.Err() != nil {
			break
		}
		state := a.system.serviceState(ctx, unit)
		if state == "running" || state == "activating" || state == "reloading" {
			continue
		}
		items = append(items, operationsAlert{ID: "service-" + unit, Category: "service-health", Title: "Managed service needs attention", Detail: label + " (" + unit + ") is not confirmed running. Review Services and its service log.", Severity: "critical", Route: "services"})
	}
	return items
}
