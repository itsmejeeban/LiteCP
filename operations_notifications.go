package main

import (
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

type alertCategory struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

var operationsAlertCategories = []alertCategory{
	{"backup", "Backup failures", "Scheduled or manual backup creation and remote upload failures."},
	{"restore", "Restore failures", "Website and folder imports that fail or require review."},
	{"backup-verification", "Backup integrity", "Retained backups that fail integrity verification."},
	{"snapshot", "System snapshot failures", "Snapshot creation, upload or restoration failures."},
	{"migration", "Migration failures", "Failed, interrupted or incomplete server migrations."},
	{"resource-limit", "Website resource limits", "CPU, memory, disk or process limits reached by a website."},
	{"resource-enforcement", "Resource enforcement failures", "Website resource isolation or recovery needs attention."},
	{"server-cpu", "High server CPU", "Server CPU usage reaches 95%."},
	{"server-memory", "High server memory", "Server RAM usage reaches 90%."},
	{"server-disk", "Low server disk space", "Server disk usage reaches 90%."},
	{"service-health", "Service failures", "An expected hosting service is unavailable or an action fails."},
	{"ssl", "SSL certificates", "Certificate installation failures, invalid certificates and expiry within 14 days."},
	{"panel-update", "LiteCP update failures", "Panel update or rollback needs attention."},
	{"system-update", "System update failures", "Operating system updates fail."},
	{"mail", "Mail configuration failures", "Mailbox, delivery, webmail or mail configuration actions fail."},
	{"dns", "DNS failures", "DNS zone, resolver or DNS service configuration fails."},
	{"ftp", "FTP account failures", "FTP account or configuration actions fail."},
	{"cron", "Cron management failures", "Website cron configuration, scheduling or a manual test fails."},
	{"application", "Application runtime failures", "PHP, Node.js, Python, Ruby or Redis management fails."},
	{"database", "Database failures", "Database creation, import, credential or management actions fail."},
	{"files", "File management failures", "Website or server file management actions fail."},
	{"security", "Security configuration failures", "Firewall, WAF, CAPTCHA, SELinux or protection configuration fails."},
	{"malware-scan", "Malware scan failures", "A website or server malware scan fails."},
	{"malware-detected", "Malware detections", "A scan reports potentially infected files."},
	{"file-integrity", "File integrity", "A monitored baseline changes or an integrity check fails."},
	{"panel-operation", "Other panel failures", "Other authenticated administrative operations fail on the server."},
}

func knownAlertCategory(id string) bool {
	for _, c := range operationsAlertCategories {
		if c.ID == id {
			return true
		}
	}
	return false
}

func legacyOperationsCategory(id string) string {
	switch {
	case id == "cpu":
		return "server-cpu"
	case id == "memory":
		return "server-memory"
	case id == "disk":
		return "server-disk"
	case strings.HasPrefix(id, "service-"):
		return "service-health"
	case strings.HasPrefix(id, "ssl-"):
		return "ssl"
	case strings.HasPrefix(id, "backup-"):
		return "backup"
	case strings.HasPrefix(id, "verification-"):
		return "backup-verification"
	case strings.HasPrefix(id, "migration-"):
		return "migration"
	case strings.HasPrefix(id, "resource-"):
		return "resource-limit"
	default:
		return "panel-operation"
	}
}

func (s operationsSettings) alertRecipients() []string {
	values := s.EmailRecipients
	if values == nil && s.EmailTo != "" {
		values = []string{s.EmailTo}
	}
	result := []string{}
	seen := map[string]bool{}
	for _, address := range values {
		address = strings.TrimSpace(address)
		key := strings.ToLower(address)
		if !seen[key] {
			seen[key] = true
			result = append(result, address)
		}
	}
	return result
}

func validateAlertDelivery(s operationsSettings) error {
	// Validate every supplied value before normalization so malformed legacy
	// values cannot be hidden by an overriding recipient list.
	addresses := append([]string{}, s.EmailRecipients...)
	if s.EmailTo != "" {
		addresses = append(addresses, s.EmailTo)
	}
	if s.EmailFrom != "" {
		addresses = append(addresses, s.EmailFrom)
	}
	if len(s.EmailRecipients) > 20 {
		return fmt.Errorf("Add at most 20 email recipients")
	}
	for _, address := range addresses {
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address || strings.ContainsAny(address, "\r\n") {
			return fmt.Errorf("Enter plain email addresses without display names or line breaks")
		}
	}
	if s.EmailEnabled && (len(s.alertRecipients()) == 0 || s.EmailFrom == "") {
		return fmt.Errorf("Email alerts require a sender and at least one recipient")
	}
	for category := range s.AlertRules {
		if !knownAlertCategory(category) {
			return fmt.Errorf("Unknown alert category %q", category)
		}
	}
	return nil
}

func (s operationsSettings) alertEmailEnabled(v operationsAlert) bool {
	if !s.EmailEnabled {
		return false
	}
	category := v.Category
	if category == "" {
		category = legacyOperationsCategory(v.ID)
	}
	enabled, configured := s.AlertRules[category]
	return !configured || enabled
}

func operationsEmailCandidates(s operationsState, now time.Time) []operationsAlert {
	items := []operationsAlert{}
	for _, v := range s.Alerts {
		cooldown := 24 * time.Hour
		if v.EmailError != "" {
			cooldown = 5 * time.Minute
		}
		if v.ResolvedAt == nil && !v.Acknowledged && s.Settings.alertEmailEnabled(v) &&
			(v.LastEmail.IsZero() || now.Sub(v.LastEmail) >= cooldown) {
			items = append(items, v)
			if len(items) == 20 {
				break
			}
		}
	}
	return items
}

// Persist failures immediately so short-lived jobs are still notified by the
// browser-independent monitor after a panel restart. Keys identify a job or
// incident. Callers provide fixed summaries, never raw command output.
func (a *App) recordOperationsEvent(category, key, title, detail, severity, route string) {
	if a.system == nil || a.system.dataDir == "" || !knownAlertCategory(category) {
		return
	}
	sum := sha256.Sum256([]byte(category + ":" + key))
	id := fmt.Sprintf("event-%x", sum[:16])
	now := time.Now().UTC()
	operationsMu.Lock()
	defer operationsMu.Unlock()
	s, err := a.readOperations()
	if err != nil {
		log.Print("LiteCP could not record an administrator alert")
		return
	}
	for i := range s.Alerts {
		if s.Alerts[i].ID == id {
			if s.Alerts[i].ResolvedAt != nil {
				s.Alerts[i].FirstSeen = now
				s.Alerts[i].Acknowledged = false
				s.Alerts[i].LastEmail = time.Time{}
				s.Alerts[i].EmailError = ""
			}
			s.Alerts[i].LastSeen = now
			s.Alerts[i].ResolvedAt = nil
			if err := a.saveOperations(s); err != nil {
				log.Print("LiteCP could not persist an administrator alert")
			}
			return
		}
	}
	if len(s.Alerts) >= 10000 {
		log.Print("LiteCP administrator alert history is full")
		return
	}
	s.Alerts = append(s.Alerts, operationsAlert{
		ID: id, Category: category, Event: true, Title: title, Detail: detail,
		Severity: severity, Route: route, FirstSeen: now, LastSeen: now,
	})
	if err := a.saveOperations(s); err != nil {
		log.Print("LiteCP could not persist an administrator alert")
	}
}

func operationsFailureCategory(path string) (string, string) {
	switch {
	case strings.Contains(path, "snapshot"):
		return "snapshot", "backups/system-snapshot"
	case strings.HasPrefix(path, "/api/system/hostname/ssl"):
		return "ssl", "settings/hostname"
	case strings.Contains(path, "ssl"):
		return "ssl", "website-ssl"
	case strings.HasPrefix(path, "/api/website-backups/restore"), strings.HasPrefix(path, "/api/backups/restore"):
		return "restore", "backups/restore"
	case strings.HasPrefix(path, "/api/backup-destinations"), strings.HasPrefix(path, "/api/backup-schedules"):
		return "backup", "backups/settings"
	case strings.HasPrefix(path, "/api/website-backups"), strings.HasPrefix(path, "/api/backups"):
		return "backup", "backups/history"
	case strings.HasPrefix(path, "/api/live-migrations"):
		return "migration", "migrations/pending"
	case strings.HasPrefix(path, "/api/migration"):
		return "restore", "migrations/pending"
	case strings.Contains(path, "resource"):
		return "resource-enforcement", "website-resource-limits"
	case strings.HasPrefix(path, "/api/system-update"):
		return "system-update", "system-updates"
	case strings.HasPrefix(path, "/api/update"):
		return "panel-update", "settings/update"
	case strings.Contains(path, "integrity"):
		return "file-integrity", "file-integrity"
	case strings.Contains(path, "scan") || strings.Contains(path, "malware"):
		return "malware-scan", "security"
	case strings.Contains(path, "database"):
		return "database", "databases"
	case strings.Contains(path, "file"):
		return "files", "file-manager"
	case strings.Contains(path, "dns") || strings.Contains(path, "resolvers"):
		return "dns", "website-dns"
	case strings.Contains(path, "ftp"):
		return "ftp", "website-ftp"
	case strings.Contains(path, "cron"):
		return "cron", "website-cron"
	case strings.Contains(path, "mail"):
		return "mail", "email/delivery"
	case strings.HasPrefix(path, "/api/services"):
		return "service-health", "services"
	case strings.Contains(path, "waf"), strings.Contains(path, "firewall"), strings.Contains(path, "security"), strings.Contains(path, "captcha"), strings.Contains(path, "brute-force"):
		return "security", "security-dashboard"
	case strings.Contains(path, "application"), strings.Contains(path, "php"), strings.Contains(path, "redis"):
		return "application", "applications"
	default:
		return "panel-operation", "operations"
	}
}

func (a *App) alertFailedOperation(r *http.Request, status int) {
	if status < 400 || status == 401 || status == 403 || status == 404 || status == 405 || status == 429 || strings.HasPrefix(r.URL.Path, "/api/operations") {
		return
	}
	category, route := operationsFailureCategory(r.URL.Path)
	// One incident per category/hour bounds repeated failures and excludes
	// request bodies, query strings, passwords and command diagnostics.
	key := category + time.Now().UTC().Format("2006-01-02T15")
	a.recordOperationsEvent(category, key, "Administrative operation failed",
		fmt.Sprintf("A %s request failed with HTTP %d. Review Recent Activities and the relevant panel section.", r.Method, status),
		"warning", route)
}
