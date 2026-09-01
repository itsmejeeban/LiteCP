package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
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
	Domain         string `json:"domain"`
	AppType        string `json:"appType"`
	PHPVersion     string `json:"phpVersion"`
	CreateDatabase bool   `json:"createDatabase"`
	EnableCron     bool   `json:"enableCron"`
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
	Key   string `json:"key"`
	Label string `json:"label"`
	Unit  string `json:"unit"`
	State string `json:"state"`
}

type SystemSnapshot struct {
	OS            string          `json:"os"`
	OSID          string          `json:"osId"`
	Hostname      string          `json:"hostname"`
	IPAddress     string          `json:"ipAddress"`
	UptimeSeconds uint64          `json:"uptimeSeconds"`
	CPUPercent    float64         `json:"cpuPercent"`
	LoadAverage   float64         `json:"loadAverage"`
	Memory        ResourceUsage   `json:"memory"`
	Disk          ResourceUsage   `json:"disk"`
	Services      []ServiceStatus `json:"services"`
	PHPVersions   []string        `json:"phpVersions"`
	Demo          bool            `json:"demo"`
}

type OSInfo struct {
	ID      string
	Name    string
	Version string
}

type System struct {
	dataDir  string
	os       OSInfo
	demo     bool
	cpuMu    sync.Mutex
	prevIdle uint64
	prevAll  uint64
}

var domainPattern = regexp.MustCompile(`^(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+)$`)
var safeDatabasePart = regexp.MustCompile(`[^a-z0-9]+`)
var fastCGIPassPattern = regexp.MustCompile(`(?m)^(\s*fastcgi_pass\s+unix:)[^;]+(;\s*)$`)
var supportedPHPVersions = []string{"7.3", "7.4", "8.1", "8.2", "8.3", "8.4"}

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

func (s *System) Snapshot(ctx context.Context) SystemSnapshot {
	hostname, _ := os.Hostname()
	snapshot := SystemSnapshot{
		OS: s.os.Name, OSID: s.os.ID, Hostname: hostname, IPAddress: primaryIP(), Demo: s.demo,
		Memory: memoryUsage(), Disk: diskUsage(ctx), PHPVersions: s.installedPHPVersions(),
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
				item.State = s.phpStackState(ctx)
			} else {
				item.State = s.serviceState(ctx, item.Unit)
			}
			snapshot.Services[index] = item
		}(i, service)
	}
	wait.Wait()
	return snapshot
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
	return []ServiceStatus{
		{Key: "nginx", Label: "Nginx", Unit: "nginx"},
		{Key: "php", Label: "Multi-PHP FPM", Unit: phpUnitLabel},
		{Key: "database", Label: "MariaDB", Unit: "mariadb"},
		{Key: "cron", Label: "App Scheduler", Unit: cronUnit},
	}
}

func (s *System) serviceState(ctx context.Context, unit string) string {
	if s.demo {
		return "running"
	}
	commandCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(commandCtx, "systemctl", "is-active", unit).Output()
	if err != nil {
		return "stopped"
	}
	state := strings.TrimSpace(string(output))
	if state == "active" {
		return "running"
	}
	return state
}

