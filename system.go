package main

import (
	"bufio"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"net/mail"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type CreateSiteInput struct {
	WordPressMode     string `json:"wordpressMode"`
	WordPressUsername string `json:"wordpressUsername"`
	WordPressPassword string `json:"wordpressPassword"`
	WordPressEmail    string `json:"wordpressEmail"`
	Domain            string `json:"domain"`
	AppType           string `json:"appType"`
	AppVersion        string `json:"appVersion"`
	PHPVersion        string `json:"phpVersion"`
	CreateDatabase    bool   `json:"createDatabase"`
	EnableCron        bool   `json:"enableCron"`
}

type DBCredentials struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Password string `json:"password"`
	Host     string `json:"host"`
}

type ResourceUsage struct {
	Used    uint64  `json:"used"`
	Total   uint64  `json:"total"`
	Percent float64 `json:"percent"`
}

type ServiceStatus struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Unit       string `json:"unit"`
	State      string `json:"state"`
	StateLabel string `json:"stateLabel,omitempty"`
}

type SystemSnapshot struct {
	OS                 string          `json:"os"`
	OSID               string          `json:"osId"`
	Hostname           string          `json:"hostname"`
	IPAddress          string          `json:"ipAddress"`
	CurrentResolvers   []string        `json:"currentResolvers"`
	UptimeSeconds      uint64          `json:"uptimeSeconds"`
	CPUPercent         float64         `json:"cpuPercent"`
	LoadAverage        float64         `json:"loadAverage"`
	Memory             ResourceUsage   `json:"memory"`
	Disk               ResourceUsage   `json:"disk"`
	Services           []ServiceStatus `json:"services"`
	PHPVersions        []string        `json:"phpVersions"`
	DatabaseEngine     string          `json:"databaseEngine"`
	FirewallBackend    string          `json:"firewallBackend"`
	WAFEngine          string          `json:"wafEngine"`
	AntivirusAvailable bool            `json:"antivirusAvailable"`
	WebmailAvailable   bool            `json:"webmailAvailable"`
	WebmailURL         string          `json:"webmailUrl"`
	Demo               bool            `json:"demo"`
}

type OSInfo struct {
	ID      string
	Name    string
	Version string
}

type System struct {
	dataDir   string
	os        OSInfo
	demo      bool
	cpuMu     sync.Mutex
	secretMu  sync.Mutex
	phpPoolMu sync.Mutex
	prevIdle  uint64
	prevAll   uint64
}

var domainPattern = regexp.MustCompile(`^(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+)$`)
var safeDatabasePart = regexp.MustCompile(`[^a-z0-9]+`)
var fastCGIPassPattern = regexp.MustCompile(`(?m)^(\s*fastcgi_pass\s+unix:)[^;]+(;\s*)$`)
var supportedPHPVersions = []string{"7.3", "7.4", "8.1", "8.2", "8.3", "8.4", "8.5"}

func NewSystem(dataDir string) *System {
	info := detectOS()
	system := &System{
		dataDir: dataDir,
		os:      info,
		demo:    runtime.GOOS != "linux" || os.Getenv("LITECP_DEMO") == "1",
	}
	if !system.demo {
		system.cpuUsage() // Prime the first delta so the initial dashboard is meaningful.
	}
	return system
}

func (s *System) Snapshot(ctx context.Context, sites []Site) SystemSnapshot {
	hostname, _ := os.Hostname()
	snapshot := SystemSnapshot{
		OS: s.os.Name, OSID: s.os.ID, Hostname: hostname, IPAddress: primaryIP(), Demo: s.demo,
		CurrentResolvers: currentSystemResolvers(),
		Memory:           memoryUsage(), Disk: diskUsage(ctx), PHPVersions: s.installedPHPVersions(),
		DatabaseEngine: s.DatabaseEngine(), FirewallBackend: s.FirewallBackend(),
		WAFEngine: s.WAFEngine(), AntivirusAvailable: s.AntivirusAvailable(), WebmailAvailable: s.WebmailAvailable(), WebmailURL: s.WebmailURL(),
	}
	snapshot.UptimeSeconds, snapshot.LoadAverage = uptimeAndLoad()
	snapshot.CPUPercent = s.cpuUsage()

	units := s.serviceUnits()
	snapshot.Services = make([]ServiceStatus, len(units))
	var wait sync.WaitGroup
	for i, service := range units {
		wait.Add(1)
		go func(index int, item ServiceStatus) {
			defer wait.Done()
			if item.Key == "php" {
				item.State = s.phpStackState(ctx, sites)
			} else if item.Key == "mail" {
				item.State = s.mailStackState(ctx)
			} else {
				item.State = s.serviceState(ctx, item.Unit)
				if item.State == "running" && !s.serviceProbe(ctx, item.Key) {
					item.State = "degraded"
				}
			}
			if strings.HasSuffix(item.Unit, ".timer") {
				subState, _ := runCommand(ctx, 3*time.Second, "systemctl", "show", "--property=SubState", "--value", item.Unit)
				item.StateLabel = timerServiceLabel(item.State, strings.TrimSpace(subState))
			}
			snapshot.Services[index] = item
		}(i, service)
	}
	wait.Wait()
	return snapshot
}

