package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPanelHostnameSSLValidation(t *testing.T) {
	for _, name := range []string{"panel.example.org", "my-panel.example.org"} {
		if !validPanelSSLHostname(name) {
			t.Errorf("rejected public hostname %q", name)
		}
	}
	for _, name := range []string{"localhost", "192.0.2.1", "::1", "*.example.org", "panel.example.org:4321", "-panel.example.org", "panel.example.org;", strings.Repeat("x", 64) + ".test"} {
		if validPanelSSLHostname(name) {
			t.Errorf("accepted invalid hostname %q", name)
		}
	}
}

func TestPanelHostnameSSLRejectsUnsignedAndStaleRequests(t *testing.T) {
	app := apiTestApp(t)
	app.system.demo = false
	for _, authenticated := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodPost, "https://panel.example/api/system/hostname/ssl", strings.NewReader(`{"hostname":"invalid host"}`))
		req.Header.Set("Origin", "https://panel.example")
		want := http.StatusUnauthorized
		if authenticated {
			req.AddCookie(&http.Cookie{Name: "litecp_session", Value: "test-session"})
			want = http.StatusBadRequest
		}
		result := httptest.NewRecorder()
		app.routes().ServeHTTP(result, req)
		if result.Code != want {
			t.Errorf("authenticated=%t returned %d, want %d: %s", authenticated, result.Code, want, result.Body)
		}
	}
}
