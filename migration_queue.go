package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func selectedMigrationAccounts(job LiveMigrationJob, ids []string) ([]string, error) {
	if len(ids) == 0 || len(ids) > 100 {
		return nil, fmt.Errorf("select between 1 and 100 accounts")
	}
	result := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		found := false
		for _, account := range job.Accounts {
			if account.ID == id {
				found = true
				break
			}
		}
		if !found || !liveAccountPattern.MatchString(id) {
			return nil, fmt.Errorf("select only discovered source accounts")
		}
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result, nil
}
func migrationThreadLimit(threads int) int {
	switch threads {
	case 1, 4, 6, 8:
		return threads
	default:
		return 1
	}
}

func migrationWorkerHasSpace(available, required int64, threads int) bool {
	const reserve = 1 << 30
	return available >= reserve && required <= (available-reserve)/int64(migrationThreadLimit(threads))
}

func (a *App) queueLiveAccounts(w http.ResponseWriter, r *http.Request, id string, ids []string, confirm bool, threads int) {
	if threads != 0 && threads != migrationThreadLimit(threads) {
		writeError(w, 400, "Select 1, 4, 6, or 8 migration threads")
		return
	}
	threads = migrationThreadLimit(threads)
	source, err := a.loadLiveJob(id)
	if err != nil {
		writeError(w, 404, "Source connection not found")
		return
	}
	if !confirm || source.Status != "connected" || !source.CredentialsRetained {
		writeError(w, 409, "Connect and confirm source backup creation first")
		return
	}
	selected, err := selectedMigrationAccounts(source, ids)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	oldRoot, _ := a.liveJobRoot(id)
	names := []string{"known_hosts", "identity"}
	if source.Source.AuthMethod == "password" {
		names = []string{"known_hosts", "password.enc"}
	}
	credentials := map[string][]byte{}
	for _, name := range names {
		value, e := os.ReadFile(filepath.Join(oldRoot, name))
		if e != nil {
			writeError(w, 409, "Reconnect the source; credentials are unavailable")
			return
		}
		credentials[name] = value
	}
	batch, err := randomToken(18)
	if err != nil {
		writeError(w, 500, "Could not allocate migration batch")
		return
	}
	queued := []LiveMigrationJob{}
	for _, account := range selected {
		token, e := randomToken(18)
		if e != nil {
			writeError(w, 500, "Could not allocate migration job")
			return
		}
		jobID := safeSiteID(token)
		root, _ := a.liveJobRoot(jobID)
		if e = os.MkdirAll(root, 0700); e != nil {
			writeError(w, 500, "Cannot create migration directory")
			return
		}
		for name, value := range credentials {
			if e = os.WriteFile(filepath.Join(root, name), value, 0600); e != nil {
				writeError(w, 500, "Cannot protect migration credentials")
				return
			}
		}
		job := LiveMigrationJob{ID: jobID, MigrationBatch: batch, MigrationThreads: threads, Source: source.Source, Accounts: source.Accounts, Account: account, Stage: "queued", Status: "queued", Message: "Queued for migration.", CredentialsRetained: true, AutoRestore: true, CreatedAt: time.Now().UTC(), StageStartedAt: time.Now().UTC()}
		if e = a.saveLiveJob(&job); e != nil {
			writeError(w, 500, "Cannot save migration queue; review existing jobs before retrying")
			return
		}
		queued = append(queued, job)
	}
	writeJSON(w, 202, map[string]any{"items": queued})
}
func (a *App) startNextQueuedMigration(statuses []migrationStatusSummary) {
	queued := []LiveMigrationJob{}
	for _, status := range statuses {
		if status.Status != "queued" {
			continue
		}
		job, err := a.loadLiveJob(status.ID)
		if err == nil && job.Status == "queued" {
			queued = append(queued, job)
		}
	}
	if len(queued) == 0 {
		return
	}
	// Check worker slots in one call. Capture units can still be unwinding
	// after their job becomes cancelled or records its final status.
	output, err := runCommand(context.Background(), 3*time.Second, "systemctl", "--no-legend", "--no-pager", "--plain", "--full", "list-units", "--type=service", "--state=active,activating,deactivating", "litecp-capture-*.service")
	if err != nil {
		return
	}
	batches := make(map[string]string, len(statuses))
	for _, status := range statuses {
		batches[status.ID] = status.MigrationBatch
	}
	active := []LiveMigrationJob{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if !strings.HasPrefix(fields[0], "litecp-capture-") || !strings.HasSuffix(fields[0], ".service") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(fields[0], "litecp-capture-"), ".service")
		if liveJobIDPattern.MatchString(id) {
			active = append(active, LiveMigrationJob{ID: id, MigrationBatch: batches[id]})
		}
	}
	sort.Slice(queued, func(i, j int) bool { return queued[i].CreatedAt.Before(queued[j].CreatedAt) })
	for _, job := range queuedMigrationWave(queued, active) {
		a.startQueuedMigration(job)
	}
}

// Only the oldest batch can start workers. A running batch keeps its slots
// until the workers exit, including workers still unwinding after cancellation.
func queuedMigrationWave(queued, active []LiveMigrationJob) []LiveMigrationJob {
	if len(queued) == 0 {
		return nil
	}
	first := queued[0]
	for _, job := range active {
		if first.MigrationBatch == "" || job.MigrationBatch != first.MigrationBatch {
			return nil
		}
	}
	slots := migrationThreadLimit(first.MigrationThreads) - len(active)
	count := 0
	for count < slots && count < len(queued) && queued[count].MigrationBatch == first.MigrationBatch {
		count++
	}
	return queued[:count]
}

func (a *App) startQueuedMigration(job LiveMigrationJob) {
	setLiveJobStage(&job, "capture", "running", "Creating source native backup for selected account.")
	if err := a.saveLiveJob(&job); err != nil {
		return
	}
	if job.Status == "cancelled" || a.liveMigrationCancelled(job.ID) {
		return
	}
	_, err := runCommand(context.Background(), 15*time.Second, "systemd-run", "--unit="+liveCaptureUnit(job.ID), "--collect", "--property=RuntimeMaxSec=7500", "--property=UMask=0077", "--setenv=LITECP_DATA_DIR="+a.system.dataDir, "/usr/local/bin/litecp", "capture-worker", job.ID)
	if err != nil {
		job.Status = "failed"
		job.Message = "Cannot start queued capture: " + err.Error()
		_ = a.saveLiveJob(&job)
	}
}
