package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type profileRule struct {
	Direction string `json:"direction"`
	Port      string `json:"port"`
	Zone      string `json:"zone"`
	Raw       string `json:"-"`
}
type profileState struct {
	Enabled bool          `json:"enabled"`
	Rules   []profileRule `json:"rules"`
	Backend string        `json:"backend"`
}

func inspectFirewallProfile(backend string, run securityProbe) (profileState, error) {
	state := profileState{Backend: backend, Rules: []profileRule{}}
	if backend == "ufw" {
		out, err := run("ufw", "status")
		if err != nil {
			return state, err
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "# litecp-required") {
				fields := strings.Fields(line)
				if len(fields) > 0 {
					direction := "inbound"
					if containsString(fields, "OUT") {
						direction = "outbound"
					}
					state.Rules = append(state.Rules, profileRule{Direction: direction, Port: fields[0]})
				}
			}
		}
	} else if backend == "firewalld" {
		zones, err := run("firewall-cmd", "--get-zones")
		if err != nil {
			return state, err
		}
		for _, zone := range strings.Fields(zones) {
			out, err := run("firewall-cmd", "--zone="+zone, "--list-rich-rules")
			if err != nil {
				return state, err
			}
			for _, line := range strings.Split(out, "\n") {
				if !strings.HasPrefix(line, `rule priority="-100" port port="`) || !strings.HasSuffix(line, " accept") {
					continue
				}
				parts := strings.Split(line, `"`)
				if len(parts) != 7 {
					continue
				}
				state.Rules = append(state.Rules, profileRule{Direction: "inbound", Port: parts[3] + "/" + parts[5], Zone: zone, Raw: line})
			}
		}
		policies, err := run("firewall-cmd", "--get-policies")
		if err != nil {
			return state, err
		}
		if containsString(strings.Fields(policies), "litecp-required") {
			out, err := run("firewall-cmd", "--policy=litecp-required", "--list-ports")
			if err != nil {
				return state, err
			}
			for _, port := range strings.Fields(out) {
				state.Rules = append(state.Rules, profileRule{Direction: "outbound", Port: port})
			}
		}
	} else {
		return state, fmt.Errorf("unsupported firewall backend")
	}
	state.Enabled = len(state.Rules) > 0
	return state, nil
}

func (a *App) litecpFirewallControl(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		a.applyLiteCPFirewall(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "GET, POST, DELETE")
		writeError(w, 405, "Method not allowed.")
		return
	}
	if a.system.demo {
		writeJSON(w, 200, profileState{Rules: []profileRule{}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	run := func(cmd string, args ...string) (string, error) {
		return runHostCommand(ctx, 25*time.Second, cmd, args...)
	}
	state, err := inspectFirewallProfile(a.system.firewallBackend(), run)
	if err != nil {
		writeError(w, 502, "Cannot read LiteCP firewall rules: "+err.Error())
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, 200, state)
		return
	}
	// Keep access ports open before removing only this profile's rules.
	ssh, err := run("/usr/sbin/sshd", "-T")
	if err != nil {
		writeError(w, 502, "Cannot verify SSH access; no rules changed.")
		return
	}
	protected := []string{"80/tcp", "443/tcp", fmt.Sprintf("%d/tcp", currentPanelPort())}
	for _, line := range strings.Split(ssh, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "port" {
			protected = append(protected, f[1]+"/tcp")
		}
	}
	if state.Backend == "firewalld" {
		if _, err = run("firewall-cmd", "--runtime-to-permanent"); err != nil {
			writeError(w, 502, "Cannot preserve current firewall rules; profile was not changed.")
			return
		}
		for _, rule := range state.Rules {
			if rule.Direction != "inbound" {
				continue
			}
			if containsString(protected, rule.Port) {
				_, err = run("firewall-cmd", "--permanent", "--zone="+rule.Zone, "--add-port="+rule.Port)
				if err != nil {
					break
				}
			}
			_, err = run("firewall-cmd", "--permanent", "--zone="+rule.Zone, "--remove-rich-rule="+rule.Raw)
			if err != nil {
				break
			}
		}
		if err == nil {
			policies, e := run("firewall-cmd", "--permanent", "--get-policies")
			err = e
			if err == nil && containsString(strings.Fields(policies), "litecp-required") {
				_, err = run("firewall-cmd", "--permanent", "--delete-policy=litecp-required")
			}
		}
		if err == nil {
			_, err = run("firewall-cmd", "--reload")
		}
	} else {
		for _, rule := range state.Rules {
			parts := strings.Split(rule.Port, "/")
			if len(parts) != 2 {
				continue
			}
			direction := "in"
			if rule.Direction == "outbound" {
				direction = "out"
			}
			number := strings.ReplaceAll(parts[0], "-", ":")
			if direction == "in" && containsString(protected, rule.Port) {
				_, err = run("ufw", "allow", "in", "to", "any", "port", number, "proto", parts[1], "comment", "litecp-access")
				if err != nil {
					break
				}
			}
			_, err = run("ufw", "--force", "delete", "allow", direction, "to", "any", "port", number, "proto", parts[1], "comment", "litecp-required")
			if err != nil {
				break
			}
		}
	}
	if err != nil {
		writeError(w, 502, "Profile disable did not finish; refresh and retry: "+err.Error())
		return
	}
	state, err = inspectFirewallProfile(state.Backend, run)
	if err != nil || state.Enabled {
		writeError(w, 502, "Profile rules still need review. Refresh status.")
		return
	}
	a.store.AddActivity("LiteCP firewall disabled", "Required-port profile removed; access ports and unrelated rules preserved.", "warning")
	writeJSON(w, 200, state)
}
