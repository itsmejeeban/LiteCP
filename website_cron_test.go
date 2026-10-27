package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func cronFixture(t *testing.T) (*App, Site, Site) {
	t.Helper()
	data := t.TempDir()
	store, err := OpenStore(data)
	if err != nil {
		t.Fatal(err)
	}
	first := Site{ID: "cron-first", Domain: "first.example.test", Root: t.TempDir(), AppType: "wordpress", PHPVersion: "8.3", RuntimeUID: 15001, RuntimeGID: 15001}
	second := Site{ID: "cron-second", Domain: "second.example.test", Root: t.TempDir(), AppType: "regular", PHPVersion: "8.3", RuntimeUID: 15002, RuntimeGID: 15002}
	store.cfg.Sites = []Site{first, second}
	return &App{store: store, system: &System{dataDir: data, demo: true}, sessions: map[string]session{}}, first, second
}

func cronJob(site Site, id string, enabled bool) WebsiteCronJob {
	stamp := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	return WebsiteCronJob{ID: id, SiteID: site.ID, Name: "Fixture command", Schedule: "*/5 * * * *", Command: "printf '%s' fixture", Enabled: enabled, CreatedAt: stamp, UpdatedAt: stamp}
}

func cronRequest(t *testing.T, app *App, method, siteID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	content, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, "/api/website-cron?siteId="+siteID, bytes.NewReader(content))
	response := httptest.NewRecorder()
	app.websiteCron(response, request)
	return response
}

func TestWebsiteCronScheduleValidation(t *testing.T) {
	for _, value := range []string{"* * * * *", "*/5 0-23/2 1,15 1-12 0,7", "0 0 1 1 0", "59 23 31 12 7", "0,15,30,45 8-18 * * 1-5", "*/60 */24 */31 */12 */8"} {
		if err := validateCronSchedule(value); err != nil {
			t.Errorf("valid schedule %q: %v", value, err)
		}
	}
	for _, value := range []string{"", "* * * *", "* * * * * root", "@reboot", "@daily", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * 32 * *", "* * * 0 *", "* * * 13 *", "* * * * 8", "*/0 * * * *", "*/61 * * * *", "5/2 * * * *", "9-2 * * * *", "-1 * * * *", "1,,2 * * * *", "* * * JAN MON", "1-2-3 * * * *", "*/2/3 * * * *", "* * * * *\nroot /bin/true", "* * * * *\r", "* * * * *\x00"} {
		if err := validateCronSchedule(value); err == nil {
			t.Errorf("unsafe schedule accepted: %q", value)
		}
	}
	for _, command := range []string{"", "  ", "true\nfalse", "true\rfalse", "true\x00false", strings.Repeat("a", 4097)} {
		if err := validateWebsiteCronJob(WebsiteCronJob{Schedule: "* * * * *", Command: command}); err == nil {
			t.Errorf("unsafe command accepted: %q", command)
		}
	}
	for _, name := range []string{"one\ntwo", "one\rtwo", "one\x00two", strings.Repeat("n", 101)} {
		if err := validateWebsiteCronJob(WebsiteCronJob{Name: name, Schedule: "* * * * *", Command: "true"}); err == nil {
			t.Error("unsafe name accepted")
		}
	}
}

func TestWebsiteCronRenderingDropsPrivilegesBeforeReadingCommands(t *testing.T) {
	app, site, _ := cronFixture(t)
	site.Root = filepath.Join(site.Root, "root'with%spaces $(never-run)")
	job := cronJob(site, "safe-job", true)
	job.Command = "printf '%s' \"$(never-run) `never-run` date +%F \\% \\\\%\""
	content, err := app.system.renderWebsiteCron(site, []WebsiteCronJob{job, cronJob(site, "disabled", false)})
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if strings.Contains(text, "never-run") || strings.Contains(text, "%") || strings.Contains(text, "# Job disabled") {
		t.Fatal("raw shell/cron control data or disabled job leaked into executable cron content")
	}
	for _, required := range []string{"--reuid=15001", "--regid=15001", "--clear-groups", "--no-new-privs", "/bin/cat", "/bin/sh", "MAILTO=\"\""} {
		if !strings.Contains(text, required) {
			t.Errorf("missing safeguard %s", required)
		}
	}
	path, script, err := app.system.websiteCronScript(site, job)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("pure renderer wrote executable script data")
	}
	if !strings.Contains(text, "/bin/cat "+cronShellQuote(path)+" | /usr/bin/setpriv") {
		t.Fatal("cron does not pipe the private script into the dropped-UID shell")
	}
	if err := app.system.saveWebsiteCron(site, []WebsiteCronJob{job}); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(saved, script) || !strings.HasSuffix(string(saved), job.Command+"\n") || !strings.Contains(string(saved), "cd "+cronShellQuote(site.Root)) {
		t.Fatal("command bytes or working directory changed in the private script", err)
	}
}

