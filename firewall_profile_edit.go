package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func profileRuleKey(rule profileRule) string {
	key := rule.Direction + "|" + rule.Zone + "|" + rule.Port
	if rule.Address != "" {
		key += "|" + rule.Address
	}
	return key
}

func parseProfilePort(value string) (int, int, string, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || (parts[1] != "tcp" && parts[1] != "udp") {
		return 0, 0, "", fmt.Errorf("select TCP or UDP")
	}
	rangeParts := strings.Split(parts[0], "-")
	if len(rangeParts) > 2 {
		return 0, 0, "", fmt.Errorf("enter a port or range, for example 443 or 50000-50049")
	}
	first, last := 0, 0
	for i, part := range rangeParts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 1 || number > 65535 || (i > 0 && number <= last) || strconv.Itoa(number) != part {
			return 0, 0, "", fmt.Errorf("ports must be between 1 and 65535 in ascending order")
		}
		if i == 0 {
			first = number
		}
		last = number
	}
	return first, last, parts[1], nil
}

func validateProfilePort(value string) error {
	_, _, _, err := parseProfilePort(value)
	return err
}

func profileRuleHasPort(rule, port string) bool {
	first, last, protocol, err := parseProfilePort(rule)
	if err != nil {
		return false
	}
	number, _, wantedProtocol, err := parseProfilePort(port)
	return err == nil && protocol == wantedProtocol && number >= first && number <= last
}

func profileContainsRule(state profileState, rule profileRule) bool {
	for _, candidate := range state.Rules {
		if profileRuleKey(candidate) == profileRuleKey(rule) {
			return true
		}
	}
	return false
}

func profileFirewalldArgs(rule profileRule, verb string) []string {
	scope := "--zone=" + rule.Zone
	if rule.Direction == "outbound" {
		scope = "--policy=litecp-required"
	}
	kind, value := "rich-rule", rule.Raw
	if rule.Direction == "outbound" && rule.Address == "" {
		kind, value = "port", rule.Port
	}
	if verb == "list" {
		return []string{scope, "--list-" + kind + "s"}
	}
	return []string{scope, "--" + verb + "-" + kind + "=" + value}
}

func profileUFWArgs(rule profileRule) []string {
	port, protocol, _ := strings.Cut(rule.Port, "/")
	direction, source, destination := "in", "any", "any"
	if rule.Direction == "outbound" {
		direction = "out"
		if rule.Address != "" {
			destination = rule.Address
		}
	} else if rule.Address != "" {
		source = rule.Address
	}
	args := []string{"allow", direction}
	if source != "any" {
		args = append(args, "from", source)
	}
	return append(args, "to", destination, "port", strings.ReplaceAll(port, "-", ":"), "proto", protocol, "comment", "litecp-required")
}

func newProfileRule(old profileRule, port, address string) profileRule {
	next := old
	next.Port, next.Address = port, address
	number, protocol, _ := strings.Cut(port, "/")
	scope := ""
	if address != "" {
		family, endpoint := "ipv4", "source"
		if strings.Contains(address, ":") {
			family = "ipv6"
		}
		if next.Direction == "outbound" {
			endpoint = "destination"
		}
		scope = fmt.Sprintf(` family="%s" %s address="%s"`, family, endpoint, address)
	}
	next.Raw = fmt.Sprintf(`rule priority="-100"%s port port="%s" protocol="%s" accept`, scope, number, protocol)
	return next
}

func firewalldProfileRuleExists(rule profileRule, permanent bool, run securityProbe) (bool, error) {
	args := profileFirewalldArgs(rule, "list")
	if permanent {
		args = append([]string{"--permanent"}, args...)
	}
	output, err := run("firewall-cmd", args...)
	if err != nil {
		return false, err
	}
	if rule.Direction == "outbound" && rule.Address == "" {
		return containsString(strings.Fields(output), rule.Port), nil
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == rule.Raw {
			return true, nil
		}
	}
	return false, nil
}