func (s *System) SiteSSLHealthy(site Site) bool {
	if !site.SSL || s.demo {
		return site.SSL
	}
	content, err := os.ReadFile(filepath.Join("/etc/letsencrypt/live", site.Domain, "fullchain.pem"))
	if err != nil {
		return false
	}
	block, _ := pem.Decode(content)
	if block == nil {
		return false
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	return err == nil && certificateMatchesSite(certificate, site.Domain, time.Now())
}

func certificateMatchesSite(certificate *x509.Certificate, domain string, now time.Time) bool {
	return certificate != nil && !certificate.IsCA && !now.Before(certificate.NotBefore) && now.Before(certificate.NotAfter) && certificate.VerifyHostname(domain) == nil
}

func (s *System) serviceProbe(ctx context.Context, key string) bool {
	if s.demo {
		return true
	}
	switch key {
	case "nginx":
		return probeTCP("127.0.0.1:80")
	case "database":
		client := firstAvailableCommand("mysql", "mariadb")
		_, err := runCommand(ctx, 3*time.Second, client, "--protocol=socket", "--user=root", "--batch", "--skip-column-names", "--execute", "SELECT 1")
		return err == nil
	case "cron":
		return true
	default:
		return true
	}
}

func probeTCP(address string) bool {
	connection, err := net.DialTimeout("tcp", address, 1200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func (s *System) serviceUnits() []ServiceStatus {
	cronUnit := "crond"
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		cronUnit = "cron"
	}
	phpUnits := s.phpUnits()
	phpUnitLabel := strings.Join(phpUnits, ", ")
	if phpUnitLabel == "" {
		phpUnitLabel = "PHP-FPM"
	}
	services := []ServiceStatus{
		{Key: "nginx", Label: "Nginx", Unit: "nginx"},
		{Key: "php", Label: "Multi-PHP FPM", Unit: phpUnitLabel},
		{Key: "database", Label: s.DatabaseEngine(), Unit: s.databaseUnit()},
		{Key: "cron", Label: "App Scheduler", Unit: cronUnit},
	}
	mailUnits := append([]string{}, s.mailUnits()...)
	for _, unit := range []string{"opendkim", "rspamd"} {
		if unitFileExists(unit) {
			mailUnits = append(mailUnits, unit)
		}
	}
	mailLabel := strings.Join(mailUnits, ", ")
	if mailLabel == "" {
		mailLabel = "Postfix, Dovecot"
	}
	services = append(services, ServiceStatus{Key: "mail", Label: "Email Hosting", Unit: mailLabel})
	if unit := ftpsServiceUnit(unitFileExists); unit != "" {
		services = append(services, ServiceStatus{Key: "ftps", Label: "FTPS", Unit: unit})
	}
	for _, extra := range []ServiceStatus{
		{Key: "redis", Label: "Redis", Unit: "redis"},
		{Key: "redis-server", Label: "Redis", Unit: "redis-server"},
		{Key: "fail2ban", Label: "Brute-force protection", Unit: "fail2ban"},
		{Key: "opendkim", Label: "DKIM signing", Unit: "opendkim"},
		{Key: "rspamd", Label: "Spam filtering", Unit: "rspamd"},
		{Key: "clamav-updates", Label: "ClamAV signature updates", Unit: "litecp-clamav-update.timer"},
	} {
		if unitFileExists(extra.Unit) {
			services = append(services, extra)
		}
	}
	return services
}

func (s *System) serviceState(ctx context.Context, unit string) string {
	if s.demo {
		return "running"
	}
	commandCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(commandCtx, "systemctl", "is-active", unit).Output()
	if commandCtx.Err() != nil {
		return "unknown"
	}
	state := strings.TrimSpace(string(output))
	if state == "" && err != nil {
		return "unknown"
	}
	return normalizeServiceState(state)

}

func (s *System) ServiceAction(ctx context.Context, key, action string) error {
	if action != "start" && action != "restart" && action != "stop" {
		return errors.New("unknown service action")
	}
	var units []string
	switch key {
	case "php":
		units = s.phpUnits()
	case "mail":
		units = append(units, s.mailUnits()...)
		for _, unit := range []string{"opendkim", "rspamd"} {
			if unitFileExists(unit) {
				units = append(units, unit)
			}
		}
	default:
		for _, service := range s.serviceUnits() {
			if service.Key == key {
				units = []string{service.Unit}
				break
			}
		}
	}
	if len(units) == 0 {
		return errors.New("service is not installed or is unknown")
	}
	if s.demo {
		return nil
	}
	for _, unit := range units {
		if _, err := runCommand(ctx, 30*time.Second, "systemctl", action, unit); err != nil {
			return err
		}
		state := s.serviceState(ctx, unit)
		if (action == "stop" && state != "stopped") || (action != "stop" && state != "running") {
			return fmt.Errorf("%s: requested %s but current state is %s", unit, action, state)
		}
	}
	return nil
}

func (s *System) CreateSite(ctx context.Context, input CreateSiteInput) (Site, *DBCredentials, error) {
	domain := strings.ToLower(strings.TrimSpace(input.Domain))
	if len(domain) > 253 || !domainPattern.MatchString(domain) {
		return Site{}, nil, errors.New("enter a valid domain such as billing.example.com")
	}
	appType := strings.ToLower(strings.TrimSpace(input.AppType))
	if appType == "" {
		appType = "whmcs"
	}
	phpVersion := strings.TrimSpace(input.PHPVersion)
	if phpVersion == "" {
		phpVersion = defaultPHPVersion(appType)
	}
	appVersion := normalizeAppVersion(appType, input.AppVersion)
	if err := validateSiteOptions(appType, appVersion, phpVersion); err != nil {
		return Site{}, nil, err
	}
	if !s.phpRuntimeAvailable(phpVersion) {
		return Site{}, nil, fmt.Errorf("PHP %s is not installed on this server", phpVersion)
	}
	id, err := randomToken(8)
	if err != nil {
		return Site{}, nil, err
	}
	site := Site{
		ID: id, Domain: domain, Root: defaultSiteRoot(domain),
		AppType: appType, AppVersion: appVersion, PHPVersion: phpVersion,
		RuntimeUID: siteRuntimeID(id), RuntimeGID: siteRuntimeID(id), CreatedAt: time.Now().UTC(),
	}
	if s.demo {
		if input.CreateDatabase {
			credentials := demoCredentials(domain)
			site.Database, site.DBUser = credentials.Name, credentials.Username
			return site, &credentials, nil
		}
		return site, nil, nil
	}

	if _, err := os.Lstat(managedSiteRoot(site)); err == nil {
		empty, inspectErr := emptyProvisioningScaffold(managedSiteRoot(site))
		if inspectErr != nil {
			return Site{}, nil, fmt.Errorf("inspect retained website home: %w", inspectErr)
		}
		if !empty {
			return Site{}, nil, errors.New("retained website data already exists for this domain; review or remove the retained directory before creating a new website")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Site{}, nil, err
	}
	_, rootErr := os.Lstat(site.Root)
	rootCreated := errors.Is(rootErr, os.ErrNotExist)
	if rootErr != nil && !rootCreated {
		return Site{}, nil, fmt.Errorf("inspect website root: %w", rootErr)
	}
	placeholderCreated := false
	committed := false
	defer func() {
		if committed {
			return
		}
		if rootCreated {
			_ = os.RemoveAll(site.Root)
			_ = os.Remove(filepath.Dir(site.Root))
		} else if placeholderCreated {
			_ = os.Remove(filepath.Join(site.Root, "index.html"))
		}
	}()
	if err := s.ensureSiteHome(site); err != nil {
		return Site{}, nil, err
	}
	if err := os.MkdirAll(site.Root, 0750); err != nil {
		return Site{}, nil, fmt.Errorf("create website root: %w", err)
	}
	if err := chownSitePath(site.Root, site, 0750, s.webUser()); err != nil {
		return Site{}, nil, err
	}
	if err := s.labelSiteHome(ctx, site); err != nil {
		return Site{}, nil, err
	}
	placeholder := filepath.Join(site.Root, "index.html")
	if _, err := os.Stat(placeholder); errors.Is(err, os.ErrNotExist) {
		appName := applicationName(site.AppType)
		guidance := "Upload your licensed WHMCS files"
		if site.AppType == "wordpress" {
			guidance = "Upload WordPress files"
		} else if site.AppType == "regular" {
			guidance = "Upload your PHP or HTML website files"
		}
		content := []byte("<!doctype html><meta charset=utf-8><title>LiteCP</title><style>body{font:16px system-ui;max-width:680px;margin:12vh auto;padding:32px;color:#173b46}code{background:#edf6f4;padding:3px 6px;border-radius:5px}</style><h1>Ready for " + appName + "</h1><p>" + guidance + " to your website's <code>public_html</code> directory, then remove this file.</p>")
		if err := os.WriteFile(placeholder, content, 0640); err != nil {
			return Site{}, nil, fmt.Errorf("write placeholder: %w", err)
		}
		placeholderCreated = true
		_ = chownSitePath(placeholder, site, 0640, s.webUser())
	}
	if err := s.ensureSitePHPPool(ctx, site); err != nil {
		return Site{}, nil, err
	}
	keepPool := false
	defer func() {
		if !keepPool {
			_ = s.removeSitePHPPool(context.Background(), site)
		}
	}()

	configPath, enabledPath := s.nginxPaths(domain)
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return Site{}, nil, fmt.Errorf("create nginx directory: %w", err)
	}
	if err := writeAtomic(configPath, []byte(s.nginxConfig(site)), 0644); err != nil {
		return Site{}, nil, fmt.Errorf("write nginx configuration: %w", err)
	}
	if enabledPath != "" {
		_ = os.Remove(enabledPath)
		if err := os.Symlink(configPath, enabledPath); err != nil {
			_ = os.Remove(configPath)
			return Site{}, nil, fmt.Errorf("enable nginx website: %w", err)
		}
	}
	rollback := func() {
		_ = os.Remove(configPath)
		if enabledPath != "" {
			_ = os.Remove(enabledPath)
		}
		_ = s.removeSitePHPPool(context.Background(), site)
	}
	if _, err := runCommand(ctx, 10*time.Second, "nginx", "-t"); err != nil {
		rollback()
		return Site{}, nil, err
	}
	if _, err := runCommand(ctx, 15*time.Second, "systemctl", "reload", "nginx"); err != nil {
		rollback()
		return Site{}, nil, err
	}

	var credentials *DBCredentials
	if input.CreateDatabase {
		created, err := s.createDatabase(ctx, domain, phpVersion)
		if err != nil {
			rollback()
			_, _ = runCommand(ctx, 10*time.Second, "systemctl", "reload", "nginx")
			return Site{}, nil, err
		}
		site.Database, site.DBUser = created.Name, created.Username
		credentials = &created
	}
	keepPool = true
	site.provisionedRoot = rootCreated
	site.provisionedPlaceholder = placeholderCreated
	committed = true
	return site, credentials, nil
}

func (s *System) CleanupFailedSiteFiles(site Site) error {
	if !domainPattern.MatchString(site.Domain) {
		return errors.New("website domain is invalid")
	}
	expected := filepath.Clean(defaultSiteRoot(site.Domain))
	if filepath.Clean(site.Root) != expected {
		return errors.New("refusing to clean an unexpected website root")
	}
	if site.provisionedRoot {
		if err := os.RemoveAll(expected); err != nil {
			return err
		}
		if err := os.Remove(filepath.Dir(expected)); err != nil && !errors.Is(err, os.ErrNotExist) {
			// A non-empty parent is preserved deliberately.
			if entries, readErr := os.ReadDir(filepath.Dir(expected)); readErr != nil || len(entries) == 0 {
				return err
			}
		}
		return nil
	}
	if site.provisionedPlaceholder {
		if err := os.Remove(filepath.Join(expected, "index.html")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *System) createDatabase(ctx context.Context, domain, phpVersion string) (DBCredentials, error) {
	part := strings.Trim(safeDatabasePart.ReplaceAllString(strings.ToLower(domain), "_"), "_")
	if len(part) > 12 {
		part = part[:12]
	}
	suffix, err := randomToken(3)
	if err != nil {
		return DBCredentials{}, err
	}
	suffix = strings.ToLower(strings.ReplaceAll(suffix, "-", "x"))
	name := "cp_" + part + "_" + suffix
	if len(name) > 32 {
		name = name[:32]
	}
	userPart := part
	if len(userPart) > 7 {
		userPart = userPart[:7]
	}
	username := "cp_" + userPart + "_" + suffix
	password, err := randomToken(24)
	if err != nil {
		return DBCredentials{}, err
	}
	sql := fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci; CREATE USER '%s'@'localhost' %s; GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost'; FLUSH PRIVILEGES;", name, username, s.databaseUserIdentification(password, legacyPHPDatabaseAuth(phpVersion)), name, username)
	databaseClient := firstAvailableCommand("mysql", "mariadb")
	if _, err := runCommandInput(ctx, 20*time.Second, sql, databaseClient, "--protocol=socket", "--user=root", "--batch"); err != nil {
		return DBCredentials{}, fmt.Errorf("create application database: %w", err)
	}
	return DBCredentials{Name: name, Username: username, Password: password, Host: "localhost"}, nil
}

func demoCredentials(domain string) DBCredentials {
	part := strings.Trim(safeDatabasePart.ReplaceAllString(strings.ToLower(domain), "_"), "_")
	if len(part) > 8 {
		part = part[:8]
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strings.ToLower(domain)))
	suffix := fmt.Sprintf("%08x", hash.Sum32())[:6]
	name := "cp_" + part + "_" + suffix
	return DBCredentials{Name: name, Username: name, Password: "saved-once-demo-password", Host: "localhost"}
}

func (s *System) EnableSSL(ctx context.Context, site Site, email string, renew ...bool) error {
	if resourceEnabled(site) {
		return errors.New("disable resource enforcement before changing SSL, then re-enable it; existing certificates remain active")
	}
	if site.Application != nil && site.Application.Enabled {
		return errors.New("disable the application runtime, enable SSL, then start the runtime again to preserve certificate routing")
	}
	if !domainPattern.MatchString(site.Domain) {
		return errors.New("website domain is invalid")
	}
	address, err := mail.ParseAddress(strings.TrimSpace(email))
	if err != nil || address.Address != strings.TrimSpace(email) {
		return errors.New("enter a valid email address for Let's Encrypt notices")
	}
	if s.demo {
		return nil
	}
	return s.enableWebrootSSL(ctx, site, address.Address, len(renew) > 0 && renew[0])
}

func (s *System) SetCron(site Site, enabled bool) error {
	if !domainPattern.MatchString(site.Domain) {
		return errors.New("website domain is invalid")
	}
	if site.AppType == "regular" && enabled {
		return errors.New("regular websites do not have an application-specific cron target")
	}
	if s.demo {
		return nil
	}
	cronPath := filepath.Join("/etc/cron.d", "litecp-"+site.ID)
	if !enabled {
		if err := os.Remove(cronPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("disable application cron: %w", err)
		}
		return nil
	}
	cronScript := filepath.Join(site.Root, "crons", "cron.php")
	if site.AppType == "wordpress" {
		cronScript = filepath.Join(site.Root, "wp-cron.php")
	}
	phpBinary := s.phpBinary(site.PHPVersion)
	if !s.demo {
		if _, err := os.Stat(phpBinary); err != nil {
			return fmt.Errorf("PHP %s CLI is unavailable: %w", site.PHPVersion, err)
		}
	}
	cronUser := "root"
	command := fmt.Sprintf("/usr/bin/setpriv --reuid=%d --regid=%d --clear-groups %s -q %s", site.RuntimeUID, site.RuntimeGID, phpBinary, cronScript)
	if s.usesPHP73Container(site.PHPVersion) {
		command = fmt.Sprintf("/usr/bin/podman exec --user %d:%d litecp-php73 php -q %s", site.RuntimeUID, site.RuntimeGID, cronScript)
	}
	if resourceEnabled(site) {
		path := filepath.Join(resourceDirectory(site.ID), "builtin.sh")
		// The host executor prepares this root-owned file during activation.
		if _, err := os.Stat(path); err != nil {
			return errors.New("resource cron script is not prepared")
		}
		command = fmt.Sprintf("%s resource-cron %s %s %s", cronShellQuote(resourceRuntimeExecutable()), cronShellQuote(s.dataDir), cronShellQuote(site.ID), cronShellQuote(path))
	}
	content := fmt.Sprintf("SHELL=/bin/sh\nPATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\nMAILTO=\"\"\n*/5 * * * * %s %s >/dev/null 2>&1\n", cronUser, command)
	return writeAtomic(cronPath, []byte(content), 0644)
}

func (s *System) SetPHPVersion(ctx context.Context, site Site, phpVersion string) error {
	if resourceEnabled(site) {
		return errors.New("disable resource enforcement before changing PHP; re-enable it after the runtime change")
	}
	if site.Application != nil && site.Application.Enabled {
		return errors.New("disable the application runtime before changing PHP")
	}
	phpVersion = strings.TrimSpace(phpVersion)
	if err := validatePHPVersionSwitch(phpVersion); err != nil {
		return err
	}
	if !s.phpRuntimeAvailable(phpVersion) {
		return fmt.Errorf("PHP %s is not installed on this server", phpVersion)
	}
	if s.demo {
		return nil
	}
	originalVersion := site.PHPVersion
	updatedSite := site
	updatedSite.PHPVersion = phpVersion
	if err := s.ensureSitePHPPool(ctx, updatedSite); err != nil {
		return fmt.Errorf("prepare isolated PHP pool: %w", err)
	}
	configPath, _ := s.nginxPaths(site.Domain)
	original, err := os.ReadFile(configPath)
	if err != nil {
		if originalVersion != phpVersion {
			_ = s.removeSitePHPPool(context.Background(), updatedSite)
		}
		return fmt.Errorf("read nginx configuration: %w", err)
	}
	updated, err := replaceFastCGISocket(string(original), s.sitePHPSocket(updatedSite))
	if err != nil {
		if originalVersion != phpVersion {
			if cleanupErr := s.removeSitePHPPool(context.Background(), updatedSite); cleanupErr != nil {
				return fmt.Errorf("%v; new PHP pool cleanup failed: %v", err, cleanupErr)
			}
		}
		return err
	}
	rollback := func(cause error) error {
		errorsFound := []string{}
		if originalVersion != phpVersion {
			if err := s.ensureSitePHPPool(context.Background(), site); err != nil {
				errorsFound = append(errorsFound, "restore original pool: "+err.Error())
			}
		}
		if err := writeAtomic(configPath, original, 0644); err != nil {
			errorsFound = append(errorsFound, "restore Nginx config: "+err.Error())
		}
		if _, err := runCommand(context.Background(), 15*time.Second, "systemctl", "reload", "nginx"); err != nil {
			errorsFound = append(errorsFound, "reload Nginx: "+err.Error())
		}
		if originalVersion != phpVersion {
			if err := s.removeSitePHPPool(context.Background(), updatedSite); err != nil {
				errorsFound = append(errorsFound, "remove new pool: "+err.Error())
			}
		}
		if len(errorsFound) > 0 {
			return fmt.Errorf("%v; PHP runtime rollback failed: %s", cause, strings.Join(errorsFound, "; "))
		}
		return cause
	}
	if err := writeAtomic(configPath, []byte(updated), 0644); err != nil {
		return rollback(fmt.Errorf("update nginx configuration: %w", err))
	}
	if _, err := runCommand(ctx, 10*time.Second, "nginx", "-t"); err != nil {
		return rollback(err)
	}
	if _, err := runCommand(ctx, 15*time.Second, "systemctl", "reload", "nginx"); err != nil {
		return rollback(err)
	}
	if site.Cron {
		if err := s.SetCron(updatedSite, true); err != nil {
			return rollback(fmt.Errorf("update application cron: %w", err))
		}
	}
	if originalVersion != phpVersion {
		if err := s.removeSitePHPPool(context.Background(), site); err != nil {
			return rollback(fmt.Errorf("remove previous PHP pool: %w", err))
		}
	}
	return nil
}

func replaceFastCGISocket(config, socket string) (string, error) {
	if !fastCGIPassPattern.MatchString(config) {
		return "", errors.New("managed nginx configuration has no FastCGI socket")
	}
	return fastCGIPassPattern.ReplaceAllString(config, "${1}"+socket+"${2}"), nil
}

type siteConfigBackup struct {
	nginx      []byte
	cron       []byte
	customCron []byte
}

func (s *System) snapshotSiteConfig(site Site) (siteConfigBackup, error) {
	if s.demo {
		return siteConfigBackup{}, nil
	}
	configPath, _ := s.nginxPaths(site.Domain)
	nginx, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return siteConfigBackup{}, fmt.Errorf("read nginx configuration: %w", err)
	}
	cron, err := os.ReadFile(filepath.Join("/etc/cron.d", "litecp-"+site.ID))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return siteConfigBackup{}, fmt.Errorf("read cron configuration: %w", err)
	}
	customCron, err := os.ReadFile(s.websiteCronPath(site.ID))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return siteConfigBackup{}, fmt.Errorf("read custom cron configuration: %w", err)
	}
	return siteConfigBackup{nginx: nginx, cron: cron, customCron: customCron}, nil
}

