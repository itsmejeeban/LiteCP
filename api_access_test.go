package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func apiTestApp(t *testing.T) *App {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.cfg.Admin.Username = "admin"
	return &App{store: store, system: &System{demo: true}, sessions: map[string]session{"test-session": {Expires: time.Now().Add(time.Hour)}}}
}
func apiManage(t *testing.T, a *App, payload any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(payload)
	r := httptest.NewRequest("POST", "https://panel.example/api/access", bytes.NewReader(b))
	r.Header.Set("Origin", "https://panel.example")
	r.AddCookie(&http.Cookie{Name: "litecp_session", Value: "test-session"})
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	return w
}
func apiTestKey(t *testing.T, a *App) (string, string) {
	t.Helper()
	w := apiManage(t, a, map[string]any{"operation": "save-rule", "rule": APIRule{Name: "Read sites", Permissions: []string{"sites.list"}}})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	rule := a.store.cfg.APIRules[0]
	w = apiManage(t, a, map[string]any{"operation": "create-credential", "credential": APICredential{Description: "Test integration", Enabled: true, RateLimit: 60, RuleIDs: []string{rule.ID}}})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var result map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	return result["identifier"].(string), result["secret"].(string)
}
func apiCall(a *App, id, secret, action, params string) *httptest.ResponseRecorder {
	if params == "" {
		params = "{}"
	}
	r := httptest.NewRequest("POST", "https://panel.example/api/v1", strings.NewReader(`{"action":"`+action+`","parameters":`+params+`}`))
	r.RemoteAddr = "203.0.113.10:1234"
	r.Header.Set("X-LiteCP-Identifier", id)
	r.Header.Set("X-LiteCP-Secret", secret)
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	return w
}
func TestAPISecretsPermissionsAndPersistence(t *testing.T) {
	a := apiTestApp(t)
	id, secret := apiTestKey(t, a)
	content, err := os.ReadFile(a.store.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(content, []byte(secret)) {
		t.Fatal("raw API secret persisted")
	}
	reopened, err := OpenStore(filepath.Dir(a.store.path))
	if err != nil {
		t.Fatal(err)
	}
	a.store = reopened
	if got := apiCall(a, id, secret, "sites.list", ""); got.Code != 200 {
		t.Fatal(got.Code, got.Body.String())
	}
	if a.store.cfg.APICredentials[0].LastAccess == nil || a.store.cfg.APICredentials[0].LastIP != "203.0.113.10" {
		t.Fatal("last access not recorded")
	}
	if got := apiCall(a, id, "wrong", "sites.list", ""); got.Code != 401 {
		t.Fatal("wrong secret accepted", got.Code)
	}
	if got := apiCall(a, id, secret, "sites.create", `{"domain":"bad.example"}`); got.Code != 403 {
		t.Fatal("ungranted provisioning accepted", got.Code)
	}
	r := httptest.NewRequest("GET", "https://panel.example/api/access", nil)
	r.AddCookie(&http.Cookie{Name: "litecp_session", Value: "test-session"})
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), "secretHash") || strings.Contains(w.Body.String(), secret) {
		t.Fatal("management list leaked secret material")
	}
	a.store.cfg.APIRules[0].Permissions = []string{"databases.list"}
	if got := apiCall(a, id, secret, "sites.list", ""); got.Code != 403 {
		t.Fatal("rule changes did not take immediate effect")
	}
}
func TestAPIExpiryIPRateAndRevocation(t *testing.T) {
	a := apiTestApp(t)
	id, secret := apiTestKey(t, a)
	c := &a.store.cfg.APICredentials[0]
	c.AllowedIPs = []string{"198.51.100.0/24"}
	if got := apiCall(a, id, secret, "sites.list", ""); got.Code != 401 {
		t.Fatal("IP restriction bypassed")
	}
	c.AllowedIPs = []string{"203.0.113.0/24"}
	past := time.Now().Add(-time.Second)
	c.ExpiresAt = &past
	if got := apiCall(a, id, secret, "sites.list", ""); got.Code != 401 {
		t.Fatal("expired key accepted")
	}
	c.ExpiresAt = nil
	c.RateLimit = 1
	if got := apiCall(a, id, secret, "sites.list", ""); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	if got := apiCall(a, id, secret, "sites.list", ""); got.Code != 429 {
		t.Fatal("key rate limit bypassed")
	}
	w := apiManage(t, a, map[string]any{"operation": "revoke-credential", "id": id})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if got := apiCall(a, id, secret, "sites.list", ""); got.Code != 401 {
		t.Fatal("revoked key accepted")
	}
	candidate := a.store.cfg.APICredentials[0]
	candidate.Enabled = true
	w = apiManage(t, a, map[string]any{"operation": "save-credential", "credential": candidate})
	if w.Code != 400 {
		t.Fatal("revoked key re-enabled")
	}
}
func TestAPINoSessionFallbackAndManagementBoundary(t *testing.T) {
	a := apiTestApp(t)
	id, secret := apiTestKey(t, a)
	for _, path := range []string{"/api/access", "/api/state"} {
		r := httptest.NewRequest("GET", "https://panel.example"+path, nil)
		r.Header.Set("X-LiteCP-Identifier", id)
		r.Header.Set("X-LiteCP-Secret", secret)
		w := httptest.NewRecorder()
		a.routes().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("API key accepted by administrator route", path, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "https://panel.example/api/v1", strings.NewReader(`{"action":"sites.list"}`))
	r.AddCookie(&http.Cookie{Name: "litecp_session", Value: "test-session"})
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("browser session accepted as API credential")
	}
	r = httptest.NewRequest("POST", "http://panel.example/api/v1", strings.NewReader(`{"action":"sites.list"}`))
	r.Header.Set("X-LiteCP-Identifier", id)
	r.Header.Set("X-LiteCP-Secret", secret)
	r.RemoteAddr = "203.0.113.10:1234"
	r.Header.Set("X-Forwarded-Proto", "https")
	w = httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("untrusted forwarded HTTPS bypassed TLS requirement")
	}
	for _, path := range []string{"/api/access", "/api/v1"} {
		op, _, err := brokerOperation(a.routeMux(), path)
		if err != nil || op != path {
			t.Fatal("new operation unavailable via broker", path, err)
		}
	}
}
func TestAPIInvalidRulesAndAssignedRuleRemoval(t *testing.T) {
	a := apiTestApp(t)
	id, _ := apiTestKey(t, a)
	_ = id
	w := apiManage(t, a, map[string]any{"operation": "save-rule", "rule": APIRule{Name: "Escalate", Permissions: []string{"terminal.exec"}}})
	if w.Code != 400 {
		t.Fatal("unknown action accepted")
	}
	w = apiManage(t, a, map[string]any{"operation": "remove-rule", "id": a.store.cfg.APIRules[0].ID})
	if w.Code != 409 {
		t.Fatal("assigned rule removed")
	}
	if !apiIPAllowed([]string{"2001:db8::/64"}, "2001:db8::123") || apiIPAllowed([]string{"2001:db8::/64"}, "203.0.113.1") {
		t.Fatal("IPv6 CIDR matching failed")
	}
}

