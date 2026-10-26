package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func resolvedCommandFixture(t *testing.T, dns, domains, defaults string) string {
	t.Helper()
	if runtime.GOOS == "windows" { t.Skip("POSIX resolvectl command fixture") }
	root := t.TempDir()
	script := "#!/bin/sh\n[ \"$LC_ALL\" = C ] || exit 10\n[ \"$SYSTEMD_COLORS\" = 0 ] || exit 11\ncase \"$2\" in\ndns) printf '%s\\n' \"$LITECP_FIXTURE_DNS\";;\ndomain) printf '%s\\n' \"$LITECP_FIXTURE_DOMAINS\";;\ndefault-route) [ -n \"$LITECP_FIXTURE_DEFAULTS\" ] || exit 12; printf '%s\\n' \"$LITECP_FIXTURE_DEFAULTS\";;\n*) exit 13;;\nesac\n"
	if err := os.WriteFile(filepath.Join(root, "resolvectl"), []byte(script), 0700); err != nil { t.Fatal(err) }
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LITECP_FIXTURE_DNS", dns)
	t.Setenv("LITECP_FIXTURE_DOMAINS", domains)
	t.Setenv("LITECP_FIXTURE_DEFAULTS", defaults)
	path := filepath.Join(root, "resolv.conf")
	if err := os.WriteFile(path, []byte("nameserver 127.0.0.53\n"), 0600); err != nil { t.Fatal(err) }
	return path
}

func TestResolvedGlobalRootExcludesDHCPFromEffectiveDefaults(t *testing.T) {
	path := resolvedCommandFixture(t, "Global: 8.8.8.8 8.8.4.4\nLink 2 (eth0): 192.0.2.53", "Global: ~.\nLink 2 (eth0): provider.example", "")
	got, err := systemResolversFromFile(context.Background(), path)
	if err != nil || !reflect.DeepEqual(got, []string{"8.8.8.8", "8.8.4.4"}) { t.Fatalf("DHCP incorrectly changed verified defaults: %v (%v)", got, err) }
	if err := checkResolvedRootConflicts(context.Background(), got); err != nil { t.Fatal(err) }
}

func TestResolvedConflictingRootLinkFailsPreflight(t *testing.T) {
	path := resolvedCommandFixture(t, "Global: 8.8.8.8 8.8.4.4\nLink 5 (vpn0): 1.1.1.1", "Global: ~.\nLink 5 (vpn0): ~.", "")
	got, err := systemResolversFromFile(context.Background(), path)
	if err != nil || defaultResolversMatch(got) { t.Fatal("conflicting root link incorrectly verified", got, err) }
	err = checkResolvedRootConflicts(context.Background(), []string{"8.8.8.8", "8.8.4.4"})
	if err == nil || !strings.Contains(err.Error(), "conflicting default DNS route") { t.Fatal("link root conflict accepted", err) }
}

func TestResolvedMatchingRootAndTLSNames(t *testing.T) {
	path := resolvedCommandFixture(t, "Global: 8.8.8.8#dns.google\n        8.8.4.4%eth0#dns.google\nLink 2 (eth0): 8.8.8.8", "Global: ~.\nLink 2 (eth0): ~.", "")
	got, err := systemResolversFromFile(context.Background(), path)
	if err != nil || !defaultResolversMatch(got) { t.Fatal("matching root/TLS metadata rejected", got, err) }
	if err := checkResolvedRootConflicts(context.Background(), got); err != nil { t.Fatal(err) }
	for value, want := range map[string]string{"[fe80::53%eth0]:53#dns.example": "fe80::53%eth0", "[2001:4860:4860::8888]:9953#dns.google": "[2001:4860:4860::8888]:9953", "8.8.8.8:53%2#dns.google": "8.8.8.8"} {
		got, err := resolvedServerAddress(value); if err != nil || got != want { t.Fatalf("%s: got %s, want %s (%v)", value, got, want, err) }
	}
}

func TestResolvedDefaultRouteLinksWithoutRootRoute(t *testing.T) {
	path := resolvedCommandFixture(t, "Global: 8.8.8.8\nLink 2 (eth0): 192.0.2.53\nLink 5 (vpn0): 10.0.0.53", "Global:\nLink 2 (eth0):\nLink 5 (vpn0): ~corp.example", "Link 2 (eth0): yes\nLink 5 (vpn0): no")
	got, err := systemResolversFromFile(context.Background(), path)
	if err != nil || !reflect.DeepEqual(got, []string{"8.8.8.8", "192.0.2.53"}) { t.Fatal("unmatched DNS default-route selection failed", got, err) }
}

func TestResolvedUnverifiedStateNeverUsesDesiredConfig(t *testing.T) {
	path := resolvedCommandFixture(t, "Global: bad-address\nLink 2 (eth0): 192.0.2.53", "Global: ~.\nLink 2 (eth0):", "")
	if _, err := systemResolversFromFile(context.Background(), path); err == nil { t.Fatal("invalid live DNS verified") }
	t.Setenv("LITECP_FIXTURE_DNS", "Global: 8.8.8.8 8.8.4.4\nLink 2 (eth0): 192.0.2.53")
	t.Setenv("LITECP_FIXTURE_DOMAINS", "Global: ~.")
	if _, err := systemResolversFromFile(context.Background(), path); err == nil { t.Fatal("missing link routing state verified") }
	if err := os.WriteFile(path, []byte("nameserver 192.0.2.53\n"), 0600); err != nil { t.Fatal(err) }
	got, err := systemResolversFromFile(context.Background(), path)
	if err != nil || !reflect.DeepEqual(got, []string{"192.0.2.53"}) { t.Fatal("direct non-resolved manager behavior changed", got, err) }
}
