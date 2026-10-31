package main

import (
	"strings"
	"testing"
)

func TestParseCloudflareNetworks(t *testing.T) {
	for _, test := range []struct {
		name, text string
		v6, valid  bool
	}{
		{"v4", "173.245.48.0/20\n103.21.244.0/22", false, true},
		{"v6", "2400:cb00::/32\n2606:4700::/32", true, true},
		{"empty", "", false, false}, {"html", "<html>error</html>", false, false},
		{"all internet", "0.0.0.0/0\n103.21.244.0/22", false, false},
		{"wrong family", "2400:cb00::/32\n2606:4700::/32", false, false},
		{"host bits", "173.245.48.1/20\n103.21.244.0/22", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseCloudflareNetworks(test.text, test.v6)
			if (err == nil) != test.valid {
				t.Fatalf("unexpected validation: %v", err)
			}
		})
	}
}
func TestCloudflareGuardRestrictsBothFamiliesBeforeHostAllows(t *testing.T) {
	rule := FirewallRule{Port: 443, Protocol: "tcp"}
	script := cloudflareGuard(rule, cloudflareNetworks{IPv4: []string{"173.245.48.0/20"}, IPv6: []string{"2400:cb00::/32"}})
	for _, want := range []string{"priority -310", "type ipv4_addr", "type ipv6_addr", "tcp dport 443 ip saddr @cf4 accept", "tcp dport 443 ip6 saddr @cf6 accept", "tcp dport 443 drop"} {
		if !strings.Contains(script, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Index(script, "@cf6 accept") > strings.Index(script, "tcp dport 443 drop") {
		t.Fatal("deny precedes Cloudflare allow")
	}
	if strings.Contains(script, "flush ruleset") {
		t.Fatal("must not flush unrelated firewall rules")
	}
}
