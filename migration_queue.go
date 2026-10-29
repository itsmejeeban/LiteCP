package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
func (a *App) queueLiveAccounts(w http.ResponseWriter, r *http.Request, id string, ids []string, confirm bool) {
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
		job := LiveMigrationJob{ID: jobID, Source: source.Source, Accounts: source.Accounts, Account: account, Stage: "queued", Status: "queued", Message: "Queued for migration. Accounts are processed one at a time.", CredentialsRetained: true, AutoRestore: true, CreatedAt: time.Now().UTC(), StageStartedAt: time.Now().UTC()}
		if e = a.saveLiveJob(&job); e != nil {
			writeError(w, 500, "Cannot save migration queue; review existing jobs before retrying")
			return
		}
		queued = append(queued, job)
	}
	writeJSON(w, 202, map[string]any{"items": queued})
}
func (a *App) startNextQueuedMigration() {
	entries, err := os.ReadDir(filepath.Join(a.system.dataDir, "live-migrations"))
	if err != nil {
		return
	}
	queued := []LiveMigrationJob{}
	for _, entry := range entries {
		job, e := a.loadLiveJob(entry.Name())
		if e != nil {
			continue
		}
		if liveCaptureActive(job.ID) {
			return
		}
		if job.Status == "queued" {
			queued = append(queued, job)
		}
	}
	if len(queued) == 0 {
		return
	}
	sort.Slice(queued, func(i, j int) bool { return queued[i].CreatedAt.Before(queued[j].CreatedAt) })
	job := queued[0]
	setLiveJobStage(&job, "capture", "running", "Creating source native backup for selected account.")
	if err = a.saveLiveJob(&job); err != nil {
		return
	}
	_, err = runCommand(context.Background(), 15*time.Second, "systemd-run", "--unit="+liveCaptureUnit(job.ID), "--collect", "--property=RuntimeMaxSec=7500", "--property=UMask=0077", "--setenv=LITECP_DATA_DIR="+a.system.dataDir, "/usr/local/bin/litecp", "capture-worker", job.ID)
	if err != nil {
		job.Status = "failed"
		job.Message = "Cannot start queued capture: " + err.Error()
		_ = a.saveLiveJob(&job)
	}
}
