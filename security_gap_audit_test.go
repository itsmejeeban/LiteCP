package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuditJSONBodyLimitIncludesTrailingWhitespace(t *testing.T) {
	for _, body := range []string{`{"value":"ok"}` + strings.Repeat(" ", 128), `{"value":"` + strings.Repeat("a", 128) + `"}`} {
		req := httptest.NewRequest("POST", "http://panel.test/api/example", strings.NewReader(body))
		var input struct {
			Value string `json:"value"`
		}
		if err := decodeJSONWithLimit(req, &input, 32); err == nil {
			t.Fatal("oversized JSON body accepted")
		}
	}
}

func TestAuditManagementRouteAuthenticationMatrix(t *testing.T) {
	a := &App{sessions: map[string]session{"audit-session": {Expires: time.Now().Add(time.Hour)}}}
	mux := a.routes()
	tested := map[string]bool{}
	for _, route := range apiRouteActions {
		if route.Path == "/api/version" {
			continue
		} // Deliberately public build information.
		path := route.Path
		if route.ObjectID {
			path += "audit-object"
		}
		key := route.Method + " " + path
		if tested[key] {
			continue
		}
		tested[key] = true
		t.Run(key, func(t *testing.T) {
			req := httptest.NewRequest(route.Method, "https://panel.test"+path, strings.NewReader(`{}`))
			req.Header.Set("X-LiteCP-Identifier", "forged")
			req.Header.Set("X-LiteCP-Secret", "forged")
			req.Header.Set("X-External-API-Grant", "forged")
			res := httptest.NewRecorder()
			mux.ServeHTTP(res, req)
			if res.Code != http.StatusUnauthorized {
				t.Fatalf("anonymous route returned %d", res.Code)
			}
			if route.Method != "GET" && route.Method != "HEAD" {
				req = httptest.NewRequest(route.Method, "https://panel.test"+path, strings.NewReader(`{}`))
				req.AddCookie(&http.Cookie{Name: "litecp_session", Value: "audit-session"})
				req.Header.Set("Origin", "https://attacker.test")
				res = httptest.NewRecorder()
				mux.ServeHTTP(res, req)
				if res.Code != http.StatusForbidden {
					t.Fatalf("cross-origin mutation returned %d", res.Code)
				}
			}
		})
	}
	t.Logf("Verified %d distinct method/path boundaries", len(tested))
}

type auditUnreadBody struct{ reads int }

func (b *auditUnreadBody) Read(p []byte) (int, error) {
	b.reads++
	return 0, fmt.Errorf("unauthenticated body was read")
}
func (*auditUnreadBody) Close() error { return nil }

func TestAuditInvalidAPICredentialsRejectBeforeReadingBody(t *testing.T) {
	a := apiTestApp(t)
	body := &auditUnreadBody{}
	req := httptest.NewRequest("POST", "https://panel.test/api/v1", nil)
	req.Body = body
	req.ContentLength = 65536
	req.Header.Set("X-LiteCP-Identifier", "unknown-credential")
	req.Header.Set("X-LiteCP-Secret", "invalid-secret")
	res := httptest.NewRecorder()
	a.routes().ServeHTTP(res, req)
	if body.reads != 0 || res.Code != http.StatusUnauthorized {
		t.Fatalf("invalid credentials read body %d times; status %d", body.reads, res.Code)
	}
}

func TestAuditJSONExactBoundaryAndMultipleObjects(t *testing.T) {
	for _, tc := range []struct {
		body  string
		limit int64
		want  bool
	}{
		{`{"value":"ok"}`, 14, true},
		{`{"value":"ok"} `, 15, true},
		{`{"value":"ok"} `, 14, false},
		{`{"value":"ok"} {}`, 64, false},
		{`{"value":"ok","unexpected":true}`, 64, false},
	} {
		var input struct {
			Value string `json:"value"`
		}
		req := httptest.NewRequest("POST", "https://panel.test/api/example", strings.NewReader(tc.body))
		if got := decodeJSONWithLimit(req, &input, tc.limit) == nil; got != tc.want {
			t.Errorf("body %q limit %d: accepted %v", tc.body, tc.limit, got)
		}
	}
}

func TestAuditAPIEarlyAuthenticationMatrix(t *testing.T) {
	for _, path := range []string{"/api/v1", "/api/v1/request"} {
		for _, mode := range []string{"wrong-secret", "disabled", "expired", "revoked", "wrong-ip"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				a := apiTestApp(t)
				past := time.Now().Add(-time.Minute)
				c := APICredential{Identifier: "audit-key", SecretHash: apiSecretHash("test-secret"), Enabled: true, RateLimit: 60}
				secret := "test-secret"
				switch mode {
				case "wrong-secret":
					secret = "incorrect"
				case "disabled":
					c.Enabled = false
				case "expired":
					c.ExpiresAt = &past
				case "revoked":
					c.RevokedAt = &past
				case "wrong-ip":
					c.AllowedIPs = []string{"198.51.100.0/24"}
				}
				a.store.cfg.APICredentials = []APICredential{c}
				body := &auditUnreadBody{}
				req := httptest.NewRequest("POST", "https://panel.test"+path, nil)
				req.Body = body
				req.ContentLength = -1
				req.RemoteAddr = "203.0.113.10:1234"
				req.Header.Set("X-LiteCP-Identifier", c.Identifier)
				req.Header.Set("X-LiteCP-Secret", secret)
				res := httptest.NewRecorder()
				a.routes().ServeHTTP(res, req)
				if body.reads != 0 || res.Code != 401 {
					t.Fatalf("read %d body chunks; status %d", body.reads, res.Code)
				}
			})
		}
	}
}