func TestWebsiteCronRejectsUnsafeIdentityAndPaths(t *testing.T) {
	app, site, _ := cronFixture(t)
	for _, change := range []func(*Site){func(s *Site) { s.RuntimeUID = 0 }, func(s *Site) { s.RuntimeGID = 0 }, func(s *Site) { s.RuntimeUID = 999 }, func(s *Site) { s.RuntimeGID = 999 }, func(s *Site) { s.Root = "relative/root" }, func(s *Site) { s.Root = "/root\nroot /bin/true" }, func(s *Site) { s.ID = "../escape" }} {
		unsafe := site
		change(&unsafe)
		if _, err := app.system.renderWebsiteCron(unsafe, nil); err == nil {
			t.Errorf("unsafe site accepted: %+v", unsafe)
		}
	}
	if _, err := app.system.renderWebsiteCron(site, []WebsiteCronJob{cronJob(Site{ID: "other"}, "foreign", true)}); err == nil {
		t.Fatal("foreign job rendered")
	}
}

func TestWebsiteCronMetadataRejectsCrossSiteAndDuplicateJobs(t *testing.T) {
	app, site, other := cronFixture(t)
	if err := os.MkdirAll(filepath.Dir(app.system.websiteCronMetadataPath(site.ID)), 0700); err != nil {
		t.Fatal(err)
	}
	for _, jobs := range [][]WebsiteCronJob{{cronJob(other, "foreign", true)}, {cronJob(site, "same", true), cronJob(site, "same", false)}, {cronJob(site, "../unsafe", true)}} {
		content, err := json.Marshal(jobs)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(app.system.websiteCronMetadataPath(site.ID), content, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := app.system.websiteCronJobs(site.ID); err == nil {
			t.Fatal("invalid stored cron metadata accepted")
		}
	}
	if _, err := app.system.websiteCronJobs("../escape"); err == nil {
		t.Fatal("unsafe metadata path accepted")
	}
}

func TestWebsiteCronCRUDAndCrossWebsiteRejection(t *testing.T) {
	app, site, other := cronFixture(t)
	input := map[string]any{"name": "Daily maintenance", "schedule": "0 2 * * *", "command": "printf fixture", "enabled": true}
	if response := cronRequest(t, app, http.MethodPost, site.ID, input); response.Code != 200 {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	items, err := app.system.websiteCronJobs(site.ID)
	if err != nil || len(items) != 1 || items[0].SiteID != site.ID || !items[0].Enabled {
		t.Fatalf("stored job: %+v %v", items, err)
	}
	created := items[0]
	if response := cronRequest(t, app, http.MethodGet, site.ID, nil); response.Code != 200 || !strings.Contains(response.Body.String(), created.ID) {
		t.Fatalf("listing: %d %s", response.Code, response.Body.String())
	}
	if err := app.system.saveWebsiteCron(other, []WebsiteCronJob{cronJob(other, "foreign", true)}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(app.system.websiteCronMetadataPath(site.ID))
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		for _, id := range []string{"foreign", "unknown"} {
			body := map[string]any{"id": id, "name": "x", "schedule": "* * * * *", "command": "true", "enabled": false, "confirm": true}
			if response := cronRequest(t, app, method, site.ID, body); response.Code != 404 {
				t.Errorf("%s accepted %s: %d", method, id, response.Code)
			}
		}
	}
	after, _ := os.ReadFile(app.system.websiteCronMetadataPath(site.ID))
	if !bytes.Equal(before, after) {
		t.Fatal("unknown or cross-website request changed schedules")
	}
	input["id"] = created.ID
	input["enabled"] = false
	input["command"] = "printf edited"
	if response := cronRequest(t, app, http.MethodPut, site.ID, input); response.Code != 200 {
		t.Fatalf("edit: %d %s", response.Code, response.Body.String())
	}
	items, err = app.system.websiteCronJobs(site.ID)
	if err != nil || len(items) != 1 || items[0].Enabled || items[0].Command != "printf edited" || !items[0].CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("edit did not preserve identity/creation date: %+v %v", items, err)
	}
	if response := cronRequest(t, app, http.MethodDelete, site.ID, map[string]any{"id": created.ID}); response.Code != 400 {
		t.Fatal("removal did not require confirmation", response.Code)
	}
	if response := cronRequest(t, app, http.MethodDelete, site.ID, map[string]any{"id": created.ID, "confirm": true}); response.Code != 200 {
		t.Fatal("fixture job removal failed", response.Code)
	}
	items, err = app.system.websiteCronJobs(site.ID)
	if err != nil || len(items) != 0 {
		t.Fatal("removed fixture remains", items, err)
	}
	foreign, err := app.system.websiteCronJobs(other.ID)
	if err != nil || len(foreign) != 1 || foreign[0].ID != "foreign" {
		t.Fatal("removal changed other website", foreign, err)
	}
	if response := cronRequest(t, app, http.MethodPatch, site.ID, input); response.Code != 405 {
		t.Fatal("unsupported method accepted", response.Code)
	}
	if response := cronRequest(t, app, http.MethodPost, "missing", input); response.Code != 404 {
		t.Fatal("unknown website accepted", response.Code)
	}
}

func TestWebsiteCronRoutesRequireAuthenticationAndOrigin(t *testing.T) {
	app, site, _ := cronFixture(t)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		request := httptest.NewRequest(method, "https://panel.example.test/api/website-cron?siteId="+site.ID, strings.NewReader(`{}`))
		response := httptest.NewRecorder()
		app.routes().ServeHTTP(response, request)
		if response.Code != 401 {
			t.Errorf("unauthenticated %s: %d", method, response.Code)
		}
	}
	app.sessions["fixture-session"] = session{Expires: time.Now().Add(time.Hour)}
	request := httptest.NewRequest(http.MethodPost, "https://panel.example.test/api/website-cron?siteId="+site.ID, strings.NewReader(`{"schedule":"* * * * *","command":"true","enabled":true}`))
	request.AddCookie(&http.Cookie{Name: "litecp_session", Value: "fixture-session"})
	request.Header.Set("Origin", "https://attacker.example.test")
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatal("cross-origin mutation accepted", response.Code)
	}
	if _, err := os.Stat(app.system.websiteCronMetadataPath(site.ID)); !os.IsNotExist(err) {
		t.Fatal("rejected request wrote schedules", err)
	}
}

