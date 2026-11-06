package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Exercise auth_request's restricted status handling through a separate loopback
// Nginx instance. No installed service, website, certificate or policy is changed.
func TestCaptchaNginxGatewayFlow(t *testing.T) {
	binary, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("Nginx unavailable")
	}
	g := captchaFixture()
	policy := captchaTestPolicy()
	policy.RequestsPerSecond = 100
	record := g.runtime.Sites["test"]
	record.Policy = policy
	g.runtime.Sites["test"] = record
	gateway := httptest.NewServer(g)
	defer gateway.Close()
	content := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "audit website reached") }))
	defer content.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	root := t.TempDir()
	site := Site{ID: "test", Domain: record.Domain, Captcha: &policy}
	config := injectCaptchaConfig("server {\nlisten "+address+";\nserver_name "+record.Domain+";\nlimit_req_dry_run on;\nlocation / { proxy_pass "+content.URL+"; proxy_intercept_errors on; "+resourceProxyErrors+" }\n location @resource_unavailable { return 503; }\n}\n", site)
	config = strings.ReplaceAll(config, "http://"+captchaAddress, gateway.URL)
	// Simulate HTTPS termination; actual certificate behavior is tested separately.
	config = strings.ReplaceAll(config, "X-LiteCP-Scheme $scheme", "X-LiteCP-Scheme https")
	config = fmt.Sprintf("pid %q;\nerror_log %q;\nevents {}\nhttp { access_log off;\n%s%s}\n", filepath.ToSlash(filepath.Join(root, "nginx.pid")), filepath.ToSlash(filepath.Join(root, "nginx.log")), captchaZones([]Site{site}), config)
	path := filepath.Join(root, "nginx.conf")
	if err = os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, binary, "-p", root+string(os.PathSeparator), "-c", path, "-g", "daemon off; master_process off;")
	if err = command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = command.Wait() })
	client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(method, path, body string, cookie *http.Cookie) (int, string, []*http.Cookie) {
		t.Helper()
		r, e := http.NewRequest(method, "http://"+address+path, strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		r.Host = record.Domain
		r.Header.Set("User-Agent", "CAPTCHA Nginx audit")
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		response, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		data, e := io.ReadAll(response.Body)
		if e != nil {
			t.Fatal(e)
		}
		return response.StatusCode, string(data), response.Cookies()
	}
	ready := false
	for attempt := 0; attempt < 40; attempt++ {
		connection, e := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if e == nil {
			connection.Close()
			ready = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !ready {
		logs, _ := os.ReadFile(filepath.Join(root, "nginx.log"))
		t.Fatalf("isolated Nginx did not start: %s", logs)
	}
	status, body, _ := request("GET", "/?audit=1", "", nil)
	if status != 403 || !strings.Contains(body, "Verify to continue") {
		t.Fatalf("visitor challenge failed: %d %s", status, body)
	}
	match := regexp.MustCompile(`name="id" value="([^"]+)"`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatal("challenge identifier missing")
	}
	status, _, _ = request("GET", captchaPrefix+"image?id="+url.QueryEscape(match[1]), "", nil)
	if status != 200 {
		t.Fatal("image did not pass through public verification route", status)
	}
	answer := captchaDigits(g.runtime.Key, match[1])
	status, _, cookies := request("POST", captchaPrefix+"verify", url.Values{"id": {match[1]}, "answer": {answer}}.Encode(), nil)
	if status != 303 || len(cookies) != 1 {
		t.Fatal("verified visitor was not cleared", status)
	}
	status, body, _ = request("GET", "/?audit=1", "", cookies[0])
	if status != 200 || body != "audit website reached" {
		t.Fatal("cleared visitor did not reach website", status, body)
	}
	g.mu.Lock()
	record.Policy.RequestsPerSecond = 1
	g.runtime.Sites["test"] = record
	now := g.now().Add(2 * time.Second)
	g.now = func() time.Time { return now }
	g.loaded = now
	g.mu.Unlock()
	status, _, _ = request("GET", "/", "", cookies[0])
	if status != 200 {
		t.Fatal("first rate-limited request failed", status)
	}
	status, body, _ = request("GET", "/", "", cookies[0])
	if status != 429 || !strings.Contains(body, "request limit reached") {
		t.Fatal("auth_request did not propagate visitor rate denial as 429", status, body)
	}
}
