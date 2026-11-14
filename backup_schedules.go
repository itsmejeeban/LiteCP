package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type BackupSchedule struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	SiteID        string            `json:"siteId"`
	DestinationID string            `json:"destinationId"`
	Cron          string            `json:"cron"`
	WeekdayTimes  map[string]string `json:"weekdayTimes,omitempty"`
	Keep          int               `json:"keep"`
	Enabled       bool              `json:"enabled"`
	LastRun       time.Time         `json:"lastRun"`
	Status        string            `json:"status"`
}

func (s *Store) backupSchedulesList() []BackupSchedule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]BackupSchedule{}, s.cfg.BackupSchedules...)
}
func (s *Store) saveBackupSchedule(value BackupSchedule, remove bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	original := s.cfg.BackupSchedules
	next := []BackupSchedule{}
	found := false
	for _, item := range original {
		if item.ID == value.ID {
			found = true
			if !remove {
				next = append(next, value)
			}
		} else {
			next = append(next, item)
		}
	}
	if !found && !remove {
		next = append(next, value)
	}
	s.cfg.BackupSchedules = next
	if err := s.saveLocked(); err != nil {
		s.cfg.BackupSchedules = original
		return err
	}
	return nil
}
func (a *App) backupSchedules(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, 200, a.store.backupSchedulesList())
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "GET, POST, PUT, DELETE")
		writeError(w, 405, "Method not allowed.")
		return
	}
	var input BackupSchedule
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	var existing *BackupSchedule
	for _, item := range a.store.backupSchedulesList() {
		if item.ID == input.ID {
			copy := item
			existing = &copy
			break
		}
	}
	if r.Method != http.MethodPost && existing == nil {
		writeError(w, 404, "Schedule not found.")
		return
	}
	if r.Method != http.MethodDelete {
		if strings.TrimSpace(input.Name) == "" || len(input.Name) > 80 || input.Keep < 0 || input.Keep > 10000 {
			writeError(w, 400, "Enter a name and retention count from 0 to 10000 (0 keeps all).")
			return
		}
		if err := input.validateTiming(); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if input.SiteID != "all" && a.store.SiteByID(input.SiteID) == nil {
			writeError(w, 400, "Choose a website or all websites.")
			return
		}
		if input.DestinationID != "" && a.store.BackupDestinationByID(input.DestinationID) == nil {
			writeError(w, 400, "Destination not found.")
			return
		}
		if r.Method == http.MethodPost {
			var err error
			input.ID, err = randomToken(12)
			if err != nil {
				writeError(w, 500, err.Error())
				return
			}
			input.LastRun = time.Time{}
			input.Status = "Not run yet"
		} else {
			input.LastRun = existing.LastRun
			input.Status = existing.Status
		}
	}
	if err := a.store.saveBackupSchedule(input, r.Method == http.MethodDelete); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	a.store.AddActivity("Backup schedule updated", input.Name, "info")
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s BackupSchedule) validateTiming() error {
	if len(s.WeekdayTimes) == 0 {
		return validateCronSchedule(s.Cron)
	}
	if s.Cron != "" {
		return fmt.Errorf("choose weekday times or a cron expression")
	}
	for day, clock := range s.WeekdayTimes {
		if len(day) != 1 || day[0] < '0' || day[0] > '6' {
			return fmt.Errorf("invalid weekday")
		}
		parsed, err := time.Parse("15:04", clock)
		if err != nil || parsed.Format("15:04") != clock {
			return fmt.Errorf("enter a valid time for every selected weekday")
		}
	}
	return nil
}

func (s BackupSchedule) due(now time.Time) bool {
	if len(s.WeekdayTimes) != 0 {
		return s.WeekdayTimes[strconv.Itoa(int(now.Weekday()))] == now.Format("15:04")
	}
	return backupScheduleDue(s.Cron, now)
}

