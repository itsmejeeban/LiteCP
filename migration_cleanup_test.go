package main

import "testing"

func TestMigrationCleanupProtectsUnfinishedJobs(t *testing.T) {
	for _, status := range []string{"queued", "connecting", "connected", "running", "restoring", "review", "awaiting-overwrite", "unknown"} {
		if migrationCleanupEligible(status) {
			t.Fatalf("cleanup must preserve %s", status)
		}
	}
	for _, status := range []string{"restored-review", "restore-review", "captured", "failed", "cancelled", "interrupted"} {
		if !migrationCleanupEligible(status) {
			t.Fatalf("cleanup should allow finished job %s", status)
		}
	}
}
