package main

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Operations data contains no passwords, request bodies, tokens or mail content.
type operationsSettings struct {
	EmailEnabled     bool   `json:"emailEnabled"`
	EmailTo          string `json:"emailTo"`
	EmailFrom        string `json:"emailFrom"`
	VerifyBackups    bool   `json:"verifyBackups"`
	RetentionEnabled bool   `json:"retentionEnabled"`
	RetentionDays    int    `json:"retentionDays"`
}
type operationsAlert struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Detail       string     `json:"detail"`
	Severity     string     `json:"severity"`
	Route        string     `json:"route"`
	FirstSeen    time.Time  `json:"firstSeen"`
	LastSeen     time.Time  `json:"lastSeen"`
	ResolvedAt   *time.Time `json:"resolvedAt,omitempty"`
	Acknowledged bool       `json:"acknowledged"`
	LastEmail    time.Time  `json:"lastEmail,omitempty"`
	EmailError   string     `json:"emailError,omitempty"`
}
type operationsState struct {
	Settings     operationsSettings            `json:"settings"`
	Alerts       []operationsAlert             `json:"alerts"`
	Verification map[string]backupVerification `json:"verification"`
	CheckedAt    time.Time                     `json:"checkedAt"`
	LastError    string                        `json:"lastError,omitempty"`
	EmailWindow  time.Time                     `json:"emailWindow"`
	EmailCount   int                           `json:"emailCount"`
}

var operationsMu sync.Mutex
var operationsWorker sync.Mutex

