package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResourcePHPCacheUsesWebsiteMemoryBudget(t *testing.T) {
	for _, tc := range []struct {
		memory          int64
		cache, interned string
	}{
		{128, "16", "4"}, {256, "16", "4"}, {257, "32", "4"}, {512, "32", "4"}, {513, "64", "8"}, {8192, "64", "8"},
	} {
		args := strings.Join(resourcePHPCacheArguments(WebsiteResourceLimits{MemoryMiB: tc.memory}), " ")
		if !strings.Contains(args, "opcache.memory_consumption="+tc.cache+" ") || !strings.Contains(args, "opcache.interned_strings_buffer="+tc.interned+" ") {
			t.Fatalf("wrong cache budget for %d MiB: %s", tc.memory, args)
		}
		for _, forbidden := range []string{"memory_limit=", "pm.max_children", "opcache.enable="} {
			if strings.Contains(args, forbidden) {
				t.Fatalf("cache tuning changed an unrelated setting: %s", args)
			}
		}
	}
}

func TestResourceSiteReaderInvalidatesAtomicSameSizeReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := []byte(`{"sites":[{"id":"abc","domain":"one.example","suspended":false}]}`)
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	var reader resourceSiteReader
	site, err := reader.read(dir, "abc")
	if err != nil || site.Domain != "one.example" {
		t.Fatal(site, err)
	}
	info := reader.info
	replacement := filepath.Join(dir, "replacement")
	if err := os.WriteFile(replacement, []byte(`{"sites":[{"id":"abc","domain":"two.example","suspended":false}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(replacement, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	site, err = reader.read(dir, "abc")
	if err != nil || site.Domain != "two.example" {
		t.Fatal("cached obsolete config", site, err)
	}
	if err := os.WriteFile(path, []byte("broken config"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = reader.read(dir, "abc"); err == nil {
		t.Fatal("invalid config silently used stale state")
	}
}

func TestResourceWatchWriterPersistsPolicyBeforeTelemetryInterval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	var writer resourceWatchWriter
	st := resourceRuntimeStatus{resourcePolicyState: resourcePolicyState{State: "Active"}, Enforced: true, MemoryBytes: 1}
	if err := writer.saveAt(path, st); err != nil {
		t.Fatal(err)
	}
	st.MemoryBytes = 2
	if err := writer.saveAt(path, st); err != nil {
		t.Fatal(err)
	}
	read := func() resourceRuntimeStatus {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var result resourceRuntimeStatus
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if read().MemoryBytes != 1 {
		t.Fatal("unchanged policy wrote telemetry too frequently")
	}
	st.State, st.Reason = "Resource blocked", "CPU"
	st.NextRecovery = time.Now().Add(5 * time.Minute)
	if err := writer.saveAt(path, st); err != nil {
		t.Fatal(err)
	}
	if got := read(); got.State != "Resource blocked" || got.Reason != "CPU" || got.MemoryBytes != 2 {
		t.Fatal("blocking transition was delayed", got)
	}
	writer.at = time.Now().Add(-4 * time.Second)
	st.MemoryBytes = 3
	if err := writer.saveAt(path, st); err != nil {
		t.Fatal(err)
	}
	if got := read(); got.MemoryBytes != 3 || time.Since(got.UpdatedAt) > time.Second {
		t.Fatal("live heartbeat did not refresh", got)
	}
}
