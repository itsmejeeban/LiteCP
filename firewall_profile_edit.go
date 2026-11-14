package main

import (
	"context"
	"encoding/json"
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
	return rule.Direction + "|" + rule.Zone + "|" + rule.Port
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

func firewalldProfileRuleExists(rule profileRule, permanent bool, run securityProbe) (bool, error) {
	args := []string{"--zone=" + rule.Zone, "--list-rich-rules"}
	value := rule.Raw
	if rule.Direction == "outbound" {
		args = []string{"--policy=litecp-required", "--list-ports"}
		value = rule.Port
	}
	if permanent {
		args = append([]string{"--permanent"}, args...)
	}
	output, err := run("firewall-cmd", args...)
	if err != nil {
		return false, err
	}
	if rule.Direction == "outbound" {
		return containsString(strings.Fields(output), value), nil
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == value {
			return true, nil
		}
	}
	return false, nil
}

func addProfileRule(backend string, rule profileRule, run securityProbe) error {
	if err := validateProfilePort(rule.Port); err != nil {
		return err
	}
	parts := strings.Split(rule.Port, "/")
	if backend == "ufw" {
		direction := "in"
		if rule.Direction == "outbound" {
			direction = "out"
		}
		_, err := run("ufw", "insert", "1", "allow", direction, "to", "any", "port", strings.ReplaceAll(parts[0], "-", ":"), "proto", parts[1], "comment", "litecp-required")
		return err
	}
	if backend != "firewalld" {
		return fmt.Errorf("unsupported firewall backend")
	}
	args := []string{"--zone=" + rule.Zone, "--add-rich-rule=" + rule.Raw}
	if rule.Direction == "outbound" {
		args = []string{"--policy=litecp-required", "--add-port=" + rule.Port}
	}
	if _, err := run("firewall-cmd", append([]string{"--permanent"}, args...)...); err != nil {
		return err
	}
	if _, err := run("firewall-cmd", args...); err != nil {
		_ = removeProfileRule(backend, rule, run)
		return err
	}
	return nil
}

func replaceProfileRule(backend string, old, next profileRule, run securityProbe) error {
	if err := addProfileRule(backend, next, run); err != nil {
		return err
	}
	if backend == "firewalld" {
		for _, permanent := range []bool{false, true} {
			exists, err := firewalldProfileRuleExists(next, permanent, run)
			if err != nil || !exists {
				cleanupErr := removeProfileRule(backend, next, run)
				return fmt.Errorf("replacement rule could not be verified. Previous rule retained (check: %v, cleanup: %v)", err, cleanupErr)
			}
		}
	} else {
		state, err := inspectFirewallProfile(backend, run)
		if err != nil {
			return fmt.Errorf("replacement rule could not be verified. Previous rule retained: %w", err)
		}
		if !profileContainsRule(state, next) {
			return fmt.Errorf("replacement rule is not active. Previous rule retained")
		}
	}
	if err := removeProfileRule(backend, old, run); err != nil {
		restoreErr := addProfileRule(backend, old, run)
		var cleanupErr error
		if restoreErr == nil {
			cleanupErr = removeProfileRule(backend, next, run)
		}
		if restoreErr != nil || cleanupErr != nil {
			return fmt.Errorf("rule edit failed (%v) and rollback was incomplete (restore: %v, cleanup: %v). Check the host firewall", err, restoreErr, cleanupErr)
		}
		return fmt.Errorf("rule edit failed. Previous rule restored: %w", err)
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
		Port      string `json:"port"`
		Direction string `json:"direction"`
		Zone      string `json:"zone"`
		NewPort   string `json:"newPort"`
		Remarks   string `json:"remarks"`
	}
	if decodeJSONWithLimit(r, &input, 8<<10) != nil || validateProfilePort(input.NewPort) != nil || utf8.RuneCountInString(input.Remarks) > 500 || strings.ContainsRune(input.Remarks, 0) {
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
		if v.Port == input.Port && v.Direction == input.Direction && v.Zone == input.Zone {
			old = v
			break
		}
	}
	if old == nil {
		writeError(w, 404, "Rule changed. Refresh and try again")
		return
	}
	next := *old
	next.Port = input.NewPort
	if old.Direction == "inbound" && next.Port != old.Port {
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
			if profileRuleHasPort(old.Port, port) {
				writeError(w, 409, "Change the SSH or panel service port before replacing its firewall rule")
				return
			}
		}
	}
	if next.Port != old.Port && profileContainsRule(state, next) {
		writeError(w, 409, "The replacement rule already exists")
		return
	}
	notes, err := a.readProfileRemarks()
	if err != nil {
		writeError(w, 500, "Cannot read saved firewall remarks")
		return
	}
	if next.Direction == "inbound" && backend == "firewalld" {
		p := strings.Split(next.Port, "/")
		next.Raw = fmt.Sprintf("rule priority=\"-100\" port port=\"%s\" protocol=\"%s\" accept", p[0], p[1])
	}
	if next.Port != old.Port && backend == "firewalld" {
		exists, checkErr := firewalldProfileRuleExists(next, true, run)
		if checkErr != nil {
			writeError(w, 502, "Cannot verify the saved firewall configuration.")
			return
		}
		if exists {
			writeError(w, 409, "The replacement rule already exists in the saved configuration.")
			return
		}
	}
	if next.Port != old.Port {
		// Finish and verify host mutations even if the browser disconnects.
		editCtx, finish := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Minute)
		defer finish()
		run = func(cmd string, args ...string) (string, error) {
			return runFirewallCommand(editCtx, 20*time.Second, cmd, args...)
		}
		if err = replaceProfileRule(backend, *old, next, run); err != nil {
			writeError(w, 502, err.Error())
			return
		}
		state, err = inspectFirewallProfile(backend, run)
		if err != nil || !profileContainsRule(state, next) || profileContainsRule(state, *old) {
			if rollback := restoreProfileRule(backend, *old, next); rollback != nil {
				writeError(w, 502, "Rule verification and rollback failed. Check the host firewall.")
				return
			}
			writeError(w, 502, "Rule verification failed. Previous rule restored.")
			return
		}
	}
	notes[profileRuleKey(next)] = input.Remarks
	data, _ := json.Marshal(notes)
	if err = writeAtomic(filepath.Join(a.system.dataDir, "firewall-profile-remarks.json"), data, 0600); err != nil {
		if next.Port != old.Port {
			if rollback := restoreProfileRule(backend, *old, next); rollback != nil {
				writeError(w, 500, "Saving remarks and firewall rollback failed. Check the host firewall immediately")
				return
			}
		}
		writeError(w, 500, "Could not save firewall remarks. Previous rule restored")
		return
	}
	a.annotateFirewallProfile(&state)
	_ = a.store.AddActivity("Firewall profile rule edited", old.Port+" to "+next.Port, "warning")
	writeJSON(w, 200, state)
}

func restoreProfileRule(backend string, old, next profileRule) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	run := func(cmd string, args ...string) (string, error) {
		return runFirewallCommand(ctx, 20*time.Second, cmd, args...)
	}
	if err := addProfileRule(backend, old, run); err != nil {
		return err
	}
	state, err := inspectFirewallProfile(backend, run)
	if err != nil {
		return err
	}
	if !profileContainsRule(state, old) {
		return fmt.Errorf("previous rule is not active")
	}
	if backend == "ufw" && !profileContainsRule(state, next) {
		return nil
	}
	return removeProfileRule(backend, next, run)
}
