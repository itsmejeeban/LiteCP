package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSELinuxRepairEligibility(t *testing.T) {
	for _, mode := range []string{"Permissive", "Enforcing", "Disabled", "Unknown", ""} {
		if selinuxRepairAllowed(mode) != (mode == "Permissive") {
			t.Fatalf("unsafe eligibility for %q", mode)
		}
	}
}

func TestSELinuxRepairStoredStatus(t *testing.T) {
	base := t.TempDir()
	if got := readSELinuxRepairStatus(base); got.State != "idle" {
		t.Fatal(got)
	}
	path := filepath.Join(base, "status.json")
	if err := os.WriteFile(path, []byte(`{"state":"failed","message":"check failed","job":"job-example"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := readSELinuxRepairStatus(base); got.State != "failed" || got.Job != "job-example" {
		t.Fatal(got)
	}
	if err := os.WriteFile(path, []byte(`invalid`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := readSELinuxRepairStatus(base); got.State != "error" {
		t.Fatal(got)
	}
}
