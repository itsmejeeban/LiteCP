package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFirewallDirectCommandBoundary(t *testing.T) {
	cases := []struct {
		command string
		args    []string
		direct  bool
	}{
		{"firewall-cmd", []string{"--get-zones"}, true},
		{"firewall-cmd", []string{"--reload"}, true},
		{"/usr/sbin/sshd", []string{"-T"}, true},
		{"/usr/sbin/sshd", []string{"-D"}, false},
		{"/usr/sbin/sshd", []string{"-T", "-f", "/tmp/config"}, false},
		{"ufw", []string{"allow", "80"}, false},
		{"/tmp/firewall-cmd", []string{"--get-zones"}, false},
	}
	for _, c := range cases {
		if firewallDirectCommand(c.command, c.args) != c.direct {
			t.Fatalf("unexpected execution boundary: %s %v", c.command, c.args)
		}
	}
}
func TestConfiguredSSHPortProtectedFromCustomRules(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "ssh-port"), []byte("369\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ports := savedFirewallSSHPorts(dataDir)
	if len(ports) != 1 || ports[0] != "369/tcp" {
		t.Fatal(ports)
	}
	system := &System{dataDir: dataDir, demo: true}
	for _, source := range []string{"any", "cloudflare"} {
		rule := FirewallRule{Direction: "inbound", Action: "allow", Protocol: "tcp", Port: 369, Source: source}
		if err := system.ApplyFirewallRule(context.Background(), rule, false); err == nil {
			t.Fatalf("SSH protection bypass for %s", source)
		}
	}
	if err := system.ApplyFirewallRule(context.Background(), FirewallRule{Direction: "inbound", Action: "deny", Protocol: "tcp", Port: 65001, Source: "203.0.113.254"}, false); err != nil {
		t.Fatal(err)
	}
}
