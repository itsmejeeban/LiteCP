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
