package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginAdmissionIsAtomicAndBounded(t *testing.T) {
	app := &App{}
	var admitted int32
	var workers sync.WaitGroup
	for i := 0; i < 64; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if app.reserveLoginAttempt("192.0.2.10") {
				atomic.AddInt32(&admitted, 1)
			}
		}()
	}
	workers.Wait()
	if admitted != 5 {
		t.Fatalf("admitted %d requests; want exactly 5", admitted)
	}
	app.attempts["192.0.2.10"] = loginAttempt{Since: time.Now().Add(-16 * time.Minute), Count: 5}
	if !app.reserveLoginAttempt("192.0.2.10") {
		t.Fatal("expired limits must allow a new attempt")
	}
	for i := 0; len(app.attempts) < 4096; i++ {
		app.attempts[fmt.Sprintf("source-%d", i)] = loginAttempt{Since: time.Now(), Count: 1}
	}
	if app.reserveLoginAttempt("new-source") || len(app.attempts) != 4096 {
		t.Fatal("login source tracking must remain bounded")
	}
}

func TestLoginRejectsConcurrentPasswordWork(t *testing.T) {
	app := &App{}
	app.loginMu.Lock()
	defer app.loginMu.Unlock()
	req := httptest.NewRequest(http.MethodPost, "http://panel.test/api/login", strings.NewReader(`{"username":"admin","password":"irrelevant"}`))
	response := httptest.NewRecorder()
	app.login(response, req)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d; want 429 without touching the password store", response.Code)
	}
}

func TestPanelSessionStorageIsBounded(t *testing.T) {
	app := &App{sessions: map[string]session{"expired": {Expires: time.Now().Add(-time.Minute)}}}
	for i := 0; i < 256; i++ {
		app.rememberSession(fmt.Sprintf("token-%d", i), time.Now().Add(time.Hour))
	}
	if len(app.sessions) != 128 || !app.validSession("token-255") {
		t.Fatal("sessions must be bounded and the newest session must remain valid")
	}
	if _, exists := app.sessions["expired"]; exists {
		t.Fatal("expired sessions must be reclaimed")
	}
}

func TestPanelOriginAndProxyTrust(t *testing.T) {
	for _, test := range []struct {
		name, remote, proto, origin, referer, fetchSite string
		want bool
	}{
		{"trusted proxy", "127.0.0.1:1234", "https", "https://panel.test", "", "same-origin", true},
		{"insecure origin", "127.0.0.1:1234", "https", "http://panel.test", "", "same-origin", false},
		{"spoofed proxy", "192.0.2.1:1234", "https", "https://panel.test", "", "", false},
		{"cross-site without origin", "127.0.0.1:1234", "https", "", "", "cross-site", false},
		{"same-site subdomain", "127.0.0.1:1234", "https", "", "", "same-site", false},
		{"referer fallback", "127.0.0.1:1234", "https", "", "https://evil.test/page", "", false},
		{"valid referer", "127.0.0.1:1234", "https", "", "https://panel.test/settings", "", true},
		{"CLI compatibility", "127.0.0.1:1234", "", "", "", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://panel.test/api/login", nil)
			req.RemoteAddr = test.remote
			req.Header.Set("X-Forwarded-Proto", test.proto)
			req.Header.Set("Origin", test.origin)
			req.Header.Set("Referer", test.referer)
			req.Header.Set("Sec-Fetch-Site", test.fetchSite)
			if got := validOrigin(req); got != test.want {
				t.Fatalf("validOrigin = %v; want %v", got, test.want)
			}
		})
	}
}