func (a *App) readOperations() (operationsState, error) {
	s := operationsState{Settings: operationsSettings{RetentionDays: 14}, Alerts: []operationsAlert{}, Verification: map[string]backupVerification{}}
	data, err := os.ReadFile(filepath.Join(a.system.dataDir, "operations.json"))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	if s.Verification == nil {
		s.Verification = map[string]backupVerification{}
	}
	if s.Alerts == nil {
		s.Alerts = []operationsAlert{}
	}
	for id, verification := range s.Verification {
		if verification.Status == "running" && id != operationsVerifyID {
			verification.Status = "interrupted"
			verification.Message = "The verifier stopped before recording a result. Verify the archive again."
			s.Verification[id] = verification
		}
	}
	return s, nil
}
func (a *App) saveOperations(s operationsState) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(a.system.dataDir, "operations.json"), b, 0600)
}
func validateOperationsSettings(s operationsSettings) error {
	if s.RetentionDays < 1 || s.RetentionDays > 3650 {
		return fmt.Errorf("Retention must be between 1 and 3650 days")
	}
	for _, value := range []string{s.EmailTo, s.EmailFrom} {
		if value != "" {
			p, e := mail.ParseAddress(value)
			if e != nil || p.Address != value || strings.ContainsAny(value, "\r\n") {
				return fmt.Errorf("Enter one plain email address for sender and recipient")
			}
		}
	}
	if s.EmailEnabled && (s.EmailTo == "" || s.EmailFrom == "") {
		return fmt.Errorf("Email alerts require a sender and recipient")
	}
	return nil
}
func mergeOperationsAlerts(s *operationsState, observations []operationsAlert, now time.Time) {
	seen := map[string]bool{}
	for _, v := range observations {
		seen[v.ID] = true
		found := false
		for i := range s.Alerts {
			old := &s.Alerts[i]
			if old.ID != v.ID {
				continue
			}
			found = true
			if old.ResolvedAt != nil {
				old.FirstSeen = now
				old.Acknowledged = false
				old.LastEmail = time.Time{}
			}
			old.Title = v.Title
			old.Detail = v.Detail
			old.Route = v.Route
			old.Severity = v.Severity
			old.LastSeen = now
			old.ResolvedAt = nil
			break
		}
		if !found && len(s.Alerts) < 10000 {
			v.FirstSeen = now
			v.LastSeen = now
			s.Alerts = append(s.Alerts, v)
		}
	}
	for i := range s.Alerts {
		v := &s.Alerts[i]
		if !seen[v.ID] && v.ResolvedAt == nil {
			at := now
			v.ResolvedAt = &at
		}
	}
	s.CheckedAt = now
}
func (a *App) collectOperationsAlerts(ctx context.Context) []operationsAlert {
	items := []operationsAlert{}
	add := func(id, title, detail, severity, route string) {
		items = append(items, operationsAlert{ID: id, Title: title, Detail: detail, Severity: severity, Route: route})
	}
	if !a.system.demo {
		usageMonitor.RLock()
		sample := usageMonitor.latest
		usageMonitor.RUnlock()
		if !sample.Time.IsZero() && time.Since(sample.Time) < time.Minute {
			if sample.Memory >= 90 {
				add("memory", "High server memory usage", fmt.Sprintf("RAM usage is %.1f%%. Review active processes and website limits.", sample.Memory), "warning", "live-process")
			}
			if sample.CPU >= 95 {
				add("cpu", "High server CPU usage", fmt.Sprintf("CPU usage is %.1f%%. Review workloads before increasing capacity.", sample.CPU), "warning", "live-process")
			}
		}
		disk := diskUsage(ctx)
		if disk.Total > 0 && disk.Percent >= 90 {
			add("disk", "Low server disk space", fmt.Sprintf("Disk is %.1f%% full. Review storage before starting large jobs.", disk.Percent), "critical", "disk-usage")
		}
		for _, unit := range []string{"nginx", "litecp-broker", a.system.databaseUnit(), "postfix", "dovecot"} {
			if (unit == "postfix" || unit == "dovecot") && !a.store.Settings().EmailHostingEnabled {
				continue
			}
			out, err := runCommand(ctx, 2*time.Second, "systemctl", "is-active", unit)
			if err != nil || strings.TrimSpace(out) != "active" {
				add("service-"+unit, "Service needs attention", unit+" is not confirmed active.", "critical", "services")
			}
		}
		for _, site := range a.store.Sites() {
			if !site.SSL || !domainPattern.MatchString(site.Domain) {
				continue
			}
			certificate, err := operationsCertificate(filepath.Join("/etc/letsencrypt/live", site.Domain, "fullchain.pem"))
			if err != nil || !certificateMatchesSite(certificate, site.Domain, time.Now()) {
				add("ssl-"+site.ID, "Website certificate needs attention", site.Domain+" does not have a currently valid matching managed certificate.", "critical", "website-ssl")
			} else if time.Until(certificate.NotAfter) < 14*24*time.Hour {
				add("ssl-"+site.ID, "Website certificate expires soon", site.Domain+" expires "+certificate.NotAfter.UTC().Format("2006-01-02")+".", "warning", "website-ssl")
			}
		}
	}
	for _, b := range a.store.PublicState().Backups {
		if strings.Contains(b.Status, "failed") || b.Status == "interrupted" {
			add("backup-"+b.ID, "Backup needs attention", b.Domain+": "+b.Status, "warning", "backups/history")
		}
	}
	entries, _ := os.ReadDir(filepath.Join(a.system.dataDir, "live-migrations"))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		j, err := a.loadLiveJob(e.Name())
		if err != nil {
			continue
		}
		if j.Status == "failed" || j.Status == "restore-review" || j.Status == "restored-review" || j.Status == "review" {
			add("migration-"+j.ID, "Migration needs review", "Open the migration details to resolve the pending action.", "warning", "migrations/pending")
		}
	}
	for _, site := range a.store.Sites() {
		if site.ResourceLimits != nil {
			plan := resourceRuntimeRead(site.ID)
			if plan.State == "Resource blocked" {
				add("resource-"+site.ID, "Website resource limit reached", site.Domain+" is resource blocked.", "warning", "website-resource-limits")
			}
		}
	}
	return items
}
func sendOperationsEmail(ctx context.Context, settings operationsSettings, alerts []operationsAlert) error {
	if len(alerts) == 0 {
		return nil
	}
	return submitOperationsEmail(ctx, settings, alerts, "127.0.0.1:25")
}

func operationsEmailMessage(settings operationsSettings, alerts []operationsAlert) (string, error) {
	if err := validateOperationsSettings(settings); err != nil {
		return "", err
	}
	if !settings.EmailEnabled {
		return "", fmt.Errorf("Email alerts are disabled")
	}
	var body strings.Builder
	fmt.Fprintf(&body, "From: %s\r\nTo: %s\r\nSubject: LiteCP administrator alerts (%d)\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n", settings.EmailFrom, settings.EmailTo, len(alerts))
	for _, v := range alerts {
		fmt.Fprintf(&body, "[%s] %s\n%s\nPanel section: %s\n\n", v.Severity, v.Title, v.Detail, v.Route)
	}
	return body.String(), nil
}

