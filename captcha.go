package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type CaptchaPolicy struct {
	Mode                  string   `json:"mode"`
	ClearanceMinutes      int      `json:"clearanceMinutes"`
	RequestsPerSecond     int      `json:"requestsPerSecond"`
	SiteRequestsPerSecond int      `json:"siteRequestsPerSecond"`
	Connections           int      `json:"connections"`
	AttemptsPerMinute     int      `json:"attemptsPerMinute"`
	ChallengesPerMinute   int      `json:"challengesPerMinute"`
	Exclusions            []string `json:"exclusions"`
	Revision              int64    `json:"revision"`
}

func captchaEnabled(site Site) bool { return site.Captcha != nil && site.Captcha.Mode != "off" }
func (p CaptchaPolicy) validate() error {
	if p.Mode != "off" && p.Mode != "monitor" && p.Mode != "challenge" && p.Mode != "auto" {
		return fmt.Errorf("Choose Off, Monitor, Challenge all visitors or Automatic")
	}
	if p.ClearanceMinutes < 5 || p.ClearanceMinutes > 1440 || p.RequestsPerSecond < 1 || p.RequestsPerSecond > 1000 || p.SiteRequestsPerSecond < 1 || p.SiteRequestsPerSecond > 100000 || p.Connections < 1 || p.Connections > 1000 || p.AttemptsPerMinute < 3 || p.AttemptsPerMinute > 120 || p.ChallengesPerMinute < 10 || p.ChallengesPerMinute > 1200 {
		return fmt.Errorf("Limits are outside the supported range")
	}
	if len(p.Exclusions) > 30 {
		return fmt.Errorf("Use at most 30 excluded path prefixes")
	}
	for _, path := range p.Exclusions {
		if len(path) > 200 || !regexp.MustCompile(`^/[A-Za-z0-9_./-]*$`).MatchString(path) || strings.Contains(path, "..") || path == "/" || strings.HasPrefix(path, captchaPrefix) {
			return fmt.Errorf("Invalid or overly broad excluded path: %s", path)
		}
	}
	return nil
}

var captchaManagedBlock = regexp.MustCompile(`(?ms)\n[ \t]*# BEGIN LITECP CAPTCHA\n.*?^[ \t]*# END LITECP CAPTCHA\n`)
var captchaServerStart = regexp.MustCompile(`(?m)^[ \t]*server\s*\{`)

func captchaResourceSignal(site Site, record captchaRuntimeSite, st resourceRuntimeStatus, now time.Time) captchaRuntimeSite {
	if record.Policy.Mode != "auto" || !resourceEnabled(site) {
		record.AutoUntil = time.Time{}
		record.AutoStarted = time.Time{}
		record.ResourceBlocked = false
		return record
	}
	p := site.ResourceLimits
	// A missing/stale enforced boundary must never become an access bypass.
	record.ResourceBlocked = !st.Enforced || st.State == "Resource blocked" || st.State == "Recovering" || st.State == "Error"
	breach := record.ResourceBlocked || st.CPUExhaustedFor > 0 || (p.MemoryMiB > 0 && st.MemoryBytes >= p.MemoryMiB*1048576) || (p.DiskMiB > 0 && st.Sample.DiskBytes >= p.DiskMiB*1048576) || (p.Tasks > 0 && st.Processes >= p.Tasks)
	if breach {
		if !record.AutoUntil.After(now) {
			record.AutoStarted = now
		}
		record.AutoUntil = now.Add(5 * time.Minute)
	}
	return record
}

