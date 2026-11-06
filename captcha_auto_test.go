package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCaptchaAutomaticResourceTransitions(t *testing.T) {
	now := time.Now()
	p := captchaTestPolicy()
	p.Mode = "auto"
	site := Site{ID: "test", Captcha: &p, ResourceLimits: &WebsiteResourceLimits{Enabled: true, CPUPercent: 10, MemoryMiB: 64, DiskMiB: 100, Tasks: 16}}
	record := captchaRuntimeSite{Domain: "audit.example.com", Policy: p}
	healthy := resourceRuntimeStatus{resourcePolicyState: resourcePolicyState{State: "Active"}, Enforced: true}
	if p.validate() != nil {
		t.Fatal("automatic mode rejected")
	}
	for _, name := range []string{"CPU", "Memory", "Disk", "Processes", "Blocked", "Recovering", "Error"} {
		t.Run(name, func(t *testing.T) {
			st := healthy
			switch name {
			case "CPU":
				st.CPUExhaustedFor = time.Second
			case "Memory":
				st.MemoryBytes = 64 * 1048576
			case "Disk":
				st.Sample.DiskBytes = 100 * 1048576
			case "Processes":
				st.Processes = 16
			case "Blocked":
				st.State = "Resource blocked"
			case "Recovering":
				st.State = "Recovering"
			case "Error":
				st.Enforced = false
			}
			active := captchaResourceSignal(site, record, st, now)
			if !active.AutoUntil.Equal(now.Add(5*time.Minute)) || !active.AutoStarted.Equal(now) {
				t.Fatal("resource breach did not activate cooldown")
			}
			next := captchaResourceSignal(site, active, healthy, now.Add(time.Minute))
			if next.ResourceBlocked || !next.AutoUntil.Equal(active.AutoUntil) || !next.AutoStarted.Equal(active.AutoStarted) {
				t.Fatal("healthy recovery lost CAPTCHA cooldown")
			}
			again := captchaResourceSignal(site, next, st, now.Add(6*time.Minute))
			if !again.AutoStarted.After(active.AutoStarted) {
				t.Fatal("new overload did not invalidate old clearance")
			}
		})
	}
	idle := captchaResourceSignal(site, record, healthy, now)
	if !idle.AutoUntil.IsZero() || idle.ResourceBlocked {
		t.Fatal("healthy site challenged")
	}
	site.ResourceLimits.Enabled = false
	cleared := captchaResourceSignal(site, captchaRuntimeSite{Policy: p, AutoUntil: now.Add(time.Minute), ResourceBlocked: true}, healthy, now)
	if !cleared.AutoUntil.IsZero() || cleared.ResourceBlocked {
		t.Fatal("disabled resource limits kept automatic mode active")
	}
}

func TestCaptchaAutomaticGatewayAndHardBlock(t *testing.T) {
	g := captchaFixture()
	record := g.runtime.Sites["test"]
	record.Policy.Mode = "auto"
	g.runtime.Sites["test"] = record
	if w := captchaRequest(g, "check", "/check", "GET", "", nil); w.Code != 204 {
		t.Fatal("idle automatic mode blocked visitor")
	}
	record.AutoUntil = g.now().Add(5 * time.Minute)
	record.AutoStarted = g.now()
	g.runtime.Sites["test"] = record
	if w := captchaRequest(g, "check", "/check", "GET", "", nil); w.Code != 401 {
		t.Fatal("active automatic mode did not challenge visitor")
	}
	// A cleared visitor remains subject to the resource safety block.
	r := http.Request{Header: make(http.Header)}
	r.Header.Set("X-LiteCP-IP", "203.0.113.10")
	r.Header.Set("User-Agent", "Audit browser")
	cookie := &http.Cookie{Name: "__Host-litecp_clearance", Value: g.clearance("test", record, captchaBinding(&r), g.now().Add(time.Minute).Unix())}
	record.ResourceBlocked = true
	g.runtime.Sites["test"] = record
	if w := captchaRequest(g, "challenge", "/", "GET", "", cookie); w.Code != 503 {
		t.Fatal("CAPTCHA clearance bypassed resource safety block", w.Code)
	}
	if w := captchaRequest(g, "check", "/check", "GET", "", cookie); w.Code != 401 {
		t.Fatal("blocked resource bypassed auth gate")
	}
	if w := captchaRequest(g, "challenge", "/", "GET", "", nil); w.Code != 403 || !strings.Contains(w.Body.String(), "Verify to continue") {
		t.Fatal("blocked website did not offer verification")
	}
	record.ResourceBlocked = false
	record.AutoUntil = g.now().Add(-time.Second)
	g.runtime.Sites["test"] = record
	if w := captchaRequest(g, "check", "/check", "GET", "", nil); w.Code != 204 {
		t.Fatal("automatic cooldown did not expire")
	}
}

func TestCaptchaAutomaticRoutingRestoresResourceGate(t *testing.T) {
	p := captchaTestPolicy()
	p.Mode = "auto"
	site := Site{ID: "test", Domain: "audit.example.com", Captcha: &p, ResourceLimits: &WebsiteResourceLimits{Enabled: true}}
	gate := " if (-f " + resourceDirectory(site.ID) + "/blocked) { return 503 'This website is temporarily unavailable due to resource limits. Please try again later.'; }\n"
	original := "# LiteCP enforced website resource boundary\nserver {\n" + gate + " if ($host = manual-suspension.test) { return 503; }\n location / { proxy_pass http://unix:/private/http.sock:; error_page 502 503 =503 @resource_unavailable; }\n}\n"
	auto := injectCaptchaConfig(original, site)
	if strings.Contains(auto, gate) || !strings.Contains(auto, "manual-suspension.test") || !strings.Contains(auto, "auth_request /_litecp_captcha_check;") {
		t.Fatal("automatic mode lost isolation or manual suspension")
	}
	if !strings.Contains(auto, captchaResourceProxyErrors) {
		t.Fatal("resource error handling overrides CAPTCHA")
	}
	if injectCaptchaConfig(auto, site) != auto {
		t.Fatal("automatic injection not idempotent")
	}
	p.Mode = "off"
	disabled := injectCaptchaConfig(auto, site)
	if strings.Contains(disabled, captchaResourceProxyErrors) {
		t.Fatal("disabled CAPTCHA left a named error handler")
	}
	if strings.Count(disabled, gate) != 1 || strings.Contains(disabled, "auth_request") {
		t.Fatal("turning off CAPTCHA did not restore resource gate")
	}
	if injectCaptchaConfig(disabled, site) != disabled {
		t.Fatal("restored resource gate duplicated")
	}
}
