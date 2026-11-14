package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func firewalldCustomRule(rule FirewallRule, address string) (kind, value string) {
	if rule.Direction == "inbound" && rule.Action == "allow" && address == "any" {
		// Retain compatibility with unscoped rules from earlier releases.
		return "port", fmt.Sprintf("%d/%s", rule.Port, rule.Protocol)
	}
	action := "accept"
	if rule.Action == "deny" {
		action = "reject"
	}
	prefix := "rule"
	if address != "any" {
		family := "ipv4"
		if strings.Contains(address, ":") {
			family = "ipv6"
		}
		term := "source"
		if rule.Direction == "outbound" {
			term = "destination"
		}
		prefix += fmt.Sprintf(` family="%s" %s address="%s"`, family, term, address)
	}
	return "rich-rule", fmt.Sprintf(`%s port port="%d" protocol="%s" %s`, prefix, rule.Port, rule.Protocol, action)
}

func firewallCustomScope(rule FirewallRule, run securityProbe) ([]string, error) {
	if rule.Direction == "inbound" {
		return []string{"--zone=public"}, nil
	}
	const policy = "litecp-outbound"
	scope := "--policy=" + policy
	policies, err := run("firewall-cmd", "--get-policies")
	if err != nil {
		return nil, err
	}
	savedPolicies, err := run("firewall-cmd", "--permanent", "--get-policies")
	if err != nil {
		return nil, err
	}
	if !containsString(strings.Fields(savedPolicies), policy) {
		commands := [][]string{
			{"--new-policy=" + policy},
			{scope, "--add-ingress-zone=HOST"},
			{scope, "--add-egress-zone=ANY"},
			{scope, "--set-priority=-500"},
		}
		for _, args := range commands {
			if _, err := run("firewall-cmd", append([]string{"--permanent"}, args...)...); err != nil {
				return nil, err
			}
		}
	}
	if !containsString(strings.Fields(policies), policy) {
		// firewalld creates policies in permanent configuration. Load the
		// policy before applying its rules to both configurations directly.
		if _, err := run("firewall-cmd", "--reload"); err != nil {
			return nil, err
		}
	}
	return []string{scope}, nil
}

func applyFirewallAddressRules(ctx context.Context, backend string, rule FirewallRule, addresses []string, remove bool, run func(context.Context, time.Duration, string, ...string) (string, error)) error {
	scope := []string(nil)
	if backend == "firewalld" {
		var err error
		scope, err = firewallCustomScope(rule, func(cmd string, args ...string) (string, error) {
			return run(ctx, 20*time.Second, cmd, args...)
		})
		if err != nil {
			return err
		}
	}
	var undo []func(context.Context) error
	rollback := func(cause error) error {
		recovery, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var rollbackErr error
		for i := len(undo) - 1; i >= 0; i-- {
			rollbackErr = errors.Join(rollbackErr, undo[i](recovery))
		}
		if rollbackErr != nil {
			return fmt.Errorf("firewall update failed (%v) and rollback was incomplete (%v). Check the host firewall", cause, rollbackErr)
		}
		return cause
	}
	for _, address := range addresses {
		if backend == "firewalld" {
			kind, value := firewalldCustomRule(rule, address)
			for _, target := range [][]string{append([]string{"--permanent"}, scope...), scope} {
				query := append(append([]string{}, target...), "--query-"+kind+"="+value)
				output, err := run(ctx, 20*time.Second, "firewall-cmd", query...)
				if !(output == "yes" && err == nil) && output != "no" {
					return rollback(fmt.Errorf("cannot verify firewall member %s: %v", address, err))
				}
				if output == "no" && ctx.Err() != nil {
					return rollback(ctx.Err())
				}
				if !remove && output == "yes" {
					return rollback(fmt.Errorf("a matching host firewall rule already exists for %s", address))
				}
				if remove && output == "no" {
					continue
				}
				change, inverse := "--add-", "--remove-"
				if remove {
					change, inverse = inverse, change
				}
				command := append(append([]string{}, target...), change+kind+"="+value)
				reverse := append(append([]string{}, target...), inverse+kind+"="+value)
				_, err = run(ctx, 20*time.Second, "firewall-cmd", command...)
				if err != nil {
					return rollback(err)
				}
				undo = append(undo, func(recovery context.Context) error {
					_, restoreErr := run(recovery, 20*time.Second, "firewall-cmd", reverse...)
					return restoreErr
				})
			}
			continue
		}
		args := ufwFirewallRuleArgs(rule, address, remove)
		if !remove {
			// Dry-run insertion detects an existing selector without replacing
			// its action or comment. Append only new rules to retain ordering.
			probe := append([]string{"--dry-run", "prepend"}, args...)
			output, err := run(ctx, 30*time.Second, "ufw", probe...)
			if err != nil {
				return rollback(err)
			}
			if ufwFirewallRuleExists(output) {
				return rollback(fmt.Errorf("a matching host firewall rule already exists for %s", address))
			}
		}
		output, err := run(ctx, 30*time.Second, "ufw", args...)
		if err != nil {
			return rollback(err)
		}
		if !remove && ufwFirewallRuleExists(output) {
			return rollback(fmt.Errorf("a matching host firewall rule already exists for %s", address))
		}
		if strings.Contains(output, "Could not delete non-existent rule") {
			continue
		}
		reverse := ufwFirewallRuleArgs(rule, address, !remove)
		undo = append(undo, func(recovery context.Context) error {
			_, restoreErr := run(recovery, 30*time.Second, "ufw", reverse...)
			return restoreErr
		})
	}
	return nil
}

func ufwFirewallRuleExists(output string) bool {
	return strings.Contains(output, "Skipping inserting existing rule") ||
		strings.Contains(output, "Skipping adding existing rule")
}