func cronFieldMatches(field string, value, min, max int) bool {
	for _, part := range strings.Split(field, ",") {
		segments := strings.Split(part, "/")
		step := 1
		if len(segments) == 2 {
			step, _ = strconv.Atoi(segments[1])
		}
		lo, hi := min, max
		if segments[0] != "*" {
			ends := strings.Split(segments[0], "-")
			lo, _ = strconv.Atoi(ends[0])
			hi = lo
			if len(ends) == 2 {
				hi, _ = strconv.Atoi(ends[1])
			}
		}
		if step > 0 && value >= lo && value <= hi && (value-lo)%step == 0 {
			return true
		}
	}
	return false
}
func backupScheduleDue(cron string, now time.Time) bool {
	if validateCronSchedule(cron) != nil {
		return false
	}
	f := strings.Fields(cron)
	if !cronFieldMatches(f[0], now.Minute(), 0, 59) || !cronFieldMatches(f[1], now.Hour(), 0, 23) || !cronFieldMatches(f[3], int(now.Month()), 1, 12) {
		return false
	}
	day := cronFieldMatches(f[2], now.Day(), 1, 31)
	week := cronFieldMatches(f[4], int(now.Weekday()), 0, 7) || (now.Weekday() == 0 && cronFieldMatches(f[4], 7, 0, 7))
	if f[2] != "*" && f[4] != "*" {
		return day || week
	}
	return day && week
}
func (a *App) startBackupSchedules() {
	if a.system.demo {
		return
	}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if !a.mutationMu.TryLock() {
				continue
			}
			now := time.Now()
			if location, err := time.LoadLocation(a.store.Settings().Timezone); err == nil {
				now = now.In(location)
			}
			for _, schedule := range a.store.backupSchedulesList() {
				if !schedule.Enabled || schedule.LastRun.Unix()/60 == now.Unix()/60 || !schedule.due(now) {
					continue
				}
				schedule.LastRun = now
				schedule.Status = "Running"
				if a.store.saveBackupSchedule(schedule, false) != nil {
					continue
				}
				err := a.runBackupSchedule(schedule)
				if err != nil {
					schedule.Status = "Failed: " + err.Error()
				} else {
					schedule.Status = "Completed"
				}
				_ = a.store.saveBackupSchedule(schedule, false)
				a.store.AddActivity("Scheduled backup", schedule.Name+": "+schedule.Status, "info")
			}
			a.mutationMu.Unlock()
		}
	}()
}
func (a *App) runBackupSchedule(schedule BackupSchedule) error {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	sites := a.store.Sites()
	if schedule.SiteID != "all" {
		site := a.store.SiteByID(schedule.SiteID)
		if site == nil {
			return fmt.Errorf("website no longer exists")
		}
		sites = []Site{*site}
	}
	if len(sites) == 0 {
		return fmt.Errorf("no websites to back up")
	}
	var destination *BackupDestination
	if schedule.DestinationID != "" {
		destination = a.store.BackupDestinationByID(schedule.DestinationID)
		if destination == nil {
			return fmt.Errorf("destination no longer exists")
		}
	}
	for _, site := range sites {
		backup, err := a.system.CreateBackup(ctx, site, a.store.websiteBackupDatabases(site.ID))
		if err != nil {
			return fmt.Errorf("%s: %w", site.Domain, err)
		}
		backup.ScheduleID = schedule.ID
		var uploadErr error
		if destination != nil {
			backup.DestinationID = destination.ID
			backup.DestinationName = destination.Name
			backup.RemotePath, uploadErr = a.system.UploadBackup(ctx, *destination, backup.Path)
			if uploadErr != nil {
				backup.Status = "remote failed"
			}
		}
		if err = a.store.AddBackup(backup); err != nil {
			return err
		}
		if uploadErr != nil {
			return uploadErr
		}
		if backup.RemotePath != "" {
			if err := a.system.RemoveBackupFile(backup.Path); err != nil {
				return err
			}
		}
		// Administrator explicitly authorizes pruning through the saved retention count.
		if schedule.Keep > 0 {
			a.store.mu.RLock()
			items := append([]Backup{}, a.store.cfg.Backups...)
			a.store.mu.RUnlock()
			count := 0
			for _, old := range items {
				if old.ScheduleID != schedule.ID || old.SiteID != site.ID {
					continue
				}
				count++
				if count <= schedule.Keep {
					continue
				}
				if err = a.system.RemoveBackupFile(old.Path); err != nil {
					return err
				}
				if err = a.store.RemoveBackups(map[string]bool{old.ID: true}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
