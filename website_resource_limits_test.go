package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validResourcePolicy() WebsiteResourceLimits {
	return WebsiteResourceLimits{Enabled: true, CPUPercent: 50, MemoryMiB: 256, DiskMiB: 1024, Tasks: 64, ConcurrentRequests: 8}
}

func TestResourcePolicyCPUConsecutiveAndRecovery(t *testing.T) {
	p := validResourcePolicy()
	state := resourcePolicyState{State: "Active"}
	sample := resourceSample{At: time.Unix(1000, 0)}
	for i := 0; i < 10; i++ {
		next := sample
		next.At = next.At.Add(time.Second)
		next.UsageUsec += 500000
		next.ThrottledUsec += 20000
		next.ThrottledPeriods++
		state, _ = evaluateResourcePolicy(p, state, sample, next)
		sample = next
		if i < 9 && state.State != "Active" {
			t.Fatal("blocked before ten seconds")
		}
	}
	if state.State != "Resource blocked" || state.Reason != "CPU" {
		t.Fatalf("unexpected: %+v", state)
	}
	early := sample
	early.At = early.At.Add(299 * time.Second)
	state, events := evaluateResourcePolicy(p, state, sample, early)
	if len(events) != 0 {
		t.Fatal("early recovery")
	}
	early.At = early.At.Add(time.Second)
	state, events = evaluateResourcePolicy(p, state, sample, early)
	if state.State != "Recovering" || len(events) != 1 {
		t.Fatal("recovery not due")
	}
}

func TestResourcePolicyBreaksStreak(t *testing.T) {
	p := validResourcePolicy()
	before := resourceSample{At: time.Unix(1000, 0), UsageUsec: 1000000}
	next := before
	next.At = next.At.Add(time.Second)
	state, _ := evaluateResourcePolicy(p, resourcePolicyState{State: "Active", CPUExhaustedFor: 9 * time.Second}, before, next)
	if state.CPUExhaustedFor != 0 || state.State != "Active" {
		t.Fatal(state)
	}
	next.At = before.At.Add(3 * time.Second)
	next.UsageUsec += 1500000
	next.ThrottledUsec++
	next.ThrottledPeriods++
	state, _ = evaluateResourcePolicy(p, resourcePolicyState{State: "Active", CPUExhaustedFor: 9 * time.Second}, before, next)
	if state.CPUExhaustedFor != 0 {
		t.Fatal("missed interval counted")
	}
}

func TestResourcePolicyDiskAndOOM(t *testing.T) {
	p := validResourcePolicy()
	p.DiskMiB = 1000
	before := resourceSample{At: time.Unix(1000, 0)}
	next := before
	next.At = next.At.Add(time.Second)
	next.OOMKills = 1
	state, _ := evaluateResourcePolicy(p, resourcePolicyState{State: "Active"}, before, next)
	if state.Reason != "Memory" {
		t.Fatal(state)
	}
	next.OOMKills = 0
	next.DiskBytes = p.DiskMiB * 1048576
	state, _ = evaluateResourcePolicy(p, resourcePolicyState{State: "Active"}, before, next)
	if state.Reason != "Disk" {
		t.Fatal(state)
	}
	next.At = state.NextRecovery
	next.DiskBytes = int64(float64(p.DiskMiB*1048576) * .95)
	state, events := evaluateResourcePolicy(p, state, before, next)
	if len(events) != 0 || state.State != "Resource blocked" {
		t.Fatal("recovered at 95 percent")
	}
	next.At = state.NextRecovery
	next.DiskBytes--
	state, events = evaluateResourcePolicy(p, state, before, next)
	if len(events) != 1 || state.State != "Recovering" {
		t.Fatal(state)
	}
}

func TestResourcePolicyPIDsDoNotBlock(t *testing.T) {
	p := validResourcePolicy()
	before := resourceSample{At: time.Unix(1000, 0)}
	next := before
	next.At = next.At.Add(time.Second)
	next.PIDsDenied = 1
	state, events := evaluateResourcePolicy(p, resourcePolicyState{State: "Active"}, before, next)
	if state.State != "Active" || len(events) != 1 {
		t.Fatal(state, events)
	}
	p.Enabled = false
	state, _ = evaluateResourcePolicy(p, state, before, next)
	if state.State != "Disabled" {
		t.Fatal(state)
	}
}

func TestResourceMountUsesNestedMount(t *testing.T) {
	info := "1 0 1:1 / / rw - xfs /dev/a rw,prjquota\n2 1 1:2 / /home/litecp rw - ext4 /dev/b rw\n"
	kind, mount, quota := resourceMount("/home/litecp/example.com", info)
	if kind != "ext4" || mount != "/home/litecp" || quota {
		t.Fatal(kind, mount, quota)
	}
	_, _, quota = resourceMount("/home/litecp-other/site", info)
	if !quota {
		t.Fatal("sibling confused with child mount")
	}
}

