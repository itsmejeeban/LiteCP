package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// The profile still owns its host allow rule. A separately owned early guard
// enforces Cloudflare-only access without taking over another rule's selector.
func (a *App) profileCloudflareGuard(rule profileRule) *FirewallRule {
	key := profileRuleKey(rule)
	for _, guard := range a.store.PublicState().FirewallRules {
		if guard.ProfileKey == key && guard.Source == "cloudflare" {
			copy := guard
			return &copy
		}
	}
	return nil
}

type profileGuardChange struct {
	old, next                 *FirewallRule
	apply                     func(context.Context, FirewallRule, bool) error
	save                      func(*FirewallRule, *FirewallRule) error
	installed, removed, saved bool
}

func sameProfileGuard(a, b *FirewallRule) bool {
	return a != nil && b != nil && a.Port == b.Port && a.Protocol == b.Protocol
}

func (a *App) planProfileCloudflare(old, next profileRule, enabled bool, remarks string) (*profileGuardChange, error) {
	change := &profileGuardChange{old: a.profileCloudflareGuard(old), apply: a.system.ApplyFirewallRule, save: a.store.replaceProfileGuard}
	if !enabled {
		return change, nil
	}
	first, last, protocol, err := parseProfilePort(next.Port)
	if err != nil || first != last || next.Direction != "inbound" {
		return nil, fmt.Errorf("Cloudflare-only access requires one inbound port, not a port range")
	}
	guard := FirewallRule{Direction: "inbound", Action: "allow", Port: first, Protocol: protocol, Source: "cloudflare", Label: "Cloudflare: " + next.Port, Remarks: remarks, ProfileKey: profileRuleKey(next), CreatedAt: time.Now().UTC()}
	guard.ID, err = randomToken(8)
	if err != nil {
		return nil, err
	}
	if change.old != nil {
		guard.ID, guard.CreatedAt = change.old.ID, change.old.CreatedAt
	}
	if err := validateFirewallRule(guard); err != nil {
		return nil, err
	}
	for _, current := range a.store.PublicState().FirewallRules {
		if current.ID != guard.ID && current.Source == "cloudflare" && current.Port == first && current.Protocol == protocol {
			return nil, fmt.Errorf("a Cloudflare restriction already manages port %s; edit its existing managed rule instead", next.Port)
		}
	}
	change.next = &guard
	return change, nil
}

func (change *profileGuardChange) prepare(ctx context.Context) error {
	if change.next == nil {
		return nil
	}
	if err := change.apply(ctx, *change.next, false); err != nil {
		return err
	}
	change.installed = !sameProfileGuard(change.old, change.next)
	return nil
}

func (change *profileGuardChange) commit(ctx context.Context) error {
	if change.old != nil && !sameProfileGuard(change.old, change.next) {
		// Mark before removal: a failure can leave a partially removed guard.
		change.removed = true
		if err := change.apply(ctx, *change.old, true); err != nil {
			return err
		}
	}
	if change.old == nil && change.next == nil {
		return nil
	}
	if err := change.save(change.old, change.next); err != nil {
		return err
	}
	change.saved = true
	return nil
}

func (change *profileGuardChange) rollback() error {
	return change.rollbackWithProfile(nil)
}

func (change *profileGuardChange) rollbackWithProfile(restoreProfile func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var failure error
	if change.removed && change.old != nil {
		if err := change.apply(ctx, *change.old, false); err != nil {
			return err
		}
	}
	if restoreProfile != nil {
		if err := restoreProfile(); err != nil {
			// Retain a new guard if removing it could expose a broad allow
			// selector whose previous address scope was not recovered.
			return err
		}
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
	}
	if change.installed && change.next != nil {
		failure = errors.Join(failure, change.apply(ctx, *change.next, true))
	}
	if change.saved {
		failure = errors.Join(failure, change.save(change.next, change.old))
	}
	return failure
}

func (s *Store) replaceProfileGuard(old, next *FirewallRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := append([]FirewallRule(nil), s.cfg.FirewallRules...)
	index := -1
	if old != nil {
		for i, rule := range s.cfg.FirewallRules {
			if rule.ID == old.ID {
				if rule != *old {
					return fmt.Errorf("Cloudflare restriction changed; refresh before editing")
				}
				index = i
				break
			}
		}
		if index == -1 {
			return fmt.Errorf("Cloudflare restriction changed; refresh before editing")
		}
	}
	if next != nil {
		if index == -1 {
			s.cfg.FirewallRules = append(s.cfg.FirewallRules, *next)
		} else {
			s.cfg.FirewallRules[index] = *next
		}
	} else if index >= 0 {
		s.cfg.FirewallRules = append(s.cfg.FirewallRules[:index], s.cfg.FirewallRules[index+1:]...)
	}
	if err := s.saveLocked(); err != nil {
		s.cfg.FirewallRules = previous
		return err
	}
	return nil
}

func (a *App) removeProfileCloudflare(ctx context.Context, rules []profileRule) error {
	var completed []*profileGuardChange
	for _, rule := range rules {
		guard := a.profileCloudflareGuard(rule)
		if guard == nil {
			continue
		}
		change := profileGuardChange{old: guard, apply: a.system.ApplyFirewallRule, save: a.store.replaceProfileGuard}
		if err := change.commit(ctx); err != nil {
			err = errors.Join(err, change.rollback())
			for i := len(completed) - 1; i >= 0; i-- {
				err = errors.Join(err, completed[i].rollback())
			}
			return err
		}
		completed = append(completed, &change)
	}
	return nil
}
