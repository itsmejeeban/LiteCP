package main

import (
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type whmcsLoginTicket struct {
	Credential string
	AdminUser  string
	SecretHash string
	SourceIP   string
	Expires    time.Time
}

// Caller holds store.mu. Recheck grants at redemption, not just issuance.
func (a *App) whmcsSSOCredentialAllowed(ticket whmcsLoginTicket) bool {
	if !a.store.cfg.TrustWHMCSAdminSSO || ticket.AdminUser != a.store.cfg.Admin.Username {
		return false
	}
	for _, credential := range a.store.cfg.APICredentials {
		if credential.Identifier != ticket.Credential || !credential.Enabled || credential.RevokedAt != nil ||
			credential.AdminUser != ticket.AdminUser || credential.SecretHash != ticket.SecretHash ||
			(credential.ExpiresAt != nil && !credential.ExpiresAt.After(time.Now())) ||
			!apiIPAllowed(credential.AllowedIPs, ticket.SourceIP) {
			continue
		}
		for _, rule := range a.store.cfg.APIRules {
			if containsAPIValue(credential.RuleIDs, rule.ID) && containsAPIValue(rule.Permissions, "whmcs.admin_sso") {
				return true
			}
		}
	}
	return false
}

func (a *App) issueWHMCSTicket(w http.ResponseWriter, r *http.Request, identifier string) {
	now := time.Now()
	ticket := whmcsLoginTicket{Credential: identifier, SourceIP: clientIP(r), Expires: now.Add(time.Minute)}
	a.store.mu.RLock()
	for _, credential := range a.store.cfg.APICredentials {
		if credential.Identifier == identifier {
			ticket.AdminUser = credential.AdminUser
			ticket.SecretHash = credential.SecretHash
			break
		}
	}
	allowed := a.whmcsSSOCredentialAllowed(ticket)
	a.store.mu.RUnlock()
	if !allowed {
		writeError(w, 403, "Enable trusted WHMCS administrator SSO and grant its dedicated API permission")
		return
	}
	value, err := randomToken(32)
	if err != nil {
		writeError(w, 500, "Could not issue login ticket")
		return
	}
	a.mu.Lock()
	if a.whmcsTickets == nil {
		a.whmcsTickets = make(map[string]whmcsLoginTicket)
	}
	for key, pending := range a.whmcsTickets {
		if !pending.Expires.After(now) {
			delete(a.whmcsTickets, key)
		}
	}
	if len(a.whmcsTickets) >= 256 {
		a.mu.Unlock()
		writeError(w, 429, "Too many pending login tickets; retry after one minute")
		return
	}
	a.whmcsTickets[apiSecretHash(value)] = ticket
	a.mu.Unlock()
	_ = a.store.AddActivity("WHMCS login ticket issued", "Dedicated API credential authorized an administrator login ticket", "info")
	// Do not log the response or raw ticket.
	writeJSON(w, 200, map[string]any{"ticket": value, "expiresIn": 60})
}

func (a *App) whmcsSSO(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !requestIsHTTPS(r) {
		writeError(w, 403, "WHMCS SSO requires HTTPS")
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, 400, "SSO does not accept query parameters")
		return
	}
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>LiteCP administrator login</title><script src="/whmcs-sso.js" defer></script></head><body><main><h1>LiteCP</h1><p id="sso-status" role="status">Signing in from WHMCS…</p><noscript>Enable JavaScript to complete secure login.</noscript><a href="/">Return to LiteCP</a></main></body></html>`))
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		writeError(w, 405, "Use GET or POST")
		return
	}
	// Redemption is a browser same-origin JSON request, never a cross-origin form.
	if r.Header.Get("Origin") == "" || !validOrigin(r) || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, 403, "Request origin was rejected")
		return
	}
	if !a.apiRateAllowed("whmcs-sso:"+clientIP(r), 30) {
		writeError(w, 429, "Too many login attempts")
		return
	}
	var input struct {
		Ticket string `json:"ticket"`
	}
	if err := decodeJSONWithLimit(r, &input, 1024); err != nil || len(input.Ticket) < 40 || len(input.Ticket) > 128 {
		writeError(w, 400, "Invalid login ticket")
		return
	}
	// Serialize with credential/settings mutations so revocation cannot race login.
	a.mutationMu.Lock()
	defer a.mutationMu.Unlock()
	a.store.mu.RLock()
	key := apiSecretHash(input.Ticket)
	a.mu.Lock()
	ticket, exists := a.whmcsTickets[key]
	delete(a.whmcsTickets, key)
	a.mu.Unlock()
	allowed := exists && ticket.Expires.After(time.Now()) && a.whmcsSSOCredentialAllowed(ticket)
	if !allowed {
		a.store.mu.RUnlock()
		writeError(w, 401, "Login ticket expired, was already used, or is no longer authorized. Retry from WHMCS.")
		return
	}
	token, err := randomToken(32)
	if err != nil {
		a.store.mu.RUnlock()
		writeError(w, 500, "Could not create a session; retry from WHMCS")
		return
	}
	expires := time.Now().Add(12 * time.Hour)
	a.rememberSession(token, expires)
	a.store.mu.RUnlock()
	// Use the regular panel cookie, without creating a persistent trusted browser.
	a.forgetBrowser(w, r)
	http.SetCookie(w, &http.Cookie{Name: "litecp_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{Name: "litecp_session", Value: token, Path: "/api/", Expires: expires, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	_ = a.store.AddActivity("WHMCS administrator signed in", "Single-use API login ticket accepted", "success")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) whmcsServerInformation(w http.ResponseWriter) {
	// /proc provides real Linux values. Do not reuse demo fallback load numbers.
	var loads []float64
	if content, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(content))
		if len(fields) >= 3 {
			for _, field := range fields[:3] {
				value, parseErr := strconv.ParseFloat(field, 64)
				if parseErr != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
					loads = nil
					break
				}
				loads = append(loads, value)
			}
		}
	}
	a.store.mu.RLock()
	count := len(a.store.cfg.Sites)
	a.store.mu.RUnlock()
	writeJSON(w, 200, map[string]any{"version": version, "websiteCount": count, "loadAverages": loads})
}
