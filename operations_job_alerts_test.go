package main

import "testing"

func TestOperationsWorkerAlerts(t *testing.T) {
	for _, input := range []struct {
		kind, state, action, category, route string
	}{
		{"snapshot", "failed", "restore", "snapshot", "backups/system-snapshot"},
		{"system-update", "interrupted", "", "system-update", "system-updates"},
		{"panel-update", "failed", "", "panel-update", "settings/update"},
		{"malware-scan", "error", "", "malware-scan", "security"},
		{"malware-scan", "infected", "", "malware-detected", "security"},
		{"selinux-repair", "failed", "", "security", "security-dashboard"},
	} {
		t.Run(input.kind+"-"+input.state, func(t *testing.T) {
			alert, ok := operationsJobStatusAlert(input.kind, input.state, input.action)
			if !ok || alert.Category != input.category || alert.Route != input.route || alert.Title == "" || alert.Detail == "" {
				t.Fatalf("unexpected worker alert: %#v, found=%v", alert, ok)
			}
		})
	}
	for _, state := range []string{"idle", "queued", "running", "completed", "complete", ""} {
		if alert, ok := operationsJobStatusAlert("snapshot", state, ""); ok {
			t.Fatalf("non-failure %q produced alert %#v", state, alert)
		}
	}
}
