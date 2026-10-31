package main

import (
	"testing"
	"time"
)

func TestBackupRequestHistoryUpdatesAndRejectsDuplicates(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	job := Backup{ID: "request", SiteID: "website", Status: "queued", CreatedAt: time.Now()}
	if err = s.saveBackupRequest(job, true); err != nil {
		t.Fatal(err)
	}
	duplicate := job
	duplicate.ID = "another"
	if s.saveBackupRequest(duplicate, true) == nil {
		t.Fatal("duplicate accepted")
	}
	job.Status = "creating"
	if err = s.saveBackupRequest(job, false); err != nil {
		t.Fatal(err)
	}
	job.Status = "complete"
	job.Path = "archive"
	if err = s.saveBackupRequest(job, false); err != nil {
		t.Fatal(err)
	}
	s.mu.RLock()
	if len(s.cfg.Backups) != 1 || s.cfg.Backups[0].Status != "complete" {
		t.Fatal(s.cfg.Backups)
	}
	s.mu.RUnlock()
}
func TestBackupRequestRestartMarksInterrupted(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"queued", "creating", "uploading", "complete"} {
		if err = s.saveBackupRequest(Backup{ID: status, SiteID: status, Status: status}, true); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.interruptBackupRequests(); err != nil {
		t.Fatal(err)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, job := range s.cfg.Backups {
		if job.ID == "complete" {
			if job.Status != "complete" {
				t.Fatal(job)
			}
		} else if job.Status != "interrupted" {
			t.Fatal(job)
		}
	}
}