func addProfileRule(backend string, rule profileRule, run securityProbe) (bool, error) {
	if err := validateProfilePort(rule.Port); err != nil {
		return false, err
	}
	if backend == "ufw" {
		args := profileUFWArgs(rule)
		output, err := run("ufw", append([]string{"--dry-run", "prepend"}, args...)...)
		if err != nil {
			return false, err
		}
		if strings.Contains(output, "Skipping inserting existing rule") || strings.Contains(output, "Skipping adding existing rule") {
			return false, fmt.Errorf("a matching host firewall rule already exists; previous rule retained")
		}
		output, err = run("ufw", append([]string{"insert", "1"}, args...)...)
		if strings.Contains(output, "Skipping inserting existing rule") || strings.Contains(output, "Skipping adding existing rule") {
			return false, fmt.Errorf("a matching host firewall rule already exists; previous rule retained")
		}
		return err == nil, err
	}
	if backend != "firewalld" {
		return false, fmt.Errorf("unsupported firewall backend")
	}
	args := profileFirewalldArgs(rule, "add")
	if _, err := run("firewall-cmd", append([]string{"--permanent"}, args...)...); err != nil {
		return false, err
	}
	if _, err := run("firewall-cmd", args...); err != nil {
		return true, err
	}
	return true, nil
}

func addVerifiedProfileRule(backend string, next profileRule, run securityProbe) (bool, error) {
	owned, err := addProfileRule(backend, next, run)
	if err != nil {
		return owned, err
	}
	if backend == "firewalld" {
		for _, permanent := range []bool{false, true} {
			exists, err := firewalldProfileRuleExists(next, permanent, run)
			if err != nil || !exists {
				return owned, fmt.Errorf("replacement rule could not be verified. Previous rule retained (check: %v)", err)
			}
		}
	} else {
		state, err := inspectFirewallProfile(backend, run)
		if err != nil {
			return owned, fmt.Errorf("replacement rule could not be verified. Previous rule retained: %w", err)
		}
		if !profileContainsRule(state, next) {
			return owned, fmt.Errorf("replacement rule is not active. Previous rule retained")
		}
	}
	return owned, nil
}

func removeAddedProfileRules(backend string, added []profileRule, run securityProbe) error {
	var failures []string
	for _, rule := range added {
		if err := removeProfileRule(backend, rule, run); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("could not remove replacement rules: %s", strings.Join(failures, "; "))
	}
	return nil
}

func replaceProfileRules(backend string, old profileRule, added []profileRule, removeOld bool, run securityProbe, rollback func([]profileRule, bool) error) error {
	for i, next := range added {
		if owned, err := addVerifiedProfileRule(backend, next, run); err != nil {
			cleanup := added[:i]
			if owned {
				cleanup = added[:i+1]
			}
			if cleanupErr := rollback(cleanup, false); cleanupErr != nil {
				return fmt.Errorf("rule edit failed (%v), cleanup failed (%v). Check the host firewall", err, cleanupErr)
			}
			return err
		}
	}
	if removeOld {
		if err := removeProfileRule(backend, old, run); err != nil {
			if rollbackErr := rollback(added, true); rollbackErr != nil {
				return fmt.Errorf("rule edit failed (%v), original rule recovery failed (%v). Check the host firewall", err, rollbackErr)
			}
			return fmt.Errorf("rule edit failed. Previous rule restored: %w", err)
		}
	}
	return nil
}

func (a *App) readProfileRemarks() (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(a.system.dataDir, "firewall-profile-remarks.json"))
	notes := map[string]string{}
	if os.IsNotExist(err) {
		return notes, nil
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &notes)
	if notes == nil {
		notes = map[string]string{}
	}
	return notes, err
}

