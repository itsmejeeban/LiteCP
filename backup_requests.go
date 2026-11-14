package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

var backupRequestSlots = make(chan struct{}, 8)
var backupRequestWorker sync.Mutex

func backupInProgress(status string) bool {
	return status == "queued" || status == "creating" || status == "uploading"
}

func (s *Store) saveBackupRequest(backup Backup, enqueue bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	original := append([]Backup{}, s.cfg.Backups...)
	if enqueue {
		for _, item := range s.cfg.Backups {
			if item.SiteID == backup.SiteID && backupInProgress(item.Status) {
				return fmt.Errorf("A backup is already queued or running for this website.")
			}
		}
	}
	found := false
	for i := range s.cfg.Backups {
		if s.cfg.Backups[i].ID == backup.ID {
			s.cfg.Backups[i] = backup
			found = true
			break
		}
	}
	if !found {
		s.cfg.Backups = append([]Backup{backup}, s.cfg.Backups...)
	}
	if err := s.saveLocked(); err != nil {
		s.cfg.Backups = original
		return err
	}
	return nil
}

func (a *App) enqueueBackup(site Site, destination *BackupDestination) (Backup, error) {
	select {
	case backupRequestSlots <- struct{}{}:
	default:
		return Backup{}, fmt.Errorf("Backup queue is full. Try again after a backup finishes.")
	}
	id, err := randomToken(16)
	if err != nil {
		<-backupRequestSlots
		return Backup{}, err
	}
	job := Backup{ID: id, SiteID: site.ID, Domain: site.Domain, CreatedAt: time.Now().UTC(), Status: "queued"}
	if destination != nil {
		job.DestinationID = destination.ID
		job.DestinationName = destination.Name
	}
	if err = a.store.saveBackupRequest(job, true); err != nil {
		<-backupRequestSlots
		return Backup{}, err
	}
	go func(job Backup) {
		defer func() { <-backupRequestSlots }()
		backupRequestWorker.Lock()
		defer backupRequestWorker.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
		defer cancel()
		finish := func(status string) bool {
			job.Status = status
			if err := a.store.saveBackupRequest(job, false); err != nil {
				a.store.AddActivity("Backup history error", site.Domain+": backup state could not be saved; retained files need review.", "warning")
				return false
			}
			return true
		}
		if !finish("creating") {
			return
		}
		result, err := a.system.CreateBackup(ctx, site, a.store.websiteBackupDatabases(site.ID))
		if err != nil {
			finish("failed")
			a.store.AddActivity("Backup failed", site.Domain+": "+err.Error(), "error")
			return
		}
		job.Path = result.Path
		job.Size = result.Size
		job.FileBytes = result.FileBytes
		job.DatabaseDumpBytes = result.DatabaseDumpBytes
		job.Scope = result.Scope
		if destination != nil {
			if !finish("uploading") {
				return
			}
			path, err := a.system.UploadBackup(ctx, *destination, job.Path)
			if err != nil {
				finish("remote failed")
				a.store.AddActivity("Remote backup failed", site.Domain+": local archive retained.", "warning")
				return
			}
			job.RemotePath = path
		}
		job.Status = "complete"
		if err = a.store.saveBackupRequest(job, false); err != nil {
			a.store.AddActivity("Backup history error", site.Domain+": completed archive retained for review.", "warning")
			return
		}
		if job.RemotePath != "" {
			if err = a.system.RemoveBackupFile(job.Path); err != nil {
				finish("complete - local retained")
			}
		}
		a.store.AddActivity("Backup completed", site.Domain+" files and database saved.", "success")
	}(job)
	return job, nil
}

// A panel restart interrupts in-process workers; never leave a stale running label.
func (s *Store) interruptBackupRequests() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	original := append([]Backup{}, s.cfg.Backups...)
	changed := false
	for i := range s.cfg.Backups {
		if backupInProgress(s.cfg.Backups[i].Status) {
			s.cfg.Backups[i].Status = "interrupted"
			changed = true
		}
	}
	if changed {
		if err := s.saveLocked(); err != nil {
			s.cfg.Backups = original
			return err
		}
	}
	return nil
}
