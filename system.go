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
		Memory: memoryUsage(), Disk: diskUsage(ctx),
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
			item.State = s.serviceState(ctx, item.Unit)
			snapshot.Services[index] = item
		}(i, service)
	}
	wait.Wait()
	return snapshot
}

func (s *System) serviceUnits() []ServiceStatus {
	phpUnit := "php-fpm"
	cronUnit := "crond"
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		phpUnit = "php8.3-fpm"
		cronUnit = "cron"
	}
	return []ServiceStatus{
		{Key: "nginx", Label: "Nginx", Unit: "nginx"},
		{Key: "php", Label: "PHP 8.3 FPM", Unit: phpUnit},
		{Key: "database", Label: "MariaDB", Unit: "mariadb"},
		{Key: "cron", Label: "WHMCS Scheduler", Unit: cronUnit},
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
	id, err := randomToken(8)
	if err != nil {
		return Site{}, nil, err
	}
	site := Site{
		ID: id, Domain: domain, Root: filepath.Join("/var/www/litecp", domain, "public"),
		PHPVersion: "8.3", CreatedAt: time.Now().UTC(),
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
		content := []byte("<!doctype html><meta charset=utf-8><title>LiteCP</title><style>body{font:16px system-ui;max-width:680px;margin:12vh auto;padding:32px;color:#173b46}code{background:#edf6f4;padding:3px 6px;border-radius:5px}</style><h1>Ready for WHMCS</h1><p>Upload your licensed WHMCS files to <code>" + site.Root + "</code>, then remove this file.</p>")
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
	username := name
	if len(username) > 16 {
		username = username[:16]
	}
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
			return fmt.Errorf("disable WHMCS cron: %w", err)
		}
		return nil
	}
	cronScript := filepath.Join(site.Root, "crons", "cron.php")
	content := fmt.Sprintf("SHELL=/bin/sh\nPATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\nMAILTO=\"\"\n*/5 * * * * %s /usr/bin/php -q %s >/dev/null 2>&1\n", s.webUser(), cronScript)
	return writeAtomic(cronPath, []byte(content), 0644)
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
		command = []string{"journalctl", "--no-pager", "-n", "100", "-u", s.phpUnit()}
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
	socket := "/run/php-fpm/www.sock"
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		socket = "/run/php/php8.3-fpm.sock"
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

    location ~ \.php$ {
        try_files $uri =404;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        fastcgi_param HTTP_PROXY "";
        fastcgi_pass unix:%s;
        fastcgi_read_timeout 120s;
    }

    location = /configuration.php { deny all; }
    location ^~ /crons/ { deny all; }
    location ~ /\.(?!well-known) { deny all; }

    location ~* \.(?:css|js|jpg|jpeg|gif|png|svg|ico|webp|woff2?)$ {
        expires 7d;
        add_header Cache-Control "public, no-transform";
        try_files $uri /index.php?$query_string;
        access_log off;
    }
}
`, site.Domain, site.Root, socket)
}

func (s *System) webUser() string {
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		return "www-data"
	}
	return "nginx"
}

func (s *System) phpUnit() string {
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		return "php8.3-fpm"
	}
	return "php-fpm"
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