func (a *App) editFirewallProfile(w http.ResponseWriter, r *http.Request) {
	if a.system.demo {
		writeError(w, 400, "Firewall changes require the installed Linux server.")
		return
	}
	var input struct {
		Port      string  `json:"port"`
		Direction string  `json:"direction"`
		Zone      string  `json:"zone"`
		Address   string  `json:"address"`
		NewPort   string  `json:"newPort"`
		Addresses *string `json:"addresses"`
		Remarks   string  `json:"remarks"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if validateProfilePort(input.NewPort) != nil || utf8.RuneCountInString(input.Remarks) > 500 || strings.ContainsRune(input.Remarks, 0) {
		writeError(w, 400, "Enter a valid TCP/UDP port or range and remarks up to 500 characters")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	run := func(cmd string, args ...string) (string, error) {
		return runFirewallCommand(ctx, 25*time.Second, cmd, args...)
	}
	backend := a.system.firewallBackend()
	state, err := inspectFirewallProfile(backend, run)
	if err != nil {
		writeError(w, 502, "Cannot inspect firewall rules")
		return
	}
	var old *profileRule
	for i := range state.Rules {
		v := &state.Rules[i]
		if v.Port == input.Port && v.Direction == input.Direction && v.Zone == input.Zone && v.Address == input.Address {
			old = v
			break
		}
	}
	if old == nil {
		writeError(w, 404, "Rule changed. Refresh and try again")
		return
	}
	addresses := []string{old.Address}
	wantCloudflare := a.profileCloudflareGuard(*old) != nil
	if input.Addresses != nil {
		if strings.TrimSpace(*input.Addresses) == "" {
			writeError(w, 400, "Enter an IP address or CIDR network, or type 'any'.")
			return
		}
		addresses, err = parseFirewallAddresses(*input.Addresses)
		if err != nil || len(addresses) == 0 {
			writeError(w, 400, "Enter IP addresses or CIDR networks, or use 'any' or the Cloudflare preset.")
			return
		}
		wantCloudflare = len(addresses) == 1 && addresses[0] == "cloudflare"
		if len(addresses) == 1 && (addresses[0] == "any" || wantCloudflare) {
			addresses[0] = ""
		} else if broadFirewallAddress(addresses) {
			writeError(w, 400, "Use 'any' for unrestricted access, or enter specific IP addresses and networks.")
			return
		}
	}
	nextRules := make([]profileRule, 0, len(addresses))
	added := make([]profileRule, 0, len(addresses))
	removeOld := true
	for _, address := range addresses {
		next := newProfileRule(*old, input.NewPort, address)
		nextRules = append(nextRules, next)
		if profileRuleKey(next) == profileRuleKey(*old) {
			removeOld = false
		} else {
			added = append(added, next)
		}
	}
	mutatesFirewall := removeOld || len(added) > 0
	if old.Direction == "inbound" && (mutatesFirewall || wantCloudflare) {
		ssh, err := run("/usr/sbin/sshd", "-T")
		if err != nil {
			writeError(w, 502, "Cannot verify current SSH access")
			return
		}
		protected := savedFirewallSSHPorts(a.system.dataDir)
		for _, line := range strings.Split(ssh, "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && f[0] == "port" && validateProfilePort(f[1]+"/tcp") == nil {
				protected = append(protected, f[1]+"/tcp")
			}
		}
		if len(protected) == 0 {
			writeError(w, 502, "Cannot verify current SSH access.")
			return
		}
		protected = append(protected, fmt.Sprintf("%d/tcp", currentPanelPort()))
		if data, err := os.ReadFile(filepath.Join(a.system.dataDir, "panel-port-pending")); err == nil {
			if fields := strings.Fields(string(data)); len(fields) == 3 {
				protected = append(protected, fields[0]+"/tcp", fields[1]+"/tcp")
			}
		}
		for _, port := range protected {
			if profileRuleHasPort(old.Port, port) || profileRuleHasPort(input.NewPort, port) {
				writeError(w, 409, "The current SSH and panel access rules only support remarks edits here.")
				return
			}
		}
	}
	guardChange, err := a.planProfileCloudflare(*old, nextRules[0], wantCloudflare, input.Remarks)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	notes, err := a.readProfileRemarks()
	if err != nil {
		writeError(w, 500, "Cannot read saved firewall remarks")
		return
	}
	existing := make(map[string]bool, len(state.Rules))
	for _, rule := range state.Rules {
		existing[profileRuleKey(rule)] = true
	}
	for _, next := range added {
		if existing[profileRuleKey(next)] {
			writeError(w, 409, "A replacement rule already exists. Edit its row separately.")
			return
		}
		if backend == "firewalld" {
			exists, checkErr := firewalldProfileRuleExists(next, true, run)
			if checkErr != nil {
				writeError(w, 502, "Cannot verify the saved firewall configuration.")
				return
			}
			if exists {
				writeError(w, 409, "A replacement rule already exists in the saved configuration.")
				return
			}
		}
	}
	// A saved guard remains independently owned so normal profile reapply
	// cannot accidentally broaden Cloudflare-only access.
	editCtx, finish := context.WithTimeout(context.WithoutCancel(r.Context()), firewallListTimeout(len(added)))
	defer finish()
	previousNotes, _ := json.Marshal(notes)
	baseApplied, notesSaved := false, false
	err = func() error {
		if err := guardChange.prepare(editCtx); err != nil {
			return err
		}
		if mutatesFirewall {
			run = func(cmd string, args ...string) (string, error) {
				return runFirewallCommand(editCtx, 20*time.Second, cmd, args...)
			}
			rollback := func(added []profileRule, restoreOld bool) error {
				return restoreProfileRules(backend, *old, added, restoreOld)
			}
			if err := replaceProfileRules(backend, *old, added, removeOld, run, rollback); err != nil {
				return err
			}
			baseApplied = true
			state, err = inspectFirewallProfile(backend, run)
			present := make(map[string]bool, len(state.Rules))
			for _, rule := range state.Rules {
				present[profileRuleKey(rule)] = true
			}
			verified := err == nil && present[profileRuleKey(*old)] != removeOld
			for _, next := range nextRules {
				verified = verified && present[profileRuleKey(next)]
			}
			if !verified {
				return fmt.Errorf("replacement rules could not be verified")
			}
		}
		for _, next := range nextRules {
			notes[profileRuleKey(next)] = input.Remarks
		}
		data, _ := json.Marshal(notes)
		if err := writeAtomic(filepath.Join(a.system.dataDir, "firewall-profile-remarks.json"), data, 0600); err != nil {
			return fmt.Errorf("could not save firewall remarks: %w", err)
		}
		notesSaved = true
		return guardChange.commit(editCtx)
	}()
	if err != nil {
		var restoreBase func() error
		if baseApplied {
			restoreBase = func() error { return restoreProfileRules(backend, *old, added, removeOld) }
		}
		recoveryErr := guardChange.rollbackWithProfile(restoreBase)
		if notesSaved {
			recoveryErr = errors.Join(recoveryErr, writeAtomic(filepath.Join(a.system.dataDir, "firewall-profile-remarks.json"), previousNotes, 0600))
		}
		if recoveryErr != nil {
			writeError(w, 502, fmt.Sprintf("Rule update failed (%v); recovery was incomplete (%v). Check the host firewall.", err, recoveryErr))
			return
		}
		writeError(w, 502, "Rule update failed; previous settings restored: "+err.Error())
		return
	}
	a.annotateFirewallProfile(&state)
	_ = a.store.AddActivity("Firewall profile rule edited", fmt.Sprintf("%s to %s (%d address rules)", old.Port, input.NewPort, len(nextRules)), "warning")
	writeJSON(w, 200, state)
}

func restoreProfileRules(backend string, old profileRule, added []profileRule, restoreOld bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), firewallListTimeout(len(added)))
	defer cancel()
	run := func(cmd string, args ...string) (string, error) {
		return runFirewallCommand(ctx, 20*time.Second, cmd, args...)
	}
	return restoreProfileRulesWithProbe(backend, old, added, restoreOld, run)
}

func restoreProfileRulesWithProbe(backend string, old profileRule, added []profileRule, restoreOld bool, run securityProbe) error {
	if restoreOld {
		if backend == "ufw" {
			// Reapply the original selector idempotently. A failed dual-stack
			// removal can leave only one family, which status deduplicates.
			if _, err := run("ufw", profileUFWArgs(old)...); err != nil {
				return err
			}
			state, err := inspectFirewallProfile(backend, run)
			if err != nil {
				return err
			}
			if !profileContainsRule(state, old) {
				return fmt.Errorf("original rule recovery could not be verified")
			}
		} else {
			if _, err := addVerifiedProfileRule(backend, old, run); err != nil {
				return err
			}
		}
	}
	return removeAddedProfileRules(backend, added, run)
}
