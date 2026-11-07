package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOperationsAlertsAcknowledgeResolveAndReopen(t *testing.T) {
	s := operationsState{Alerts: []operationsAlert{}}
	now := time.Now().UTC()
	v := operationsAlert{ID: "disk", Title: "Disk", Severity: "critical"}
	mergeOperationsAlerts(&s, []operationsAlert{v}, now)
	s.Alerts[0].Acknowledged = true
	s.Alerts[0].LastEmail = now
	mergeOperationsAlerts(&s, []operationsAlert{v}, now.Add(time.Minute))
	if len(s.Alerts) != 1 || !s.Alerts[0].Acknowledged {
		t.Fatal("Duplicate or acknowledgement lost")
	}
	mergeOperationsAlerts(&s, nil, now.Add(2*time.Minute))
	if s.Alerts[0].ResolvedAt == nil {
		t.Fatal("Resolved condition remains active")
	}
	mergeOperationsAlerts(&s, []operationsAlert{v}, now.Add(3*time.Minute))
	if s.Alerts[0].Acknowledged || s.Alerts[0].ResolvedAt != nil || !s.Alerts[0].LastEmail.IsZero() {
		t.Fatal("Reopened condition must notify again")
	}
}

func TestOperationsEmailFormatsSafeHeaders(t *testing.T) {
	settings := operationsSettings{RetentionDays: 14, EmailEnabled: true, EmailTo: "operator@example.test", EmailFrom: "alerts@example.test"}
	body, err := operationsEmailMessage(settings, []operationsAlert{{Title: "Disk usage", Severity: "critical", Detail: "Disk is 95% full."}})
	if err != nil || !strings.Contains(body, "To: operator@example.test\r\n") ||
		!strings.Contains(body, "\r\n\r\n[critical] Disk usage") {
		t.Fatalf("Unexpected alert message: %q (%v)", body, err)
	}
	settings.EmailFrom += "\r\nBcc: another@example.test"
	if _, err = operationsEmailMessage(settings, nil); err == nil {
		t.Fatal("Injected mail header accepted")
	}
}