func (s *System) restoreSiteConfig(ctx context.Context, site Site, backup siteConfigBackup) error {
	if s.demo {
		return nil
	}
	if len(backup.customCron) > 0 {
		if err := writeAtomic(s.websiteCronPath(site.ID), backup.customCron, 0600); err != nil {
			return fmt.Errorf("restore custom cron configuration: %w", err)
		}
	}
	if err := s.ensureSitePHPPool(ctx, site); err != nil {
		return fmt.Errorf("restore isolated PHP pool: %w", err)
	}
	configPath, enabledPath := s.nginxPaths(site.Domain)
	nginx := backup.nginx
	if len(nginx) == 0 {
		nginx = []byte(s.nginxConfig(site))
	}
	if err := writeAtomic(configPath, nginx, 0644); err != nil {
		return fmt.Errorf("restore nginx configuration: %w", err)
	}
	if enabledPath != "" {
		_ = os.Remove(enabledPath)
		if err := os.Symlink(configPath, enabledPath); err != nil {
			return fmt.Errorf("re-enable nginx website: %w", err)
		}
	}
	cronPath := filepath.Join("/etc/cron.d", "litecp-"+site.ID)
	if len(backup.cron) > 0 {
		if err := writeAtomic(cronPath, backup.cron, 0644); err != nil {
			return fmt.Errorf("restore cron configuration: %w", err)
		}
	} else if site.Cron {
		if err := s.SetCron(site, true); err != nil {
			return err
		}
	}
	if _, err := runCommand(ctx, 15*time.Second, "nginx", "-t"); err != nil {
		return err
	}
	_, err := runCommand(ctx, 15*time.Second, "systemctl", "reload", "nginx")
	return err
}

