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
	Remarks   string `json:"remarks"`
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
		return runFirewallCommand(ctx, 25*time.Second, cmd, args...)
	}
	state, err := inspectFirewallProfile(a.system.firewallBackend(), run)
	if err != nil {
		writeError(w, 502, "Cannot read LiteCP firewall rules: "+err.Error())
		return
	}
	a.annotateFirewallProfile(&state)
	if r.Method == http.MethodGet {
		writeJSON(w, 200, state)
		return
	}
	if r.URL.Query().Get("port") != "" {
		port, direction, zone := r.URL.Query().Get("port"), r.URL.Query().Get("direction"), r.URL.Query().Get("zone")
		var selected *profileRule
		for i := range state.Rules {
			rule := &state.Rules[i]
			if rule.Port == port && rule.Direction == direction && rule.Zone == zone {
				selected = rule
				break
			}
		}
		if selected == nil {
			writeError(w, 404, "Rule no longer exists. Refresh the list.")
			return
		}
		if direction == "inbound" {
			ssh, e := run("/usr/sbin/sshd", "-T")
			if e != nil {
				writeError(w, 502, "Cannot verify SSH access; no rule changed.")
				return
			}
			protected := port == fmt.Sprintf("%d/tcp", currentPanelPort()) || containsString(savedFirewallSSHPorts(a.system.dataDir), port)
			for _, line := range strings.Split(ssh, "\n") {
				f := strings.Fields(line)
				if len(f) == 2 && f[0] == "port" && port == f[1]+"/tcp" {
					protected = true
				}
			}
			if protected {
				writeError(w, 409, "This port provides current SSH or panel access. Change the service port first.")
				return
			}
		}
		if err := removeProfileRule(state.Backend, *selected, run); err != nil {
			writeError(w, 502, "Rule removal did not finish; refresh status: "+err.Error())
			return
		}
		updated, e := inspectFirewallProfile(state.Backend, run)
		if e != nil {
			writeError(w, 502, "Cannot verify removal. Refresh status.")
			return
		}
		a.annotateFirewallProfile(&updated)
		for _, rule := range updated.Rules {
			if rule.Port == port && rule.Direction == direction && rule.Zone == zone {
				writeError(w, 502, "Rule is still active. Refresh and retry.")
				return
			}
		}
		a.store.AddActivity("Firewall rule removed", direction+" "+port, "warning")
		writeJSON(w, 200, updated)
		return
	}
	// Keep access ports open before removing only this profile's rules.
	ssh, err := run("/usr/sbin/sshd", "-T")
	if err != nil {
		writeError(w, 502, "Cannot verify SSH access; no rules changed.")
		return
	}
	protected := []string{"80/tcp", "443/tcp", fmt.Sprintf("%d/tcp", currentPanelPort())}
	protected = append(protected, savedFirewallSSHPorts(a.system.dataDir)...)
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
	a.annotateFirewallProfile(&state)
	writeJSON(w, 200, state)
}

// Remove only a rule previously identified as belonging to the LiteCP profile.
func removeProfileRule(backend string, rule profileRule, run securityProbe) error {
	if backend == "firewalld" {
		args := []string{"--zone=" + rule.Zone, "--remove-rich-rule=" + rule.Raw}
		if rule.Direction == "outbound" {
			args = []string{"--policy=litecp-required", "--remove-port=" + rule.Port}
		}
		if _, err := run("firewall-cmd", append([]string{"--permanent"}, args...)...); err != nil {
			return err
		}
		_, err := run("firewall-cmd", args...)
		return err
	}
	if backend != "ufw" {
		return fmt.Errorf("unsupported firewall backend")
	}
	parts := strings.Split(rule.Port, "/")
	if len(parts) != 2 {
		return fmt.Errorf("unsupported rule port")
	}
	direction := "in"
	if rule.Direction == "outbound" {
		direction = "out"
	}
	_, err := run("ufw", "--force", "delete", "allow", direction, "to", "any", "port", strings.ReplaceAll(parts[0], "-", ":"), "proto", parts[1], "comment", "litecp-required")
	return err
}
