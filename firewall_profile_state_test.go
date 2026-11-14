package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestFirewallProfileScopedRulesRoundTrip(t *testing.T) {
	tests := []struct {
		rule profileRule
		ufw  string
		args []string
	}{
		{profileRule{Direction: "inbound", Port: "25/tcp", Address: "192.0.2.7"},
			"25/tcp  ALLOW IN  192.0.2.7 # litecp-required",
			[]string{"allow", "in", "from", "192.0.2.7", "to", "any", "port", "25", "proto", "tcp", "comment", "litecp-required"}},
		{profileRule{Direction: "outbound", Port: "53/udp", Address: "2001:db8::/64"},
			"2001:db8::/64 53/udp  ALLOW OUT  Anywhere (v6) # litecp-required",
			[]string{"allow", "out", "to", "2001:db8::/64", "port", "53", "proto", "udp", "comment", "litecp-required"}},
	}
	for _, tc := range tests {
		if got, ok := parseProfileUFWRule(tc.ufw); !ok || got != tc.rule {
			t.Fatal(got, ok)
		}
		generated := newProfileRule(tc.rule, tc.rule.Port, tc.rule.Address)
		if got, ok := parseProfileRichRule(generated.Raw, tc.rule.Direction, ""); !ok || got != generated {
			t.Fatal(got, ok)
		}
		if got := profileUFWArgs(tc.rule); !reflect.DeepEqual(got, tc.args) {
			t.Fatal(got)
		}
		if tc.rule.Direction == "outbound" && profileFirewalldArgs(generated, "add")[1] != "--add-rich-rule="+generated.Raw {
			t.Fatal("a scoped destination must stay scoped")
		}
	}
}

func TestFirewallProfileSecondAddressFailureKeepsOriginal(t *testing.T) {
	old := newProfileRule(profileRule{Direction: "inbound", Zone: "public"}, "25/tcp", "")
	first := newProfileRule(old, old.Port, "192.0.2.7")
	second := newProfileRule(old, old.Port, "2001:db8::7")
	persistent := map[string]bool{old.Raw: true}
	runtime := map[string]bool{old.Raw: true}
	run := func(_ string, args ...string) (string, error) {
		rules := runtime
		if args[0] == "--permanent" {
			rules = persistent
		}
		verb, raw, _ := strings.Cut(args[len(args)-1], "=")
		switch verb {
		case "--add-rich-rule":
			if raw == second.Raw {
				return "", errors.New("address failed")
			}
			rules[raw] = true
		case "--remove-rich-rule":
			delete(rules, raw)
		case "--list-rich-rules":
			var output []string
			for rule := range rules {
				output = append(output, rule)
			}
			return strings.Join(output, "\n"), nil
		}
		return "", nil
	}
	rollback := func(added []profileRule, restoreOld bool) error {
		return restoreProfileRulesWithProbe("firewalld", old, added, restoreOld, run)
	}
	if err := replaceProfileRules("firewalld", old, []profileRule{first, second}, true, run, rollback); err == nil ||
		len(persistent) != 1 || !persistent[old.Raw] || len(runtime) != 1 || !runtime[old.Raw] {
		t.Fatalf("wrong rollback: %v, saved=%v, active=%v", err, persistent, runtime)
	}
}

func TestInspectFirewallProfile(t *testing.T) {
	state, err := inspectFirewallProfile("firewalld", func(_ string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "--get-zones":
			return "public internal", nil
		case "--zone=public --list-rich-rules":
			return `rule priority="-100" port port="4321" protocol="tcp" accept` + "\n" + `rule port port="9000" protocol="tcp" accept`, nil
		case "--get-policies":
			return "litecp-required other", nil
		case "--policy=litecp-required --list-ports":
			return "53/udp 443/tcp", nil
		}
		return "", nil
	})
	if err != nil || !state.Enabled || len(state.Rules) != 3 || state.Rules[0].Port != "4321/tcp" {
		t.Fatal(state, err)
	}
	_, err = inspectFirewallProfile("firewalld", func(string, ...string) (string, error) { return "", errors.New("denied") })
	if err == nil {
		t.Fatal("status error suppressed")
	}
	state, err = inspectFirewallProfile("ufw", func(string, ...string) (string, error) {
		return "53/udp ALLOW   OUT Anywhere # litecp-required\n22/tcp ALLOW IN Anywhere", nil
	})
	if err != nil || len(state.Rules) != 1 || state.Rules[0].Direction != "outbound" {
		t.Fatal(state, err)
	}
}

