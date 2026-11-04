package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnknownAdministratorStillVerifiesPassword(t *testing.T) {
	for _, name := range []string{"admin", "unknown", ""} {
		calls := 0
		accepted := checkAdminCredentials(Admin{Username: "admin", PasswordHash: "stored"}, name, "candidate", func(hash, password string) bool {
			calls++
			if hash != "stored" || password != "candidate" {
				t.Fatal("credentials changed")
			}
			return true
		})
		if calls != 1 || accepted != (name == "admin") {
			t.Fatalf("name %q calls=%d accepted=%v", name, calls, accepted)
		}
	}
}

func TestAuthenticationEventsOmitSecretsAndRejectLogInjection(t *testing.T) {
	var output bytes.Buffer
	old := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(old)
	request := httptest.NewRequest(http.MethodPost, "https://panel.test/api/login?password=query-secret", strings.NewReader("body-secret"))
	request.RemoteAddr = "192.0.2.7:9999"
	request.Header.Set("Cookie", "session=cookie-secret")
	(&App{}).authenticationEvent("failed", request)
	if !strings.Contains(output.String(), "event=failed remote=192.0.2.7") {
		t.Fatal("missing canonical event")
	}
	if strings.Contains(output.String(), "secret") {
		t.Fatal("request secrets leaked")
	}
	before := output.Len()
	request.RemoteAddr = "bad\nforged"
	(&App{}).authenticationEvent("failed", request)
	(&App{}).authenticationEvent("bad\nforged", request)
	if before != output.Len() {
		t.Fatal("invalid log fields accepted")
	}
}

func TestSecurityPostureDoesNotInventHealthyStates(t *testing.T) {
	status := collectSecurityPosture(func(string, ...string) (string, error) { return "", errors.New("not available") }, 0)
	for _, check := range status.Checks {
		if check.Status == "good" {
			t.Fatalf("unavailable probe claimed healthy: %s", check.ID)
		}
	}
	if status.Checks[0].Status != "critical" {
		t.Fatal("root privilege not flagged")
	}
	status = collectSecurityPosture(func(cmd string, args ...string) (string, error) {
		switch cmd {
		case "getenforce":
			return "Permissive", nil
		case "sshd":
			return "permitrootlogin yes\npasswordauthentication yes\n", nil
		case "fail2ban-client":
			return "Jail list: not-sshd, postfix", nil
		case "systemctl":
			return "active", nil
		}
		return "", nil
	}, 1000)
	for _, check := range status.Checks {
		if check.ID == "selinux" && check.Status != "critical" {
			t.Fatal("permissive not flagged")
		}
		if check.ID == "ssh-jail" && check.Status == "good" {
			t.Fatal("partial jail-name match accepted")
		}
		if check.ID == "ssh-auth" && check.Status != "warning" {
			t.Fatal("root password login not flagged")
		}
	}
}

func TestSecurityDashboardRequiresAuthenticationAndGET(t *testing.T) {
	app := &App{system: &System{demo: true}, sessions: map[string]session{}}
	request := httptest.NewRequest(http.MethodGet, "https://panel.test/api/security/posture", nil)
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", response.Code)
	}
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "https://panel.test/api/security/posture", nil)
	app.routes().ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("mutating method status=%d", response.Code)
	}
	response = httptest.NewRecorder()
	app.securityPosture(response, httptest.NewRequest(http.MethodGet, "https://panel.test/api/security/posture", nil).WithContext(context.Background()))
	if strings.Contains(response.Body.String(), `"status":"good"`) {
		t.Fatal("demo reported real protection")
	}
}

func TestSecurityPostureRecognizesUFW(t *testing.T) {
	for _, scenario := range []struct{ output, status string }{
		{"Status: active\n", "good"},
		{"Status: inactive\n", "warning"},
		{"unrecognized output", "unknown"},
	} {
		t.Run(scenario.status, func(t *testing.T) {
			result := collectSecurityPosture(func(command string, args ...string) (string, error) {
				if command == "ufw" {
					return scenario.output, nil
				}
				return "", errors.New("unavailable")
			}, 1000)
			for _, check := range result.Checks {
				if check.ID == "firewall" && check.Status != scenario.status {
					t.Fatalf("firewall status=%s, want %s", check.Status, scenario.status)
				}
			}
		})
	}
}