func submitOperationsEmail(ctx context.Context, settings operationsSettings, alerts []operationsAlert, address string) error {
	message, err := operationsEmailMessage(settings, alerts)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("Local SMTP is unavailable. Check the mail service and port 25.")
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	client, err := smtp.NewClient(conn, "localhost")
	failed := fmt.Errorf("Local SMTP submission failed. Check Email Delivery.")
	if err != nil {
		return failed
	}
	defer client.Close()
	if client.Mail(settings.EmailFrom) != nil || client.Rcpt(settings.EmailTo) != nil {
		return failed
	}
	body, err := client.Data()
	if err != nil {
		return failed
	}
	if _, err = body.Write([]byte(message)); err != nil {
		return failed
	}
	if body.Close() != nil {
		return failed
	}
	_ = client.Quit()
	return nil
}
func (a *App) operationsCycle(ctx context.Context) error {
	if !operationsWorker.TryLock() {
		return nil
	}
	defer operationsWorker.Unlock()
	observed := a.collectOperationsAlerts(ctx)
	a.recordOperationsCapacity(ctx)
	operationsMu.Lock()
	s, err := a.readOperations()
	if err != nil {
		operationsMu.Unlock()
		return err
	}
	for id, v := range s.Verification {
		if v.Status == "failed" || v.Status == "changed" {
			observed = append(observed, operationsAlert{ID: "verification-" + id, Title: "Archive verification needs review", Detail: "A retained backup failed integrity verification. Review Operations before restoring.", Severity: "critical", Route: "operations"})
		}
	}
	s.LastError = ""
	mergeOperationsAlerts(&s, observed, time.Now().UTC())
	if time.Since(s.EmailWindow) >= time.Hour {
		s.EmailWindow = time.Now().UTC()
		s.EmailCount = 0
	}
	mailItems := []operationsAlert{}
	if s.Settings.EmailEnabled && !a.system.demo && s.EmailCount < 20 {
		for i := range s.Alerts {
			v := &s.Alerts[i]
			if v.ResolvedAt == nil && !v.Acknowledged && time.Since(v.LastEmail) >= 24*time.Hour && len(mailItems) < 20 {
				mailItems = append(mailItems, *v)
				v.LastEmail = time.Now().UTC()
			}
		}
	}
	if len(mailItems) > 0 {
		s.EmailCount++
	}
	err = a.saveOperations(s)
	operationsMu.Unlock()
	if err != nil {
		return err
	}
	if len(mailItems) > 0 {
		mailErr := sendOperationsEmail(ctx, s.Settings, mailItems)
		operationsMu.Lock()
		next, e := a.readOperations()
		if e == nil {
			for i := range next.Alerts {
				for _, sent := range mailItems {
					if next.Alerts[i].ID == sent.ID {
						next.Alerts[i].EmailError = ""
						if mailErr != nil {
							next.Alerts[i].EmailError = mailErr.Error()
						}
					}
				}
			}
			e = a.saveOperations(next)
		}
		operationsMu.Unlock()
		if e != nil {
			return e
		}
	}
	if s.Settings.VerifyBackups {
		a.verifyNextOperationsBackup()
	}
	return nil
}
func (a *App) startOperationsMonitor() {
	go func() {
		time.Sleep(15 * time.Second)
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			err := a.operationsCycle(ctx)
			cancel()
			if err != nil {
				operationsMu.Lock()
				s, e := a.readOperations()
				if e == nil {
					s.LastError = "Background operations check failed; review service logs."
					_ = a.saveOperations(s)
				}
				operationsMu.Unlock()
			}
			time.Sleep(time.Minute)
		}
	}()
}

type readinessCheck struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Route  string `json:"route"`
}

