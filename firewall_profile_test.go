package main

import (
	"errors"
	"strings"
	"testing"
)

func TestFirewallProfileFailureIncludesCommandAndStops(t *testing.T) {
	calls := 0
	err := applyRequiredFirewall(requiredFirewallProfile([]string{"369"}, false, false, ""), "firewalld", func(cmd string, args ...string) (string, error) {
		calls++
		return "", errors.New("permission denied")
	})
	if err == nil || !strings.Contains(err.Error(), "--state") || calls != 1 {
		t.Fatalf("unexpected failure: %v, calls %d", err, calls)
	}
	if err := applyRequiredFirewall(litecpFirewallProfile{}, "unknown", func(string, ...string) (string, error) {
		t.Fatal("unsupported backend executed command")
		return "", nil
	}); err == nil {
		t.Fatal("unknown backend accepted")
	}
	ports := uniqueFirewallPorts([]string{"369/tcp", "4321/tcp", "369/tcp"})
	if len(ports) != 2 {
		t.Fatal(ports)
	}
}

func TestRequiredFirewallProfile(t *testing.T) {
	p := requiredFirewallProfile([]string{"2222"}, false, false, "")
	if !containsString(p.Inbound, "2222/tcp") || containsString(p.Inbound, "25/tcp") || containsString(p.Inbound, "3306/tcp") {
		t.Fatal(p)
	}
	p = requiredFirewallProfile([]string{"22"}, true, true, "8444")
	for _, port := range []string{"25/tcp", "993/tcp", "21/tcp", "50000-50049/tcp", "8444/tcp"} {
		if !containsString(p.Inbound, port) {
			t.Fatal(port)
		}
	}
	if !containsString(p.Outbound, "53/udp") {
		t.Fatal("DNS missing")
	}
}

func TestRequiredFirewallAdditive(t *testing.T) {
	calls := []string{}
	err := applyRequiredFirewall(requiredFirewallProfile([]string{"22"}, false, false, ""), "firewalld", func(cmd string, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		calls = append(calls, joined)
		if joined == "--get-active-zones" {
			return "public\n  interfaces: eth0\n", nil
		}
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, permanent := false, false
	for _, call := range calls {
		if strings.Contains(call, "--remove") {
			t.Fatal(call)
		}
		if strings.Contains(call, "--add-rich-rule") {
			if strings.Contains(call, "--permanent") {
				permanent = true
			} else {
				runtime = true
			}
		}
	}
	_ = runtime
	if !permanent {
		t.Fatal("runtime and persistent rules required")
	}
}

func TestRequiredFirewallReapplyRetainsAddressRestrictions(t *testing.T) {
	p := litecpFirewallProfile{Inbound: []string{"25/tcp"}, Outbound: []string{"53/udp"}}
	run := func(cmd string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "--get-active-zones", "--get-zones":
			return "public", nil
		case "--get-policies", "--permanent --get-policies":
			return "litecp-required", nil
		case "--zone=public --list-rich-rules":
			return `rule priority="-100" family="ipv4" source address="192.0.2.10" port port="25" protocol="tcp" accept`, nil
		case "--policy=litecp-required --list-rich-rules":
			return `rule priority="-100" family="ipv6" destination address="2001:db8::1" port port="53" protocol="udp" accept`, nil
		case "status":
			return "Status: active\n25/tcp ALLOW IN 192.0.2.10 # litecp-required\n2001:db8::1 53/udp ALLOW OUT Anywhere (v6) # litecp-required", nil
		}
		line := strings.Join(args, " ")
		if strings.Contains(line, "--add-rich-rule") || strings.Contains(line, "--add-port=") || args[0] == "insert" {
			t.Fatalf("reapply added unrestricted access: %s %s", cmd, line)
		}
		return "", nil
	}
	for _, backend := range []string{"firewalld", "ufw"} {
		if err := applyRequiredFirewall(p, backend, run); err != nil {
			t.Fatal(backend, err)
		}
	}
}
