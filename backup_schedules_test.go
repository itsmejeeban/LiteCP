package main

import (
	"testing"
	"time"
)

func TestBackupScheduleDue(t *testing.T) {
	now := time.Date(2026, 11, 1, 2, 30, 0, 0, time.UTC)
	for _, value := range []string{"*/30 * * * *", "30 2 1 * *", "30 2 * * 0", "30 2 * * 7", "30 2 3 * 0"} {
		if !backupScheduleDue(value, now) {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"15 * * * *", "30 3 * * *", "30 2 * 12 *", "bad"} {
		if backupScheduleDue(value, now) {
			t.Fatal(value)
		}
	}
}

func TestBackupScheduleWeekdayTimes(t *testing.T) {
	schedule := BackupSchedule{WeekdayTimes: map[string]string{"0": "02:30", "1": "09:15"}}
	if err := schedule.validateTiming(); err != nil {
		t.Fatal(err)
	}
	sunday := time.Date(2026, 11, 1, 2, 30, 0, 0, time.UTC)
	if !schedule.due(sunday) || schedule.due(sunday.AddDate(0, 0, 1)) || !schedule.due(sunday.AddDate(0, 0, 1).Add(6*time.Hour+45*time.Minute)) || schedule.due(sunday.AddDate(0, 0, 2)) {
		t.Fatal("weekly schedule did not honor each selected day's time")
	}
	schedule.WeekdayTimes["1"] = "25:00"
	if schedule.validateTiming() == nil {
		t.Fatal("invalid time was accepted")
	}
}