func TestWebsiteCronPersistencePauseAndReconciliation(t *testing.T) {
	app, site, other := cronFixture(t)
	if err := app.system.saveWebsiteCron(site, []WebsiteCronJob{cronJob(site, "persisted", true), cronJob(site, "disabled", false)}); err != nil {
		t.Fatal(err)
	}
	metadata, err := os.ReadFile(app.system.websiteCronMetadataPath(site.ID))
	if err != nil {
		t.Fatal(err)
	}
	reopened := &System{dataDir: app.system.dataDir, demo: true}
	items, err := reopened.websiteCronJobs(site.ID)
	if err != nil || len(items) != 2 {
		t.Fatal("jobs did not survive reopening", items, err)
	}
	if runtime.GOOS != "windows" {
		for _, path := range []string{reopened.websiteCronPath(site.ID), reopened.websiteCronMetadataPath(site.ID)} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("cron commands are not private", path, err)
			}
		}
	}
	if err := reopened.pauseWebsiteCron(site.ID); err != nil {
		t.Fatal(err)
	}
	paused, err := os.ReadFile(reopened.websiteCronPath(site.ID))
	if err != nil || strings.Contains(string(paused), " root ") {
		t.Fatal("paused schedule remains active", err)
	}
	preserved, _ := os.ReadFile(reopened.websiteCronMetadataPath(site.ID))
	if !bytes.Equal(metadata, preserved) {
		t.Fatal("pause changed saved definitions")
	}
	if err := reopened.saveWebsiteCron(other, []WebsiteCronJob{cronJob(other, "orphan", true)}); err != nil {
		t.Fatal(err)
	}
	if err := reopened.reconcileWebsiteCron([]Site{site}); err != nil {
		t.Fatal(err)
	}
	active, _ := os.ReadFile(reopened.websiteCronPath(site.ID))
	orphan, _ := os.ReadFile(reopened.websiteCronPath(other.ID))
	if !strings.Contains(string(active), "# Job persisted") || strings.Contains(string(active), "# Job disabled") || strings.Contains(string(orphan), " root ") {
		t.Fatal("reconcile did not restore enabled jobs and pause orphan schedules")
	}
	orphanJobs, err := reopened.websiteCronJobs(other.ID)
	if err != nil || len(orphanJobs) != 1 {
		t.Fatal("orphan definitions were not retained", err)
	}
}

