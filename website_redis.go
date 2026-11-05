package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Redis is deliberately cache-only. Sessions and durable queue data must not
// share an evicting instance or be silently discarded by a cache restart.
type WebsiteRedis struct {
	Enabled   bool `json:"enabled"`
	MemoryMiB int  `json:"memoryMiB"`
}
type redisPlan struct {
	Site     Site         `json:"site"`
	Settings WebsiteRedis `json:"settings"`
	Action   string       `json:"action"`
}

func redisUnit(id string) string      { return fmt.Sprintf("litecpredis%x.service", []byte(id)) }
func redisSocket(id string) string    { return fmt.Sprintf("/run/litecp-redis-%x/redis.sock", []byte(id)) }
func redisDirectory(id string) string { return fmt.Sprintf("/var/lib/litecp-redis/%x", []byte(id)) }
func redisEnabled(site Site) bool     { return site.Redis != nil && site.Redis.Enabled }
func redisBinary() string {
	for _, name := range []string{"redis-server", "valkey-server"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}
func redisCLI() string {
	for _, name := range []string{"redis-cli", "valkey-cli"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}
func validateRedisPlan(p redisPlan) error {
	if !domainPattern.MatchString(p.Site.Domain) || p.Site.ID == "" || !websiteCronID.MatchString(p.Site.ID) || len(redisSocket(p.Site.ID)) > 104 || p.Site.RuntimeUID != siteRuntimeID(p.Site.ID) || p.Site.RuntimeGID != p.Site.RuntimeUID {
		return fmt.Errorf("website runtime identity is not ready")
	}
	switch p.Action {
	case "status", "save", "restart":
	default:
		return fmt.Errorf("unsupported Redis action")
	}
	if p.Settings.MemoryMiB < 16 || p.Settings.MemoryMiB > 65536 {
		return fmt.Errorf("Redis memory must be between 16 and 65536 MiB")
	}
	if p.Settings.Enabled && p.Action != "status" {
		if p.Site.Application != nil || p.Site.PHPVersion == "7.3" {
			return fmt.Errorf("Redis sockets currently support native PHP/static websites only")
		}
		if p.Site.Suspended {
			return fmt.Errorf("unsuspend the website before starting Redis")
		}
		if resourceEnabled(p.Site) && int64(p.Settings.MemoryMiB+64) >= p.Site.ResourceLimits.MemoryMiB {
			return fmt.Errorf("Redis needs its cache allowance plus 64 MiB overhead below the website RAM limit; leave additional RAM for PHP and HTTP workers")
		}
	}
	return nil
}
func redisConfig(p redisPlan) string {
	return fmt.Sprintf("port 0\nunixsocket %s\nunixsocketperm 600\ndaemonize no\nsupervised no\nlogfile \"\"\nsave \"\"\nappendonly no\nmaxmemory %dmb\nmaxmemory-policy allkeys-lru\nmaxclients 128\nprotected-mode yes\nuser default on nopass ~* &* +@all -config -module -acl -debug -shutdown -save -bgsave -bgrewriteaof -replicaof -slaveof\n", redisSocket(p.Site.ID), p.Settings.MemoryMiB)
}
func redisService(p redisPlan, binary string) string {
	slice := ""
	if resourceEnabled(p.Site) {
		slice = "Slice=" + resourceSliceName(p.Site.ID) + "\n"
	}
	// DynamicUser supports numeric IDs without passwd entries. Execute the vendor
	// binary directly so SELinux enters redis_t; no root cache worker is needed.
	return fmt.Sprintf("[Unit]\nDescription=LiteCP website Redis cache\nAfter=local-fs.target\n[Service]\nType=simple\nDynamicUser=yes\nUser=%d\nGroup=%d\nRuntimeDirectory=litecp-redis-%x\nRuntimeDirectoryMode=0700\nExecStart=%s %s/redis.conf\nRestart=no\nUMask=0077\nNoNewPrivileges=yes\nPrivateTmp=yes\nProtectSystem=strict\nProtectHome=yes\nProtectKernelTunables=yes\nProtectKernelModules=yes\nProtectControlGroups=yes\nRestrictAddressFamilies=AF_UNIX\nCapabilityBoundingSet=\nTasksMax=32\nMemoryMax=%dM\nMemorySwapMax=0\n%s[Install]\nWantedBy=multi-user.target\n", p.Site.RuntimeUID, p.Site.RuntimeGID, []byte(p.Site.ID), binary, redisDirectory(p.Site.ID), p.Settings.MemoryMiB+64, slice)
}
func redisRuntimeCommand(args []string) (result error) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 || len(args) != 1 {
		return fmt.Errorf("Redis management requires the root Linux executor")
	}
	b, err := base64.RawURLEncoding.DecodeString(args[0])
	if err != nil || len(b) > 65536 {
		return fmt.Errorf("invalid Redis plan")
	}
	var p redisPlan
	if json.Unmarshal(b, &p) != nil {
		return fmt.Errorf("invalid Redis plan")
	}
	if err = validateRedisPlan(p); err != nil {
		return err
	}
	unlock, err := resourceRuntimeLock(p.Site.ID, false)
	if err != nil {
		return err
	}
	defer unlock()
	if p.Action == "status" {
		return redisPrintStatus(p)
	}
	if !p.Settings.Enabled {
		if _, e := os.Stat(filepath.Join("/etc/systemd/system", redisUnit(p.Site.ID))); os.IsNotExist(e) {
			return redisPrintStatus(p)
		}
		if err = resourceRun("systemctl", "disable", "--now", redisUnit(p.Site.ID)); err != nil {
			return err
		}
		return redisPrintStatus(p)
	}
	if redisBinary() == "" || redisCLI() == "" {
		return fmt.Errorf("install Redis server and CLI through the OS package manager first")
	}
	if warning := redisPHPPolicyWarning(); warning != "" {
		return fmt.Errorf("%s", warning)
	}
	if resourceEnabled(p.Site) {
		st := resourceStatusForSite(p.Site)
		if st.State != "Active" || !st.Enforced {
			return fmt.Errorf("recover the website resource boundary before starting Redis")
		}
	}
	if p.Action == "save" && p.Site.Redis != nil && *p.Site.Redis == p.Settings && redisStatus(p)["running"] == true {
		return redisPrintStatus(p)
	}
	// Preserve the previous instance configuration if health verification fails.
	configPath := filepath.Join(redisDirectory(p.Site.ID), "redis.conf")
	unitPath := filepath.Join("/etc/systemd/system", redisUnit(p.Site.ID))
	previousConfig, configErr := os.ReadFile(configPath)
	previousUnit, unitErr := os.ReadFile(unitPath)
	wasRunning := redisStatus(p)["running"] == true
	enabledOut, _ := exec.Command("systemctl", "is-enabled", redisUnit(p.Site.ID)).Output()
	wasEnabled := strings.TrimSpace(string(enabledOut)) == "enabled"
	defer func() {
		if result != nil {
			var failures []string
			check := func(e error) {
				if e != nil {
					failures = append(failures, e.Error())
				}
			}
			check(resourceRun("systemctl", "disable", "--now", redisUnit(p.Site.ID)))
			if configErr == nil {
				check(writeAtomic(configPath, previousConfig, 0640))
				check(os.Chown(configPath, 0, p.Site.RuntimeGID))
			}
			if unitErr == nil {
				check(writeAtomic(unitPath, previousUnit, 0644))
				check(resourceRun("systemctl", "daemon-reload"))
				if wasEnabled {
					check(resourceRun("systemctl", "enable", redisUnit(p.Site.ID)))
				}
				if wasRunning {
					check(resourceRun("systemctl", "start", redisUnit(p.Site.ID)))
				}
			}
			if len(failures) > 0 {
				result = fmt.Errorf("%w; rollback: %s", result, strings.Join(failures, "; "))
			}
		}
	}()
	if err = os.MkdirAll("/var/lib/litecp-redis", 0755); err != nil {
		return err
	}
	if err = os.Chmod("/var/lib/litecp-redis", 0755); err != nil {
		return err
	}
	dir := redisDirectory(p.Site.ID)
	if err = os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	if err = os.Chmod(dir, 0750); err != nil {
		return err
	}
	if err = os.Chown(dir, 0, p.Site.RuntimeGID); err != nil {
		return err
	}
	runtimeDir := filepath.Dir(redisSocket(p.Site.ID))
	if err = os.MkdirAll(runtimeDir, 0700); err != nil {
		return err
	}
	if err = os.Chmod(runtimeDir, 0700); err != nil {
		return err
	}
	if err = os.Chown(runtimeDir, p.Site.RuntimeUID, p.Site.RuntimeGID); err != nil {
		return err
	}
	// Keep the configuration outside the website-owned tree.
	if err = writeAtomic(filepath.Join(dir, "redis.conf"), []byte(redisConfig(p)), 0640); err != nil {
		return err
	}
	if err = os.Chown(filepath.Join(dir, "redis.conf"), 0, p.Site.RuntimeGID); err != nil {
		return err
	}
	if err = writeAtomic(filepath.Join("/etc/systemd/system", redisUnit(p.Site.ID)), []byte(redisService(p, redisBinary())), 0644); err != nil {
		return err
	}
	if enforce, e := exec.Command("getenforce").Output(); e == nil && strings.TrimSpace(string(enforce)) != "Disabled" {
		for _, item := range [][2]string{{"/var/lib/litecp-redis(/.*)?", "redis_conf_t"}, {"/run/litecp-redis-[0-9a-f]+(/.*)?", "redis_var_run_t"}} {
			if e = resourceRun("semanage", "fcontext", "-a", "-t", item[1], item[0]); e != nil {
				if e = resourceRun("semanage", "fcontext", "-m", "-t", item[1], item[0]); e != nil {
					return fmt.Errorf("Redis SELinux labels: %w", e)
				}
			}
		}
		if err = resourceRun("restorecon", "-R", "/var/lib/litecp-redis", runtimeDir); err != nil {
			return err
		}
	}
	if err = resourceRun("systemctl", "daemon-reload"); err != nil {
		return err
	}
	bootAction := "enable"
	if resourceEnabled(p.Site) {
		bootAction = "disable"
	}
	if err = resourceRun("systemctl", bootAction, redisUnit(p.Site.ID)); err != nil {
		return err
	}
	if err = resourceRun("systemctl", "restart", redisUnit(p.Site.ID)); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	for ctx.Err() == nil {
		out, e := runCommand(ctx, time.Second, redisCLI(), "-s", redisSocket(p.Site.ID), "PING")
		if e == nil && strings.TrimSpace(out) == "PONG" {
			if err = redisPreparePHP(p.Site); err != nil {
				return err
			}
			return redisPrintStatus(p)
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = resourceRun("systemctl", "disable", "--now", redisUnit(p.Site.ID))
	return fmt.Errorf("Redis health check failed; inspect journalctl -u %s", redisUnit(p.Site.ID))
}
func redisStatus(p redisPlan) map[string]any {
	out := map[string]any{"installed": redisBinary() != "" && redisCLI() != "", "running": false, "socket": redisSocket(p.Site.ID), "mode": "Cache only", "settings": p.Settings, "phpWarning": redisPHPPolicyWarning(), "metrics": map[string]string{}}
	if redisCLI() != "" {
		raw, err := runCommand(context.Background(), 3*time.Second, redisCLI(), "-s", redisSocket(p.Site.ID), "INFO")
		if err == nil && !strings.HasPrefix(raw, "Could not") {
			metrics := map[string]string{}
			allowed := map[string]bool{"used_memory_human": true, "used_memory_rss_human": true, "maxmemory_human": true, "connected_clients": true, "keyspace_hits": true, "keyspace_misses": true, "evicted_keys": true, "uptime_in_seconds": true, "redis_version": true, "valkey_version": true}
			for _, line := range strings.Split(raw, "\n") {
				kv := strings.SplitN(strings.TrimSpace(line), ":", 2)
				if len(kv) == 2 && allowed[kv[0]] {
					metrics[kv[0]] = kv[1]
				}
			}
			out["running"] = metrics["redis_version"] != ""
			out["metrics"] = metrics
		}
	}
	return out
}
func redisPHPPolicyWarning() string {
	out, err := exec.Command("getenforce").Output()
	if err != nil || strings.TrimSpace(string(out)) != "Enforcing" {
		return ""
	}
	out, err = exec.Command("getsebool", "httpd_can_network_redis").Output()
	if err == nil && strings.HasSuffix(strings.TrimSpace(string(out)), "off") {
		return "SELinux httpd_can_network_redis is off. Administrator approval is required before enabling this shared Redis access policy; PHP socket access has not been verified."
	}
	return ""
}
func redisPrintStatus(p redisPlan) error {
	b, err := json.Marshal(redisStatus(p))
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
func (s *System) redisHostAction(ctx context.Context, site Site, settings WebsiteRedis, action string) (map[string]any, error) {
	p := redisPlan{site, settings, action}
	if err := validateRedisPlan(p); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(p)
	raw, err := s.applicationHostCommand(ctx, time.Minute, "/usr/local/bin/litecp", "website-redis", base64.RawURLEncoding.EncodeToString(b))
	if err != nil {
		return nil, fmt.Errorf("Redis operation failed: %s", strings.TrimSpace(raw))
	}
	var result map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &result) != nil {
		return nil, fmt.Errorf("invalid Redis status response")
	}
	return result, nil
}
func (a *App) websiteRedis(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "PUT" && r.Method != "POST" {
		writeError(w, 405, "Use GET, PUT or POST")
		return
	}
	site := a.store.SiteByID(r.URL.Query().Get("site"))
	if site == nil {
		writeError(w, 404, "Website not found")
		return
	}
	settings := WebsiteRedis{MemoryMiB: 64}
	if site.Redis != nil {
		settings = *site.Redis
	}
	action := "status"
	if r.Method == "PUT" {
		if decodeJSONWithLimit(r, &settings, 4096) != nil {
			writeError(w, 400, "Invalid Redis settings")
			return
		}
		action = "save"
	}
	if r.Method == "POST" {
		if !settings.Enabled {
			writeError(w, 409, "Enable Redis first")
			return
		}
		action = "restart"
	}
	if a.system == nil || a.system.demo || runtime.GOOS != "linux" {
		writeError(w, 409, "Redis requires the installed Linux host")
		return
	}
	// Status and disabling remain available even when the website is suspended.
	validation := redisPlan{*site, settings, action}
	if action == "status" {
		validation.Settings.Enabled = false
	}
	if err := validateRedisPlan(validation); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if action == "status" {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, redisStatus(redisPlan{*site, settings, action}))
		return
	}
	result, err := a.system.redisHostAction(r.Context(), *site, settings, action)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	if action == "save" {
		old := WebsiteRedis{MemoryMiB: 64}
		if site.Redis != nil {
			old = *site.Redis
		}
		if err = a.store.UpdateSite(site.ID, func(s *Site) { s.Redis = &settings }); err != nil {
			_, rollback := a.system.redisHostAction(context.Background(), *site, old, "save")
			writeError(w, 500, fmt.Sprintf("Could not save Redis settings; rollback: %v", rollback))
			return
		}
		_ = a.store.AddActivity("Redis cache settings saved", site.Domain, "info")
	}
	writeJSON(w, 200, result)
}

func redisResumeForResource(p resourceRuntimePlan) error {
	current, err := resourceSavedSite(p.DataDir, p.Site.ID)
	if err != nil {
		return err
	}
	if current != nil && redisEnabled(*current) {
		unit := redisUnit(current.ID)
		if err = resourceRun("systemctl", "reset-failed", unit); err != nil {
			return err
		}
		return resourceRun("systemctl", "start", unit)
	}
	return nil
}

func redisPreparePHP(site Site) error {
	s := NewSystem("/var/lib/litecp")
	if !resourceEnabled(site) {
		return s.ensureSitePHPPool(context.Background(), site)
	}
	file := filepath.Join(resourceDirectory(site.ID), "php.conf")
	previous, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	php, _, _ := resourceNativeConfigs(s, resourceRuntimePlan{Site: site, Policy: *site.ResourceLimits})
	if err = writeAtomic(file, []byte(php), 0644); err != nil {
		return err
	}
	if err = resourceRun(resourcePHPExecutable(s, site), "-t", "-y", file); err != nil {
		_ = writeAtomic(file, previous, 0644)
		return err
	}
	if err = resourceRun("systemctl", "kill", "--kill-whom=main", "--signal=USR2", resourceUnit(site.ID, "php")); err != nil {
		_ = writeAtomic(file, previous, 0644)
		return err
	}
	return nil
}
