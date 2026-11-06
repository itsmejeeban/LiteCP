package main

import (
	"encoding/json"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCaptchaMetricsAndClearanceScope(t *testing.T) {
	g := captchaFixture()
	r := httptest.NewRequest("GET", "http://localhost/metrics", nil)
	r.Header.Set("X-LiteCP-Mode", "metrics")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("metrics accepted without secret")
	}
	r.Header.Set("Authorization", "Bearer "+g.runtime.Key)
	w = httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("authenticated metrics rejected")
	}
	record := g.runtime.Sites["test"]
	r = httptest.NewRequest("GET", "https://audit.example.com/", nil)
	r.Header.Set("X-LiteCP-IP", "192.0.2.1")
	r.AddCookie(&http.Cookie{Name: "__Host-litecp_clearance", Value: g.clearance("test", record, captchaBinding(r), g.now().Add(time.Minute).Unix())})
	if !g.verified(r, "test", record) {
		t.Fatal("valid clearance rejected")
	}
	record.Policy.Revision++
	if g.verified(r, "test", record) {
		t.Fatal("old policy clearance accepted")
	}
	record = g.runtime.Sites["test"]
	record.Domain = "other.example.com"
	if g.verified(r, "test", record) {
		t.Fatal("cross-domain clearance accepted")
	}
}

func TestCaptchaNginxParser(t *testing.T) {
	binary, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("Nginx parser unavailable on this host")
	}
	root := t.TempDir()
	policy := captchaTestPolicy()
	site := Site{ID: "parser", Domain: "captcha.example.test", Captcha: &policy}
	config := "pid " + filepath.ToSlash(filepath.Join(root, "nginx.pid")) + ";\nerror_log stderr;\nevents {}\nhttp { access_log off;\n" + captchaZones([]Site{site}) + injectCaptchaConfig("server {\nlisten 127.0.0.1:19876;\nserver_name captcha.example.test;\nlocation / { root /var/empty; }\n}\n", site) + "}\n"
	path := filepath.Join(root, "nginx.conf")
	if err = os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(binary, "-t", "-p", root+string(os.PathSeparator), "-c", path).CombinedOutput(); err != nil {
		t.Fatalf("CAPTCHA Nginx syntax rejected: %v %s", err, output)
	}
}

