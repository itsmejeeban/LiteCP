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
