package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFirewallAddressListValidationAndCanonicalForm(t *testing.T) {
	got, err := parseFirewallAddresses("192.0.2.8, 192.0.2.200/24\n2001:db8::1 192.0.2.8/32")
	if err != nil || !reflect.DeepEqual(got, []string{"192.0.2.0/24", "192.0.2.8", "2001:db8::1"}) {
		t.Fatal(got, err)
	}
	for _, value := range []string{"any,192.0.2.1", "cloudflare\n192.0.2.1", "--bad", "192.0.2.999", "192.0.2.1/99", "fe80::1%eth0", strings.Repeat("192.0.2.1,", 33)} {
		if _, err := parseFirewallAddresses(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	rule := normalizeFirewallRule(FirewallRule{Port: 65001, Protocol: "tcp", Source: "192.0.2.1,2001:db8::/64"})
	if err := validateFirewallRule(rule); err != nil {
		t.Fatal(err)
	}
	rule.Port, rule.Source = 22, "0.0.0.0/0"
	if validateFirewallRule(rule) == nil {
		t.Fatal("unscoped SSH protection was bypassed")
	}
}

func TestFirewallAddressEditKeepsUnchangedMembers(t *testing.T) {
	old := normalizeFirewallRule(FirewallRule{Port: 65001, Protocol: "tcp", Source: "192.0.2.1,192.0.2.2"})
	next := canonicalFirewallRule(withFirewallAddresses(old, "192.0.2.2,2001:db8::1"))
	removed, added := firewallRuleChanges(old, next)
	if removed == nil || removed.Source != "192.0.2.1" || added == nil || added.Source != "2001:db8::1" {
		t.Fatal(removed, added)
	}
	store := &Store{cfg: Config{FirewallRules: []FirewallRule{old}}}
	if !store.HasFirewallRule(next) {
		t.Fatal("shared member was accepted as a second managed rule")
	}
	next.ID, old.ID = "same", "same"
	store.cfg.FirewallRules[0] = old
	if store.HasFirewallRule(next) {
		t.Fatal("an edit conflicts with its own original members")
	}
}

func TestCustomFirewallAddressListCreateAndEdit(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, system: &System{demo: true}}
	response := httptest.NewRecorder()
	app.createFirewallRule(response, httptest.NewRequest("POST", "/api/firewall", strings.NewReader(`{"port":65001,"protocol":"tcp","source":"192.0.2.2,192.0.2.1\n2001:db8::1","label":"Test addresses"}`)))
	if response.Code != 201 || len(store.PublicState().FirewallRules) != 1 {
		t.Fatal(response.Code, response.Body.String())
	}
	rule := store.PublicState().FirewallRules[0]
	if rule.Source != "192.0.2.1,192.0.2.2,2001:db8::1" {
		t.Fatal(rule)
	}
	response = httptest.NewRecorder()
	app.firewallRuleAction(response, httptest.NewRequest("PUT", "/api/firewall/"+rule.ID, strings.NewReader(`{"direction":"outbound","action":"deny","port":65001,"protocol":"udp","destination":"192.0.2.3,2001:db8::/64"}`)))
	updated := store.FirewallRuleByID(rule.ID)
	if response.Code != 200 || updated.Source != "" || updated.Destination != "192.0.2.3,2001:db8::/64" {
		t.Fatal(response.Code, response.Body.String(), updated)
	}
}

func TestFirewallMultipleAddressesRollbackPartialFailure(t *testing.T) {
	rule := normalizeFirewallRule(FirewallRule{Direction: "inbound", Port: 65001, Protocol: "tcp", Source: "192.0.2.1,2001:db8::1"})
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "add", true: "remove"}[remove], func(t *testing.T) {
			saved, active := map[string]bool{}, map[string]bool{}
			if remove {
				for _, address := range strings.Split(rule.Source, ",") {
					_, value := firewalldCustomRule(rule, address)
					saved[value], active[value] = true, true
				}
			}
			failed := false
			run := func(_ context.Context, _ time.Duration, cmd string, args ...string) (string, error) {
				if cmd != "firewall-cmd" {
					t.Fatal(cmd)
				}
				rules := active
				if args[0] == "--permanent" {
					rules = saved
				}
				verb, value, _ := strings.Cut(args[len(args)-1], "=")
				if strings.HasPrefix(verb, "--query-") {
					if rules[value] {
						return "yes", nil
					}
					return "no", errors.New("exit status 1")
				}
				if strings.Contains(value, "2001:db8::1") && args[0] != "--permanent" && !failed {
					failed = true
					return "", errors.New("runtime operation failed")
				}
				if strings.HasPrefix(verb, "--add-") {
					rules[value] = true
				} else if strings.HasPrefix(verb, "--remove-") {
					delete(rules, value)
				} else {
					t.Fatal(args)
				}
				return "", nil
			}
			err := applyFirewallAddressRules(context.Background(), "firewalld", rule, strings.Split(rule.Source, ","), remove, run)
			want := 0
			if remove {
				want = 2
			}
			if err == nil || len(saved) != want || len(active) != want {
				t.Fatalf("partial update left behind: %v saved=%v active=%v", err, saved, active)
			}
		})
	}
}

