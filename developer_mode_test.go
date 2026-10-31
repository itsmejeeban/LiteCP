package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTrustedBrowserPersistsAndRevokes(t *testing.T) {
	dir := t.TempDir()
	a := &App{system: &System{dataDir: dir}, store: &Store{}}
	a.store.cfg.Admin.PasswordHash = "first"
	state := developerSettings{Enabled: true, Browsers: map[string]trustedBrowser{trustHash("secret"): {Expires: time.Now().Add(time.Hour), Credential: a.credentialStamp()}}}
	if err := a.developerWrite(state); err != nil {
		t.Fatal(err)
	}
	b := &App{system: a.system, store: a.store}
	r := httptest.NewRequest("GET", "https://panel/api/state", nil)
	r.AddCookie(&http.Cookie{Name: "litecp_trusted", Value: "secret"})
	if !b.trustedRequest(r) {
		t.Fatal("trust did not survive restart")
	}
	a.store.cfg.Admin.PasswordHash = "changed"
	if b.trustedRequest(r) {
		t.Fatal("password change did not revoke trust")
	}
	a.store.cfg.Admin.PasswordHash = "first"
	state.RequirePassword = true
	a.developerWrite(state)
	if b.trustedRequest(r) {
		t.Fatal("password requirement did not revoke trust")
	}
	state.RequirePassword = false
	state.Browsers[trustHash("secret")] = trustedBrowser{Expires: time.Now().Add(-time.Hour), Credential: a.credentialStamp()}
	a.developerWrite(state)
	if b.trustedRequest(r) {
		t.Fatal("expired token accepted")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "developer-mode.json"))
	if string(data) == "secret" {
		t.Fatal("raw token stored")
	}
}

func TestAuthenticatedLoginEnrollsTrustedBrowser(t *testing.T) {
	dir := t.TempDir()
	a := &App{system: &System{dataDir: dir}, store: &Store{}}
	a.store.cfg.Admin.PasswordHash = "password-hash"
	if err := a.developerWrite(developerSettings{Enabled: true, RequirePassword: false, Browsers: map[string]trustedBrowser{}}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "https://panel/api/login", nil)
	w := httptest.NewRecorder()
	trusted, err := a.trustAuthenticatedBrowser(w, r)
	if err != nil || !trusted {
		t.Fatalf("enrolment: %v", err)
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "litecp_trusted" {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || !cookie.Secure || cookie.MaxAge != 30*24*60*60 {
		t.Fatal("invalid trust cookie")
	}
	restarted := &App{system: a.system, store: a.store}
	request := httptest.NewRequest("GET", "https://panel/api/state", nil)
	request.AddCookie(cookie)
	if !restarted.trustedRequest(request) {
		t.Fatal("trust did not survive panel restart")
	}
	restarted.forgetBrowser(httptest.NewRecorder(), request)
	if restarted.trustedRequest(request) {
		t.Fatal("sign-out did not revoke browser")
	}
}
func TestAuthenticatedLoginDoesNotBypassPolicy(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		a := &App{system: &System{dataDir: t.TempDir()}, store: &Store{}}
		a.developerWrite(developerSettings{Enabled: enabled, RequirePassword: true, Browsers: map[string]trustedBrowser{}})
		ok, err := a.trustAuthenticatedBrowser(httptest.NewRecorder(), httptest.NewRequest("POST", "https://panel/api/login", nil))
		if err != nil || ok {
			t.Fatal("password requirement bypassed")
		}
	}
}