func (s *System) ServiceAction(ctx context.Context, key, action string) error {
	if action != "start" && action != "restart" {
		return errors.New("only start and restart actions are allowed")
	}
	if key == "php" {
		units := s.phpUnits()
		if len(units) == 0 {
			return errors.New("no PHP-FPM runtime is installed")
		}
		if s.demo {
			return nil
		}
		for _, unit := range units {
			if _, err := runCommand(ctx, 30*time.Second, "systemctl", action, unit); err != nil {
				return err
			}
		}
		return nil
	}
	var unit string
	for _, service := range s.serviceUnits() {
		if service.Key == key {
			unit = service.Unit
			break
		}
	}
	if unit == "" {
		return errors.New("unknown service")
	}
	if s.demo {
		return nil
	}
	_, err := runCommand(ctx, 20*time.Second, "systemctl", action, unit)
	return err
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
	if err := validateSiteOptions(appType, phpVersion); err != nil {
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
		ID: id, Domain: domain, Root: filepath.Join("/var/www/litecp", domain, "public"),
		AppType: appType, PHPVersion: phpVersion, CreatedAt: time.Now().UTC(),
	}
	if s.demo {
		if input.CreateDatabase {
			credentials := demoCredentials(domain)
			site.Database, site.DBUser = credentials.Name, credentials.Username
			return site, &credentials, nil
		}
		return site, nil, nil
	}

	if err := os.MkdirAll(site.Root, 0750); err != nil {
		return Site{}, nil, fmt.Errorf("create website root: %w", err)
	}
	if err := chownWebTree(filepath.Dir(site.Root), s.webUser()); err != nil {
		return Site{}, nil, err
	}
	if err := chownWebTree(site.Root, s.webUser()); err != nil {
		return Site{}, nil, err
	}
	placeholder := filepath.Join(site.Root, "index.html")
	if _, err := os.Stat(placeholder); errors.Is(err, os.ErrNotExist) {
		appName := applicationName(site.AppType)
		guidance := "Upload your licensed WHMCS files"
		if site.AppType == "wordpress" {
			guidance = "Upload WordPress files"
		}
		content := []byte("<!doctype html><meta charset=utf-8><title>LiteCP</title><style>body{font:16px system-ui;max-width:680px;margin:12vh auto;padding:32px;color:#173b46}code{background:#edf6f4;padding:3px 6px;border-radius:5px}</style><h1>Ready for " + appName + "</h1><p>" + guidance + " to <code>" + site.Root + "</code>, then remove this file.</p>")
		if err := os.WriteFile(placeholder, content, 0640); err != nil {
			return Site{}, nil, fmt.Errorf("write placeholder: %w", err)
		}
		_ = chownWebTree(placeholder, s.webUser())
	}

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
		created, err := s.createDatabase(ctx, domain)
		if err != nil {
			rollback()
			_, _ = runCommand(ctx, 10*time.Second, "systemctl", "reload", "nginx")
			return Site{}, nil, err
		}
		site.Database, site.DBUser = created.Name, created.Username
		credentials = &created
	}
	return site, credentials, nil
}

func (s *System) createDatabase(ctx context.Context, domain string) (DBCredentials, error) {
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
	sql := fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci; CREATE USER '%s'@'localhost' IDENTIFIED BY '%s'; GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost'; FLUSH PRIVILEGES;", name, username, password, name, username)
	databaseClient := firstAvailableCommand("mariadb", "mysql")
	if _, err := runCommand(ctx, 20*time.Second, databaseClient, "--protocol=socket", "--user=root", "--execute", sql); err != nil {
		return DBCredentials{}, fmt.Errorf("create MariaDB database: %w", err)
	}
	return DBCredentials{Name: name, Username: username, Password: password, Host: "localhost"}, nil
}

func demoCredentials(domain string) DBCredentials {
	part := strings.Trim(safeDatabasePart.ReplaceAllString(strings.ToLower(domain), "_"), "_")
	if len(part) > 10 {
		part = part[:10]
	}
	return DBCredentials{Name: "cp_" + part, Username: "cp_whmcs", Password: "saved-once-demo-password", Host: "localhost"}
}

func (s *System) EnableSSL(ctx context.Context, site Site, email string) error {
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
	_, err = runCommand(ctx, 90*time.Second, "certbot", "--nginx", "--domain", site.Domain, "--non-interactive", "--agree-tos", "--email", email, "--redirect")
	return err
}

