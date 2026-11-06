package main

import "testing"

func TestPHPHealthSocketUsesDedicatedRuntime(t *testing.T) {
	site := Site{ID: "site-immutable"}
	if got := phpHealthSocket("shared.sock", site, resourceRuntimeStatus{}); got != "shared.sock" {
		t.Fatal(got)
	}
	site.ResourceLimits = &WebsiteResourceLimits{Enabled: true}
	for _, state := range []string{"Active", "Recovering", "Error", "Disabled"} {
		status := resourceRuntimeStatus{resourcePolicyState: resourcePolicyState{State: state}, Enforced: true}
		if got := phpHealthSocket("shared.sock", site, status); got != resourceSocket(site.ID, "php") {
			t.Fatal(state, got)
		}
	}
	blocked := resourceRuntimeStatus{resourcePolicyState: resourcePolicyState{State: "Resource blocked"}, Enforced: true}
	if got := phpHealthSocket("shared.sock", site, blocked); got != "" {
		t.Fatal(got)
	}
	blocked.Enforced = false
	if got := phpHealthSocket("shared.sock", site, blocked); got == "" {
		t.Fatal("unverified blocking must not hide missing worker")
	}
	site.Suspended = true
	if got := phpHealthSocket("shared.sock", site, resourceRuntimeStatus{}); got != "" {
		t.Fatal("manual suspension reported as missing worker")
	}
	site.Suspended = false
	site.ResourceLimits.Enabled = false
	if got := phpHealthSocket("shared.sock", site, blocked); got != "shared.sock" {
		t.Fatal(got)
	}
}
