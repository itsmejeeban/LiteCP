package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAlertRecipientsAndRules(t *testing.T) {
	s := operationsSettings{RetentionDays: 14, EmailEnabled: true, EmailFrom: "alerts@example.test", EmailRecipients: []string{"one@example.test", "two@example.test", "ONE@example.test"}, AlertRules: map[string]bool{"backup": false}}
	if err := validateOperationsSettings(s); err != nil {
		t.Fatal(err)
	}
	if got := s.alertRecipients(); len(got) != 2 {
		t.Fatalf("Expected deduplicated recipients, got %v", got)
	}
	for _, recipients := range [][]string{{""}, {"valid@example.test", "bad\r\nBcc: other@example.test"}, {"Operator <valid@example.test>"}, make([]string, 21)} {
		s.EmailRecipients = recipients
		if validateOperationsSettings(s) == nil {
			t.Fatal("Unsafe recipient list accepted")
		}
	}
	s.EmailRecipients = []string{"one@example.test", "two@example.test"}
	now := time.Now().UTC()
	state := operationsState{Settings: s, Alerts: []operationsAlert{
		{ID: "backup-a", Category: "backup"},
		{ID: "resource-a", Category: "resource-limit"},
		{ID: "ssl-a", Category: "ssl", LastEmail: now.Add(-23 * time.Hour)},
		{ID: "failed", Category: "ssl", LastEmail: now.Add(-6 * time.Minute), EmailError: "SMTP failed"},
		{ID: "ack", Acknowledged: true},
	}}
	items := operationsEmailCandidates(state, now)
	if len(items) != 2 || items[0].ID != "resource-a" || items[1].ID != "failed" {
		t.Fatalf("Rule filtering or retry cooldown failed: %+v", items)
	}
	body, err := operationsEmailMessage(s, items)
	if err != nil || !strings.Contains(body, "To: undisclosed-recipients:;") || strings.Contains(body, "one@example.test") || strings.Contains(body, "two@example.test") {
		t.Fatal("Recipient addresses should be private in multi-recipient messages", err)
	}
	s.AlertRules["unknown"] = true
	if validateOperationsSettings(s) == nil {
		t.Fatal("Unknown rule accepted")
	}
}

func TestAlertSettingsPreserveBackupPolicyAndLegacyRecipient(t *testing.T) {
	a := apiTestApp(t)
	a.system.dataDir = t.TempDir()
	initial := operationsState{Settings: operationsSettings{RetentionDays: 37, VerifyBackups: true, EmailTo: "old@example.test"}, Verification: map[string]backupVerification{}}
	if err := a.saveOperations(initial); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.operationsAPI(w, httptest.NewRequest("GET", "/api/operations?alerts=1", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"emailRecipients":["old@example.test"]`) {
		t.Fatal("Legacy recipient migration failed", w.Body.String())
	}
	post := func(body string) int {
		t.Helper()
		w := httptest.NewRecorder()
		a.operationsAPI(w, httptest.NewRequest("POST", "/api/operations", strings.NewReader(body)))
		return w.Code
	}
	if code := post(`{"action":"alert-settings","settings":{"emailEnabled":true,"emailFrom":"alerts@example.test","emailRecipients":["one@example.test","two@example.test"],"alertRules":{"backup":false}}}`); code != 200 {
		t.Fatal("Alert settings save failed", code)
	}
	s, _ := a.readOperations()
	if s.Settings.RetentionDays != 37 || !s.Settings.VerifyBackups || s.Settings.AlertRules["backup"] || len(s.Settings.alertRecipients()) != 2 {
		t.Fatal("Saving alert settings changed backup policy")
	}
	if code := post(`{"action":"backup-settings","settings":{"retentionDays":40}}`); code != 200 {
		t.Fatal("Backup policy save failed", code)
	}
	s, _ = a.readOperations()
	if !s.Settings.EmailEnabled || len(s.Settings.alertRecipients()) != 2 || s.Settings.AlertRules["backup"] {
		t.Fatal("Saving backup policy changed alerts")
	}
	if code := post(`{"action":"alert-settings","settings":{"emailEnabled":true,"emailRecipients":["invalid"]}}`); code != 400 {
		t.Fatal("Invalid alert settings accepted", code)
	}
	s, _ = a.readOperations()
	if !s.Settings.EmailEnabled || len(s.Settings.alertRecipients()) != 2 {
		t.Fatal("Failed save damaged previous settings")
	}
}

func TestOperationFailureAlertsPersistWithoutPrivateContent(t *testing.T) {
	a := apiTestApp(t)
	a.system.dataDir = t.TempDir()
	r := httptest.NewRequest("POST", "/api/website-ftp?password=private", strings.NewReader("secret"))
	for i := 0; i < 2; i++ {
		a.auditOperation(httptest.NewRecorder(), r, func(w http.ResponseWriter, r *http.Request) {
			writeError(w, 502, "secret raw command output")
		})
	}
	s, err := a.readOperations()
	if err != nil || len(s.Alerts) != 1 || s.Alerts[0].Category != "ftp" || strings.Contains(s.Alerts[0].Detail, "secret") {
		t.Fatal("Failure event must be deduplicated and sanitized", err)
	}
	mergeOperationsAlerts(&s, nil, time.Now().UTC().Add(time.Minute))
	if s.Alerts[0].ResolvedAt != nil {
		t.Fatal("A short-lived failure disappeared before notification")
	}
	a.alertFailedOperation(r, 403)
	s, _ = a.readOperations()
	if len(s.Alerts) != 1 {
		t.Fatal("Authorization refusal should not generate an operational alert")
	}
}
