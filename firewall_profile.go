package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type litecpFirewallProfile struct {
	Inbound  []string `json:"inbound"`
	Outbound []string `json:"outbound"`
}

func requiredFirewallProfile(sshPorts []string, mail, ftp bool, webmailPort string) litecpFirewallProfile {
	p := litecpFirewallProfile{Inbound: []string{"80/tcp", "443/tcp", "4321/tcp"}, Outbound: []string{"53/tcp", "53/udp", "123/udp", "80/tcp", "443/tcp", "22/tcp"}}
	for _, port := range sshPorts {
		p.Inbound = append(p.Inbound, port+"/tcp")
	}
	if mail {
		p.Inbound = append(p.Inbound, "25/tcp", "465/tcp", "587/tcp", "993/tcp")
		p.Outbound = append(p.Outbound, "25/tcp", "465/tcp", "587/tcp", "993/tcp")
	}
	if ftp {
		p.Inbound = append(p.Inbound, "21/tcp", "50000-50049/tcp")
	}
	if webmailPort != "" {
		p.Inbound = append(p.Inbound, webmailPort+"/tcp")
	}
	return p
}

// Add required access without flushing rules, changing defaults, or exposing
// expose MySQL, PHP-FPM, internal DNS or application backend ports.
// Current runtime rules are persisted before the required policy reload.
func applyRequiredFirewall(p litecpFirewallProfile, backend string, run securityProbe) error {
	if backend != "ufw" && backend != "firewalld" {
		return fmt.Errorf("unsupported firewall backend: %s", backend)
	}
	call := func(args ...string) error {
		_, err := run("firewall-cmd", args...)
		if err != nil {
			return fmt.Errorf("firewalld %s: %w", strings.Join(args, " "), err)
		}
		return nil
	}
	if backend == "ufw" {
		status, err := run("ufw", "status")
		if err != nil {
			return err
		}
		if !strings.Contains(status, "Status: active") {
			return fmt.Errorf("UFW must already be active")
		}
		for _, direction := range []string{"in", "out"} {
			ports := p.Inbound
			if direction == "out" {
				ports = p.Outbound
			}
			for _, port := range ports {
				parts := strings.Split(port, "/")
				number := strings.ReplaceAll(parts[0], "-", ":")
				if _, err = run("ufw", "insert", "1", "allow", direction, "to", "any", "port", number, "proto", parts[1]); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := call("--state"); err != nil {
		return fmt.Errorf("firewalld must already be active: %w", err)
	}
	zones, err := run("firewall-cmd", "--get-active-zones")
	if err != nil {
		return err
	}
	names := []string{}
	for _, line := range strings.Split(zones, "\n") {
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			names = append(names, strings.Fields(line)[0])
		}
	}
	if len(names) == 0 {
		zone, e := run("firewall-cmd", "--get-default-zone")
		if e != nil {
			return e
		}
		names = append(names, strings.TrimSpace(zone))
	}
	if err := call("--runtime-to-permanent"); err != nil {
		return err
	}
	for _, permanent := range []bool{true} {
		prefix := []string{}
		if permanent {
			prefix = append(prefix, "--permanent")
		}
		for _, zone := range names {
			for _, port := range p.Inbound {
				parts := strings.Split(port, "/")
				rule := fmt.Sprintf(`rule priority="-100" port port="%s" protocol="%s" accept`, parts[0], parts[1])
				if err := call(append(append([]string{}, prefix...), "--zone="+zone, "--add-rich-rule="+rule)...); err != nil {
					return err
				}
			}
		}
		policies, e := run("firewall-cmd", append(append([]string{}, prefix...), "--get-policies")...)
		if e != nil {
			return e
		}
		if !containsString(strings.Fields(policies), "litecp-required") {
			if err := call(append(append([]string{}, prefix...), "--new-policy=litecp-required")...); err != nil {
				return err
			}
		}
		scope := append(append([]string{}, prefix...), "--policy=litecp-required")
		for _, arg := range []string{"--add-ingress-zone=HOST", "--add-egress-zone=ANY", "--set-priority=-1000"} {
			if err := call(append(append([]string{}, scope...), arg)...); err != nil {
				return err
			}
		}
		for _, port := range p.Outbound {
			if err := call(append(append([]string{}, scope...), "--add-port="+port)...); err != nil {
				return err
			}
		}
	}
	return call("--reload")
}

func (a *App) applyLiteCPFirewall(w http.ResponseWriter, r *http.Request) {
	if a.system.demo {
		writeError(w, 400, "Firewall changes require the installed Linux server.")
		return
	}
	// withAuth already serializes this mutation.
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	// Inspect sshd on the host, outside ProtectHome/PrivateDevices and with an
	// absolute path (service PATH may not contain /usr/sbin).
	ssh, err := runHostCommand(ctx, 20*time.Second, "/usr/sbin/sshd", "-T")
	if err != nil {
		writeError(w, 502, "Cannot verify SSH ports; firewall was not changed: "+err.Error())
		return
	}
	ports := []string{}
	if data, readErr := os.ReadFile(filepath.Join(a.system.dataDir, "ssh-port")); readErr == nil {
		value := strings.TrimSpace(string(data))
		if port, parseErr := strconv.Atoi(value); parseErr == nil && port > 0 && port <= 65535 {
			ports = append(ports, value)
		}
	}
	for _, line := range strings.Split(ssh, "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[0] == "port" {
			n, e := strconv.Atoi(parts[1])
			if e != nil || n < 1 || n > 65535 {
				writeError(w, 502, "Invalid SSH configuration.")
				return
			}
			ports = append(ports, parts[1])
		}
	}
	if len(ports) == 0 {
		writeError(w, 502, "No SSH port detected; firewall was not changed.")
		return
	}
	_, ftpErr := os.Stat(filepath.Join(a.system.ftpRoot(), "installed"))
	webmailPort := ""
	if a.system.WebmailAvailable() {
		u, e := url.Parse(a.system.WebmailURL())
		if e == nil {
			webmailPort = u.Port()
			if webmailPort == "" {
				webmailPort = "443"
			}
		}
	}
	profile := requiredFirewallProfile(ports, a.store.Settings().EmailHostingEnabled, ftpErr == nil, webmailPort)
	profile.Inbound = append(profile.Inbound, strconv.Itoa(currentPanelPort())+"/tcp")
	// Protect the new listener while a panel-port change awaits confirmation.
	if b, e := os.ReadFile("/var/lib/litecp/panel-port-pending"); e == nil {
		fields := strings.Fields(string(b))
		if len(fields) == 3 {
			if p, e := strconv.Atoi(fields[1]); e == nil && p >= 1024 && p <= 65535 {
				profile.Inbound = append(profile.Inbound, strconv.Itoa(p)+"/tcp")
			}
		}
	}
	profile.Inbound = uniqueFirewallPorts(profile.Inbound)
	profile.Outbound = uniqueFirewallPorts(profile.Outbound)
	err = applyRequiredFirewall(profile, a.system.firewallBackend(), func(cmd string, args ...string) (string, error) {
		return runHostCommand(ctx, 25*time.Second, cmd, args...)
	})
	if err != nil {
		writeError(w, 502, "Firewall profile was not fully applied; existing rules were preserved. Reapply to finish: "+err.Error())
		return
	}
	a.store.AddActivity("LiteCP firewall applied", "Required inbound and outbound ports allowed; existing rules preserved.", "success")
	writeJSON(w, 200, profile)
}

func uniqueFirewallPorts(ports []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, port := range ports {
		if !seen[port] {
			result = append(result, port)
			seen[port] = true
		}
	}
	return result
}