func TestFirewallUFWAddressListDoesNotOwnExistingRules(t *testing.T) {
	for _, addresses := range [][]string{{"any"}, {"192.0.2.1", "2001:db8::1"}} {
		t.Run(strings.Join(addresses, ","), func(t *testing.T) {
			rule := normalizeFirewallRule(FirewallRule{Port: 65001, Protocol: "tcp", Source: strings.Join(addresses, ",")})
			var changes [][]string
			probes := 0
			run := func(_ context.Context, _ time.Duration, command string, args ...string) (string, error) {
				if command != "ufw" {
					t.Fatal(command)
				}
				if args[0] == "--dry-run" {
					probes++
					if probes == len(addresses) {
						return "Skipping inserting existing rule", nil
					}
					return "Rules updated", nil
				}
				changes = append(changes, append([]string{}, args...))
				return "Rules updated", nil
			}
			if err := applyFirewallAddressRules(context.Background(), "ufw", rule, addresses, false, run); err == nil {
				t.Fatal("existing rule was taken over")
			}
			if len(addresses) == 1 {
				if len(changes) != 0 {
					t.Fatal("modified an existing unscoped rule", changes)
				}
			} else if len(changes) != 2 || strings.Join(changes[0], " ") != "allow from 192.0.2.1 to any port 65001 proto tcp" || strings.Join(changes[1], " ") != "--force delete allow from 192.0.2.1 to any port 65001 proto tcp" {
				t.Fatal("did not roll back only the newly owned member", changes)
			}
		})
	}
}

func TestFirewallUFWMultipleDestinationsRollbackAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rule := normalizeFirewallRule(FirewallRule{Direction: "outbound", Action: "deny", Port: 65001, Protocol: "udp"})
	var changes [][]string
	run := func(current context.Context, _ time.Duration, command string, args ...string) (string, error) {
		if err := current.Err(); err != nil {
			return "", err
		}
		if args[0] == "--dry-run" {
			return "Rules updated", nil
		}
		changes = append(changes, append([]string{}, args...))
		if strings.Contains(strings.Join(args, " "), "2001:db8::1") {
			cancel()
			return "", context.Canceled
		}
		return "Rules updated", nil
	}
	err := applyFirewallAddressRules(ctx, "ufw", rule, []string{"192.0.2.1", "2001:db8::1"}, false, run)
	if !errors.Is(err, context.Canceled) || len(changes) != 3 || strings.Join(changes[2], " ") != "--force delete deny out to 192.0.2.1 port 65001 proto udp" {
		t.Fatal("rollback did not compensate after request cancellation", err, changes)
	}
}
