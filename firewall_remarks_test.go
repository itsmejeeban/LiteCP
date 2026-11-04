package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirewallRemarksPersistWithoutChangingHostRule(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	rule := normalizeFirewallRule(FirewallRule{ID: "notes", Port: 65001, Protocol: "tcp", Source: "203.0.113.254", Label: "QA", Remarks: "original"})
	if err = store.AddFirewallRule(rule); err != nil {
		t.Fatal(err)
	}
	// demo=false: a mistaken host-rule rewrite would execute a missing firewall
	// command on Windows or fail on an unconfigured Linux test host.
	app := &App{store: store, system: &System{dataDir: dir}}
	request := httptest.NewRequest("PUT", "/api/firewall/notes", strings.NewReader(`{"port":65001,"protocol":"tcp","source":"203.0.113.254","label":"QA","remarks":"Reason for access\n<script>text only</script>"}`))
	response := httptest.NewRecorder()
	app.firewallRuleAction(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	reopened, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	saved := reopened.FirewallRuleByID("notes")
	if saved == nil || saved.Remarks != "Reason for access\n<script>text only</script>" {
		t.Fatal(saved)
	}
	// Older clients omitting remarks must preserve the existing note.
	response = httptest.NewRecorder()
	app.firewallRuleAction(response, httptest.NewRequest("PUT", "/api/firewall/notes", strings.NewReader(`{"port":65001,"protocol":"tcp","source":"203.0.113.254","label":"Updated label"}`)))
	if response.Code != 200 || store.FirewallRuleByID("notes").Remarks != saved.Remarks {
		t.Fatal(response.Code, response.Body.String())
	}
}
func TestFirewallRemarksValidation(t *testing.T) {
	rule := normalizeFirewallRule(FirewallRule{Port: 65001, Protocol: "tcp", Source: "203.0.113.254", Remarks: "  note\nsecond line  "})
	if rule.Remarks != "note\nsecond line" {
		t.Fatal(rule.Remarks)
	}
	if err := validateFirewallRule(rule); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{strings.Repeat("x", 501), "nul\x00byte"} {
		rule.Remarks = invalid
		if validateFirewallRule(rule) == nil {
			t.Fatal("invalid remarks accepted")
		}
	}
}
func TestRequiredFirewallServiceRemarks(t *testing.T) {
	cases := []struct{ direction, port, expected string }{
		{"inbound", "369/tcp", "SSH"}, {"inbound", "9443/tcp", "administrator panel"},
		{"inbound", "443/tcp", "HTTPS"}, {"inbound", "50000:50049/tcp", "Passive FTPS"},
		{"inbound", "8444/tcp", "Webmail"}, {"inbound", "53/udp", "Authoritative DNS"},
		{"outbound", "53/udp", "DNS resolution"}, {"outbound", "123/udp", "NTP"},
		{"inbound", "65009/tcp", "review"},
	}
	for _, c := range cases {
		note := requiredFirewallRemark(profileRule{Direction: c.direction, Port: c.port}, 9443, []string{"369/tcp"}, "8444")
		if !strings.Contains(note, c.expected) {
			t.Fatal(c, note)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ssh-port"), []byte("369"), 0600); err != nil {
		t.Fatal(err)
	}
	app := &App{system: &System{dataDir: dir}}
	state := profileState{Rules: []profileRule{{Direction: "inbound", Port: "369/tcp"}, {Direction: "outbound", Port: "123/udp"}}}
	app.annotateFirewallProfile(&state)
	if !strings.Contains(state.Rules[0].Remarks, "SSH") || !strings.Contains(state.Rules[1].Remarks, "NTP") {
		t.Fatal(state)
	}
}