func injectCaptchaConfig(config string, site Site) string {
	config = captchaManagedBlock.ReplaceAllString(config, "")
	gate := fmt.Sprintf(" if (-f %s/blocked) { return 503 'This website is temporarily unavailable due to resource limits. Please try again later.'; }\n", resourceDirectory(site.ID))
	if resourceEnabled(site) && strings.Contains(config, "# LiteCP enforced website resource boundary") && (site.Captcha == nil || site.Captcha.Mode != "auto") && !strings.Contains(config, gate) {
		config = captchaServerStart.ReplaceAllStringFunc(config, func(start string) string { return start + "\n" + gate })
	}
	if !captchaEnabled(site) {
		return config
	}
	if site.Captcha.Mode == "auto" {
		// The gateway handles the resource gate before proxying; this permits
		// verification while stopped workloads remain inaccessible. Suspension
		// directives and the private worker's cgroup limits are never removed.
		config = strings.ReplaceAll(config, fmt.Sprintf(" if (-f %s/blocked) { return 503 'This website is temporarily unavailable due to resource limits. Please try again later.'; }\n", resourceDirectory(site.ID)), "")
	}
	p := site.Captcha
	dry := "off"
	if p.Mode == "monitor" {
		dry = "on"
	}
	headers := fmt.Sprintf("proxy_set_header X-LiteCP-Site %q;\n proxy_set_header X-LiteCP-Host $host;\n proxy_set_header X-LiteCP-IP $remote_addr;\n proxy_set_header X-LiteCP-Scheme $scheme;\n proxy_set_header X-LiteCP-URI $request_uri;\n proxy_set_header X-LiteCP-Method $request_method;\n proxy_set_header User-Agent $http_user_agent;\n proxy_set_header Cookie $http_cookie;\n", site.ID)
	block := fmt.Sprintf(`
 # BEGIN LITECP CAPTCHA
 auth_request /_litecp_captcha_check;
 auth_request_set $litecp_captcha_limited $upstream_http_x_litecp_captcha_limited;
 error_page 401 = @litecp_captcha;
 limit_conn cap_conn %d;
 limit_conn_status 429;
 limit_conn_dry_run %s;
 location = /_litecp_captcha_check {
 internal;
 auth_request off;
 proxy_pass http://127.0.0.1:9092/check;
 proxy_pass_request_body off;
 proxy_set_header Content-Length "";
 proxy_set_header X-LiteCP-Mode check;
 %s
 }
 location ^~ /_litecp_captcha/ {
 auth_request off;
 proxy_pass http://127.0.0.1:9092;
 proxy_set_header X-LiteCP-Mode public;
 %s
 }
 location @litecp_captcha {
 auth_request off;
 proxy_pass http://127.0.0.1:9092;
 proxy_pass_request_body off;
 proxy_set_header Content-Length "";
 proxy_set_header X-LiteCP-Mode challenge;
 proxy_set_header X-LiteCP-Limited $litecp_captcha_limited;
 %s
 }
 # END LITECP CAPTCHA
`, p.Connections, dry, headers, headers, headers)
	return captchaServerStart.ReplaceAllStringFunc(config, func(start string) string { return start + block })
}
func captchaZones(_ []Site) string {
	// Request rates live in the gateway so WAF and CAPTCHA can independently
	// monitor or enforce. Nginx only permits one limit_req_dry_run per context.
	return "# Managed LiteCP CAPTCHA zones\nlimit_conn_zone \"$binary_remote_addr$server_name\" zone=cap_conn:4m;\n"
}
func (a *App) syncCaptchaRuntime(sites []Site) error {
	if a.system.demo {
		return nil
	}
	if _, err := os.Stat(filepath.Dir(captchaRuntimeFile)); os.IsNotExist(err) {
		for _, site := range sites {
			if captchaEnabled(site) {
				return fmt.Errorf("install the CAPTCHA gateway service before restoring protected websites")
			}
		}
		return nil
	}
	keyfile := filepath.Join(a.system.dataDir, "captcha-key")
	key, err := os.ReadFile(keyfile)
	if os.IsNotExist(err) {
		value, e := randomToken(48)
		if e != nil {
			return e
		}
		key = []byte(value)
		if e = writeAtomic(keyfile, key, 0600); e != nil {
			return e
		}
	} else if err != nil {
		return err
	}
	runtime := captchaRuntime{Key: string(key), Sites: map[string]captchaRuntimeSite{}}
	var previous captchaRuntime
	if content, e := os.ReadFile(captchaRuntimeFile); e == nil {
		_ = json.Unmarshal(content, &previous)
	}
	for _, site := range sites {
		if captchaEnabled(site) {
			record := captchaRuntimeSite{Domain: site.Domain, Policy: *site.Captcha}
			if site.Captcha.Mode == "auto" && resourceEnabled(site) {
				old := previous.Sites[site.ID]
				if old.Policy.Revision == record.Policy.Revision && old.Domain == record.Domain {
					record.AutoUntil = old.AutoUntil
					record.AutoStarted = old.AutoStarted
				}
				record = captchaResourceSignal(site, record, resourceStatusForSite(site), time.Now())
				if _, e := os.Stat(filepath.Join(resourceDirectory(site.ID), "blocked")); e == nil {
					if !record.AutoUntil.After(time.Now()) {
						record.AutoStarted = time.Now()
					}
					record.ResourceBlocked = true
					record.AutoUntil = time.Now().Add(5 * time.Minute)
				}
			}
			runtime.Sites[site.ID] = record
		}
	}
	content, _ := json.Marshal(runtime)
	zonePath := "/etc/nginx/conf.d/00-litecp-captcha-zones.conf"
	zoneContent := captchaZones(sites)
	if previous, e := os.ReadFile(zonePath); (e == nil && string(previous) != zoneContent) || (os.IsNotExist(e) && len(runtime.Sites) > 0) {
		if e = writeAtomic(zonePath, []byte(zoneContent), 0644); e != nil {
			return e
		}
	}
	if old, e := os.ReadFile(captchaRuntimeFile); e == nil && string(old) == string(content) {
		if len(runtime.Sites) > 0 {
			_, e = runCommand(context.Background(), 10*time.Second, "systemctl", "start", "litecp-captcha")
			return e
		}
		return nil
	}
	if err = writeAtomic(captchaRuntimeFile, content, 0640); err != nil {
		return err
	}
	group, err := user.LookupGroup("litecp-captcha")
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return err
	}
	if err = os.Chown(captchaRuntimeFile, 0, gid); err != nil {
		return err
	}
	if len(runtime.Sites) == 0 {
		return nil
	}
	_, err = runCommand(context.Background(), 10*time.Second, "systemctl", "start", "litecp-captcha")
	return err
}
func (a *App) startCaptchaReconcile() {
	if a.system.demo {
		return
	}
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.mutationMu.Lock()
			_ = a.syncCaptchaRuntime(a.store.Sites())
			a.mutationMu.Unlock()
		}
	}()
}
func (a *App) captchaSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		items := []map[string]any{}
		var runtime captchaRuntime
		if !a.system.demo {
			if content, e := os.ReadFile(captchaRuntimeFile); e == nil {
				_ = json.Unmarshal(content, &runtime)
			}
		}
		for _, site := range a.store.Sites() {
			automatic := runtime.Sites[site.ID]
			items = append(items, map[string]any{"id": site.ID, "domain": site.Domain, "ssl": site.SSL || (site.Application != nil && site.Application.Certificate != ""), "settings": site.Captcha, "automaticActive": automatic.AutoUntil.After(time.Now()), "automaticUntil": automatic.AutoUntil, "resourceBlocked": automatic.ResourceBlocked, "resourceLimitsEnabled": resourceEnabled(site)})
		}
		metrics := map[string]any{}
		if !a.system.demo {
			if key, err := os.ReadFile(filepath.Join(a.system.dataDir, "captcha-key")); err == nil {
				req, _ := http.NewRequestWithContext(r.Context(), "GET", "http://"+captchaAddress+"/metrics", nil)
				req.Header.Set("X-LiteCP-Mode", "metrics")
				req.Header.Set("Authorization", "Bearer "+string(key))
				client := &http.Client{Timeout: 2 * time.Second}
				if response, e := client.Do(req); e == nil {
					if response.StatusCode == 200 {
						_ = json.NewDecoder(io.LimitReader(response.Body, 128<<10)).Decode(&metrics)
					}
					response.Body.Close()
				}
			}
		}
		writeJSON(w, 200, map[string]any{"items": items, "metrics": metrics})
		return
	}
	if r.Method != http.MethodPut {
		writeError(w, 405, "Use GET or PUT")
		return
	}
	var input struct {
		SiteID   string        `json:"siteId"`
		Settings CaptchaPolicy `json:"settings"`
	}
	if decodeJSONWithLimit(r, &input, 16<<10) != nil {
		writeError(w, 400, "Invalid CAPTCHA settings")
		return
	}
	if err := input.Settings.validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	site := a.store.SiteByID(input.SiteID)
	if site == nil {
		writeError(w, 404, "Website not found")
		return
	}
	input.Settings.Revision = time.Now().UnixNano()
	updated := *site
	updated.Captcha = &input.Settings
	if input.Settings.Mode == "auto" && !resourceEnabled(updated) {
		writeError(w, 409, "Enable Website Resource Limit for this website before selecting Automatic CAPTCHA")
		return
	}
	if a.system.demo {
		if err := a.store.UpdateSite(site.ID, func(s *Site) { s.Captcha = updated.Captcha }); err != nil {
			writeError(w, 500, "Could not save CAPTCHA policy")
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if captchaEnabled(updated) {
		if !site.SSL && (site.Application == nil || site.Application.Certificate == "") {
			writeError(w, 409, "Issue SSL for this website before enabling CAPTCHA protection")
			return
		}
		output, err := runCommand(r.Context(), 10*time.Second, "nginx", "-V")
		if err != nil || !strings.Contains(output, "--with-http_auth_request_module") {
			writeError(w, 409, "Nginx needs the http_auth_request module. Update/install Nginx with --with-http_auth_request_module before enabling protection.")
			return
		}
	}
	sites := a.store.Sites()
	for i := range sites {
		if sites[i].ID == site.ID {
			sites[i] = updated
		}
	}
	path, _ := a.system.nginxPaths(site.Domain)
	old, err := os.ReadFile(path)
	if err != nil {
		writeError(w, 500, "Cannot read website Nginx configuration")
		return
	}
	zonePath := "/etc/nginx/conf.d/00-litecp-captcha-zones.conf"
	oldZones, err := os.ReadFile(zonePath)
	if err != nil && !os.IsNotExist(err) {
		writeError(w, 500, "Cannot read CAPTCHA zones")
		return
	}
	if len(oldZones) == 0 {
		oldZones = []byte("# No CAPTCHA zones configured\n")
	}
	type savedFile struct {
		path    string
		content []byte
	}
	files := []savedFile{{path, old}, {zonePath, oldZones}}
	resourceFront := ""
	if resourceEnabled(*site) {
		resourceFront = filepath.Join(resourceDirectory(site.ID), "frontend.conf")
		if content, e := os.ReadFile(resourceFront); e == nil {
			files = append(files, savedFile{resourceFront, content})
		} else {
			writeError(w, 500, "Cannot read resource-limited website frontend")
			return
		}
	}
	rollback := func() error {
		var failures []string
		for _, file := range files {
			if e := writeAtomic(file.path, file.content, 0644); e != nil {
				failures = append(failures, e.Error())
			}
		}
		if e := a.syncCaptchaRuntime(a.store.Sites()); e != nil {
			failures = append(failures, e.Error())
		}
		if _, e := runCommand(context.Background(), 10*time.Second, "nginx", "-t"); e != nil {
			failures = append(failures, e.Error())
		} else if _, e = runCommand(context.Background(), 10*time.Second, "systemctl", "reload", "nginx"); e != nil {
			failures = append(failures, e.Error())
		}
		if len(failures) > 0 {
			return fmt.Errorf("rollback needs administrator review: %s", strings.Join(failures, "; "))
		}
		return nil
	}
	if err = a.syncCaptchaRuntime(sites); err == nil {
		err = writeAtomic(zonePath, []byte(captchaZones(sites)), 0644)
	}
	if err == nil {
		err = writeAtomic(path, []byte(injectCaptchaConfig(string(old), updated)), 0644)
	}
	if err == nil && resourceFront != "" {
		err = writeAtomic(resourceFront, []byte(injectCaptchaConfig(string(files[2].content), updated)), 0644)
	}
	if err == nil {
		_, err = runCommand(r.Context(), 15*time.Second, "nginx", "-t")
	}
	if err == nil && captchaEnabled(updated) {
		client := &http.Client{Timeout: 2 * time.Second}
		key, e := os.ReadFile(filepath.Join(a.system.dataDir, "captcha-key"))
		if e != nil {
			err = e
		} else {
			probe, _ := http.NewRequestWithContext(r.Context(), "GET", "http://"+captchaAddress+"/metrics", nil)
			probe.Header.Set("X-LiteCP-Mode", "metrics")
			probe.Header.Set("Authorization", "Bearer "+string(key))
			var response *http.Response
			for attempt := 0; attempt < 5; attempt++ {
				response, e = client.Do(probe)
				if e == nil {
					break
				}
				select {
				case <-r.Context().Done():
					attempt = 5
				case <-time.After(200 * time.Millisecond):
				}
			}
			if e != nil {
				err = fmt.Errorf("CAPTCHA gateway health check failed: %w", e)
			} else {
				response.Body.Close()
				if response.StatusCode != 200 {
					err = fmt.Errorf("CAPTCHA gateway health returned %d", response.StatusCode)
				}
			}
		}
	}
	if err == nil {
		_, err = runCommand(r.Context(), 15*time.Second, "systemctl", "reload", "nginx")
	}
	if err != nil {
		if e := rollback(); e != nil {
			writeError(w, 502, "CAPTCHA apply failed: "+err.Error()+"; "+e.Error())
			return
		}
		writeError(w, 502, "CAPTCHA policy could not be applied; previous configuration retained: "+err.Error())
		return
	}
	if err = a.store.UpdateSite(site.ID, func(s *Site) { s.Captcha = updated.Captcha }); err != nil {
		if e := rollback(); e != nil {
			writeError(w, 500, "Could not save CAPTCHA policy; "+e.Error())
			return
		}
		writeError(w, 500, "Could not save CAPTCHA policy; previous configuration retained")
		return
	}
	a.store.AddActivity("Self-hosted CAPTCHA updated", site.Domain+": "+input.Settings.Mode, "info")
	writeJSON(w, 200, map[string]bool{"ok": true})
}
