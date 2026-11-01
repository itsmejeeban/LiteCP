package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (a *App) stageRetainedCapture(job *LiveMigrationJob) error {
	if !job.NativeReady {
		return fmt.Errorf("verified native backup is required")
	}
	root, err := a.liveJobRoot(job.ID)
	if err != nil {
		return err
	}
	archive := filepath.Join(root, "portable-backup")
	info, err := os.Lstat(archive)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("complete portable archive is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	required, err := migrationArchiveBytes(ctx, archive)
	if err != nil {
		return err
	}
	available, err := migrationFreeBytes(ctx, root)
	if err != nil || available < required+(1<<30) {
		return fmt.Errorf("insufficient disk space for archive plus safety reserve")
	}
	parent := filepath.Join(a.system.dataDir, "migrations")
	if err = os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	fresh, err := os.MkdirTemp(parent, ".restage-"+job.ID+"-")
	if err != nil {
		return err
	}
	if err = os.Mkdir(filepath.Join(fresh, "files"), 0700); err != nil {
		return err
	}
	preview, err := extractMigrationWithLimit(ctx, archive, filepath.Join(fresh, "files"), job.Source.Platform, liveMigrationExtractLimit)
	if err != nil {
		return err
	}
	preview.ID = job.ID
	content, _ := json.Marshal(preview)
	if err = writeAtomic(filepath.Join(fresh, "manifest.json"), content, 0600); err != nil {
		return err
	}
	stage := filepath.Join(parent, job.ID)
	previous := ""
	if _, err = os.Lstat(stage); err == nil {
		previous = fresh + "-previous"
		if err = os.Rename(stage, previous); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = os.Rename(fresh, stage); err != nil {
		if previous != "" {
			_ = os.Rename(previous, stage)
		}
		return err
	}
	job.Preview = &preview
	job.Status = "review"
	return a.saveLiveJob(job)
}
