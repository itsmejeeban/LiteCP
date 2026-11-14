package main

import (
	"context"
	"errors"
	"testing"
)

func TestFirewallProfileCloudflareOwnershipAndSwitchBack(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, system: &System{demo: true}}
	base := newProfileRule(profileRule{Direction: "inbound", Zone: "public"}, "443/tcp", "")
	change, err := app.planProfileCloudflare(base, base, true, "Proxied HTTPS")
	if err != nil {
		t.Fatal(err)
	}
	active := false
	apply := func(_ context.Context, rule FirewallRule, remove bool) error {
		if rule.Source != "cloudflare" || rule.ProfileKey != profileRuleKey(base) || cloudflareProfileOnly(rule) != "1" {
			t.Fatal("not an independently owned profile guard", rule)
		}
		active = !remove
		return nil
	}
	change.apply = apply
	if err := change.prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := change.commit(context.Background()); err != nil || !active || app.profileCloudflareGuard(base) == nil {
		t.Fatal("guard not persisted for refresh", active, err)
	}
	state := profileState{Rules: []profileRule{base}}
	app.annotateFirewallProfile(&state)
	if state.Rules[0].SourceMode != "cloudflare" || state.Rules[0].Address != "" {
		t.Fatal("annotation changed selector identity", state)
	}
	back, err := app.planProfileCloudflare(base, base, false, "")
	if err != nil {
		t.Fatal(err)
	}
	back.apply = apply
	if err := back.prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := back.commit(context.Background()); err != nil || active || app.profileCloudflareGuard(base) != nil {
		t.Fatal("linked guard was not removed", active, err)
	}
}

func TestFirewallProfileCloudflareRejectsUnrelatedGuardAndRanges(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, system: &System{demo: true}}
	base := newProfileRule(profileRule{Direction: "inbound", Zone: "public"}, "443/tcp", "")
	if err := store.AddFirewallRule(FirewallRule{ID: "custom", Direction: "inbound", Action: "allow", Port: 443, Protocol: "tcp", Source: "cloudflare"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.planProfileCloudflare(base, base, true, ""); err == nil {
		t.Fatal("took over an unrelated managed Cloudflare guard")
	}
	for _, next := range []profileRule{newProfileRule(base, "400-500/tcp", ""), newProfileRule(profileRule{Direction: "outbound"}, "443/tcp", "")} {
		if _, err := app.planProfileCloudflare(base, next, true, ""); err == nil {
			t.Fatal("accepted unsupported Cloudflare range/direction", next)
		}
	}
	change, err := app.planProfileCloudflare(base, base, false, "")
	if err != nil || change.old != nil || change.next != nil {
		t.Fatal("custom mode would remove an unrelated guard", change, err)
	}
}

func TestFirewallProfileCloudflareFailedSaveRestoresOriginalGuard(t *testing.T) {
	old := &FirewallRule{ID: "old", Port: 443, Protocol: "tcp", Source: "cloudflare", ProfileKey: "inbound|public|443/tcp"}
	next := &FirewallRule{ID: "old", Port: 8443, Protocol: "tcp", Source: "cloudflare", ProfileKey: "inbound|public|8443/tcp"}
	active := map[int]bool{443: true}
	change := profileGuardChange{old: old, next: next,
		apply: func(ctx context.Context, rule FirewallRule, remove bool) error {
			if ctx.Err() != nil {
				t.Fatal("recovery used a canceled context")
			}
			active[rule.Port] = !remove
			return nil
		},
		save: func(*FirewallRule, *FirewallRule) error { return errors.New("disk full") },
	}
	if err := change.prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := change.commit(context.Background()); err == nil {
		t.Fatal("save failure hidden")
	}
	if err := change.rollback(); err != nil || !active[443] || active[8443] {
		t.Fatal("old restriction was not restored", active, err)
	}
}

func TestFirewallProfileCloudflareRollbackKeepsGuardUntilBaseRecovered(t *testing.T) {
	next := &FirewallRule{Port: 443, Protocol: "tcp", Source: "cloudflare", ProfileKey: "inbound|public|443/tcp"}
	active := true
	change := profileGuardChange{next: next, installed: true,
		apply: func(_ context.Context, _ FirewallRule, remove bool) error { active = !remove; return nil },
	}
	if err := change.rollbackWithProfile(func() error {
		if !active {
			t.Fatal("guard removed while replacement broad allow remained active")
		}
		return errors.New("profile recovery failed")
	}); err == nil || !active {
		t.Fatal("must retain protection when address scope cannot be restored", active, err)
	}
	if err := change.rollbackWithProfile(func() error {
		if !active {
			t.Fatal("guard removed before profile recovery")
		}
		return nil
	}); err != nil || active {
		t.Fatal("guard cleanup did not follow successful profile recovery", active, err)
	}
}
