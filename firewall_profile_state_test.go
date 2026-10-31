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
