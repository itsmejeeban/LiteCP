package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHostnameTLSProtocolUpgrade(t *testing.T) {
	for _, fixture := range []struct {
		protocol string
		commands []string
		replies  []string
	}{
		{"ftp", []string{"AUTH TLS"}, []string{"234 Ready"}},
		{"smtp", []string{"EHLO localhost", "STARTTLS"}, []string{"250-localhost\r\n250 STARTTLS", "220 Ready"}},
	} {
		t.Run(fixture.protocol, func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			_ = client.SetDeadline(time.Now().Add(2 * time.Second))
			_ = server.SetDeadline(time.Now().Add(2 * time.Second))
			result := make(chan error, 1)
			go func() {
				defer server.Close()
				reader := bufio.NewReader(server)
				_, _ = fmt.Fprint(server, "220-test\r\n220 Ready\r\n")
				for index, command := range fixture.commands {
					line, err := reader.ReadString('\n')
					if err != nil || strings.TrimSpace(line) != command {
						result <- fmt.Errorf("expected %q, got %q: %v", command, line, err)
						return
					}
					_, _ = fmt.Fprint(server, fixture.replies[index]+"\r\n")
				}
				result <- nil
			}()
			if err := startHostnameTLS(client, fixture.protocol); err != nil {
				t.Fatal(err)
			}
			if err := <-result; err != nil {
				t.Fatal(err)
			}
		})
	}
}

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