func TestOperationsEmailSubmitsToLoopbackSMTP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	message := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		_, _ = fmt.Fprint(conn, "220 localhost test SMTP\r\n")
		reader := bufio.NewReader(conn)
		var body strings.Builder
		inData := false
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if inData && line != ".\r\n" {
				body.WriteString(line)
				continue
			}
			switch {
			case inData:
				inData = false
				message <- body.String()
				_, _ = fmt.Fprint(conn, "250 queued\r\n")
			case line == "DATA\r\n":
				inData = true
				_, _ = fmt.Fprint(conn, "354 end with dot\r\n")
			case line == "QUIT\r\n":
				_, _ = fmt.Fprint(conn, "221 goodbye\r\n")
				return
			default:
				_, _ = fmt.Fprint(conn, "250 ok\r\n")
			}
		}
	}()
	settings := operationsSettings{RetentionDays: 14, EmailEnabled: true, EmailTo: "operator@example.test", EmailFrom: "alerts@example.test"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = submitOperationsEmail(ctx, settings, []operationsAlert{{Title: "Backup verification", Detail: "Fixture alert", Severity: "warning"}}, listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case received := <-message:
		if !strings.Contains(received, "To: operator@example.test") || !strings.Contains(received, "Fixture alert") {
			t.Fatal("Message did not reach the SMTP sink intact")
		}
	case <-ctx.Done():
		t.Fatal("No alert accepted by the SMTP sink")
	}
}
func TestOperationsSettingsRejectHeaderInjection(t *testing.T) {
	for _, s := range []operationsSettings{{RetentionDays: 0}, {RetentionDays: 14, EmailEnabled: true}, {RetentionDays: 14, EmailTo: "admin@example.com\r\nBcc: attacker@example.com"}} {
		if validateOperationsSettings(s) == nil {
			t.Fatal("Unsafe settings accepted", s)
		}
	}
	a := apiTestApp(t)
	a.system.dataDir = t.TempDir()
	body := `{"action":"settings","settings":{"retentionDays":14,"retentionEnabled":true}}`
	w := httptest.NewRecorder()
	a.operationsAPI(w, httptest.NewRequest("POST", "/api/operations", strings.NewReader(body)))
	if w.Code != 200 {
		t.Fatal("Could not enable retention review", w.Code)
	}
	s, e := a.readOperations()
	if e != nil || !s.Settings.RetentionEnabled {
		t.Fatal("Retention review setting was not persisted", e)
	}
}
func TestOperationsRetentionPreservesLatestRemoteUnverifiedAndDisabled(t *testing.T) {
	now := time.Now()
	backups := []Backup{{ID: "old", SiteID: "a", Status: "complete", CreatedAt: now.Add(-40 * 24 * time.Hour)}, {ID: "latest", SiteID: "a", Status: "complete", CreatedAt: now.Add(-30 * 24 * time.Hour)}, {ID: "remote", SiteID: "a", Status: "complete", RemotePath: "sftp://backup/archive", CreatedAt: now.Add(-40 * 24 * time.Hour)}, {ID: "unchecked", SiteID: "a", Status: "complete", CreatedAt: now.Add(-40 * 24 * time.Hour)}}
	s := operationsState{Settings: operationsSettings{RetentionDays: 14}, Verification: map[string]backupVerification{}}
	for _, id := range []string{"old", "latest", "remote"} {
		s.Verification["history:"+id] = backupVerification{Status: "verified", CheckedAt: now}
	}
	if len(retentionCandidates(backups, s, now)) != 0 {
		t.Fatal("Default policy flags archives")
	}
	s.Settings.RetentionEnabled = true
	got := retentionCandidates(backups, s, now)
	if len(got) != 1 || got[0].ID != "old" {
		t.Fatalf("Unsafe retention selection: %+v", got)
	}
}
func TestOperationsArchiveVerificationRejectsCorruptionAndTraversal(t *testing.T) {
	makeZIP := func(name string) []byte {
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		f, e := z.CreateHeader(header)
		if e != nil {
			t.Fatal(e)
		}
		_, _ = f.Write([]byte("unique-archive-audit-marker"))
		_ = z.Close()
		return buf.Bytes()
	}
	root := t.TempDir()
	p := filepath.Join(root, "archive.zip")
	content := makeZIP("public_html/index.txt")
	if e := os.WriteFile(p, content, 0600); e != nil {
		t.Fatal(e)
	}
	item := operationsBackup{ID: "audit", Path: p, Size: int64(len(content))}
	v, e := verifyOperationsArchive(context.Background(), item)
	if e != nil || v.Status != "verified" || len(v.SHA256) != 64 {
		t.Fatal("Valid archive rejected", v, e)
	}
	corrupt := append([]byte{}, content...)
	i := bytes.Index(corrupt, []byte("unique-archive-audit-marker"))
	corrupt[i] = 'X'
	_ = os.WriteFile(p, corrupt, 0600)
	if _, e = verifyOperationsArchive(context.Background(), item); e == nil {
		t.Fatal("CRC corruption accepted")
	}
	bad := makeZIP("../escape.txt")
	_ = os.WriteFile(p, bad, 0600)
	item.Size = int64(len(bad))
	if _, e = verifyOperationsArchive(context.Background(), item); e == nil {
		t.Fatal("Traversal archive accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = verifyOperationsArchive(ctx, item); e == nil {
		t.Fatal("Cancellation ignored")
	}
}
func TestOperatorRolesDefaultDenyAndStateRedaction(t *testing.T) {
	for _, role := range []string{"viewer", "websites", "email", "backups", "unknown"} {
		for _, path := range []string{"/api/operators", "/api/operations", "/api/settings", "/api/terminal/session", "/api/access", "/api/website-backups/restore", "/api/system/reboot", "/api/phpmyadmin/"} {
			if operatorRouteAllowed(role, "POST", path) {
				t.Fatal("Privileged path allowed", role, path)
			}
		}
	}
	if !operatorRouteAllowed("websites", "PUT", "/api/sites/suspension") || operatorRouteAllowed("websites", "DELETE", "/api/sites/anything") {
		t.Fatal("Website route boundary failed")
	}
	s := PublicState{Settings: Settings{Email: "owner@example.com"}, Databases: []ManagedDatabase{{Name: "secret"}}, Activities: []Activity{{Detail: "sensitive"}}, Mailboxes: []Mailbox{{PasswordHash: "hash"}}, BackupDestinations: []BackupDestination{{PasswordCipher: "cipher"}}}
	redactOperatorState(&s, PanelOperator{Username: "viewer", Role: "viewer"})
	b, _ := json.Marshal(s)
	for _, secret := range []string{"owner@example.com", "sensitive", "cipher", "hash", "secret"} {
		if bytes.Contains(b, []byte(secret)) {
			t.Fatal("Operator state leaked", secret)
		}
	}
}
func TestOperatorDisableInvalidatesSessionsAndTOTPReplay(t *testing.T) {
	a := apiTestApp(t)
	a.system.dataDir = t.TempDir()
	password := "test-only-long-password"
	hash, e := hashPassword(password)
	if e != nil {
		t.Fatal(e)
	}
	secret := "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
	cipher, e := a.system.EncryptSecret(secret)
	if e != nil {
		t.Fatal(e)
	}
	op := PanelOperator{ID: "operator", Username: "viewer", Role: "viewer", Enabled: true, Revision: "r1", PasswordHash: hash, TOTPCipher: cipher, LastStep: -1}
	a.store.cfg.Operators = []PanelOperator{op}
	code, _ := totpCode(secret, time.Now().Unix()/30)
	if _, ok := a.authenticateOperator(op.Username, password, code); !ok {
		t.Fatal("Valid operator authentication rejected")
	}
	if _, ok := a.authenticateOperator(op.Username, password, code); ok {
		t.Fatal("Replayed operator TOTP accepted")
	}
	a.sessions["operator-session"] = session{Expires: time.Now().Add(time.Hour), OperatorID: op.ID, OperatorRevision: op.Revision}
	if !a.validSession("operator-session") {
		t.Fatal("Valid operator session rejected")
	}
	a.store.cfg.Operators[0].Enabled = false
	if a.validSession("operator-session") {
		t.Fatal("Disabled operator session remains valid")
	}
}
func TestOperatorCannotReadOwnerOperationsThroughAuthenticatedRoutes(t *testing.T) {
	a := apiTestApp(t)
	a.system.dataDir = t.TempDir()
	a.store.cfg.Operators = []PanelOperator{{ID: "viewer", Username: "viewer", Role: "viewer", Revision: "r", Enabled: true}}
	a.sessions["limited"] = session{Expires: time.Now().Add(time.Hour), OperatorID: "viewer", OperatorRevision: "r"}
	r := httptest.NewRequest("GET", "https://panel.example/api/operations", nil)
	r.AddCookie(&http.Cookie{Name: "litecp_session", Value: "limited"})
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("Limited role read owner settings", w.Code, w.Body.String())
	}
}
func TestOperationAuditNeverRecordsBodyOrQuerySecrets(t *testing.T) {
	a := apiTestApp(t)
	a.system.dataDir = t.TempDir()
	r := httptest.NewRequest("POST", "https://panel.example/api/example?token=private-secret", strings.NewReader(`{"password":"private-secret"}`))
	w := httptest.NewRecorder()
	a.auditOperation(w, r, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(409) })
	items := a.readOperationAudit("example")
	if len(items) != 1 || items[0].Status != 409 {
		t.Fatal("Missing outcome", items)
	}
	b, _ := os.ReadFile(filepath.Join(a.system.dataDir, "operation-audit.jsonl"))
	if bytes.Contains(b, []byte("private-secret")) {
		t.Fatal("Audit leaked credential")
	}
}