func TestAPIProvisioningUsesValidatedHandlers(t *testing.T) {
	a := apiTestApp(t)
	id, secret := apiTestKey(t, a)
	a.store.cfg.APIRules[0].Permissions = []string{"databases.create", "sites.delete", "mailboxes.list"}
	w := apiCall(a, id, secret, "databases.create", `{"name":"api_fixture","username":"api_fixture","password":"private-test-password-123"}`)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(a.store.cfg.Databases) != 1 || a.store.cfg.Databases[0].Name != "api_fixture" {
		t.Fatal("database handler was not dispatched")
	}
	a.store.cfg.Mailboxes = []Mailbox{{ID: "mail", Address: "audit@example.com", PasswordHash: "private-hash"}}
	w = apiCall(a, id, secret, "mailboxes.list", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "private-hash") {
		t.Fatal("mailbox list leaked hash")
	}
	a.store.cfg.Sites = []Site{{ID: "fixture", Domain: "audit.example.com"}}
	w = apiCall(a, id, secret, "sites.delete", `{"id":"fixture","domain":"wrong.example.com"}`)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "exact website domain") {
		t.Fatal("delete confirmation not enforced", w.Code, w.Body.String())
	}
	if len(a.store.cfg.Sites) != 1 {
		t.Fatal("website removed with wrong confirmation")
	}
}
