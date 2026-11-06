package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotRebuildsMissingWAFRateZones(t *testing.T) {
	dir := t.TempDir()
	policyPath, httpPath := filepath.Join(dir, "policies.json"), filepath.Join(dir, "http.conf")
	local := defaultWAFPolicy()
	local.Mode = "block"
	local.RateLimits = []WAFRateLimit{{ID: "login", Name: "Login", Enabled: true, PathPrefix: "/login", Requests: 30, Burst: 2}}
	policies := WAFPolicies{Server: defaultWAFPolicy(), Websites: map[string]WAFPolicy{"site": local}}
	data, err := json.Marshal(policies)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(policyPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = reconcileSnapshotWAFHTTP(policyPath, httpPath); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(httpPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(actual), "zone="+wafRateName("site", "login")+":1m rate=30r/m;") {
		t.Fatal("restored site limiter has no shared zone")
	}
}

func TestSnapshotInvalidWAFPolicyPreservesExistingInclude(t *testing.T) {
	dir := t.TempDir()
	policyPath, httpPath := filepath.Join(dir, "policies.json"), filepath.Join(dir, "http.conf")
	if err := os.WriteFile(policyPath, []byte(`{"server":{"mode":"invalid"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(httpPath, []byte("existing configuration"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := reconcileSnapshotWAFHTTP(policyPath, httpPath); err == nil {
		t.Fatal("malformed policy accepted")
	}
	data, err := os.ReadFile(httpPath)
	if err != nil || string(data) != "existing configuration" {
		t.Fatal("existing configuration changed")
	}
}
