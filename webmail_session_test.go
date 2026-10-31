package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebmailSessionRestoresTrustedBrowser(t *testing.T) {
	a := &App{system: &System{dataDir: t.TempDir()}, store: &Store{}}
	state := developerSettings{Enabled: true}
	state.Browsers = map[string]trustedBrowser{trustHash("trusted"): {Expires: time.Now().Add(time.Hour), Credential: a.credentialStamp()}}
	if err := a.developerWrite(state); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "https://panel/api/mail/webmail-login", nil)
	r.AddCookie(&http.Cookie{Name: "litecp_session", Value: "stale"})
	r.AddCookie(&http.Cookie{Name: "litecp_trusted", Value: "trusted"})
	w := httptest.NewRecorder()
	token, err := a.webmailSession(w, r)
	if err != nil || token == "stale" || !a.validSession(token) {
		t.Fatal("trusted browser did not receive a live session")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Path != "/api/" || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal("invalid session cookie")
	}
	a.store.cfg.Admin.TwoFactor.Enabled = true
	if _, err := a.webmailSession(httptest.NewRecorder(), r); err == nil {
		t.Fatal("trusted browser bypassed 2FA")
	}
}

func TestWebmailSessionRequiresAuthentication(t *testing.T) {
	a := &App{system: &System{dataDir: t.TempDir()}, store: &Store{}}
	r := httptest.NewRequest("POST", "https://panel/api/mail/webmail-login", nil)
	if _, err := a.webmailSession(httptest.NewRecorder(), r); err == nil {
		t.Fatal("anonymous request accepted")
	}
	a.rememberSession("active", time.Now().Add(time.Hour))
	r.AddCookie(&http.Cookie{Name: "litecp_session", Value: "active"})
	w := httptest.NewRecorder()
	if token, err := a.webmailSession(w, r); err != nil || token != "active" || len(w.Result().Cookies()) != 0 {
		t.Fatal("existing session was not reused")
	}
}