func (s *System) RemoveSiteConfig(ctx context.Context, site Site) error {
 if redisEnabled(site) && !s.demo {settings:=*site.Redis;settings.Enabled=false;if _,err:=s.redisHostAction(ctx,site,settings,"save");err!=nil{return err}}

	if resourceEnabled(site) {
		if err := resourceHostAction(ctx, s, site, *site.ResourceLimits, "disable"); err != nil {
			return err
		}
	}
	if !domainPattern.MatchString(site.Domain) {
		return errors.New("website domain is invalid")
	}
	if s.demo {
		return nil
	}
	if err := s.pauseWebsiteCron(site.ID); err != nil {
		return fmt.Errorf("pause custom cron jobs: %w", err)
	}
	if site.Application != nil && site.Application.Container != "" {
		if err := s.stopApplicationContainer(ctx, site.Application.Container); err != nil {
			return errors.New("stop the application before detaching this website")
		}
	}
	configPath, enabledPath := s.nginxPaths(site.Domain)
	if err := os.Remove(configPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove nginx configuration: %w", err)
	}
	if enabledPath != "" {
		if err := os.Remove(enabledPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("disable nginx website: %w", err)
		}
	}
	_ = os.Remove(filepath.Join("/etc/cron.d", "litecp-"+site.ID))
	if _, err := runCommand(ctx, 15*time.Second, "systemctl", "reload", "nginx"); err != nil {
		return err
	}
	return s.removeSitePHPPool(ctx, site)
}