func TestFirewallProfileEditKeepsOldRuleWhenReplacementIsSkipped(t *testing.T) {
	old := profileRule{Direction: "inbound", Port: "8444/tcp"}
	next := profileRule{Direction: "inbound", Port: "9443/tcp"}
	removed := false
	run := func(_ string, args ...string) (string, error) {
		switch args[0] {
		case "insert":
			return "Skipping adding existing rule", nil
		case "status":
			return "8444/tcp ALLOW IN Anywhere # litecp-required\n9443/tcp ALLOW IN Anywhere", nil
		case "--force":
			removed = true
		}
		return "", nil
	}
	rollback := func(added []profileRule, restoreOld bool) error {
		return restoreProfileRulesWithProbe("ufw", old, added, restoreOld, run)
	}
	err := replaceProfileRules("ufw", old, []profileRule{next}, true, run, rollback)
	if err == nil || removed {
		t.Fatalf("conflicting unmanaged rule must leave both rules untouched: %v, remove=%t", err, removed)
	}
}

func TestFirewallProfileEditRestoresPermanentAndRuntimeRules(t *testing.T) {
	old := profileRule{Direction: "outbound", Port: "53/udp"}
	next := profileRule{Direction: "outbound", Port: "5353/udp"}
	permanent := map[string]bool{old.Port: true}
	active := map[string]bool{old.Port: true}
	run := func(_ string, args ...string) (string, error) {
		rules := active
		if args[0] == "--permanent" {
			rules = permanent
		}
		command := args[len(args)-1]
		switch {
		case strings.HasPrefix(command, "--add-port="):
			rules[strings.TrimPrefix(command, "--add-port=")] = true
		case strings.HasPrefix(command, "--remove-port="):
			port := strings.TrimPrefix(command, "--remove-port=")
			if args[0] != "--permanent" && port == old.Port {
				return "", errors.New("runtime removal failed")
			}
			delete(rules, port)
		case command == "--list-ports":
			var ports []string
			for port := range rules {
				ports = append(ports, port)
			}
			return strings.Join(ports, " "), nil
		}
		return "", nil
	}
	rollback := func(added []profileRule, restoreOld bool) error {
		return restoreProfileRulesWithProbe("firewalld", old, added, restoreOld, run)
	}
	err := replaceProfileRules("firewalld", old, []profileRule{next}, true, run, rollback)
	if err == nil || !permanent[old.Port] || !active[old.Port] || permanent[next.Port] || active[next.Port] {
		t.Fatalf("wrong rollback result: %v, saved=%v, active=%v", err, permanent, active)
	}
}

func TestFirewallProfileRangeProtectsPanelAccess(t *testing.T) {
	if !profileRuleHasPort("4300-4400/tcp", "4321/tcp") ||
		profileRuleHasPort("4300-4400/udp", "4321/tcp") {
		t.Fatal("invalid access check for port range")
	}
}

