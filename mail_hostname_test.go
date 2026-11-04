package main

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestMailDNSRejectsSingleLabelHost(t *testing.T) {
	hostname, _ := os.Hostname()
	if domainPattern.MatchString(hostname) {
		t.Skip("requires single-label workstation hostname")
	}
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.cfg.MailDomains = []MailDomain{{ID: "qa", Domain: "qa.example.test"}}
	app := &App{store: store, system: &System{demo: true}}
	result := httptest.NewRecorder()
	app.mailDNS(result, httptest.NewRequest("GET", "/api/mail/dns?domain=qa.example.test", nil))
	if result.Code != 409 || !strings.Contains(result.Body.String(), "public mail hostname") {
		t.Fatal(result.Code, result.Body.String())
	}
	if strings.Contains(result.Body.String(), `"records"`) {
		t.Fatal("invalid records exposed")
	}
}