func (s *System) ChangeSiteDomain(ctx context.Context, current, updated Site) error {
	if resourceEnabled(current) {
		return errors.New("disable resource enforcement before renaming this website; re-enable it after the rename")
	}
	if !domainPattern.MatchString(current.Domain) || !domainPattern.MatchString(updated.Domain) {
		return errors.New("website domain is invalid")
	}
	if current.ID != updated.ID {
		return errors.New("website identity cannot be changed")
	}
	if s.demo {
		return nil
	}
	oldConfig, oldEnabled := s.nginxPaths(current.Domain)
	newConfig, newEnabled := s.nginxPaths(updated.Domain)
	if _, err := os.Lstat(newConfig); newConfig != oldConfig && err == nil {
		return errors.New("the new domain already has an Nginx configuration")
	} else if newConfig != oldConfig && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect new domain configuration: %w", err)
	}
	undoHome, err := s.relocateDomainHome(ctx, current, updated)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			undoHome()
		}
	}()
	suffix := fmt.Sprintf(".renamed-%d", time.Now().UnixNano())
	archivedConfig := oldConfig + suffix
	archivedEnabled := oldEnabled + suffix
	if err := os.Rename(oldConfig, archivedConfig); err != nil {
		return fmt.Errorf("preserve previous domain configuration: %w", err)
	}
	enabledMoved := false
	if oldEnabled != "" {
		if err := os.Rename(oldEnabled, archivedEnabled); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = os.Rename(archivedConfig, oldConfig)
			return fmt.Errorf("preserve previous enabled website link: %w", err)
		} else if err == nil {
			enabledMoved = true
		}
	}
	rollback := func(cause error) error {
		failedConfig := newConfig + suffix + ".failed"
		if _, err := os.Lstat(newConfig); err == nil {
			_ = os.Rename(newConfig, failedConfig)
		}
		if newEnabled != "" {
			if _, err := os.Lstat(newEnabled); err == nil {
				_ = os.Rename(newEnabled, newEnabled+suffix+".failed")
			}
		}
		_ = os.Rename(archivedConfig, oldConfig)
		if enabledMoved {
			_ = os.Rename(archivedEnabled, oldEnabled)
		}
		return cause
	}
	if err := writeAtomic(newConfig, []byte(s.nginxConfig(updated)), 0644); err != nil {
		return rollback(fmt.Errorf("write new domain configuration: %w", err))
	}
	if newEnabled != "" {
		if err := os.Symlink(newConfig, newEnabled); err != nil {
			return rollback(fmt.Errorf("enable new domain configuration: %w", err))
		}
	}
	if _, err := runCommand(ctx, 15*time.Second, "nginx", "-t"); err != nil {
		return rollback(err)
	}
	if _, err := runCommand(ctx, 15*time.Second, "systemctl", "reload", "nginx"); err != nil {
		return rollback(err)
	}
	committed = true
	return nil
}

func (s *System) CreateBackup(ctx context.Context, site Site) (Backup, error) {
	backupID, err := randomToken(8)
	if err != nil {
		return Backup{}, err
	}
	created := time.Now().UTC()
	stamp := created.Format("20060102-150405.000000000")
	backupDir := siteScheduledBackupRoot(site)
	finalPath := filepath.Join(backupDir, site.Domain+"-"+stamp+"-"+backupID+".tar.gz")
	backup := Backup{ID: backupID, SiteID: site.ID, Domain: site.Domain, Path: finalPath, Status: "complete", CreatedAt: created}
	if s.demo {
		backup.Size = 184*1024*1024 + 739*1024
		return backup, nil
	}
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return Backup{}, fmt.Errorf("create backup directory: %w", err)
	}
	staging, err := os.MkdirTemp(backupDir, ".litecp-staging-")
	if err != nil {
		return Backup{}, fmt.Errorf("create backup staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	webArchive := filepath.Join(staging, "website.tar.gz")
	if _, err := runCommand(ctx, 5*time.Minute, "tar", "-czf", webArchive, "-C", site.Root, "."); err != nil {
		return Backup{}, fmt.Errorf("archive website: %w", err)
	}
	if site.Database != "" {
		dumpPath := filepath.Join(staging, "database.sql")
		dumpCommand := firstAvailableCommand("mysqldump", "mariadb-dump")
		if _, err := runCommand(ctx, 5*time.Minute, dumpCommand, "--protocol=socket", "--user=root", "--single-transaction", "--result-file="+dumpPath, site.Database); err != nil {
			return Backup{}, fmt.Errorf("dump database: %w", err)
		}
	}
	if _, err := runCommand(ctx, 5*time.Minute, "tar", "-czf", finalPath, "-C", staging, "."); err != nil {
		return Backup{}, fmt.Errorf("finalize backup: %w", err)
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		return Backup{}, fmt.Errorf("inspect backup: %w", err)
	}
	backup.Size = info.Size()
	return backup, nil
}

func (s *System) RemoveBackupFile(path string) error {
	path = filepath.Clean(path)
	if !validScheduledBackupPath(path) {
		return errors.New("backup path is outside LiteCP storage")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("backup path is not a regular file")
	}
	return os.Remove(path)
}

func (s *System) Logs(ctx context.Context, target string) ([]string, error) {
	if s.demo {
		now := time.Now().Format("2006/01/02 15:04:05")
		return []string{
			now + " [notice] LiteCP preview log stream connected",
			now + " [info] PHP-FPM pool www is healthy (2 idle workers)",
			now + " [info] GET /clientarea.php 200 42ms",
			now + " [info] WHMCS automation cron completed successfully",
		}, nil
	}
	var command []string
	switch target {
	case "nginx-access":
		command = []string{"tail", "-n", "100", "/var/log/nginx/access.log"}
	case "nginx-error", "":
		command = []string{"tail", "-n", "100", "/var/log/nginx/error.log"}
	case "php":
		command = []string{"journalctl", "--no-pager", "-n", "100"}
		for _, unit := range s.phpUnits() {
			command = append(command, "-u", unit)
		}
		if len(command) == 4 {
			return nil, errors.New("no PHP-FPM runtime is installed")
		}
	case "database":
		command = []string{"journalctl", "--no-pager", "-n", "100", "-u", s.databaseUnit()}
	case "cron":
		command = []string{"journalctl", "--no-pager", "-n", "100", "-u", s.cronUnit()}
	case "mail":
		command = []string{"journalctl", "--no-pager", "-n", "100", "-u", "postfix", "-u", "dovecot"}
	default:
		return nil, errors.New("unknown log target")
	}
	output, err := runCommand(ctx, 10*time.Second, command[0], command[1:]...)
	if err != nil {
		return nil, err
	}
	output = strings.ReplaceAll(output, "\r", "")
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []string{}, nil
	}
	return lines, nil
}

func (s *System) nginxPaths(domain string) (string, string) {
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		return filepath.Join("/etc/nginx/sites-available", "litecp-"+domain+".conf"), filepath.Join("/etc/nginx/sites-enabled", "litecp-"+domain+".conf")
	}
	return filepath.Join("/etc/nginx/conf.d", "litecp-"+domain+".conf"), ""
}

