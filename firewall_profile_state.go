package main

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type profileRule struct {
	Direction string `json:"direction"`
	Port      string `json:"port"`
	Zone      string `json:"zone"`
	Address   string `json:"address,omitempty"`
	Raw       string `json:"-"`
	Remarks   string `json:"remarks"`
}
type profileState struct {
	Enabled bool          `json:"enabled"`
	Rules   []profileRule `json:"rules"`
	Backend string        `json:"backend"`
}

var profileRichRulePattern = regexp.MustCompile(`^rule priority="-100"(?: family="(ipv4|ipv6)" (source|destination) address="([0-9A-Fa-f:./]+)")? port port="([0-9-]+)" protocol="(tcp|udp)" accept$`)
var profileUFWRulePattern = regexp.MustCompile(`^(.+?)\s+ALLOW(?:\s+(IN|OUT))?\s+(.+?)\s+# litecp-required$`)

func parseProfileRichRule(line, direction, zone string) (profileRule, bool) {
	parts := profileRichRulePattern.FindStringSubmatch(line)
	if parts == nil || (direction == "outbound" && parts[2] != "destination") ||
		(direction == "inbound" && parts[2] != "" && parts[2] != "source") {
		return profileRule{}, false
	}
	rule := profileRule{Direction: direction, Zone: zone, Port: parts[4] + "/" + parts[5], Address: parts[3], Raw: line}
	return rule, validateProfilePort(rule.Port) == nil
}

func parseProfileUFWRule(line string) (profileRule, bool) {
	fields := profileUFWRulePattern.FindStringSubmatch(strings.TrimSpace(line))
	if fields == nil {
		return profileRule{}, false
	}
	to := strings.TrimSuffix(fields[1], " (v6)")
	from := strings.TrimSuffix(fields[3], " (v6)")
	rule := profileRule{Direction: "inbound"}
	port, address := to, from
	if fields[2] == "OUT" {
		rule.Direction = "outbound"
		if host, targetPort, ok := strings.Cut(to, " "); ok {
			port, address = strings.TrimSpace(targetPort), host
		} else {
			address = ""
		}
	}
	rule.Port = strings.ReplaceAll(port, ":", "-")
	if address != "" && address != "Anywhere" {
		values, err := parseFirewallAddresses(address)
		if err != nil || len(values) != 1 || values[0] == "cloudflare" || values[0] == "any" {
			return profileRule{}, false
		}
		rule.Address = values[0]
	}
	return rule, validateProfilePort(rule.Port) == nil
}

func inspectFirewallProfile(backend string, run securityProbe) (profileState, error) {
	state := profileState{Backend: backend, Rules: []profileRule{}}
	if backend == "ufw" {
		out, err := run("ufw", "status")
		if err != nil {
			return state, err
		}
		seen := make(map[string]bool)
		for _, line := range strings.Split(out, "\n") {
			rule, ok := parseProfileUFWRule(line)
			key := profileRuleKey(rule)
			if ok && !seen[key] {
				state.Rules = append(state.Rules, rule)
				seen[key] = true
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
				if rule, ok := parseProfileRichRule(strings.TrimSpace(line), "inbound", zone); ok {
					state.Rules = append(state.Rules, rule)
				}
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
			out, err = run("firewall-cmd", "--policy=litecp-required", "--list-rich-rules")
			if err != nil {
				return state, err
			}
			for _, line := range strings.Split(out, "\n") {
				if rule, ok := parseProfileRichRule(strings.TrimSpace(line), "outbound", ""); ok {
					state.Rules = append(state.Rules, rule)
				}
			}
		}
	} else {
		return state, fmt.Errorf("unsupported firewall backend")
	}
	state.Enabled = len(state.Rules) > 0
	return state, nil
}

func (a *App) litecpFirewallControl(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		a.editFirewallProfile(w, r)
		return
	}
	if r.Method == http.MethodPost {
		a.applyLiteCPFirewall(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "GET, POST, PUT, DELETE")
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
		address := r.URL.Query().Get("address")
		var selected *profileRule
		for i := range state.Rules {
			rule := &state.Rules[i]
			if rule.Port == port && rule.Direction == direction && rule.Zone == zone && rule.Address == address {
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
			protected := profileRuleHasPort(port, fmt.Sprintf("%d/tcp", currentPanelPort()))
			for _, saved := range savedFirewallSSHPorts(a.system.dataDir) {
				protected = protected || profileRuleHasPort(port, saved)
			}
			for _, line := range strings.Split(ssh, "\n") {
				f := strings.Fields(line)
				if len(f) == 2 && f[0] == "port" && profileRuleHasPort(port, f[1]+"/tcp") {
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
		if profileContainsRule(updated, *selected) {
			writeError(w, 502, "Rule is still active. Refresh and retry.")
			return
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
			if _, err = preserveProfileAccessRule(state.Backend, rule, protected, run); err != nil {
				break
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
			var retained bool
			retained, err = preserveProfileAccessRule(state.Backend, rule, protected, run)
			if err != nil {
				break
			}
			if retained {
				continue
			}
			err = removeProfileRule(state.Backend, rule, run)
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

// Keep only protected members of a profile range, with its existing address
// scope. Exact UFW selectors are adopted by relabeling and must not be deleted.
func preserveProfileAccessRule(backend string, rule profileRule, protected []string, run securityProbe) (bool, error) {
	if rule.Direction != "inbound" {
		return false, nil
	}
	retained := false
	for _, port := range uniqueFirewallPorts(protected) {
		if !profileRuleHasPort(rule.Port, port) {
			continue
		}
		next := newProfileRule(rule, port, rule.Address)
		if backend == "firewalld" {
			argument := "--add-port=" + port
			if rule.Address != "" {
				argument = "--add-rich-rule=" + strings.Replace(next.Raw, `priority="-100"`, `priority="-90"`, 1)
			}
			if _, err := run("firewall-cmd", "--permanent", "--zone="+rule.Zone, argument); err != nil {
				return false, err
			}
			continue
		}
		args := profileUFWArgs(next)
		args[len(args)-1] = "litecp-access"
		if _, err := run("ufw", args...); err != nil {
			return false, err
		}
		retained = retained || rule.Port == port
	}
	return retained, nil
}

// Remove only a rule previously identified as belonging to the LiteCP profile.
func removeProfileRule(backend string, rule profileRule, run securityProbe) error {
	if backend == "firewalld" {
		args := profileFirewalldArgs(rule, "remove")
		if _, err := run("firewall-cmd", append([]string{"--permanent"}, args...)...); err != nil {
			return err
		}
		_, err := run("firewall-cmd", args...)
		return err
	}
	if backend != "ufw" {
		return fmt.Errorf("unsupported firewall backend")
	}
	if err := validateProfilePort(rule.Port); err != nil {
		return fmt.Errorf("unsupported rule port")
	}
	_, err := run("ufw", append([]string{"--force", "delete"}, profileUFWArgs(rule)...)...)
	return err
}
