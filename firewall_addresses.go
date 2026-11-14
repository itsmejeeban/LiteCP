package main

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Lists have no address-count cap. Keep one administrative operation finite
// while allowing larger lists more time for host commands and compensation.
func firewallListTimeout(count int) time.Duration {
	if count >= 216 {
		return 40 * time.Minute
	}
	return 4*time.Minute + time.Duration(count)*10*time.Second
}

func parseFirewallAddresses(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "any":
		return []string{"any"}, nil
	case "cloudflare":
		return []string{"cloudflare"}, nil
	}
	values := splitFirewallAddresses(value)
	if len(values) == 0 {
		return nil, fmt.Errorf("enter IP addresses or CIDR networks, or use 'any'")
	}
	addresses := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, member := range values {
		var address string
		if ip, err := netip.ParseAddr(member); err == nil && ip.Zone() == "" {
			address = ip.String()
		} else if prefix, err := netip.ParsePrefix(member); err == nil {
			if prefix.IsSingleIP() {
				address = prefix.Addr().String()
			} else {
				address = prefix.Masked().String()
			}
		} else {
			return nil, fmt.Errorf("enter IP addresses or CIDR networks separated by commas or new lines; Any and Cloudflare must each be used alone")
		}
		if !seen[address] {
			addresses = append(addresses, address)
			seen[address] = true
		}
	}
	sort.Strings(addresses)
	return addresses, nil
}

func splitFirewallAddresses(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
}

func broadFirewallAddress(addresses []string) bool {
	for _, address := range addresses {
		if address == "any" || strings.HasSuffix(address, "/0") {
			return true
		}
	}
	return false
}

func firewallAddresses(rule FirewallRule) string {
	if rule.Direction == "outbound" {
		return rule.Destination
	}
	return rule.Source
}

func withFirewallAddresses(rule FirewallRule, value string) FirewallRule {
	if rule.Direction == "outbound" {
		rule.Destination = value
	} else {
		rule.Source = value
	}
	return rule
}

func canonicalFirewallRule(rule FirewallRule) FirewallRule {
	addresses, err := parseFirewallAddresses(firewallAddresses(rule))
	if err != nil {
		return rule
	}
	return withFirewallAddresses(rule, strings.Join(addresses, ","))
}

// A retained address already has its host rule. Leave it in place when only
// the members of an otherwise identical rule change.
func firewallRuleChanges(old, next FirewallRule) (removed, added *FirewallRule) {
	before := normalizeFirewallRule(old)
	if before.Direction != next.Direction || before.Action != next.Action || before.Protocol != next.Protocol || before.Port != next.Port {
		return &old, &next
	}
	beforeAddresses := strings.Split(firewallAddresses(canonicalFirewallRule(before)), ",")
	afterAddresses := strings.Split(firewallAddresses(next), ",")
	return firewallRuleAddressDifference(before, beforeAddresses, afterAddresses),
		firewallRuleAddressDifference(next, afterAddresses, beforeAddresses)
}

func firewallRuleAddressDifference(rule FirewallRule, values, keep []string) *FirewallRule {
	var delta []string
	retained := make(map[string]bool, len(keep))
	for _, address := range keep {
		retained[address] = true
	}
	for _, address := range values {
		if !retained[address] {
			delta = append(delta, address)
		}
	}
	if len(delta) == 0 {
		return nil
	}
	if len(values) == 1 {
		return &rule
	}
	changed := withFirewallAddresses(rule, strings.Join(delta, ","))
	return &changed
}