func (s *System) nginxConfig(site Site) (content string) {
	if resourceEnabled(site) {
		if saved, err := os.ReadFile(filepath.Join(resourceDirectory(site.ID), "frontend.conf")); err == nil {
			updated, e := replaceWAFBlock(string(saved), site, site.WAFMode)
			if e == nil {
				return suspensionSiteConfig(updated, site, site.Suspended)
			}
		}
		return fmt.Sprintf("server { listen 80; listen [::]:80; server_name %s; return 503 'Website resource boundary requires revalidation.'; }\n", site.Domain)
	}
	defer func() { content = injectTrafficLogging(content, site) }()
	defer func() { content = suspensionSiteConfig(content, site, site.Suspended) }()
	if site.Application != nil {
		return s.applicationNginx(site)
	}
	applicationRules := `    location = /configuration.php { deny all; }
    location ^~ /crons/ { deny all; }`
	if site.AppType == "wordpress" {
		applicationRules = `    location = /wp-config.php { deny all; }
    location ~* ^/wp-content/uploads/.*\.php$ { deny all; }`
	} else if site.AppType == "regular" {
		applicationRules = `    location ~* ^/(?:\.env|composer\.(?:json|lock)|package(?:-lock)?\.json|yarn\.lock)$ { deny all; }`
	}
	return fmt.Sprintf(`# Managed by LiteCP - By Hostingtom
server {
%s
    listen 80;
    listen [::]:80;
    server_name %s;
    root %s;
%s
    index index.php index.html;

    client_max_body_size 64m;
    charset utf-8;

    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }

%s

    location ~ \.php$ {
        try_files $uri =404;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_param HTTP_PROXY "";
		fastcgi_pass unix:%s;
        fastcgi_read_timeout 120s;
    }

    location ~ /\.(?!well-known) { deny all; }

    location ~* \.(?:css|js|jpg|jpeg|gif|png|svg|ico|webp|woff2?)$ {
        expires 7d;
        add_header Cache-Control "public, no-transform";
        try_files $uri /index.php?$query_string;
        access_log off;
    }
}
`, wafBlock(site, site.WAFMode), site.Domain, site.Root, siteLogDirectives(site), applicationRules, s.sitePHPSocket(site))
}

func siteRuntimeID(id string) int {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(id))
	return 100000 + int(hash.Sum32()%2000000000)
}

func (s *System) sitePHPSocket(site Site) string {
	name := safeSiteID(site.ID)
	if s.usesPHP73Container(site.PHPVersion) {
		return filepath.Join("/run/litecp-php73", name+".sock")
	}
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		return filepath.Join("/run/php", "php"+site.PHPVersion+"-litecp-"+name+".sock")
	}
	compact := strings.ReplaceAll(site.PHPVersion, ".", "")
	if site.PHPVersion == "8.3" && !unitFileExists("php83-php-fpm") && unitFileExists("php-fpm") {
		return filepath.Join("/run/php-fpm", "litecp-"+name+".sock")
	}
	return filepath.Join("/var/opt/remi", "php"+compact, "run/php-fpm", "litecp-"+name+".sock")
}

func safeSiteID(id string) string {
	var result strings.Builder
	for _, character := range id {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
			result.WriteRune(character)
		} else {
			result.WriteByte('_')
		}
	}
	if result.Len() == 0 {
		return "site"
	}
	return result.String()
}

func (s *System) phpPoolConfigPath(site Site) string {
	name := "litecp-" + safeSiteID(site.ID) + ".conf"
	if s.usesPHP73Container(site.PHPVersion) {
		return filepath.Join("/etc/litecp/php73-pools", name)
	}
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		return filepath.Join("/etc/php", site.PHPVersion, "fpm", "pool.d", name)
	}
	compact := strings.ReplaceAll(site.PHPVersion, ".", "")
	if site.PHPVersion == "8.3" && !unitFileExists("php83-php-fpm") && unitFileExists("php-fpm") {
		return filepath.Join("/etc/php-fpm.d", name)
	}
	return filepath.Join("/etc/opt/remi", "php"+compact, "php-fpm.d", name)
}

func (s *System) phpPoolConfig(site Site) string {
	allowed, temporary := site.Root, "/tmp"
	if home := siteHome(site); home != "" {
		allowed = home
		temporary = filepath.Join(home, "tmp")
	}
	webUser := s.webUser()
	socketOwner, socketGroup := webUser, webUser
	if s.usesPHP73Container(site.PHPVersion) {
		// The container remaps www-data to the host Nginx UID/GID at image
		// build time. FPM's listen.owner/listen.group require account names,
		// while the worker user/group directives accept isolated numeric IDs.
		socketOwner, socketGroup = "www-data", "www-data"
	}
	return fmt.Sprintf(`[litecp_%s]
user = %d
group = %d
listen = %s
listen.owner = %s
listen.group = %s
listen.mode = 0660
pm = ondemand
pm.max_children = 5
pm.process_idle_timeout = 10s
pm.max_requests = 300
request_terminate_timeout = 120s
catch_workers_output = yes
clear_env = yes
security.limit_extensions = .php
php_admin_value[open_basedir] = %s:/tmp
php_admin_value[upload_tmp_dir] = %s
php_admin_value[session.save_path] = %s
`, safeSiteID(site.ID), site.RuntimeUID, site.RuntimeGID, s.sitePHPSocket(site), socketOwner, socketGroup, allowed, temporary, temporary) + phpPoolSettings(site.PHPSettings)
}