func TestWebsiteCronReconcileClearsStaleScheduleWhenDefinitionsAreEmpty(t *testing.T) {
	app, site, _ := cronFixture(t)
	active, err := app.system.renderWebsiteCron(site, []WebsiteCronJob{cronJob(site, "obsolete", true)})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.system.saveWebsiteCron(site, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(app.system.websiteCronPath(site.ID), active, 0600); err != nil {
		t.Fatal(err)
	}
	if err := app.system.reconcileWebsiteCron([]Site{site}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(app.system.websiteCronPath(site.ID))
	if err != nil || strings.Contains(string(content), "# Job obsolete") {
		t.Fatal("an empty definition set left obsolete jobs active", err)
	}
}

func TestWebsiteCronMetadataFailureRestoresPreviousSchedule(t *testing.T) {
	app, site, _ := cronFixture(t)
	if err := os.MkdirAll(app.system.websiteCronMetadataPath(site.ID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(app.system.websiteCronConfigDir(), 0755); err != nil {
		t.Fatal(err)
	}
	before := []byte("# Previous fixture schedule retained.\n")
	if err := os.WriteFile(app.system.websiteCronPath(site.ID), before, 0600); err != nil {
		t.Fatal(err)
	}
	if err := app.system.saveWebsiteCron(site, []WebsiteCronJob{cronJob(site, "new-job", true)}); err == nil {
		t.Fatal("metadata write failure reported success")
	}
	after, err := os.ReadFile(app.system.websiteCronPath(site.ID))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed save left the new schedule active", err)
	}
}

func TestWebsiteCronLongCommandsUsePrivateImmutableScripts(t *testing.T) {
	app, site, _ := cronFixture(t)
	job := cronJob(site, "long-command", true)
	job.Command = strings.Repeat("a", 4096)
	if err := app.system.saveWebsiteCron(site, []WebsiteCronJob{job}); err != nil {
		t.Fatal(err)
	}
	firstPath, firstContent, err := app.system.websiteCronScript(site, job)
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(app.system.websiteCronPath(site.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(config), "\n") {
		if strings.Contains(line, " root ") && len(strings.SplitN(line, " root ", 2)[1]) >= 1000 {
			t.Fatal("cron command exceeds portable scheduler limit")
		}
	}
	if strings.Contains(string(config), job.Command) {
		t.Fatal("long command embedded in scheduler file")
	}
	if runtime.GOOS != "windows" {
		for path, mode := range map[string]os.FileMode{firstPath: 0600, filepath.Dir(firstPath): 0700, filepath.Dir(filepath.Dir(firstPath)): 0700} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != mode {
				t.Fatalf("private script permissions %s: %v", path, err)
			}
		}
	}
	job.Command = "printf updated"
	if err := app.system.saveWebsiteCron(site, []WebsiteCronJob{job}); err != nil {
		t.Fatal(err)
	}
	nextPath, _, err := app.system.websiteCronScript(site, job)
	if err != nil || firstPath == nextPath {
		t.Fatal("changed command reused immutable path", err)
	}
	retained, err := os.ReadFile(firstPath)
	if err != nil || !bytes.Equal(retained, firstContent) {
		t.Fatal("previous script was changed or removed", err)
	}
	if err := os.WriteFile(nextPath, []byte("modified fixture content"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(app.system.websiteCronPath(site.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := app.system.saveWebsiteCron(site, []WebsiteCronJob{job}); err == nil {
		t.Fatal("content hash mismatch accepted")
	}
	after, err := os.ReadFile(app.system.websiteCronPath(site.ID))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("script validation failure changed active schedules", err)
	}
}

func TestWebsiteCronRejectsUnsafeAndOversizedScriptPaths(t *testing.T) {
	app, site, _ := cronFixture(t)
	for _, path := range []string{"relative/data", filepath.Join(app.system.dataDir, "unsafe%path"), filepath.Join(app.system.dataDir, "unsafe\npath"), filepath.Join(app.system.dataDir, strings.Repeat("long", 300))} {
		system := &System{dataDir: path, demo: true}
		if _, err := system.renderWebsiteCron(site, []WebsiteCronJob{cronJob(site, "fixture", true)}); err == nil {
			t.Fatal("unsafe or oversized scheduler path accepted", path)
		}
	}
}
