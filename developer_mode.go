package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var developerMu sync.Mutex

type trustedBrowser struct {
	Expires    time.Time `json:"expires"`
	Credential string    `json:"credential"`
}
type developerSettings struct {
	Enabled         bool                      `json:"enabled"`
	RequirePassword bool                      `json:"requirePassword"`
	Browsers        map[string]trustedBrowser `json:"browsers"`
}

func trustHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func (a *App) developerRead() developerSettings {
	state := developerSettings{RequirePassword: true, Browsers: map[string]trustedBrowser{}}
	data, err := os.ReadFile(filepath.Join(a.system.dataDir, "developer-mode.json"))
	if err == nil {
		if json.Unmarshal(data, &state) != nil {
			return developerSettings{RequirePassword: true, Browsers: map[string]trustedBrowser{}}
		}
	}
	if state.Browsers == nil {
		state.Browsers = map[string]trustedBrowser{}
	}
	return state
}
func (a *App) developerWrite(state developerSettings) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	path := filepath.Join(a.system.dataDir, "developer-mode.json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}
func (a *App) credentialStamp() string {
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	if a.store.cfg.Admin.TwoFactor.Revision == "" {
		return trustHash(a.store.cfg.Admin.PasswordHash)
	}
	return trustHash(a.store.cfg.Admin.PasswordHash + ":" + a.store.cfg.Admin.TwoFactor.Revision)
}
func (a *App) trustedRequest(r *http.Request) bool {
	cookie, err := r.Cookie("litecp_trusted")
	if err != nil || cookie.Value == "" {
		return false
	}
	if a.store.twoFactor().Enabled {
		return false
	}
	developerMu.Lock()
	defer developerMu.Unlock()
	state := a.developerRead()
	browser, ok := state.Browsers[trustHash(cookie.Value)]
	return state.Enabled && !state.RequirePassword && ok && time.Now().Before(browser.Expires) && browser.Credential == a.credentialStamp()
}
func (a *App) forgetBrowser(w http.ResponseWriter, r *http.Request) {
	developerMu.Lock()
	defer developerMu.Unlock()
	state := a.developerRead()
	if cookie, err := r.Cookie("litecp_trusted"); err == nil {
		delete(state.Browsers, trustHash(cookie.Value))
		_ = a.developerWrite(state)
	}
	http.SetCookie(w, &http.Cookie{Name: "litecp_trusted", Path: "/api/", MaxAge: -1, HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteStrictMode})
}
func (a *App) developerMode(w http.ResponseWriter, r *http.Request) {
	developerMu.Lock()
	defer developerMu.Unlock()
	state := a.developerRead()
	if r.Method == http.MethodPut {
		var input struct {
			Enabled         bool `json:"enabled"`
			RequirePassword bool `json:"requirePassword"`
			Password        string `json:"password"`
		}
		if decodeJSON(r, &input) != nil {
			writeError(w, 400, "Invalid developer settings")
			return
		}
		var token string
		if input.Enabled && !input.RequirePassword {
			if a.store.twoFactor().Enabled {
				writeError(w, 400, "Trusted browser bypass is unavailable while two-factor authentication is enabled.")
				return
			}
			if !a.loginMu.TryLock() {
				writeError(w, 429, "Another authentication request is being checked.")
				return
			}
			defer a.loginMu.Unlock()
			if !a.reserveLoginAttempt(clientIP(r)) {
				writeError(w, 429, "Too many authentication attempts. Try again later.")
				return
			}
			if !a.store.CheckPassword(a.store.Username(), input.Password) {
				a.authenticationEvent("failed", r)
				writeError(w, 401, "Authentication failed.")
				return
			}
			a.clearLoginFailures(clientIP(r))
			if !requestIsHTTPS(r) {
				writeError(w, 400, "Trusted browser mode requires HTTPS")
				return
			}
			var err error
			token, err = randomToken(32)
			if err != nil {
				writeError(w, 500, "Could not trust this browser")
				return
			}
		}
		state.Enabled = input.Enabled
		state.RequirePassword = input.RequirePassword
		state.Browsers = map[string]trustedBrowser{}
		expires := time.Now().Add(30 * 24 * time.Hour)
		if token != "" {
			state.Browsers[trustHash(token)] = trustedBrowser{expires, a.credentialStamp()}
		}
		if a.developerWrite(state) != nil {
			writeError(w, 500, "Could not save developer settings")
			return
		}
		cookie := &http.Cookie{Name: "litecp_trusted", Value: token, Path: "/api/", HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteStrictMode}
		if token != "" {
			cookie.Expires = expires
			cookie.MaxAge = 30 * 24 * 60 * 60
		} else {
			cookie.MaxAge = -1
		}
		http.SetCookie(w, cookie)
		a.store.AddActivity("Developer mode updated", "Trusted-browser login settings changed", "info")
	}
	writeJSON(w, 200, map[string]bool{"enabled": state.Enabled, "requirePassword": state.RequirePassword})
}