func (s *System) ensureSitePHPPool(ctx context.Context, site Site) error {
	if s.demo {
		return nil
	}
	s.phpPoolMu.Lock()
	defer s.phpPoolMu.Unlock()
	if site.RuntimeUID == 0 || site.RuntimeGID == 0 {
		return errors.New("website runtime identity is missing")
	}
	configPath := s.phpPoolConfigPath(site)
	if err := os.MkdirAll(filepath.Dir(configPath), 0750); err != nil {
		return err
	}
	desiredConfig := s.phpPoolConfig(site)
	existingConfig, readErr := os.ReadFile(configPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	configUnchanged := readErr == nil && string(existingConfig) == desiredConfig
	if !configUnchanged {
		if err := writeAtomic(configPath, []byte(desiredConfig), 0640); err != nil {
			return err
		}
	}
	if configUnchanged {
		if _, err := os.Stat(s.sitePHPSocket(site)); err == nil {
			return nil
		}
	}
	if s.usesPHP73Container(site.PHPVersion) {
		if err := s.syncPHP73Pools(); err != nil {
			return err
		}
	}
	if err := applyPHPPoolRuntime(ctx, s.phpUnitForVersion(site.PHPVersion), s.usesPHP73Container(site.PHPVersion), runCommand); err != nil {
		return err
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(s.sitePHPSocket(site)); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("isolated PHP socket was not created: %s", s.sitePHPSocket(site))
}

func (s *System) removeSitePHPPool(ctx context.Context, site Site) error {
	if s.demo {
		return nil
	}
	s.phpPoolMu.Lock()
	defer s.phpPoolMu.Unlock()
	configPath := s.phpPoolConfigPath(site)
	if err := os.Remove(configPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if s.usesPHP73Container(site.PHPVersion) {
		if err := s.syncPHP73Pools(); err != nil {
			return err
		}
	}
	return applyPHPPoolRuntime(ctx, s.phpUnitForVersion(site.PHPVersion), s.usesPHP73Container(site.PHPVersion), runCommand)
}

func (s *System) syncPHP73Pools() error {
	directory := "/etc/litecp/php73-pools"
	entries, err := os.ReadDir(directory)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var content strings.Builder
	content.WriteString("; Generated by LiteCP. Do not edit.\n")
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		value, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return err
		}
		content.Write(value)
		content.WriteByte('\n')
	}
	// This pre-created file is a writable systemd bind mount and a read-only
	// container mount. Preserve its inode: atomic rename needs parent-directory
	// access and would leave the running container attached to the old inode.
	return writePHP73MountedConfig("/etc/litecp/php73-pools.conf", []byte(content.String()))
}

func (s *System) ReconcileSites(ctx context.Context, sites []Site) error {
	if s.demo {
		return nil
	}
	if err := s.reconcileTrafficLogging(ctx, sites); err != nil {
		return fmt.Errorf("prepare website analytics logging: %w", err)
	}
	if err := s.reconcileWebsiteCron(sites); err != nil {
		return fmt.Errorf("reconcile custom cron jobs: %w", err)
	}
	for _, site := range sites {
		configPath, _ := s.nginxPaths(site.Domain)
		if err := s.reconcileSiteRuntime(ctx, site, configPath); err != nil {
			return err
		}
	}
	if _, err := runCommand(ctx, 15*time.Second, "nginx", "-t"); err != nil {
		return err
	}
	_, err := runCommand(ctx, 15*time.Second, "systemctl", "reload", "nginx")
	return err
}

func (s *System) reconcileSiteRuntime(ctx context.Context, site Site, configPath string) error {
	if resourceEnabled(site) {
		return s.reconcileSiteNginx(site, configPath)
	}
	if siteHome(site) != "" {
		if err := s.ensureSiteHome(site); err != nil {
			return err
		}
	} else if err := chownSitePath(filepath.Dir(site.Root), site, 0750, s.webUser()); err != nil {
		return fmt.Errorf("secure %s parent ownership: %w", site.Domain, err)
	}
	if err := chownSiteTree(site.Root, site, s.webUser()); err != nil {
		return fmt.Errorf("secure %s ownership: %w", site.Domain, err)
	}
	if site.Application != nil {
		if err := s.reconcileSiteNginx(site, configPath); err != nil {
			return fmt.Errorf("prepare %s application routing: %w", site.Domain, err)
		}
		return nil
	}
	if err := s.ensureSitePHPPool(ctx, site); err != nil {
		return fmt.Errorf("prepare %s PHP pool: %w", site.Domain, err)
	}
	if _, err := os.Stat(configPath); err == nil {
		if err := s.reconcileSiteNginx(site, configPath); err != nil {
			return err
		}
	}
	return nil
}

func (s *System) reconcileSiteNginx(site Site, configPath string) error {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var updated string
	if site.Application != nil {
		app := *site.Application
		if err := updateApplicationCertificates(site, &app, content); err != nil {
			return err
		}
		site.Application = &app
		updated = s.applicationNginx(site)
	} else {
		updated, err = replaceFastCGISocket(string(content), s.sitePHPSocket(site))
		if err != nil {
			return err
		}
	}
	if updated == string(content) {
		return nil
	}
	return writeAtomic(configPath, []byte(updated), 0644)
}

func chownSitePath(path string, site Site, mode os.FileMode, webUser string) error {
	account, err := user.Lookup(webUser)
	if err != nil {
		return err
	}
	groupID, err := strconv.Atoi(account.Gid)
	if err != nil {
		return err
	}
	if err := os.Chown(path, site.RuntimeUID, groupID); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func lookupUserGroupID(username string) (int, error) {
	account, err := user.Lookup(username)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(account.Gid)
}

func (s *System) siteFileOwnership(site Site) (int, int, error) {
	if s.demo {
		return site.RuntimeUID, site.RuntimeGID, nil
	}
	groupID, err := lookupUserGroupID(s.webUser())
	return site.RuntimeUID, groupID, err
}

func chownSiteTree(root string, site Site, webUser string) error {
	account, err := user.Lookup(webUser)
	if err != nil {
		return err
	}
	groupID, err := strconv.Atoi(account.Gid)
	if err != nil {
		return err
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return os.Lchown(path, site.RuntimeUID, groupID)
	})
}

func (s *System) webUser() string {
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		return "www-data"
	}
	return "nginx"
}

func (s *System) installedPHPVersions() []string {
	versions := make([]string, 0, len(supportedPHPVersions))
	for _, version := range supportedPHPVersions {
		if s.phpRuntimeAvailable(version) {
			versions = append(versions, version)
		}
	}
	return versions
}

func (s *System) phpRuntimeAvailable(version string) bool {
	if s.demo {
		return containsString(supportedPHPVersions, version)
	}
	if _, err := os.Stat(s.phpBinary(version)); err != nil {
		return false
	}
	return unitFileExists(s.phpUnitForVersion(version))
}

func (s *System) phpUnits() []string {
	units := []string{}
	seen := map[string]bool{}
	for _, version := range supportedPHPVersions {
		if !s.phpRuntimeAvailable(version) {
			continue
		}
		unit := s.phpUnitForVersion(version)
		if !seen[unit] {
			seen[unit] = true
			units = append(units, unit)
		}
	}
	return units
}

func (s *System) phpStackState(ctx context.Context, sites []Site) string {
	units := s.phpUnits()
	if len(units) == 0 {
		return "not installed"
	}
	states := make([]string, 0, len(units))
	for _, unit := range units {
		states = append(states, s.serviceState(ctx, unit))
	}
	if state := aggregateServiceStates(states); state != "running" {
		return state
	}
	for _, site := range sites {
		info, err := os.Stat(s.sitePHPSocket(site))
		if err != nil || info.Mode()&os.ModeSocket == 0 {
			return "degraded"
		}
	}
	return "running"
}

func (s *System) phpUnitForVersion(version string) string {
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		return "php" + version + "-fpm"
	}
	if version == "7.3" && unitFileExists("litecp-php73-fpm") {
		return "litecp-php73-fpm"
	}
	unit := "php" + strings.ReplaceAll(version, ".", "") + "-php-fpm"
	if version == "8.3" && !unitFileExists(unit) && unitFileExists("php-fpm") {
		return "php-fpm"
	}
	return unit
}

func (s *System) phpSocket(version string) string {
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		return "/run/php/php" + version + "-fpm.sock"
	}
	if s.usesPHP73Container(version) {
		return "/run/litecp-php73/php-fpm.sock"
	}
	compact := strings.ReplaceAll(version, ".", "")
	if version == "8.3" && !unitFileExists("php83-php-fpm") && unitFileExists("php-fpm") {
		return "/run/php-fpm/www.sock"
	}
	return "/var/opt/remi/php" + compact + "/run/php-fpm/www.sock"
}

func (s *System) phpBinary(version string) string {
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		return "/usr/bin/php" + version
	}
	if s.usesPHP73Container(version) {
		return "/usr/local/bin/litecp-php73"
	}
	compact := strings.ReplaceAll(version, ".", "")
	path := "/opt/remi/php" + compact + "/root/usr/bin/php"
	if version == "8.3" {
		if _, err := os.Stat(path); err != nil {
			return "/usr/bin/php"
		}
	}
	return path
}

func (s *System) usesPHP73Container(version string) bool {
	return version == "7.3" && s.os.ID == "almalinux" && unitFileExists("litecp-php73-fpm")
}

func unitFileExists(unit string) bool {
	if !strings.HasSuffix(unit, ".service") && !strings.HasSuffix(unit, ".timer") {
		unit += ".service"
	}
	for _, directory := range []string{"/etc/systemd/system", "/usr/lib/systemd/system", "/lib/systemd/system"} {
		if _, err := os.Stat(filepath.Join(directory, unit)); err == nil {
			return true
		}
	}
	return false
}