func captchaTestPolicy() CaptchaPolicy {
	return CaptchaPolicy{Mode: "challenge", ClearanceMinutes: 30, RequestsPerSecond: 10, SiteRequestsPerSecond: 200, Connections: 20, AttemptsPerMinute: 30, ChallengesPerMinute: 300, Revision: 1}
}
func captchaFixture() *captchaGateway {
	g := newCaptchaGateway()
	now := time.Now()
	g.now = func() time.Time { return now }
	g.loaded = now
	g.runtime = captchaRuntime{Key: strings.Repeat("a", 64), Sites: map[string]captchaRuntimeSite{"test": {Domain: "audit.example.com", Policy: captchaTestPolicy()}}}
	return g
}
func captchaRequest(g *captchaGateway, mode, path, method, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:9092"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-LiteCP-Mode", mode)
	r.Header.Set("X-LiteCP-Site", "test")
	r.Header.Set("X-LiteCP-Host", "audit.example.com")
	r.Header.Set("X-LiteCP-IP", "203.0.113.10")
	r.Header.Set("User-Agent", "Audit browser")
	r.Header.Set("X-LiteCP-Scheme", "https")
	r.Header.Set("X-LiteCP-URI", "/private/page?x=1")
	r.Header.Set("X-LiteCP-Method", method)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	return w
}
func issueCaptcha(t *testing.T, g *captchaGateway) string {
	t.Helper()
	w := captchaRequest(g, "challenge", "/private/page", "GET", "", nil)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "Verify to continue") {
		t.Fatal(w.Code, w.Body.String())
	}
	for id := range g.challenges {
		return id
	}
	t.Fatal("challenge not retained")
	return ""
}
func TestCaptchaChallengeClearanceAndReplay(t *testing.T) {
	g := captchaFixture()
	if w := captchaRequest(g, "check", "/check", "GET", "", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	id := issueCaptcha(t, g)
	image := captchaRequest(g, "public", captchaPrefix+"image?id="+id, "GET", "", nil)
	if image.Code != 200 {
		t.Fatal(image.Code)
	}
	decoded, err := png.Decode(image.Body)
	if err != nil || decoded.Bounds().Dx() != 264 {
		t.Fatal("invalid challenge image", err)
	}
	answer := captchaDigits(g.runtime.Key, id)
	body := url.Values{"id": {id}, "answer": {answer}}.Encode()
	w := captchaRequest(g, "public", captchaPrefix+"verify", "POST", body, nil)
	if w.Code != 303 || w.Header().Get("Location") != "/private/page?x=1" {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].Domain != "" {
		t.Fatal("unsafe clearance cookie")
	}
	cookie := cookies[0]
	if w := captchaRequest(g, "check", "/check", "GET", "", cookie); w.Code != 204 {
		t.Fatal("verified browser denied", w.Code)
	}
	if w := captchaRequest(g, "public", captchaPrefix+"verify", "POST", body, nil); w.Code != 403 {
		t.Fatal("challenge replay accepted")
	}
	cookie.Value += "forged"
	if w := captchaRequest(g, "check", "/check", "GET", "", cookie); w.Code != 401 {
		t.Fatal("forged cookie accepted")
	}
	if g.stats["test"].Passed != 1 || g.stats["test"].Failed != 1 {
		t.Fatal("incorrect counters")
	}
}
func TestCaptchaInvalidExpiryBindingAndLimits(t *testing.T) {
	g := captchaFixture()
	id := issueCaptcha(t, g)
	challenge := g.challenges[id]
	challenge.Expires = g.now().Add(-time.Second)
	g.challenges[id] = challenge
	if w := captchaRequest(g, "public", captchaPrefix+"verify", "POST", url.Values{"id": {id}, "answer": {captchaDigits(g.runtime.Key, id)}}.Encode(), nil); w.Code != 403 {
		t.Fatal("expired challenge accepted")
	}
	delete(g.challenges, id)
	id = issueCaptcha(t, g)
	body := url.Values{"id": {id}, "answer": {"wrong"}}.Encode()
	if w := captchaRequest(g, "public", captchaPrefix+"verify", "POST", body, nil); w.Code != 403 {
		t.Fatal("incorrect answer accepted")
	}
	if _, ok := g.challenges[id]; ok {
		t.Fatal("incorrect answer did not consume challenge")
	}
	g = captchaFixture()
	for i := 0; i < 30; i++ {
		_ = captchaRequest(g, "challenge", "/", "GET", "", nil)
	}
	if w := captchaRequest(g, "challenge", "/", "GET", "", nil); w.Code != 429 {
		t.Fatal("challenge rate limit bypassed", w.Code)
	}
	g = captchaFixture()
	if w := captchaRequest(g, "challenge", "/", "POST", "a=1", nil); w.Code != 403 || !strings.Contains(w.Body.String(), "not replayed") {
		t.Fatal("unsafe POST challenge handling")
	}
}
func TestCaptchaMonitorExemptionsAndCookieScope(t *testing.T) {
	g := captchaFixture()
	record := g.runtime.Sites["test"]
	record.Policy.Mode = "monitor"
	g.runtime.Sites["test"] = record
	if w := captchaRequest(g, "check", "/check", "GET", "", nil); w.Code != 204 {
		t.Fatal("monitor blocked visitor")
	}
	record.Policy.Mode = "challenge"
	record.Policy.Exclusions = []string{"/private"}
	g.runtime.Sites["test"] = record
	if w := captchaRequest(g, "check", "/check", "GET", "", nil); w.Code != 204 {
		t.Fatal("explicit path exclusion ignored")
	}
	for _, path := range []string{"https://evil.example/", "//evil.example/", "/\\evil.example/", "/_litecp_captcha/verify", "/\r\nHost:evil"} {
		if safeCaptchaReturn(path) != "/" {
			t.Fatal("unsafe redirect accepted", path)
		}
	}
	binding := strings.Repeat("a", 32)
	token := g.clearance("test", record, binding, g.now().Add(time.Hour).Unix())
	r := httptest.NewRequest("GET", "https://audit.example.com", nil)
	r.AddCookie(&http.Cookie{Name: "__Host-litecp_clearance", Value: token})
	if g.verified(r, "test", record) {
		t.Fatal("clearance with different browser binding accepted")
	}
}
func TestCaptchaNginxIntegrationAndValidation(t *testing.T) {
	policy := captchaTestPolicy()
	site := Site{ID: "captcha-audit", Domain: "audit.example.com", Root: "/home/litecp/audit.example.com/public_html", PHPVersion: "8.4", AppType: "regular", Captcha: &policy}
	system := &System{demo: true}
	config := system.nginxConfig(site)
	for _, text := range []string{"auth_request /_litecp_captcha_check;", "location @litecp_captcha", "location ^~ /_litecp_captcha/", "limit_req_status 429;", "location ~ \\.php$", "location ~* \\.(?:css"} {
		if !strings.Contains(config, text) {
			t.Fatal("missing protected route or control", text)
		}
	}
	if injectCaptchaConfig(config, site) != config {
		t.Fatal("CAPTCHA injection is not idempotent")
	}
	site.Captcha = nil
	if strings.Contains(injectCaptchaConfig(config, site), "auth_request") {
		t.Fatal("disabled policy leaves protection active")
	}
	policy.Exclusions = []string{"/"}
	if policy.validate() == nil {
		t.Fatal("entire website bypass accepted")
	}
	policy.Exclusions = []string{"/webhook"}
	if err := policy.validate(); err != nil {
		t.Fatal(err)
	}
	site.Captcha = &policy
	site.Application = &ApplicationHosting{Language: "node", Enabled: true, Port: 20001}
	if !strings.Contains(system.applicationNginx(site), "auth_request /_litecp_captcha_check;") {
		t.Fatal("application hosting bypasses protection")
	}
}
func TestCaptchaAuthenticatedSettingsAndPersistence(t *testing.T) {
	a := apiTestApp(t)
	a.store.cfg.Sites = []Site{{ID: "test", Domain: "audit.example.com"}}
	payload, _ := json.Marshal(map[string]any{"siteId": "test", "settings": captchaTestPolicy()})
	r := httptest.NewRequest("PUT", "https://panel.example/api/security/captcha", strings.NewReader(string(payload)))
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("unauthenticated settings mutation accepted")
	}
	r = httptest.NewRequest("PUT", "https://panel.example/api/security/captcha", strings.NewReader(string(payload)))
	r.Header.Set("Origin", "https://panel.example")
	r.AddCookie(&http.Cookie{Name: "litecp_session", Value: "test-session"})
	w = httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	reopened, err := OpenStore(filepath.Dir(a.store.path))
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Sites()[0].Captcha == nil {
		t.Fatal("CAPTCHA policy not persisted")
	}
	// A settings fixture contains no signing secret in public JSON.
	r = httptest.NewRequest("GET", "https://panel.example/api/security/captcha", nil)
	r.AddCookie(&http.Cookie{Name: "litecp_session", Value: "test-session"})
	w = httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	data, _ := io.ReadAll(w.Result().Body)
	if strings.Contains(string(data), "captcha-key") {
		t.Fatal("signing secret leaked")
	}
}
