package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestMigrationHealthSettlesAfterTransientForbidden(t *testing.T) {
	calls := 0
	code, err := retryMigrationHealth(context.Background(), 0, func() (string, error) {
		calls++
		if calls < 3 {
			return "403", nil
		}
		return "200", nil
	})
	if err != nil || code != "200" || calls != 3 {
		t.Fatalf("code=%s calls=%d err=%v", code, calls, err)
	}
}
func TestMigrationHealthPreservesPersistentFailure(t *testing.T) {
	calls := 0
	code, err := retryMigrationHealth(context.Background(), 0, func() (string, error) { calls++; return "403", nil })
	if err != nil || code != "403" || calls != 4 {
		t.Fatalf("code=%s calls=%d err=%v", code, calls, err)
	}
}
func TestMigrationHealthHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := retryMigrationHealth(ctx, 0, func() (string, error) { t.Fatal("probe after cancellation"); return "200", nil })
	if err != context.Canceled {
		t.Fatal(err)
	}
}

func TestMigrationHealthProbeUsesPinnedVirtualHost(t *testing.T) {
	for _, test := range []struct {
		ssl      bool
		protocol string
		address  string
		url      string
	}{
		{false, "HTTP", "workspace.example.test:80:127.0.0.1", "http://workspace.example.test/"},
		{true, "HTTPS", "workspace.example.test:443:127.0.0.1", "https://workspace.example.test/"},
	} {
		protocol, args := migrationHealthCurl(Site{Domain: "workspace.example.test", SSL: test.ssl})
		want := []string{"--disable", "--silent", "--noproxy", "*", "--output", "/dev/null", "--write-out", "%{http_code}", "--max-time", "20", "--resolve", test.address, test.url}
		if protocol != test.protocol || !reflect.DeepEqual(args, want) {
			t.Fatalf("unexpected probe options for %s: %s %v", test.protocol, protocol, args)
		}
	}
}

func TestMigrationHealthDistinguishesTLSFromHTTPFailure(t *testing.T) {
	for _, test := range []struct {
		name, protocol, code, status, message string
		probeErr                              error
	}{
		{name: "HTTPS success", protocol: "HTTPS", code: "200", status: "restored", message: "Local HTTPS status: 200"},
		{name: "HTTP bad request", protocol: "HTTP", code: "400", status: "restored-review", message: "If the application requires HTTPS"},
		{name: "HTTPS bad request", protocol: "HTTPS", code: "400", status: "restored-review", message: "local HTTPS health check returned 400"},
		{name: "TLS error", protocol: "HTTPS", code: "000", status: "restored-review", message: "Check the destination TLS listener and certificate", probeErr: errors.New("certificate verification failed")},
		{name: "application error", protocol: "HTTP", code: "500", status: "restored-review", message: "local HTTP health check returned 500"},
	} {
		t.Run(test.name, func(t *testing.T) {
			job := LiveMigrationJob{Source: LiveSource{Platform: "cpanel"}}
			recordMigrationHealth(&job, test.protocol, test.code, test.probeErr)
			if job.Stage != "result" || job.Status != test.status || !strings.Contains(job.Message, test.message) {
				t.Fatalf("unexpected migration result: %s %s %s", job.Stage, job.Status, job.Message)
			}
		})
	}
}
