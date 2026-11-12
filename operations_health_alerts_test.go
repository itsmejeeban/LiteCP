package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOperationsResourceAlertsRetainBriefBreaches(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	site := Site{ID: "audit", Domain: "audit.example.com", ResourceLimits: &WebsiteResourceLimits{Enabled: true}}
	history := []any{
		map[string]any{"event": "block", "reason": "CPU", "at": now.Add(-5 * time.Minute).Format(time.RFC3339Nano)},
		map[string]any{"event": "process limit reached", "at": now.Add(-2 * time.Minute).Format(time.RFC3339Nano)},
		map[string]any{"event": "block", "reason": "Disk", "at": now.Add(-25 * time.Hour).Format(time.RFC3339Nano)},
		map[string]any{"event": "block", "reason": "Memory", "at": now.Add(time.Hour).Format(time.RFC3339Nano)},
	}
	alerts := operationsResourceAlerts(site, resourceRuntimeStatus{resourcePolicyState: resourcePolicyState{State: "Active"}}, history, now)
	if len(alerts) != 2 || alerts[0].ID != "resource-audit" || alerts[1].ID != "resource-process-audit" {
		t.Fatalf("recovered CPU or process breach missing: %+v", alerts)
	}
	if !strings.Contains(alerts[0].Detail, "cpu") || strings.Contains(alerts[0].Detail, "disk") {
		t.Fatalf("stale or future history replaced the recent breach: %+v", alerts[0])
	}
	site.ResourceLimits.Enabled = false
	if alerts := operationsResourceAlerts(site, resourceRuntimeStatus{}, history, now); len(alerts) != 0 {
		t.Fatalf("disabled policy generated resource alerts: %+v", alerts)
	}
}

func TestOperationsResourceAlertsClassifyRuntimeFailure(t *testing.T) {
	site := Site{ID: "audit", Domain: "audit.example.com", ResourceLimits: &WebsiteResourceLimits{Enabled: true}}
	status := resourceRuntimeStatus{resourcePolicyState: resourcePolicyState{State: "Resource blocked", Reason: "Monitor error"}, Error: "private command arguments must not leave the server"}
	alerts := operationsResourceAlerts(site, status, nil, time.Now())
	if len(alerts) != 1 || alerts[0].Category != "resource-enforcement" || strings.Contains(alerts[0].Detail, "private command") {
		t.Fatalf("runtime failure classified incorrectly or leaked command detail: %+v", alerts)
	}
}

func TestOperationsIntegrityAlerts(t *testing.T) {
	if alerts := operationsIntegrityAlerts(integrityState{Changes: []integrityChange{{Path: "private"}}}, nil); len(alerts) != 0 {
		t.Fatalf("disabled integrity monitor generated alerts: %+v", alerts)
	}
	state := integrityState{Enabled: true, Error: "private path", Changes: []integrityChange{{Path: "/private/key"}}}
	alerts := operationsIntegrityAlerts(state, nil)
	if len(alerts) != 2 {
		t.Fatalf("expected error and change alerts: %+v", alerts)
	}
	for _, alert := range alerts {
		if alert.Category != "file-integrity" || strings.Contains(alert.Detail, "private") {
			t.Fatalf("integrity alert leaked internal detail: %+v", alert)
		}
	}
	if alerts := operationsIntegrityAlerts(integrityState{}, errors.New("private read error")); len(alerts) != 1 || alerts[0].ID != "integrity-check" {
		t.Fatalf("unreadable integrity state was ignored: %+v", alerts)
	}
}
