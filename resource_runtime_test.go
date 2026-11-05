package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func nativeResourcePlan() resourceRuntimePlan {
	return resourceRuntimePlan{Site: Site{ID: "audit-site", Domain: "audit.example.test", Root: "/home/litecp/audit.example.test/public_html", RuntimeUID: siteRuntimeID("audit-site"), RuntimeGID: siteRuntimeID("audit-site"), PHPVersion: "8.4", AppType: "regular"}, DataDir: "/var/lib/litecp", Policy: validResourcePolicy()}
}
func TestResourceFrontendWaitsForReloadedWorker(t *testing.T) {
	site := nativeResourcePlan().Site
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != site.Domain {
			t.Error("health check lost domain Host header")
		}
		if requests.Add(1) < 3 {
			w.Header().Set("X-LiteCP-Resource-Boundary", fmt.Sprintf("%x", []byte(site.ID)))
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	if err := resourceWaitFrontendAt(site, false, server.URL); err != nil {
		t.Fatal(err)
	}
	if requests.Load() < 3 {
		t.Fatal("stopped old workers before new routing became visible")
	}
}
func TestResourceFastOOMBeforeFirstSampleStillBlocks(t *testing.T) {
	p := validResourcePolicy()
	state, events := resourceEvaluateSample(p, resourcePolicyState{State: "Active"}, resourceSample{}, resourceSample{At: time.Now(), OOMKills: 1})
	if state.State != "Resource blocked" || state.Reason != "Memory" || len(events) != 1 || events[0] != "block" {
		t.Fatalf("early OOM was ignored: %+v %v", state, events)
	}
}
func TestResourceNativeBoundaryUsesPrivateWorkers(t *testing.T) {
	p := nativeResourcePlan()
	s := &System{os: OSInfo{ID: "ubuntu"}}
	php, web, front := resourceNativeConfigs(s, p)
	for _, expected := range []string{"daemonize = no", resourceSocket(p.Site.ID, "php"), "pm.max_children = 8", "user = " + strconv.Itoa(p.Site.RuntimeUID)} {
		if !strings.Contains(php, expected) {
			t.Fatalf("PHP boundary missing %q", expected)
		}
	}
	if strings.Contains(web, "listen 80;") || strings.Contains(web, "listen [::]:80;") || strings.Contains(web, "litecp_traffic") {
		t.Fatal("private worker depends on public listeners or shared log formats")
	}
	if !strings.Contains(web, "listen unix:"+resourceSocket(p.Site.ID, "http")) || !strings.Contains(web, "limit_conn website 8") {
		t.Fatal("private HTTP boundary or concurrency allowance missing")
	}
	for _, bad := range []string{"fastcgi_pass", "try_files", "expires 7d"} {
		if strings.Contains(front, bad) {
			t.Fatalf("shared frontend still executes website work: %s", bad)
		}
	}
	if !strings.Contains(front, "proxy_pass http://unix:") || !strings.Contains(front, "/blocked)") || !strings.Contains(front, "due to resource limits") {
		t.Fatal("frontend must proxy every request and fail closed")
	}
	unit := resourceServiceUnit(p, "web", "/usr/sbin/nginx -c /managed.conf")
	for _, value := range []string{"Slice=" + resourceSliceName(p.Site.ID), "OOMPolicy=kill", "KillMode=control-group", "NoNewPrivileges=yes", "CapabilityBoundingSet=~CAP_SYS_RESOURCE", "BindPaths=" + resourceProtectedStorage(p.Site) + "/tmp:/tmp"} {
		if !strings.Contains(unit, value) {
			t.Fatalf("missing boundary property %s", value)
		}
	}
	if !strings.Contains(filepath.ToSlash(web), filepath.ToSlash(filepath.Join(resourceProtectedStorage(p.Site), "web", "body"))) {
		t.Fatal("HTTP request temporary files bypass website-home disk quota")
	}
}
func TestResourceNativeRejectsUncoveredExecutionPaths(t *testing.T) {
	base := nativeResourcePlan()
	if err := resourceRuntimeValidate(base); err != nil {
		t.Fatal(err)
	}
	cases := []resourceRuntimePlan{}
	p := base
	p.Site.Root = "/etc"
	cases = append(cases, p)
	p = base
	p.Site.ID = "../../system"
	cases = append(cases, p)
	p = base
	p.Site.RuntimeUID = 0
	cases = append(cases, p)
	p = base
	p.Site.PHPVersion = "7.3"
	cases = append(cases, p)
	p = base
	p.Site.Application = &ApplicationHosting{Language: "node", Enabled: true}
	cases = append(cases, p)
	p = base
	p.Policy.WriteMiBPerSecond = 10
	cases = append(cases, p)
	for i, p := range cases {
		if resourceRuntimeValidate(p) == nil {
			t.Fatalf("unsafe/uncovered plan %d accepted", i)
		}
	}
}
func TestResourceCronTestUsesSameSliceOnlyWhenEnabled(t *testing.T) {
	p := nativeResourcePlan()
	site := p.Site
	if strings.Contains(strings.Join(cronTestArguments(site), " "), "--slice=") {
		t.Fatal("disabled website unexpectedly changed its cron boundary")
	}
	site.ResourceLimits = &p.Policy
	if !strings.Contains(strings.Join(cronTestArguments(site), " "), "--slice="+resourceSliceName(site.ID)) {
		t.Fatal("cron test escapes enabled website slice")
	}
}

func TestResourceRetrySurvivesMonitorHeartbeatAndIsConsumedOnce(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	st := resourceRuntimeStatus{resourcePolicyState: resourcePolicyState{State: "Resource blocked", NextRecovery: now.Add(5 * time.Minute)}, RetryRequestedAt: now.Add(-time.Minute)}
	if err := resourceWriteRetry(dir, now); err != nil {
		t.Fatal(err)
	}
	// A status heartbeat retains its old cooldown; the separate request survives.
	st = resourceConsumeRetry(dir, st)
	if !st.NextRecovery.Equal(now) {
		t.Fatal("recovery request was lost")
	}
	later := now.Add(5 * time.Minute)
	st.NextRecovery = later
	st = resourceConsumeRetry(dir, st)
	if !st.NextRecovery.Equal(later) {
		t.Fatal("old retry bypassed a later cooldown")
	}
	if err := resourceWriteRetry(dir, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	st = resourceConsumeRetry(dir, st)
	if !st.NextRecovery.Equal(now.Add(time.Second)) {
		t.Fatal("new retry was ignored")
	}
}