func validateSiteOptions(appType, appVersion, phpVersion string) error {
	if appType != "whmcs" && appType != "wordpress" && appType != "regular" {
		return errors.New("application must be WHMCS, WordPress or Regular Website")
	}
	if !containsString(supportedPHPVersions, phpVersion) {
		return errors.New("choose PHP 7.3, 7.4, 8.1, 8.2, 8.3, 8.4 or 8.5")
	}
	if appType == "whmcs" {
		if appVersion != "8.13" && appVersion != "9.0" {
			return errors.New("choose WHMCS 8.13 LTS or WHMCS 9.0")
		}
		if appVersion == "9.0" && phpVersion != "8.2" && phpVersion != "8.3" {
			return errors.New("WHMCS 9.0 requires PHP 8.2 or 8.3")
		}
		if appVersion == "8.13" && (phpVersion == "8.4" || phpVersion == "8.5") {
			return errors.New("WHMCS 8.13 does not support PHP 8.4 or 8.5")
		}
	}
	if appType == "wordpress" && phpVersion == "7.3" {
		return errors.New("current WordPress requires PHP 7.4 or newer")
	}
	return nil
}

func normalizeAppVersion(appType, version string) string {
	if appType != "whmcs" {
		return ""
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return "9.0"
	}
	return version
}

func defaultPHPVersion(appType string) string {
	if appType == "wordpress" || appType == "regular" {
		return "8.4"
	}
	return "8.3"
}

func applicationName(appType string) string {
	if appType == "wordpress" {
		return "WordPress"
	}
	if appType == "regular" {
		return "Regular Website"
	}
	return "WHMCS"
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func (s *System) cronUnit() string {
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		return "cron"
	}
	return "crond"
}

func detectOS() OSInfo {
	info := OSInfo{ID: runtime.GOOS, Name: strings.Title(runtime.GOOS)}
	content, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return info
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(content), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found {
			values[key] = strings.Trim(strings.TrimSpace(value), `"`)
		}
	}
	info.ID = strings.ToLower(values["ID"])
	info.Name = values["NAME"]
	info.Version = values["VERSION_ID"]
	if info.Name == "" {
		info.Name = info.ID
	}
	if info.Version != "" {
		info.Name += " " + info.Version
	}
	return info
}

func memoryUsage() ResourceUsage {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return ResourceUsage{Used: 846 * 1024 * 1024, Total: 2 * 1024 * 1024 * 1024, Percent: 41.3}
	}
	defer file.Close()
	values := map[string]uint64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 {
			value, _ := strconv.ParseUint(fields[1], 10, 64)
			values[strings.TrimSuffix(fields[0], ":")] = value * 1024
		}
	}
	total := values["MemTotal"]
	available := values["MemAvailable"]
	used := total - available
	return usage(used, total)
}

func diskUsage(ctx context.Context) ResourceUsage {
	output, err := runCommand(ctx, 3*time.Second, "df", "-Pk", "/")
	if err != nil {
		return ResourceUsage{Used: 18 * 1024 * 1024 * 1024, Total: 80 * 1024 * 1024 * 1024, Percent: 22.5}
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return ResourceUsage{}
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 5 {
		return ResourceUsage{}
	}
	total, _ := strconv.ParseUint(fields[1], 10, 64)
	used, _ := strconv.ParseUint(fields[2], 10, 64)
	return usage(used*1024, total*1024)
}

func usage(used, total uint64) ResourceUsage {
	percent := 0.0
	if total > 0 {
		percent = float64(used) / float64(total) * 100
	}
	return ResourceUsage{Used: used, Total: total, Percent: percent}
}

func uptimeAndLoad() (uint64, float64) {
	content, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 14*24*60*60 + 8*60*60 + 42*60, 0.18
	}
	fields := strings.Fields(string(content))
	uptime, _ := strconv.ParseFloat(fields[0], 64)
	loadContent, _ := os.ReadFile("/proc/loadavg")
	loadFields := strings.Fields(string(loadContent))
	load := 0.0
	if len(loadFields) > 0 {
		load, _ = strconv.ParseFloat(loadFields[0], 64)
	}
	return uint64(uptime), load
}

func (s *System) cpuUsage() float64 {
	content, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 12.4
	}
	line := strings.SplitN(string(content), "\n", 2)[0]
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return 0
	}
	var total uint64
	values := make([]uint64, 0, len(fields)-1)
	for _, field := range fields[1:] {
		value, _ := strconv.ParseUint(field, 10, 64)
		values = append(values, value)
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	s.cpuMu.Lock()
	defer s.cpuMu.Unlock()
	deltaTotal := total - s.prevAll
	deltaIdle := idle - s.prevIdle
	s.prevAll, s.prevIdle = total, idle
	if deltaTotal == 0 || s.prevAll == deltaTotal {
		return 0
	}
	return (1 - float64(deltaIdle)/float64(deltaTotal)) * 100
}

func primaryIP() string {
	if connection, err := net.DialTimeout("udp", "1.1.1.1:53", time.Second); err == nil {
		if local, ok := connection.LocalAddr().(*net.UDPAddr); ok && local.IP != nil && !local.IP.IsLoopback() {
			_ = connection.Close()
			return local.IP.String()
		}
		_ = connection.Close()
	}
	addresses, _ := net.InterfaceAddrs()
	publicCandidates := make([]string, 0)
	privateCandidates := make([]string, 0)
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.To4() != nil && !ip.IsLoopback() {
			if ip.IsPrivate() || ip.IsLinkLocalUnicast() {
				privateCandidates = append(privateCandidates, ip.String())
			} else {
				publicCandidates = append(publicCandidates, ip.String())
			}
		}
	}
	sort.Strings(publicCandidates)
	if len(publicCandidates) > 0 {
		return publicCandidates[0]
	}
	sort.Strings(privateCandidates)
	if len(privateCandidates) > 0 {
		return privateCandidates[0]
	}
	return "127.0.0.1"
}

func chownWebTree(path, username string) error {
	account, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("look up web user %s: %w", username, err)
	}
	uid, _ := strconv.Atoi(account.Uid)
	gid, _ := strconv.Atoi(account.Gid)
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("set website ownership: %w", err)
	}
	return nil
}

func writeAtomic(path string, content []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".litecp-write-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func runCommand(parent context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	text := strings.TrimSpace(string(output))
	if ctx.Err() == context.DeadlineExceeded {
		return text, fmt.Errorf("%s timed out", name)
	}
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		if len(text) > 1200 {
			text = text[len(text)-1200:]
		}
		return text, fmt.Errorf("%s failed: %s", name, text)
	}
	return text, nil
}

func runCommandInput(parent context.Context, timeout time.Duration, input, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = strings.NewReader(input)
	output, err := command.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if ctx.Err() == context.DeadlineExceeded {
		return text, fmt.Errorf("%s timed out", name)
	}
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		if len(text) > 1200 {
			text = text[len(text)-1200:]
		}
		return text, fmt.Errorf("%s failed: %s", name, text)
	}
	return text, nil
}

func firstAvailableCommand(names ...string) string {
	for _, name := range names {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	return names[0]
}

func validatePHPVersionSwitch(version string) error {
	if !containsString(supportedPHPVersions, version) {
		return errors.New("choose an available PHP version")
	}
	return nil
}

// writePHP73MountedConfig updates only an existing regular file; it never
// creates files in the protected configuration parent directory.
func writePHP73MountedConfig(path string, content []byte) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("PHP 7.3 combined configuration is not a regular file")
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, opened) {
		return errors.New("PHP 7.3 configuration changed while opening")
	}
	original, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	write := func(value []byte) error {
		if _, err := file.WriteAt(value, 0); err != nil {
			return err
		}
		if err := file.Truncate(int64(len(value))); err != nil {
			return err
		}
		return file.Sync()
	}
	if err := write(content); err != nil {
		if recovery := write(original); recovery != nil {
			return fmt.Errorf("write PHP 7.3 configuration: %v; restore previous content: %w", err, recovery)
		}
		return err
	}
	return nil
}