func (a *App) operationsReadiness(r *http.Request, s operationsState) []readinessCheck {
	checks := []readinessCheck{}
	add := func(id, label, status, detail, route string) {
		checks = append(checks, readinessCheck{id, label, status, detail, route})
	}
	status := "review"
	if a.store.twoFactor().Enabled {
		status = "pass"
	}
	add("mfa", "Administrator 2FA", status, "Authenticator enrollment status; keep recovery codes offline.", "two-factor")
	status = "review"
	if _, ok := r.Context().Value(brokerPeerUIDKey{}).(uint32); ok {
		status = "pass"
	}
	add("split", "Privilege separation", status, "Web-to-broker boundary observed for this request.", "security-dashboard")
	if a.system.demo {
		add("live", "Server checks", "unknown", "Unavailable in demo mode.", "security-dashboard")
	} else {
		out, e := runCommand(r.Context(), 2*time.Second, "getenforce")
		status = "review"
		if e == nil && strings.TrimSpace(out) == "Enforcing" {
			status = "pass"
		}
		add("selinux", "SELinux", status, "Live enforcement status.", "security-dashboard")
	}
	if sites := a.store.Sites(); len(sites) > 0 && !a.system.demo {
		cap := inspectResourceCapabilities(sites[0])
		status = "review"
		detail := strings.Join(cap.Prerequisites, "; ")
		if cap.Ready {
			status = "pass"
			detail = "Kernel controllers and website-storage project quotas detected."
		}
		add("quotas", "Resource isolation prerequisites", status, detail, "website-resource-limits")
	}
	status = "unknown"
	if !a.system.demo {
		status = "review"
		if checkRelease(version).Checked {
			status = "pass"
		}
	}
	add("updates", "Release access", status, "Authenticated or public stable release access; a full upgrade remains a separate acceptance test.", "settings/update")
	status = "review"
	for _, v := range s.Verification {
		if v.Status == "verified" && time.Since(v.CheckedAt) < 7*24*time.Hour {
			status = "pass"
			break
		}
	}
	add("backups", "Archive verification", status, "A recent integrity check verifies stored bytes, not disaster-recovery success.", "operations")
	status = "review"
	if s.Settings.EmailEnabled {
		status = "configured"
	}
	add("alerts", "Email alerts", status, "Local queue acceptance does not prove inbox delivery.", "operations")
	tlsStatus, tlsDetail := a.operationsPanelTLS(r.Host)
	add("tls", "Trusted panel TLS", tlsStatus, tlsDetail, "settings")
	add("mail-dns", "Mail authentication DNS", a.operationsMailDNS(r.Context()), "Checks a configured domain's expected SPF, DKIM and DMARC records. Use Mail Domains for all-domain verification.", "email/domains")
	add("recovery", "Independent recovery", "review", "Verify provider console and a restore on a separate server; do not infer this from service health.", "backups/system-snapshot")
	return checks
}