func TestResourceValidationAndIdentity(t *testing.T) {
	p := validResourcePolicy()
	if err := p.validate(); err != nil {
		t.Fatal(err)
	}
	p.DiskMiB = 0
	if p.validate() == nil {
		t.Fatal("missing quota accepted")
	}
	if resourceSliceName("a-b") == resourceSliceName("a_b") || strings.Contains(strings.TrimSuffix(resourceSliceName("a-b"), ".slice"), "-") {
		t.Fatal("slice alias or hierarchy injection")
	}
}

func TestResourceActivationFailsWithoutMutatingSite(t *testing.T) {
	p := validResourcePolicy()
	body, _ := json.Marshal(p)
	app := &App{store: &Store{cfg: Config{Sites: []Site{{ID: "test", Domain: "example.test"}}}}}
	req := httptest.NewRequest("PUT", "/api/sites/resource-limits?siteId=test", strings.NewReader(string(body)))
	result := httptest.NewRecorder()
	app.websiteResourceLimits(result, req)
	if result.Code != 409 {
		t.Fatalf("expected refusal, got %d: %s", result.Code, result.Body.String())
	}
	if app.store.SiteByID("test").ResourceLimits != nil {
		t.Fatal("failed activation changed settings")
	}
	missing := httptest.NewRecorder()
	app.websiteResourceLimits(missing, httptest.NewRequest("GET", "/api/sites/resource-limits?siteId=missing", nil))
	if missing.Code != 404 {
		t.Fatal(missing.Code)
	}
}

func TestResourceBulkCreatesIndependentDrafts(t *testing.T) {
	app := apiTestApp(t)
	policy := validResourcePolicy()
	policy.Enabled = false
	existing := validResourcePolicy()
	existing.CPUPercent = 75
	app.store.cfg.Sites = []Site{
		{ID: "first", Domain: "first.test"},
		{ID: "second", Domain: "second.test", Suspended: true},
		{ID: "existing", Domain: "existing.test", ResourceLimits: &existing},
	}
	submit := func(p WebsiteResourceLimits, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "https://panel.example/api/sites/resource-limits/bulk", strings.NewReader(string(body)))
		req.Header.Set("Origin", "https://panel.example")
		if authenticated {
			req.AddCookie(&http.Cookie{Name: "litecp_session", Value: "test-session"})
		}
		result := httptest.NewRecorder()
		app.routes().ServeHTTP(result, req)
		return result
	}
	if result := submit(policy, false); result.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated request returned %d", result.Code)
	}
	if result := submit(existing, true); result.Code != http.StatusBadRequest {
		t.Fatalf("bulk enforcement returned %d", result.Code)
	}
	if result := submit(WebsiteResourceLimits{}, true); result.Code != http.StatusBadRequest {
		t.Fatalf("invalid limits returned %d", result.Code)
	}
	for attempt, expected := range []string{`{"created":2,"skipped":1}`, `{"created":0,"skipped":3}`} {
		result := submit(policy, true)
		if result.Code != http.StatusOK || strings.TrimSpace(result.Body.String()) != expected {
			t.Fatalf("attempt %d: %d %s", attempt+1, result.Code, result.Body.String())
		}
	}
	sites := app.store.Sites()
	if sites[0].ResourceLimits == sites[1].ResourceLimits {
		t.Fatal("websites share a mutable policy")
	}
	changed := policy
	changed.CPUPercent = 125
	if err := app.store.UpdateSite("first", func(site *Site) { site.ResourceLimits = &changed }); err != nil {
		t.Fatal(err)
	}
	persisted, err := OpenStore(filepath.Dir(app.store.path))
	if err != nil {
		t.Fatal(err)
	}
	sites = persisted.Sites()
	if *sites[0].ResourceLimits != changed || *sites[1].ResourceLimits != policy || !sites[1].Suspended || *sites[2].ResourceLimits != existing {
		t.Fatal("saved policies or suspension were changed unexpectedly")
	}
	if _, total := persisted.Activities(1, 10); total != 1 {
		t.Fatalf("expected one bulk activity, got %d", total)
	}
}

func TestResourceBulkDraftsRollBackOnSaveFailure(t *testing.T) {
	policy := validResourcePolicy()
	policy.Enabled = false
	store := &Store{path: filepath.Join(t.TempDir(), "missing", "config.json")}
	if created, skipped, err := store.CreateResourceLimitDrafts(policy); created != 0 || skipped != 0 || err != nil {
		t.Fatalf("empty store should succeed without a save: %d %d %v", created, skipped, err)
	}
	store.cfg.Sites = []Site{{ID: "first", Domain: "first.test"}, {ID: "second", Domain: "second.test"}}
	if _, _, err := store.CreateResourceLimitDrafts(policy); err == nil {
		t.Fatal("expected save failure")
	}
	for _, site := range store.Sites() {
		if site.ResourceLimits != nil {
			t.Fatalf("settings for %s survived a failed save", site.Domain)
		}
	}
	if _, total := store.Activities(1, 10); total != 0 {
		t.Fatal("failed bulk save created an activity")
	}
}
