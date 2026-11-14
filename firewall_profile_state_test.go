package main

import (
	"errors"
	"strings"
	"testing"
)

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
	err := replaceProfileRule("ufw", old, next, func(_ string, args ...string) (string, error) {
		switch args[0] {
		case "insert":
			return "Skipping adding existing rule", nil
		case "status":
			return "8444/tcp ALLOW IN Anywhere # litecp-required\n9443/tcp ALLOW IN Anywhere", nil
		case "--force":
			removed = true
		}
		return "", nil
	})
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
	err := replaceProfileRule("firewalld", old, next, run)
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