func (a *App) operationsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		operationsMu.Lock()
		s, e := a.readOperations()
		operationsMu.Unlock()
		if e != nil {
			writeError(w, 500, "Cannot read operations settings; existing data retained.")
			return
		}
		sort.Slice(s.Alerts, func(i, j int) bool { return s.Alerts[i].LastSeen.After(s.Alerts[j].LastSeen) })
		checks := []readinessCheck{}
		if r.URL.Query().Get("checks") == "1" {
			checks = a.operationsReadiness(r, s)
		}
		site := r.URL.Query().Get("site")
		writeJSON(w, 200, map[string]any{"state": s, "checks": checks, "backups": a.operationsBackupInventory(), "retentionReview": a.operationsRetentionReview(s), "audit": a.readOperationAudit(r.URL.Query().Get("search")), "trends": a.operationsTrends(), "siteUsage": a.readOperationsSiteUsage(), "siteHistory": a.operationsSiteHistory(site)})
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "Use GET or POST.")
		return
	}
	var input struct {
		Action   string             `json:"action"`
		ID       string             `json:"id"`
		Settings operationsSettings `json:"settings"`
	}
	if decodeJSON(r, &input) != nil {
		writeError(w, 400, "Invalid operations request")
		return
	}
	switch input.Action {
	case "settings":
		if e := validateOperationsSettings(input.Settings); e != nil {
			writeError(w, 400, e.Error())
			return
		}
		operationsMu.Lock()
		s, e := a.readOperations()
		if e == nil {
			s.Settings = input.Settings
			e = a.saveOperations(s)
		}
		operationsMu.Unlock()
		if e != nil {
			writeError(w, 400, e.Error())
			return
		}
		_ = a.store.AddActivity("Operations settings saved", "Alert, archive verification and retention policy updated.", "info")
	case "acknowledge":
		operationsMu.Lock()
		s, e := a.readOperations()
		found := false
		if e == nil {
			for i := range s.Alerts {
				if s.Alerts[i].ID == input.ID {
					s.Alerts[i].Acknowledged = true
					found = true
				}
			}
			if !found {
				e = fmt.Errorf("Alert not found")
			}
			if e == nil {
				e = a.saveOperations(s)
			}
		}
		operationsMu.Unlock()
		if e != nil {
			writeError(w, 400, e.Error())
			return
		}
	case "check":
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			_ = a.operationsCycle(ctx)
		}()
	case "verify":
		if e := a.startOperationsVerification(input.ID); e != nil {
			writeError(w, 409, e.Error())
			return
		}
	case "cancel-verification":
		operationsMu.Lock()
		if operationsVerifyID != input.ID || operationsVerifyCancel == nil {
			operationsMu.Unlock()
			writeError(w, 409, "This archive verification is no longer running")
			return
		}
		operationsVerifyCancel()
		operationsMu.Unlock()
	case "test-email":
		if a.system.demo {
			writeError(w, 409, "Live mail delivery is unavailable in demo mode")
			return
		}
		settings := operationsSettings{
			RetentionDays: 14,
			EmailEnabled:  true,
			EmailTo:       input.Settings.EmailTo,
			EmailFrom:     input.Settings.EmailFrom,
		}
		if settings.EmailTo == "" && settings.EmailFrom == "" {
			operationsMu.Lock()
			s, err := a.readOperations()
			operationsMu.Unlock()
			if err != nil {
				writeError(w, 500, "Could not read email alert settings")
				return
			}
			settings.EmailTo, settings.EmailFrom = s.Settings.EmailTo, s.Settings.EmailFrom
		}
		if err := validateOperationsSettings(settings); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if !a.apiRateAllowed("operations-test-email", 2) {
			writeError(w, 429, "Wait before sending another test alert")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := sendOperationsEmail(ctx, settings, []operationsAlert{{Title: "Notification test", Detail: "This test confirms local mail submission only. Check recipient delivery.", Severity: "info", Route: "operations"}}); err != nil {
			writeError(w, 502, err.Error())
			return
		}
	default:
		writeError(w, 400, "Unknown operations action")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func operationsCertificate(path string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("certificate PEM is unavailable")
	}
	return x509.ParseCertificate(block.Bytes)
}

func (a *App) operationsPanelTLS(host string) (string, string) {
	if a.system.demo {
		return "unknown", "Live certificate inspection is unavailable in demo mode."
	}
	if value, _, err := net.SplitHostPort(host); err == nil {
		host = value
	}
	for _, config := range []string{"/etc/nginx/conf.d/litecp-panel.conf", "/etc/nginx/sites-available/litecp-panel.conf"} {
		data, err := os.ReadFile(config)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) != 2 || fields[0] != "ssl_certificate" {
				continue
			}
			bundle, err := os.ReadFile(strings.TrimSuffix(fields[1], ";"))
			if err != nil {
				continue
			}
			first, rest := pem.Decode(bundle)
			if first == nil {
				continue
			}
			cert, err := x509.ParseCertificate(first.Bytes)
			if err != nil {
				continue
			}
			intermediates := x509.NewCertPool()
			intermediates.AppendCertsFromPEM(rest)
			if _, err = cert.Verify(x509.VerifyOptions{DNSName: host, Intermediates: intermediates}); err != nil {
				return "review", "Configured certificate trust or hostname verification failed for this panel address."
			}
			expiry := cert.NotAfter.UTC().Format("2006-01-02")
			if time.Until(cert.NotAfter) < 14*24*time.Hour {
				return "review", "The trusted configured panel certificate expires " + expiry + "."
			}
			return "pass", "Configured certificate matches this panel address and the server trust store. Expires " + expiry + "."
		}
	}
	return "unknown", "Unable to locate the configured panel certificate."
}

func (a *App) operationsMailDNS(parent context.Context) string {
	if a.system.demo || !a.store.Settings().EmailHostingEnabled {
		return "unknown"
	}
	domains, _, _ := a.store.MailConfig()
	if len(domains) == 0 {
		return "unknown"
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	host := a.system.mailHostname(ctx)
	if !domainPattern.MatchString(host) {
		return "review"
	}
	records, dkim := a.mailDNSRecords(host, domains[0].Domain)
	if dkim == "" {
		return "review"
	}
	for _, check := range verifyMailDNS(ctx, records[2:], net.DefaultResolver) {
		if check.Status != "matching" {
			return "review"
		}
	}
	return "pass"
}
