package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func whmcsFixture(t *testing.T) (*App, string, string) {
	t.Helper()
	a := apiTestApp(t)
	id, secret := apiTestKey(t, a)
	a.store.cfg.TrustWHMCSAdminSSO = true
	a.store.cfg.APIRules[0].Permissions = []string{"whmcs.server_info", "whmcs.admin_sso"}
	return a, id, secret
}

func whmcsIssue(t *testing.T, a *App, id, secret string) string {
	t.Helper()
	w := apiCall(a, id, secret, "whmcs.admin_sso", "")
	if w.Code != 200 { t.Fatal(w.Code, w.Body.String()) }
	var result struct { Ticket string `json:"ticket"`; ExpiresIn int `json:"expiresIn"` }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Ticket) < 40 || result.ExpiresIn != 60 {
		t.Fatal("invalid ticket response")
	}
	if _, exists := a.whmcsTickets[result.Ticket]; exists { t.Fatal("raw ticket stored") }
	return result.Ticket
}

func whmcsRedeem(a *App, ticket string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "https://panel.example/api/whmcs/sso", strings.NewReader(`{"ticket":"`+ticket+`"}`))
	r.Header.Set("Origin", "https://panel.example")
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "198.51.100.23:4444" // Browser IP differs from the allowed WHMCS API source.
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	return w
}

func TestWHMCSTicketLoginAndReplay(t *testing.T) {
	a, id, secret := whmcsFixture(t)
	a.store.cfg.Admin.TwoFactor.Enabled = true // Explicit trusted SSO bypasses the extra prompt.
	a.store.cfg.APICredentials[0].AllowedIPs = []string{"203.0.113.10"}
	a.store.cfg.Sites = []Site{{ID:"one", Domain:"example.test"}}
	w := apiCall(a, id, secret, "whmcs.server_info", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"websiteCount":1`) { t.Fatal(w.Code, w.Body.String()) }
	ticket := whmcsIssue(t, a, id, secret)
	w = whmcsRedeem(a, ticket)
	if w.Code != 200 { t.Fatal(w.Code, w.Body.String()) }
	var sessionCookie *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "litecp_session" && cookie.Value != "" { sessionCookie = cookie }
		if cookie.Name != "litecp_session" && cookie.Value != "" { t.Fatal("persistent trust created") }
	}
	if sessionCookie == nil || !sessionCookie.Secure || !sessionCookie.HttpOnly || sessionCookie.Path != "/api/" || sessionCookie.SameSite != http.SameSiteStrictMode || !a.validSession(sessionCookie.Value) {
		t.Fatal("normal secure administrator session not created")
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" { t.Fatal("unsafe response headers") }
	if whmcsRedeem(a, ticket).Code != 401 { t.Fatal("replayed ticket accepted") }
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	for _, event := range a.store.cfg.Activities {
		b, _ := json.Marshal(event)
		if strings.Contains(string(b), ticket) || strings.Contains(string(b), secret) { t.Fatal("secret logged") }
	}
}

func TestWHMCSTicketRevalidation(t *testing.T) {
	for _, scenario := range []string{"expired", "disabled", "revoked", "credential-expired", "permission", "setting", "owner", "secret", "source"} {
		t.Run(scenario, func(t *testing.T) {
			a, id, secret := whmcsFixture(t)
			ticket := whmcsIssue(t, a, id, secret)
			switch scenario {
			case "expired":
				pending := a.whmcsTickets[apiSecretHash(ticket)]; pending.Expires = time.Now().Add(-time.Second); a.whmcsTickets[apiSecretHash(ticket)] = pending
			case "disabled": a.store.cfg.APICredentials[0].Enabled = false
			case "revoked": now := time.Now(); a.store.cfg.APICredentials[0].RevokedAt = &now
			case "credential-expired": past := time.Now().Add(-time.Second); a.store.cfg.APICredentials[0].ExpiresAt = &past
			case "permission": a.store.cfg.APIRules[0].Permissions = []string{"whmcs.server_info"}
			case "setting": a.store.cfg.TrustWHMCSAdminSSO = false
			case "owner": a.store.cfg.Admin.Username = "changed"
			case "secret": a.store.cfg.APICredentials[0].SecretHash = apiSecretHash("changed")
			case "source": a.store.cfg.APICredentials[0].AllowedIPs = []string{"192.0.2.1"}
			}
			if w := whmcsRedeem(a, ticket); w.Code != 401 { t.Fatal("unauthorized ticket accepted", w.Code) }
		})
	}
}

func TestWHMCSTicketConcurrentConsumption(t *testing.T) {
	a, id, secret := whmcsFixture(t)
	ticket := whmcsIssue(t, a, id, secret)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i:=0; i<2; i++ { wg.Add(1); go func(){ defer wg.Done(); codes <- whmcsRedeem(a, ticket).Code }() }
	wg.Wait(); close(codes)
	success := 0
	for code := range codes { if code == 200 { success++ } else if code != 401 { t.Fatal(code) } }
	if success != 1 { t.Fatal("ticket consumed more than once", success) }
}

func TestWHMCSSSOTrustAndProtocol(t *testing.T) {
	a, id, secret := whmcsFixture(t)
	a.store.cfg.TrustWHMCSAdminSSO = false
	if apiCall(a, id, secret, "whmcs.admin_sso", "").Code != 403 { t.Fatal("disabled trust accepted") }
	if apiCall(a, id, secret, "whmcs.server_info", "").Code != 200 { t.Fatal("information needs SSO trust") }
	a.store.cfg.APIRules[0].Permissions = []string{"panel.access.post"}
	if apiCall(a,id,secret,"panel.access.post",`{"operation":"save-whmcs-sso","trustWHMCSAdminSSO":true}`).Code != 403 { t.Fatal("API enabled trust") }
	if a.store.cfg.TrustWHMCSAdminSSO { t.Fatal("trust mutated") }
	if apiManage(t,a,map[string]any{"operation":"save-whmcs-sso","trustWHMCSAdminSSO":true}).Code != 200 || !a.store.cfg.TrustWHMCSAdminSSO { t.Fatal("owner cannot enable trust") }
	for _, target := range []string{"http://panel.example/api/whmcs/sso", "https://panel.example/api/whmcs/sso?ticket=secret"} {
		w:=httptest.NewRecorder(); a.routes().ServeHTTP(w,httptest.NewRequest("GET",target,nil)); if w.Code < 400 { t.Fatal("unsafe protocol accepted") }
	}
	r:=httptest.NewRequest("POST","https://panel.example/api/whmcs/sso",strings.NewReader(`{"ticket":"`+strings.Repeat("a",43)+`"}`))
	r.Header.Set("Origin","https://attacker.example"); r.Header.Set("Content-Type","application/json")
	w:=httptest.NewRecorder();a.routes().ServeHTTP(w,r);if w.Code!=403 { t.Fatal("cross-origin accepted") }
	if !brokerAllowedOperations["/api/whmcs/sso"] { t.Fatal("broker route missing") }
}

func TestWHMCSTicketBound(t *testing.T) {
	a,id,secret:=whmcsFixture(t)
	a.whmcsTickets=make(map[string]whmcsLoginTicket)
	for i:=0;i<256;i++ { a.whmcsTickets[strings.Repeat("x",i+1)] = whmcsLoginTicket{Expires:time.Now().Add(time.Minute)} }
	if apiCall(a,id,secret,"whmcs.admin_sso", "").Code != 429 { t.Fatal("ticket bound not enforced") }
}
