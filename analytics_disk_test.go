package main

import (
	"path/filepath"
	"testing"
)

func TestSafeDiskUsagePath(t *testing.T) {
	if target, relative, err := safeDiskUsagePath("/", "var/log"); err != nil || target != "/var/log" || relative != "var/log" {
		t.Fatalf("server root navigation failed: %q %q %v", target, relative, err)
	}
	base := filepath.Clean("/home/litecp")
	target, relative, err := safeDiskUsagePath(base, "example.com/public_html")
	if err != nil {
		t.Fatalf("expected managed path to be accepted: %v", err)
	}
	if target != filepath.Join(base, "example.com", "public_html") || relative != filepath.Join("example.com", "public_html") {
		t.Fatalf("unexpected path resolution: target=%q relative=%q", target, relative)
	}

	for _, candidate := range []string{"../etc", "example.com/../../../etc", "/etc"} {
		if _, _, err := safeDiskUsagePath(base, candidate); err == nil {
			t.Fatalf("expected unsafe path %q to be rejected", candidate)
		}
	}
}