func TestFirewallProfileDisablePreservesScopedAccessMembers(t *testing.T) {
	rule := newProfileRule(profileRule{Direction: "inbound", Zone: "public"}, "400-500/tcp", "2001:db8::/64")
	for _, backend := range []string{"firewalld", "ufw"} {
		var commands []string
		run := func(cmd string, args ...string) (string, error) {
			commands = append(commands, cmd+" "+strings.Join(args, " "))
			return "", nil
		}
		retained, err := preserveProfileAccessRule(backend, rule, []string{"80/tcp", "443/tcp", "443/tcp"}, run)
		if err != nil || retained || len(commands) != 1 || !strings.Contains(commands[0], "2001:db8::/64") || strings.Contains(commands[0], "400-500") {
			t.Fatal(backend, retained, commands, err)
		}
		if backend == "firewalld" && (!strings.Contains(commands[0], `priority="-90"`) || !strings.Contains(commands[0], `port="443"`)) {
			t.Fatal(commands)
		}
		if backend == "ufw" && !strings.Contains(commands[0], "port 443 proto tcp comment litecp-access") {
			t.Fatal(commands)
		}
	}
	exact := newProfileRule(rule, "443/tcp", rule.Address)
	retained, err := preserveProfileAccessRule("ufw", exact, []string{"443/tcp"}, func(_ string, args ...string) (string, error) {
		if args[0] != "allow" || args[len(args)-1] != "litecp-access" {
			t.Fatal("exact access rule must be adopted without deletion", args)
		}
		return "", nil
	})
	if err != nil || !retained {
		t.Fatal(retained, err)
	}
}

func TestFirewallProfileRollsBackOwnedCurrentRuleAfterInspectionTimeout(t *testing.T) {
	old := profileRule{Direction: "inbound", Port: "8444/tcp"}
	next := profileRule{Direction: "inbound", Port: "9443/tcp", Address: "192.0.2.9"}
	editCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	created, removed := false, false
	run := func(_ string, args ...string) (string, error) {
		if editCtx.Err() != nil {
			return "", editCtx.Err()
		}
		if args[0] == "insert" {
			created = true
		}
		if args[0] == "status" {
			cancel()
			return "", editCtx.Err()
		}
		return "", nil
	}
	recovery := func(_ string, args ...string) (string, error) {
		if editCtx.Err() == nil || !created {
			t.Fatal("expected recovery after edit context was canceled")
		}
		if args[0] == "--force" {
			removed = true
		}
		return "", nil
	}
	rollback := func(added []profileRule, restoreOld bool) error {
		if len(added) != 1 || added[0] != next || restoreOld {
			t.Fatal("owned current selector was not tracked", added, restoreOld)
		}
		return restoreProfileRulesWithProbe("ufw", old, added, restoreOld, recovery)
	}
	if err := replaceProfileRules("ufw", old, []profileRule{next}, true, run, rollback); err == nil || !removed {
		t.Fatal("inspection timeout left newly added access active", removed, err)
	}
}

func TestFirewallProfileExistingUFWSelectorIsNeverOwned(t *testing.T) {
	old := profileRule{Direction: "inbound", Port: "8444/tcp"}
	next := profileRule{Direction: "inbound", Port: "9443/tcp"}
	run := func(_ string, args ...string) (string, error) {
		if args[0] != "--dry-run" {
			t.Fatal("duplicate selector reached a mutation", args)
		}
		return "Skipping inserting existing rule", nil
	}
	rollback := func(added []profileRule, restoreOld bool) error {
		if len(added) != 0 || restoreOld {
			t.Fatal("an existing selector must not be deleted", added, restoreOld)
		}
		return nil
	}
	if err := replaceProfileRules("ufw", old, []profileRule{next}, true, run, rollback); err == nil {
		t.Fatal("duplicate selector was accepted")
	}
}

func TestFirewallProfileUFWRecoveryReappliesBothFamilies(t *testing.T) {
	old := profileRule{Direction: "inbound", Port: "8444/tcp"}
	reapplied := false
	run := func(_ string, args ...string) (string, error) {
		switch args[0] {
		case "allow":
			reapplied = true
		case "status":
			if !reapplied {
				t.Fatal("an existing IPv6 selector must not skip IPv4 recovery")
			}
			return "8444/tcp ALLOW IN Anywhere # litecp-required\n8444/tcp (v6) ALLOW IN Anywhere (v6) # litecp-required", nil
		default:
			t.Fatal(args)
		}
		return "", nil
	}
	if err := restoreProfileRulesWithProbe("ufw", old, nil, true, run); err != nil || !reapplied {
		t.Fatal(reapplied, err)
	}
}