func (s *System) SetCron(site Site, enabled bool) error {
	if !domainPattern.MatchString(site.Domain) {
		return errors.New("website domain is invalid")
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
	content := fmt.Sprintf("SHELL=/bin/sh\nPATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\nMAILTO=\"\"\n*/5 * * * * %s %s -q %s >/dev/null 2>&1\n", s.webUser(), phpBinary, cronScript)
	return writeAtomic(cronPath, []byte(content), 0644)
}

func (s *System) SetPHPVersion(ctx context.Context, site Site, phpVersion string) error {
	phpVersion = strings.TrimSpace(phpVersion)
	if err := validateSiteOptions(site.AppType, phpVersion); err != nil {
		return err
	}
	if !s.phpRuntimeAvailable(phpVersion) {
		return fmt.Errorf("PHP %s is not installed on this server", phpVersion)
	}
	if s.demo {
		return nil
	}
	configPath, _ := s.nginxPaths(site.Domain)
	original, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read nginx configuration: %w", err)
	}
	updated, err := replaceFastCGISocket(string(original), s.phpSocket(phpVersion))
	if err != nil {
		return err
	}
	rollback := func() {
		_ = writeAtomic(configPath, original, 0644)
		_, _ = runCommand(ctx, 15*time.Second, "systemctl", "reload", "nginx")
	}
	if err := writeAtomic(configPath, []byte(updated), 0644); err != nil {
		return fmt.Errorf("update nginx configuration: %w", err)
	}
	if _, err := runCommand(ctx, 10*time.Second, "nginx", "-t"); err != nil {
		rollback()
		return err
	}
	if _, err := runCommand(ctx, 15*time.Second, "systemctl", "reload", "nginx"); err != nil {
		rollback()
		return err
	}
	if site.Cron {
		site.PHPVersion = phpVersion
		if err := s.SetCron(site, true); err != nil {
			rollback()
			return fmt.Errorf("update application cron: %w", err)
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

func (s *System) RemoveSiteConfig(ctx context.Context, site Site) error {
	if !domainPattern.MatchString(site.Domain) {
		return errors.New("website domain is invalid")
	}
	if s.demo {
		return nil
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
	_, err := runCommand(ctx, 15*time.Second, "systemctl", "reload", "nginx")
	return err
}

func (s *System) CreateBackup(ctx context.Context, site Site) (Backup, error) {
	backupID, err := randomToken(8)
	if err != nil {
		return Backup{}, err
	}
	created := time.Now().UTC()
	stamp := created.Format("20060102-150405")
	backupDir := "/var/backups/litecp"
	finalPath := filepath.Join(backupDir, site.Domain+"-"+stamp+".tar.gz")
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
		dumpCommand := firstAvailableCommand("mariadb-dump", "mysqldump")
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
		command = []string{"journalctl", "--no-pager", "-n", "100", "-u", "mariadb"}
	case "cron":
		command = []string{"journalctl", "--no-pager", "-n", "100", "-u", s.cronUnit()}
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

func (s *System) nginxConfig(site Site) string {
	applicationRules := `    location = /configuration.php { deny all; }
    location ^~ /crons/ { deny all; }`
	if site.AppType == "wordpress" {
		applicationRules = `    location = /wp-config.php { deny all; }
    location ~* ^/wp-content/uploads/.*\.php$ { deny all; }`
	}
	return fmt.Sprintf(`# Managed by LiteCP - By Hostingtom
server {
    listen 80;
    listen [::]:80;
    server_name %s;
    root %s;
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
`, site.Domain, site.Root, applicationRules, s.phpSocket(site.PHPVersion))
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

func (s *System) phpStackState(ctx context.Context) string {
	units := s.phpUnits()
	if len(units) == 0 {
		return "stopped"
	}
	for _, unit := range units {
		if s.serviceState(ctx, unit) != "running" {
			return "degraded"
		}
	}
	return "running"
}

func (s *System) phpUnitForVersion(version string) string {
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		return "php" + version + "-fpm"
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
	compact := strings.ReplaceAll(version, ".", "")
	path := "/opt/remi/php" + compact + "/root/usr/bin/php"
	if version == "8.3" {
		if _, err := os.Stat(path); err != nil {
			return "/usr/bin/php"
		}
	}
	return path
}

func unitFileExists(unit string) bool {
	for _, directory := range []string{"/etc/systemd/system", "/usr/lib/systemd/system", "/lib/systemd/system"} {
		if _, err := os.Stat(filepath.Join(directory, unit+".service")); err == nil {
			return true
		}
	}
	return false
}

func validateSiteOptions(appType, phpVersion string) error {
	if appType != "whmcs" && appType != "wordpress" {
		return errors.New("application must be WHMCS or WordPress")
	}
	if !containsString(supportedPHPVersions, phpVersion) {
		return errors.New("choose PHP 7.3, 7.4, 8.1, 8.2, 8.3 or 8.4")
	}
	if appType == "whmcs" && phpVersion == "8.4" {
		return errors.New("WHMCS does not currently support PHP 8.4; choose PHP 7.3 through 8.3")
	}
	if appType == "wordpress" && phpVersion == "7.3" {
		return errors.New("current WordPress requires PHP 7.4 or newer")
	}
	return nil
}

func defaultPHPVersion(appType string) string {
	if appType == "wordpress" {
		return "8.4"
	}
	return "8.3"
}

func applicationName(appType string) string {
	if appType == "wordpress" {
		return "WordPress"
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
	addresses, _ := net.InterfaceAddrs()
	candidates := make([]string, 0)
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.To4() != nil && !ip.IsLoopback() {
			candidates = append(candidates, ip.String())
		}
	}
	sort.Strings(candidates)
	if len(candidates) > 0 {
		return candidates[0]
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
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, content, mode); err != nil {
		return err
	}
	if err := os.Chmod(temporary, mode); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return os.Rename(temporary, path)
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

func firstAvailableCommand(names ...string) string {
	for _, name := range names {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	return names[0]
}
