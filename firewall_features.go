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
	systemdArgs := []string{"--quiet", "--wait", "--pipe", "--collect", "--service-type=exec", "--setenv=DEBIAN_FRONTEND=noninteractive", "--setenv=LC_ALL=C", fmt.Sprintf("--property=RuntimeMaxSec=%d", int(timeout.Seconds())), "--", command}
	return runCommand(ctx, timeout, "systemd-run", append(systemdArgs, args...)...)
}

// firewalld owns configuration writes through D-Bus, so its client can remain
// inside the broker sandbox. Passing broker-owned pipes through systemd-run can
// fail under SELinux. The exact read-only sshd probe also needs no host writer.
func firewallDirectCommand(command string, args []string) bool {
	return command == "firewall-cmd" || (command == "/usr/sbin/sshd" && len(args) == 1 && args[0] == "-T")
}
func runFirewallCommand(ctx context.Context, timeout time.Duration, command string, args ...string) (string, error) {
	if firewallDirectCommand(command, args) {
		return runCommand(ctx, timeout, command, args...)
	}
	return runHostCommand(ctx, timeout, command, args...)
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
	rule.Remarks = strings.TrimSpace(rule.Remarks)
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
	var input struct {
		FirewallRule
		Remarks *string `json:"remarks"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	input.FirewallRule.Remarks = old.Remarks
	if input.Remarks != nil {
		input.FirewallRule.Remarks = *input.Remarks
	}
	rule := normalizeFirewallRule(input.FirewallRule)
	rule.ID, rule.CreatedAt = old.ID, old.CreatedAt
	rule.ProfileKey = old.ProfileKey
	if err := validateFirewallRule(rule); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	rule = canonicalFirewallRule(rule)
	oldNormalized := canonicalFirewallRule(normalizeFirewallRule(*old))
	if oldNormalized == rule {
		writeJSON(w, 200, rule)
		return
	}
	notesOnly := oldNormalized
	notesOnly.Label = rule.Label
	notesOnly.Remarks = rule.Remarks
	if notesOnly == rule {
		rule = withFirewallAddresses(rule, firewallAddresses(normalizeFirewallRule(*old)))
		if err := a.store.UpdateFirewallRule(id, rule); err != nil {
			writeError(w, 500, "Cannot save firewall remarks.")
			return
		}
		a.store.AddActivity("Firewall remarks updated", rule.Label, "success")
		writeJSON(w, 200, rule)
		return
	}
	if a.store.HasFirewallRule(rule) {
		writeError(w, 409, "At least one address already has this firewall rule in LiteCP.")
		return
	}
	if old.ProfileKey != "" {
		writeError(w, 409, "This Cloudflare restriction is linked to a required-port rule. Edit its address type in the required-port table.")
		return
	}
	removed, added := firewallRuleChanges(*old, rule)
	apply := func(ctx context.Context, rule *FirewallRule, remove bool) error {
		if rule == nil {
			return nil
		}
		return a.system.ApplyFirewallRule(ctx, *rule, remove)
	}
	if err := apply(r.Context(), removed, true); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	if err := apply(r.Context(), added, false); err != nil {
		restoreErr := apply(context.Background(), removed, false)
		if restoreErr != nil {
			writeError(w, 502, fmt.Sprintf("Rule update failed (%v), and the original rule could not be restored (%v). Check the host firewall immediately.", err, restoreErr))
			return
		}
		writeError(w, 502, "Rule update failed; the original rule was restored: "+err.Error())
		return
	}
	if err := a.store.UpdateFirewallRule(id, rule); err != nil {
		removeErr := apply(context.Background(), added, true)
		restoreErr := apply(context.Background(), removed, false)
		if removeErr != nil || restoreErr != nil {
			writeError(w, 500, fmt.Sprintf("Panel state could not be saved, and firewall recovery was incomplete (remove new: %v; restore original: %v). Check the host firewall immediately.", removeErr, restoreErr))
			return
		}
		writeError(w, 500, "Firewall edit was rolled back because panel state could not be saved.")
		return
	}
	a.store.AddActivity("Firewall rule edited", rule.Direction+" "+rule.Action+": "+rule.Label, "warning")
	writeJSON(w, 200, rule)
}
