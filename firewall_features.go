package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Root-only, fixed executables called after allowlist validation. Running a
// transient unit keeps the panel's own filesystem sandbox read-only.
func runHostCommand(ctx context.Context, timeout time.Duration, command string, args ...string) (string, error) {
	systemdArgs := []string{"--quiet", "--wait", "--pipe", "--collect", "--service-type=exec", "--setenv=DEBIAN_FRONTEND=noninteractive", fmt.Sprintf("--property=RuntimeMaxSec=%d", int(timeout.Seconds())), "--", command}
	return runCommand(ctx, timeout, "systemd-run", append(systemdArgs, args...)...)
}

func normalizeFirewallRule(rule FirewallRule) FirewallRule {
	rule.Direction = strings.ToLower(strings.TrimSpace(rule.Direction))
	if rule.Direction == "" {
		rule.Direction = "inbound"
	}
	rule.Action = strings.ToLower(strings.TrimSpace(rule.Action))
	if rule.Action == "" {
		rule.Action = "allow"
	}
	rule.Protocol = strings.ToLower(strings.TrimSpace(rule.Protocol))
	rule.Label = strings.TrimSpace(rule.Label)
	if rule.Direction == "outbound" {
		rule.Source = ""
		rule.Destination = strings.TrimSpace(rule.Destination)
		if rule.Destination == "" {
			rule.Destination = "any"
		}
	} else {
		rule.Destination = ""
		rule.Source = strings.TrimSpace(rule.Source)
		if rule.Source == "" {
			rule.Source = "any"
		}
	}
	return rule
}

func (a *App) firewallRuleAction(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		a.deleteFirewallRule(w, r)
		return
	}
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "PUT, DELETE")
		writeError(w, 405, "Method not allowed.")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/firewall/")
	old := a.store.FirewallRuleByID(id)
	if old == nil {
		writeError(w, 404, "Firewall rule not found.")
		return
	}
	var input FirewallRule
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	rule := normalizeFirewallRule(input)
	rule.ID, rule.CreatedAt = old.ID, old.CreatedAt
	if err := validateFirewallRule(rule); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	oldNormalized := normalizeFirewallRule(*old)
	if oldNormalized == rule {
		writeJSON(w, 200, rule)
		return
	}
	if a.store.HasFirewallRule(rule) {
		writeError(w, 409, "That rule is already managed.")
		return
	}
	if err := a.system.ApplyFirewallRule(r.Context(), *old, true); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	if err := a.system.ApplyFirewallRule(r.Context(), rule, false); err != nil {
		_ = a.system.ApplyFirewallRule(context.Background(), *old, false)
		writeError(w, 502, "Rule update failed; the original rule was reapplied: "+err.Error())
		return
	}
	if err := a.store.UpdateFirewallRule(id, rule); err != nil {
		_ = a.system.ApplyFirewallRule(context.Background(), rule, true)
		_ = a.system.ApplyFirewallRule(context.Background(), *old, false)
		writeError(w, 500, "Firewall edit was rolled back because state could not be saved.")
		return
	}
	a.store.AddActivity("Firewall rule edited", rule.Direction+" "+rule.Action+": "+rule.Label, "warning")
	writeJSON(w, 200, rule)
}
