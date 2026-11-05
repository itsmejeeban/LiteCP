package main

// Resource runtime plans are deliberately separate from Site settings. A saved
// policy is not evidence that Linux accepted it; only the host executor writes
// verified runtime state. The public web process cannot invoke this executor.
import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const resourceRuntimeBase = "/var/lib/litecp-resources"

type resourceRuntimePlan struct {
	Site    Site                  `json:"site"`
	DataDir string                `json:"dataDir"`
	Policy  WebsiteResourceLimits `json:"policy"`
}
type resourceRuntimeStatus struct {
	resourcePolicyState
	Enforced    bool           `json:"enforced"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	Sample      resourceSample `json:"sample"`
	CPUPercent  *float64       `json:"cpuPercent,omitempty"`
	MemoryBytes int64          `json:"memoryBytes"`
	Processes   int            `json:"processes"`
	Error       string         `json:"error,omitempty"`
}

func resourceDirectory(id string) string {
	return filepath.Join(resourceRuntimeBase, fmt.Sprintf("%x", []byte(id)))
}
func resourceUnit(id, kind string) string {
	return fmt.Sprintf("litecpresource%x-%s.service", []byte(id), kind)
}
func resourceRuntimeExecutable() string {
	path, err := os.Executable()
	if err != nil {
		return "/usr/local/bin/litecp"
	}
	return path
}
func resourceSocket(id, kind string) string {
	return filepath.Join(resourceDirectory(id), kind+".sock")
}
func resourceRuntimeRead(id string) resourceRuntimeStatus {
	var state resourceRuntimeStatus
	b, err := os.ReadFile(filepath.Join(resourceDirectory(id), "status.json"))
	if err != nil || json.Unmarshal(b, &state) != nil {
		state.State = "Disabled"
	}
	return state
}
func resourceSavedSite(dataDir, id string) (*Site, error) {
	f, err := os.Open(filepath.Join(dataDir, "config.json"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var config struct {
		Sites []Site `json:"sites"`
	}
	if err = json.NewDecoder(io.LimitReader(f, 64<<20)).Decode(&config); err != nil {
		return nil, err
	}
	for i := range config.Sites {
		if config.Sites[i].ID == id {
			return &config.Sites[i], nil
		}
	}
	return nil, nil
}
func resourceRuntimeValidate(plan resourceRuntimePlan) error {
	s := plan.Site
	if err := plan.Policy.validate(); err != nil {
		return err
	}
	if len(s.ID) == 0 || len(resourceSocket(s.ID, "http")) > 104 || len(s.ID) > 96 || !websiteCronID.MatchString(s.ID) || !domainPattern.MatchString(s.Domain) || siteHome(s) == "" || s.RuntimeUID != siteRuntimeID(s.ID) || s.RuntimeGID != siteRuntimeID(s.ID) {
		return fmt.Errorf("website requires a managed home and isolated runtime identity")
	}
	// Refuse execution paths which are not yet included in this executor, rather
	// than quietly running them outside the cgroup.
	if plan.Policy.ReadMiBPerSecond != 0 || plan.Policy.WriteMiBPerSecond != 0 {
		return fmt.Errorf("disk I/O bandwidth enforcement is not yet verified by this executor; leave both rates at zero")
	}
	if s.Application != nil {
		return fmt.Errorf("disable application hosting before enabling this native PHP/static resource boundary")
	}
	if s.PHPVersion == "7.3" {
		return fmt.Errorf("PHP 7.3 compatibility-container resource isolation is not available; choose a native PHP runtime first")
	}
	if !path.IsAbs(plan.DataDir) || strings.ContainsAny(plan.DataDir, "\x00\r\n\"%") {
		return fmt.Errorf("invalid panel data directory")
	}
	if err := validatePHPVersionSwitch(s.PHPVersion); err != nil {
		return err
	}
	return nil
}
func resourcePHPExecutable(s *System, site Site) string {
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		return "/usr/sbin/php-fpm" + site.PHPVersion
	}
	p := "/opt/remi/php" + strings.ReplaceAll(site.PHPVersion, ".", "") + "/root/usr/sbin/php-fpm"
	if site.PHPVersion == "8.3" {
		if _, err := os.Stat(p); err != nil {
			return "/usr/sbin/php-fpm"
		}
	}
	return p
}
func resourceNativeConfigs(s *System, p resourceRuntimePlan) (php, web, front string) {
	return resourceNativeConfigsAt(s, p, resourceDirectory(p.Site.ID))
}
func resourceNativeConfigsAt(s *System, p resourceRuntimePlan, dir string) (php, web, front string) {
	site := p.Site
	temporary := resourceWebTemporary(site, dir)
	pool := strings.ReplaceAll(s.phpPoolConfig(site), s.sitePHPSocket(site), filepath.Join(dir, "php.sock"))
	pool = strings.ReplaceAll(pool, "pm.max_children = 5", fmt.Sprintf("pm.max_children = %d", p.Policy.ConcurrentRequests))
	logPath := filepath.Join(dir, "php-error.log")
	if siteHome(site) != "" {
		logPath = filepath.Join(resourceProtectedStorage(site), "php-error.log")
	}
	if home := siteHome(site); home != "" {
		pool = strings.ReplaceAll(pool, filepath.Join(home, "tmp"), "/tmp")
	}
	php = "[global]\ndaemonize = no\nerror_log = " + logPath + "\npid = " + dir + "/php.pid\n" + pool
	// Reuse application-specific source-file restrictions and PHP routing in a
	// private Nginx instance; only its dedicated master/workers enter the slice.
	private := site
	private.ResourceLimits = nil
	private.Suspended = false
	inner := s.nginxConfig(private)
	inner = strings.ReplaceAll(inner, "include fastcgi_params;", "include /etc/nginx/fastcgi_params;")
	inner = strings.ReplaceAll(inner, "    listen 80;", "    listen unix:"+filepath.Join(dir, "http.sock")+";")
	inner = strings.ReplaceAll(inner, "    listen [::]:80;", "")
	inner = strings.ReplaceAll(inner, s.sitePHPSocket(site), filepath.Join(dir, "php.sock"))
	inner = strings.ReplaceAll(inner, wafBlock(private, private.WAFMode), "")
	// WAF and access collection stay on the public edge, not the inner worker.
	if logs := siteLogDirectives(private); logs != "" {
		inner = strings.ReplaceAll(inner, logs, "access_log off;")
	}
	var clean []string
	for _, line := range strings.Split(inner, "\n") {
		if !strings.Contains(line, "litecp_traffic") {
			clean = append(clean, line)
		}
	}
	inner = strings.Join(clean, "\n")
	inner = strings.Replace(inner, "server {", fmt.Sprintf("server {\n limit_conn website %d;", p.Policy.ConcurrentRequests), 1)
	inner = strings.Replace(inner, "server {", "server {\n set_real_ip_from unix:;\n real_ip_header X-Real-IP;", 1)
	web = fmt.Sprintf("daemon off;\nmaster_process on;\nuser %s;\nworker_processes 1;\npid %s/nginx.pid;\nerror_log stderr;\nevents { worker_connections %d; }\nhttp { include /etc/nginx/mime.types; include /etc/nginx/fastcgi_params; access_log off; limit_conn_zone $server_name zone=website:1m; client_body_temp_path %s/body; fastcgi_temp_path %s/fastcgi; proxy_temp_path %s/proxy;\n%s\n}\n", s.webUser(), dir, p.Policy.ConcurrentRequests+32, temporary, temporary, temporary, inner)
	// Public routing never serves static files or executes PHP itself.
	front = fmt.Sprintf("# LiteCP enforced website resource boundary\nserver {\n listen 80;\n listen [::]:80;\n server_name %s;\n%s\n%s\n if (-f %s/blocked) { return 503 'This website is temporarily unavailable due to resource limits. Please try again later.'; }\n location / { proxy_pass http://unix:%s:; proxy_set_header Host $host; proxy_set_header X-Real-IP $remote_addr; proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for; proxy_set_header X-Forwarded-Proto $scheme; proxy_read_timeout 120s; proxy_intercept_errors on; error_page 502 503 =503 @resource_unavailable; }\n location @resource_unavailable { return 503 'This website is temporarily unavailable due to resource limits. Please try again later.'; }\n}\n", site.Domain, wafBlock(site, site.WAFMode), siteLogDirectives(site), dir, filepath.Join(dir, "http.sock"))
	front = suspensionSiteConfig(front, site, site.Suspended)
	front = strings.Replace(front, "server {", "server {\n add_header X-LiteCP-Resource-Boundary "+fmt.Sprintf("%x", []byte(site.ID))+" always;", 1)
	return
}
func resourceServiceUnit(p resourceRuntimePlan, kind, command string) string {
	return fmt.Sprintf("[Unit]\nDescription=LiteCP website %s boundary\nRequires=%s\nAfter=%s\n[Service]\nType=simple\nSlice=%s\nExecStart=%s\nKillMode=control-group\nOOMPolicy=kill\nRestart=no\nTimeoutStopSec=10\nNoNewPrivileges=yes\nCapabilityBoundingSet=~CAP_SYS_RESOURCE\nPrivateTmp=yes\nBindPaths=%s/tmp:/tmp %s/tmp:/var/tmp\nUMask=0007\n", kind, resourceSliceName(p.Site.ID), resourceSliceName(p.Site.ID), resourceSliceName(p.Site.ID), command, resourceProtectedStorage(p.Site), resourceProtectedStorage(p.Site))
}
func resourceProtectedStorage(site Site) string {
	return filepath.Join(siteStorageBase, ".litecp-resource-runtime", fmt.Sprintf("%x", []byte(site.ID)))
}
func resourceWebTemporary(site Site, fallback string) string {
	if home := siteHome(site); home != "" {
		return filepath.Join(resourceProtectedStorage(site), "web")
	}
	return fallback
}
func resourceHostAction(ctx context.Context, s *System, site Site, p WebsiteResourceLimits, action string) error {
	if runtime.GOOS != "linux" || s == nil || s.demo {
		return fmt.Errorf("resource enforcement requires the installed Linux host")
	}
	plan := resourceRuntimePlan{Site: site, Policy: p, DataDir: s.dataDir}
	if err := resourceRuntimeValidate(plan); err != nil {
		return err
	}
	b, _ := json.Marshal(plan)
	_, err := s.applicationHostCommand(ctx, 3*time.Minute, "/usr/local/bin/litecp", "resource-runtime", action, base64.RawURLEncoding.EncodeToString(b))
	return err
}
func resourceRuntimeCommand(args []string) error {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return fmt.Errorf("resource runtime requires the root host executor")
	}
	if len(args) != 2 {
		return fmt.Errorf("resource runtime requires an action and plan")
	}
	b, err := base64.RawURLEncoding.DecodeString(args[1])
	if err != nil || len(b) > 64<<10 {
		return fmt.Errorf("invalid resource plan")
	}
	var p resourceRuntimePlan
	if json.Unmarshal(b, &p) != nil {
		return fmt.Errorf("invalid resource plan")
	}
	if err := resourceRuntimeValidate(p); err != nil {
		return err
	}
	if (args[0] == "apply" || args[0] == "watch") && !p.Policy.Enabled {
		return fmt.Errorf("an enabled policy is required for resource activation")
	}
	unlock, err := resourceRuntimeLock(p.Site.ID, args[0] == "watch")
	if err != nil {
		return err
	}
	defer unlock()
	s := NewSystem(p.DataDir)
	switch args[0] {
	case "apply":
		return resourceRuntimeApply(s, p)
	case "disable":
		return resourceRuntimeDisable(s, p)
	case "retry":
		st := resourceRuntimeRead(p.Site.ID)
		st.NextRecovery = time.Now()
		return resourceRuntimeSave(p.Site.ID, st)
	case "watch":
		return resourceRuntimeWatch(s, p)
	}
	return fmt.Errorf("unknown resource runtime action")
}
func resourceRuntimeSave(id string, st resourceRuntimeStatus) error {
	st.UpdatedAt = time.Now()
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(resourceDirectory(id), "status.json"), b, 0644)
}
func resourceRun(name string, args ...string) error {
	_, err := runCommand(context.Background(), 45*time.Second, name, args...)
	return err
}
func resourceRuntimeGate(id string, blocked bool) error {
	file := filepath.Join(resourceDirectory(id), "blocked")
	if blocked {
		return writeAtomic(file, []byte("Resource limits: temporarily unavailable\n"), 0644)
	}
	// This is a runtime gate created by this feature, not website data.
	if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
func resourceRuntimeStart(p resourceRuntimePlan) error {
	for _, kind := range []string{"php", "http"} {
		path := resourceSocket(p.Site.ID, kind)
		if info, err := os.Lstat(path); err == nil {
			if info.Mode()&os.ModeSocket == 0 {
				return fmt.Errorf("runtime socket path contains a non-socket entry")
			}
			conn, e := net.DialTimeout("unix", path, 100*time.Millisecond)
			if e == nil {
				conn.Close()
				continue
			}
			// Retain stale runtime socket entries rather than overwriting data.
			if err = os.Rename(path, path+".stale-"+time.Now().UTC().Format("20060102T150405.000000000")); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if err := resourceRun("systemctl", "reset-failed", resourceUnit(p.Site.ID, "php"), resourceUnit(p.Site.ID, "web")); err != nil {
		return err
	}
	if err := resourceRun("systemctl", "start", resourceSliceName(p.Site.ID)); err != nil {
		return err
	}
	if err := resourceSetOOMGroup(p.Site.ID); err != nil {
		return err
	}
	if err := resourceRun("systemctl", "start", resourceUnit(p.Site.ID, "php"), resourceUnit(p.Site.ID, "web")); err != nil {
		return err
	}
	// systemd may reset the slice OOM grouping while attaching child services.
	return resourceSetOOMGroup(p.Site.ID)
}
func resourceRuntimeStop(p resourceRuntimePlan) error {
	// Stop cron descendants and all worker processes as one website boundary.
	if err := resourceKillCgroup(p.Site.ID); err != nil {
		return err
	}
	return resourceRun("systemctl", "stop", resourceSliceName(p.Site.ID))
}
func resourceHealth(id, domain string) error {
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", resourceSocket(id, "http"))
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		r, err := client.Get("http://" + domain + "/")
		if err == nil {
			r.Body.Close()
			if r.StatusCode < 500 {
				return nil
			}
			return fmt.Errorf("website health returned HTTP %d", r.StatusCode)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("isolated website worker did not become healthy")
}

func resourceSecureSockets(s *System, id string) error {
	account, err := user.Lookup(s.webUser())
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return err
	}
	for _, kind := range []string{"http", "php"} {
		path := resourceSocket(id, kind)
		var info os.FileInfo
		deadline := time.Now().Add(8 * time.Second)
		for {
			info, err = os.Lstat(path)
			if err == nil {
				break
			}
			if !os.IsNotExist(err) || time.Now().After(deadline) {
				return fmt.Errorf("wait for %s worker socket: %w", kind, err)
			}
			time.Sleep(50 * time.Millisecond)
		}
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("private worker socket is not a socket")
		}
		if err = os.Chown(path, 0, gid); err != nil {
			return err
		}
		if err = os.Chmod(path, 0660); err != nil {
			return err
		}
	}
	return nil
}

func resourceWaitFrontend(site Site, limited bool) error {
	return resourceWaitFrontendAt(site, limited, "http://127.0.0.1/")
}
func resourceWaitFrontendAt(site Site, limited bool, endpoint string) error {
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		req.Host = site.Domain
		response, err := client.Do(req)
		if err == nil {
			marker := response.Header.Get("X-LiteCP-Resource-Boundary")
			response.Body.Close()
			if limited && marker == fmt.Sprintf("%x", []byte(site.ID)) {
				return nil
			}
			if !limited && marker == "" && (response.StatusCode < 500 || site.Suspended && response.StatusCode == 503) {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("Nginx has not confirmed the requested resource routing transition")
}

func resourceRuntimeApply(s *System, p resourceRuntimePlan) (result error) {
	caps := inspectResourceCapabilities(p.Site)
	if !caps.Ready {
		return fmt.Errorf("resource prerequisites: %s", strings.Join(caps.Prerequisites, "; "))
	}
	dir := resourceDirectory(p.Site.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	existing := resourceRuntimeRead(p.Site.ID)
	if existing.Enforced {
		return fmt.Errorf("disable the current resource boundary before changing its limits")
	}
	configPath, _ := s.nginxPaths(p.Site.Domain)
	original, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	if err = writeAtomic(filepath.Join(dir, "previous-nginx.conf"), original, 0600); err != nil {
		return err
	}
	if err = resourceRuntimeGate(p.Site.ID, true); err != nil {
		return err
	}
	poolOriginal, poolReadErr := os.ReadFile(s.phpPoolConfigPath(p.Site))
	if poolReadErr != nil {
		return poolReadErr
	}
	quotaChanged, watchTouched := false, false
	recovery := filepath.Join(dir, "recovery-"+time.Now().UTC().Format("20060102T150405.000000000"))
	if err = os.Mkdir(recovery, 0700); err != nil {
		return err
	}
	if err = writeAtomic(filepath.Join(recovery, "nginx.conf"), original, 0600); err != nil {
		return err
	}
	if err = writeAtomic(filepath.Join(recovery, "pool.conf"), poolOriginal, 0600); err != nil {
		return err
	}
	defer func() {
		if result != nil {
			var failures []string
			check := func(label string, err error) {
				if err != nil {
					failures = append(failures, label+": "+err.Error())
				}
			}
			if watchTouched {
				check("stop monitor", resourceRun("systemctl", "disable", "--now", resourceUnit(p.Site.ID, "watch")))
			}
			check("restore pool", writeAtomic(s.phpPoolConfigPath(p.Site), poolOriginal, 0640))
			check("reload original PHP", resourceRun("systemctl", "reload", s.phpUnitForVersion(p.Site.PHPVersion)))
			check("restore routing", writeApplicationRoutingFile(context.Background(), configPath, original))
			check("stop limited workers", resourceRuntimeStop(p))
			if quotaChanged {
				check("restore unbounded quota", resourceQuotaSet(p.Site, 0))
			}
			if p.Site.Cron {
				check("restore application cron", s.SetCron(p.Site, true))
			}
			if jobs, e := s.websiteCronJobs(p.Site.ID); e == nil && len(jobs) > 0 {
				check("restore custom cron", s.saveWebsiteCron(p.Site, jobs))
			}
			if len(failures) > 0 {
				result = fmt.Errorf("%w; rollback requires review: %s", result, strings.Join(failures, "; "))
			}
			if e := resourceRuntimeSave(p.Site.ID, resourceRuntimeStatus{resourcePolicyState: resourcePolicyState{State: "Error"}, Error: result.Error()}); e != nil {
				result = fmt.Errorf("%w; save failure: %v", result, e)
			}
		}
	}()
	if err = resourcePrepareStorage(p.Site); err != nil {
		return err
	}
	if err = resourceQuotaPrepare(p.Site, p.Policy.DiskMiB); err != nil {
		return err
	}
	quotaChanged = true
	php, web, front := resourceNativeConfigs(s, p)
	// Certbot's already-issued certificate remains on the public edge.
	for _, m := range applicationCertificatePattern.FindAllStringSubmatch(string(original), -1) {
		if m[1] == "ssl_certificate" || m[1] == "ssl_certificate_key" {
			front = strings.Replace(front, " server_name ", " "+m[1]+" "+m[2]+";\n server_name ", 1)
		}
	}
	if strings.Contains(string(original), "listen 443") {
		front = strings.Replace(front, " listen 80;", " listen 443 ssl;\n listen [::]:443 ssl;\n listen 80;", 1)
	}
	for name, content := range map[string]string{"php.conf": php, "nginx.conf": web} {
		if err = writeAtomic(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			return err
		}
	}
	for _, sub := range []string{"body", "fastcgi", "proxy"} {
		path := filepath.Join(resourceWebTemporary(p.Site, dir), sub)
		if err = os.MkdirAll(path, 0750); err != nil {
			return err
		}
		account, e := user.Lookup(s.webUser())
		if e != nil {
			return e
		}
		u, e := strconv.Atoi(account.Uid)
		if e != nil {
			return e
		}
		g, e := strconv.Atoi(account.Gid)
		if e != nil {
			return e
		}
		if err = os.Chown(path, u, g); err != nil {
			return err
		}
		if err = os.Chown(resourceWebTemporary(p.Site, dir), 0, g); err != nil {
			return err
		}
	}
	if output, e := runCommand(context.Background(), 5*time.Second, "getenforce"); e == nil && strings.TrimSpace(output) == "Enforcing" {
		for _, path := range []string{filepath.Dir(resourceProtectedStorage(p.Site)), resourceProtectedStorage(p.Site), filepath.Join(resourceProtectedStorage(p.Site), "tmp")} {
			if e = resourceRun("chcon", "-t", "httpd_sys_rw_content_t", path); e != nil {
				return e
			}
		}
		pattern := dir + "(/.*)?"
		if e = resourceRun("semanage", "fcontext", "-a", "-t", "httpd_var_run_t", pattern); e != nil {
			if e = resourceRun("semanage", "fcontext", "-m", "-t", "httpd_var_run_t", pattern); e != nil {
				return e
			}
		}
		if e = resourceRun("restorecon", "-RF", dir); e != nil {
			return e
		}
		for _, name := range []string{"php.conf", "nginx.conf"} {
			if e = resourceRun("chcon", "-t", "httpd_config_t", filepath.Join(dir, name)); e != nil {
				return e
			}
		}
		for _, name := range []string{"body", "fastcgi", "proxy"} {
			if e = resourceRun("chcon", "-t", "httpd_cache_t", filepath.Join(resourceWebTemporary(p.Site, dir), name)); e != nil {
				return e
			}
		}
		if e = resourceRun("chcon", "-R", "-t", "httpd_sys_rw_content_t", filepath.Join(resourceProtectedStorage(p.Site), "tmp")); e != nil {
			return e
		}
	}
	if err = resourceRun(resourcePHPExecutable(s, p.Site), "-t", "-y", filepath.Join(dir, "php.conf")); err != nil {
		return err
	}
	if err = resourceRun("nginx", "-t", "-c", filepath.Join(dir, "nginx.conf"), "-p", dir); err != nil {
		return err
	}
	slice, err := resourceSliceUnit(p.Site.ID, p.Policy, resourceBlockDevice(p.Site))
	if err != nil {
		return err
	}
	units := map[string]string{resourceSliceName(p.Site.ID): slice,
		resourceUnit(p.Site.ID, "php"): resourceServiceUnit(p, "php", resourcePHPExecutable(s, p.Site)+" --nodaemonize --fpm-config "+dir+"/php.conf"),
		resourceUnit(p.Site.ID, "web"): resourceServiceUnit(p, "web", "/usr/sbin/nginx -c "+dir+"/nginx.conf -p "+dir)}
	encoded, _ := json.Marshal(p)
	if err = writeAtomic(filepath.Join(dir, "plan.json"), encoded, 0600); err != nil {
		return err
	}
	watch := fmt.Sprintf("[Unit]\nDescription=LiteCP website resource monitor\nAfter=local-fs.target\nRequiresMountsFor=%s\nBefore=nginx.service\n[Service]\nExecStart=%s resource-runtime watch %s\nRestart=always\nRestartSec=5\n[Install]\nWantedBy=multi-user.target\n", siteHome(p.Site), resourceRuntimeExecutable(), base64.RawURLEncoding.EncodeToString(encoded))
	units[resourceUnit(p.Site.ID, "watch")] = watch
	for name, content := range units {
		if err = writeAtomic(filepath.Join("/etc/systemd/system", name), []byte(content), 0644); err != nil {
			return err
		}
	}
	if err = resourceRun("systemctl", "daemon-reload"); err != nil {
		return err
	}
	// Prepare an immutable built-in application cron script under the host executor.
	builtin := filepath.Join(p.Site.Root, "crons", "cron.php")
	if p.Site.AppType == "wordpress" {
		builtin = filepath.Join(p.Site.Root, "wp-cron.php")
	}
	if err = writeAtomic(filepath.Join(dir, "builtin.sh"), []byte("cd "+cronShellQuote(p.Site.Root)+" || exit 1\n"+cronShellQuote(s.phpBinary(p.Site.PHPVersion))+" -q "+cronShellQuote(builtin)+"\n"), 0644); err != nil {
		return err
	}
	if err = resourceRuntimeStart(p); err != nil {
		return err
	}
	if err = resourceVerifyKernel(p.Site.ID, p.Policy); err != nil {
		return err
	}
	if err = resourceHealth(p.Site.ID, p.Site.Domain); err != nil {
		return err
	}
	if err = resourceSecureSockets(s, p.Site.ID); err != nil {
		return err
	}
	if err = writeAtomic(filepath.Join(dir, "frontend.conf"), []byte(front), 0644); err != nil {
		return err
	}
	if err = writeApplicationRoutingFile(context.Background(), configPath, []byte(front)); err != nil {
		return err
	}
	if err = resourceWaitFrontend(p.Site, true); err != nil {
		return err
	}
	// Retire only this website's pool. Existing workers finish in the shared
	// master but are moved into this website's cgroup before access is opened.
	if err = writeAtomic(filepath.Join(dir, "previous-pool.conf"), poolOriginal, 0600); err != nil {
		return err
	}
	if err = writeAtomic(s.phpPoolConfigPath(p.Site), []byte("; Website uses a dedicated resource-limited FPM master.\n"), 0640); err != nil {
		return err
	}
	if err = resourceRun("systemctl", "reload", s.phpUnitForVersion(p.Site.PHPVersion)); err != nil {
		return err
	}
	if err = resourceAdoptUID(p.Site); err != nil {
		return err
	}
	limited := p.Site
	limited.ResourceLimits = &p.Policy
	if limited.Cron {
		if err = s.SetCron(limited, true); err != nil {
			return err
		}
	}
	jobs, e := s.websiteCronJobs(limited.ID)
	if e != nil {
		return e
	}
	if len(jobs) > 0 {
		if err = s.saveWebsiteCron(limited, jobs); err != nil {
			return err
		}
	}
	if err = resourceRuntimeSave(p.Site.ID, resourceRuntimeStatus{resourcePolicyState: resourcePolicyState{State: "Active"}, Enforced: true}); err != nil {
		return err
	}
	watchTouched = true
	if err = resourceRun("systemctl", "enable", "--now", resourceUnit(p.Site.ID, "watch")); err != nil {
		return err
	}
	return resourceRuntimeGate(p.Site.ID, false)
}

func resourceRuntimeDisable(s *System, p resourceRuntimePlan) (result error) {
	dir := resourceDirectory(p.Site.ID)
	original, err := os.ReadFile(filepath.Join(dir, "previous-nginx.conf"))
	if err != nil {
		return err
	}
	planBytes, err := os.ReadFile(filepath.Join(dir, "plan.json"))
	if err != nil {
		return err
	}
	var previous resourceRuntimePlan
	if json.Unmarshal(planBytes, &previous) != nil {
		return fmt.Errorf("cannot read previous resource policy")
	}
	limited, err := os.ReadFile(filepath.Join(dir, "frontend.conf"))
	if err != nil {
		return err
	}
	defer func() {
		if result != nil {
			var failures []string
			check := func(label string, e error) {
				if e != nil {
					failures = append(failures, label+": "+e.Error())
				}
			}
			check("close resource gate", resourceRuntimeGate(p.Site.ID, true))
			check("pause monitor", resourceRun("systemctl", "disable", "--now", resourceUnit(p.Site.ID, "watch")))
			path, _ := s.nginxPaths(p.Site.Domain)
			check("restore limited routing", writeApplicationRoutingFile(context.Background(), path, limited))
			check("stop workloads", resourceRuntimeStop(previous))
			check("restore disk quota", resourceQuotaSet(previous.Site, previous.Policy.DiskMiB))
			check("save blocked state", resourceRuntimeSave(p.Site.ID, resourceRuntimeStatus{resourcePolicyState: resourcePolicyState{State: "Resource blocked", Reason: "Disable transition failed", NextRecovery: time.Now().Add(5 * time.Minute)}, Enforced: true, Error: result.Error()}))
			check("restart monitor", resourceRun("systemctl", "enable", "--now", resourceUnit(p.Site.ID, "watch")))
			if len(failures) > 0 {
				result = fmt.Errorf("%w; resource rollback requires review: %s", result, strings.Join(failures, "; "))
			}
		}
	}()
	// Switch back to the existing shared pool only after verifying it is ready.
	site := p.Site
	site.ResourceLimits = nil
	if err = s.ensureSitePHPPool(context.Background(), site); err != nil {
		return err
	}
	path, _ := s.nginxPaths(site.Domain)
	if err = writeApplicationRoutingFile(context.Background(), path, []byte(suspensionSiteConfig(string(original), site, site.Suspended))); err != nil {
		return err
	}
	if err = resourceWaitFrontend(site, false); err != nil {
		return err
	}
	if site.Cron {
		if err = s.SetCron(site, true); err != nil {
			return err
		}
	}
	jobs, e := s.websiteCronJobs(site.ID)
	if e != nil {
		return e
	}
	if len(jobs) > 0 {
		if err = s.saveWebsiteCron(site, jobs); err != nil {
			return err
		}
	}
	if err = resourceRun("systemctl", "disable", "--now", resourceUnit(site.ID, "watch")); err != nil {
		return err
	}
	if err = resourceRuntimeStop(p); err != nil {
		return err
	}
	if err = resourceQuotaSet(site, 0); err != nil {
		return err
	}
	return resourceRuntimeSave(site.ID, resourceRuntimeStatus{resourcePolicyState: resourcePolicyState{State: "Disabled"}})
}

func resourceRuntimeWatch(s *System, p resourceRuntimePlan) (result error) {
	defer func() {
		if result != nil {
			_ = resourceRuntimeGate(p.Site.ID, true)
			_ = resourceRuntimeStop(p)
			st := resourceRuntimeRead(p.Site.ID)
			st.State = "Resource blocked"
			st.Reason = "Monitor error"
			st.Error = result.Error()
			st.NextRecovery = time.Now().Add(5 * time.Minute)
			_ = resourceRuntimeSave(p.Site.ID, st)
		}
	}()
	// A separate systemd service owns this loop; it survives browser/panel exits.
	st := resourceRuntimeRead(p.Site.ID)
	if !st.Enforced {
		return fmt.Errorf("no verified active resource boundary")
	}
	if err := resourceRuntimeGate(p.Site.ID, true); err != nil {
		return err
	}
	initialSite, e := resourceSavedSite(p.DataDir, p.Site.ID)
	if e != nil {
		return e
	}
	if initialSite == nil {
		return fmt.Errorf("website no longer exists")
	}
	manualStopped := initialSite.Suspended
	if st.State == "Active" && !manualStopped {
		if err := resourceRuntimeStart(p); err != nil {
			return err
		}
		if err := resourceHealth(p.Site.ID, p.Site.Domain); err != nil {
			return err
		}
		if err := resourceSecureSockets(s, p.Site.ID); err != nil {
			return err
		}
		if err := resourceRuntimeGate(p.Site.ID, false); err != nil {
			return err
		}
	}
	st.CPUExhaustedFor = 0
	var before resourceSample
	for {
		time.Sleep(time.Second)
		// Read manual suspension independently; automatic recovery never clears it.
		site, err := resourceSavedSite(p.DataDir, p.Site.ID)
		if err != nil {
			return err
		}
		if site == nil || site.Root != p.Site.Root || site.Domain != p.Site.Domain || site.PHPVersion != p.Site.PHPVersion {
			_ = resourceRuntimeGate(p.Site.ID, true)
			_ = resourceRuntimeStop(p)
			return fmt.Errorf("website lifecycle changed; resource boundary requires revalidation")
		}
		if site.Suspended {
			manualStopped = true
			if err = resourceRuntimeSave(site.ID, st); err != nil {
				return err
			}
			_ = resourceRuntimeGate(site.ID, true)
			_ = resourceRuntimeStop(p)
			continue
		}
		if manualStopped {
			if st.State == "Active" {
				if err = resourceRuntimeStart(p); err != nil {
					return err
				}
				if err = resourceHealth(site.ID, site.Domain); err != nil {
					return err
				}
				if err = resourceSecureSockets(s, site.ID); err != nil {
					return err
				}
				if err = resourceRuntimeGate(site.ID, false); err != nil {
					return err
				}
			}
			manualStopped = false
			before = resourceSample{}
		}
		disk, err := resourceQuotaVerifiedUsage(p.Site, p.Policy.DiskMiB)
		if err != nil {
			return err
		}
		if st.State == "Resource blocked" {
			st.Sample.DiskBytes = disk
			latest := resourceRuntimeRead(site.ID)
			if latest.NextRecovery.Before(st.NextRecovery) {
				st.NextRecovery = latest.NextRecovery
			}
			if time.Now().Before(st.NextRecovery) {
				if err = resourceRuntimeSave(site.ID, st); err != nil {
					return err
				}
				continue
			}
			if st.Reason == "Disk" && float64(disk) >= float64(p.Policy.DiskMiB)*1048576*.95 {
				st.NextRecovery = time.Now().Add(5 * time.Minute)
				if err = resourceRuntimeSave(site.ID, st); err != nil {
					return err
				}
				continue
			}
			st.State = "Recovering"
			_ = resourceRuntimeSave(site.ID, st)
			err := resourceRuntimeStart(p)
			if err == nil {
				err = resourceHealth(site.ID, site.Domain)
			}
			if err == nil {
				err = resourceSecureSockets(s, site.ID)
			}
			if err != nil {
				_ = resourceRuntimeStop(p)
				st.State = "Resource blocked"
				st.NextRecovery = time.Now().Add(5 * time.Minute)
				st.Error = err.Error()
			} else {
				st.State = "Active"
				st.Reason = ""
				st.NextRecovery = time.Time{}
				st.CPUExhaustedFor = 0
				st.Error = ""
				if err = resourceRuntimeGate(site.ID, false); err != nil {
					return err
				}
			}
			if err = resourceRuntimeSave(site.ID, st); err != nil {
				return err
			}
			before = resourceSample{}
			continue
		}
		if err = resourceVerifyKernel(site.ID, p.Policy); err != nil {
			return err
		}
		now, mem, tasks, err := resourceLiveSample(site.ID, disk)
		if err != nil {
			return err
		}
		{
			next, events := resourceEvaluateSample(p.Policy, st.resourcePolicyState, before, now)
			st.resourcePolicyState = next
			elapsed := now.At.Sub(before.At).Seconds()
			if !before.At.IsZero() && elapsed > 0 && now.UsageUsec >= before.UsageUsec {
				v := float64(now.UsageUsec-before.UsageUsec) / 1e6 / elapsed * 100
				st.CPUPercent = &v
			}
			for _, event := range events {
				if err = resourceRecordEvent(site.ID, event, st.Reason); err != nil {
					return err
				}
			}
		}
		st.Sample = now
		st.MemoryBytes = mem
		st.Processes = tasks
		if st.State == "Resource blocked" {
			zero := 0.0
			st.CPUPercent = &zero
			st.MemoryBytes = 0
			st.Processes = 0
			if err = resourceRuntimeGate(site.ID, true); err != nil {
				return err
			}
			if err = resourceRuntimeSave(site.ID, st); err != nil {
				return err
			}
			if err = resourceRuntimeStop(p); err != nil {
				return err
			}
		}
		if err = resourceRuntimeSave(site.ID, st); err != nil {
			return err
		}
		before = now
	}
}

func resourceEvaluateSample(p WebsiteResourceLimits, state resourcePolicyState, before, now resourceSample) (resourcePolicyState, []string) {
	if before.At.IsZero() {
		// Preserve a zero OOM baseline: a fast OOM immediately after startup
		// must not be mistaken for an already-accounted historical event.
		before = resourceSample{At: now.At, UsageUsec: now.UsageUsec, PIDsDenied: now.PIDsDenied}
	}
	return evaluateResourcePolicy(p, state, before, now)
}
func resourceRecordEvent(id, event, reason string) error {
	f, err := os.OpenFile(filepath.Join(resourceDirectory(id), "history.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		return err
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil {
		return err
	} else if info.Size() > 1<<20 {
		return nil
	}
	return json.NewEncoder(f).Encode(map[string]any{"at": time.Now(), "event": event, "reason": reason})
}
func resourceExecutableAvailable() bool { _, err := exec.LookPath("setquota"); return err == nil }

func resourceEnabled(site Site) bool {
	return site.ResourceLimits != nil && site.ResourceLimits.Enabled
}
func resourceHistory(id string) []any {
	result := []any{}
	f, err := os.Open(filepath.Join(resourceDirectory(id), "history.jsonl"))
	if err != nil {
		return result
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 65536)
	for scan.Scan() {
		var row map[string]any
		if json.Unmarshal(scan.Bytes(), &row) == nil {
			result = append(result, row)
		}
		if len(result) > 200 {
			result = result[1:]
		}
	}
	return result
}
func resourceCronCommand(args []string) error {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 || len(args) != 3 {
		return fmt.Errorf("invalid privileged resource cron request")
	}
	site, err := resourceSavedSite(args[0], args[1])
	if err != nil {
		return err
	}
	if site == nil || !resourceEnabled(*site) || site.Suspended {
		return fmt.Errorf("website cron is blocked")
	}
	st := resourceRuntimeRead(site.ID)
	if st.State != "Active" || !st.Enforced || time.Since(st.UpdatedAt) > 10*time.Second {
		return fmt.Errorf("website resource boundary is not active")
	}
	path := filepath.Clean(args[2])
	allowed := filepath.Join(args[0], "website-cron", "scripts", site.ID) + string(filepath.Separator)
	if !strings.HasPrefix(path, allowed) && path != filepath.Join(resourceDirectory(site.ID), "builtin.sh") {
		return fmt.Errorf("cron script does not belong to the website")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("unsafe cron script")
	}
	script, err := os.ReadFile(path)
	if err != nil || len(script) > 65536 {
		return fmt.Errorf("cannot read cron script")
	}
	argv := []string{"--quiet", "--wait", "--pipe", "--collect", "--service-type=exec", "--slice=" + resourceSliceName(site.ID), "--property=NoNewPrivileges=yes", "--property=RuntimeMaxSec=3600", "--", "/usr/bin/setpriv", fmt.Sprintf("--reuid=%d", site.RuntimeUID), fmt.Sprintf("--regid=%d", site.RuntimeGID), "--clear-groups", "--no-new-privs", "/bin/sh"}
	argv = append([]string{"--property=PrivateTmp=yes", "--property=BindPaths=" + resourceProtectedStorage(*site) + "/tmp:/tmp " + resourceProtectedStorage(*site) + "/tmp:/var/tmp"}, argv...)
	command := exec.Command("systemd-run", argv...)
	command.Stdin = strings.NewReader(string(script))
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func resourceStatusForSite(site Site) resourceRuntimeStatus {
	st := resourceRuntimeRead(site.ID)
	if !resourceEnabled(site) {
		st.State = "Disabled"
		st.Enforced = false
		return st
	}
	var plan resourceRuntimePlan
	b, e := os.ReadFile(filepath.Join(resourceDirectory(site.ID), "plan.json"))
	if e != nil || json.Unmarshal(b, &plan) != nil || plan.Site.ID != site.ID || plan.Site.Root != site.Root || plan.Site.Domain != site.Domain || plan.Site.PHPVersion != site.PHPVersion || plan.Site.RuntimeUID != site.RuntimeUID || plan.Policy != *site.ResourceLimits || time.Since(st.UpdatedAt) > 10*time.Second {
		st.Enforced = false
		st.State = "Error"
		st.Error = "Runtime policy is stale or requires revalidation"
	}
	return st
}
