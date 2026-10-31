package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebmailSetupLogTail(t *testing.T) {
	file := filepath.Join(t.TempDir(), "setup.log")
	if err := os.WriteFile(file, []byte(strings.Repeat("old line\n", 100)+"password=private\nInstall failed: missing plugin\n"), 0600); err != nil {
		t.Fatal(err)
	}
	lines := webmailSetupLog(file)
	if len(lines) != 60 {
		t.Fatalf("got %d lines", len(lines))
	}
	text := strings.Join(lines, "\n")
	if strings.Contains(text, "private") {
		t.Fatal("sensitive output exposed")
	}
	if !strings.HasSuffix(text, "Install failed: missing plugin") {
		t.Fatal("failure lost")
	}
}
func TestWebmailSetupLogMissing(t *testing.T) {
	if lines := webmailSetupLog(filepath.Join(t.TempDir(), "absent")); len(lines) != 0 {
		t.Fatal(lines)
	}
}
func TestWebmailSetupLogBounds(t *testing.T) {
	file := filepath.Join(t.TempDir(), "setup.log")
	os.WriteFile(file, []byte(strings.Repeat("x", 80000)), 0600)
	lines := webmailSetupLog(file)
	if len(lines) != 1 || len(lines[0]) > 1010 {
		t.Fatal("unbounded log output")
	}
}
