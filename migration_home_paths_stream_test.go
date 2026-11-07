package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationHomePathsLargeUnchangedSource(t *testing.T) {
	root, recovery := t.TempDir(), t.TempDir()
	large := strings.Repeat("x", (2<<20)+1)
	file := filepath.Join(root, "encoded.php")
	if err := os.WriteFile(file, []byte(large), 0600); err != nil {
		t.Fatal(err)
	}
	if err := relocateMigrationHomePaths([]string{root}, "/home/old", "/home/litecp/example.test", recovery, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != large {
		t.Fatal("unrelated large source changed", err)
	}
}

func TestMigrationHomePathStreamBoundary(t *testing.T) {
	root := t.TempDir()
	content := strings.Repeat("x", (64<<10)+4) + "/home/old/application"
	if err := os.WriteFile(filepath.Join(root, "config.php"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	found, err := migrationSourceContainsHome(root, "config.php", "/home/old")
	if err != nil || !found {
		t.Fatal("source-home path crossing a read boundary was missed", err)
	}
}

func TestMigrationHomePathsLargeRequiredRewriteNeedsReview(t *testing.T) {
	root, recovery := t.TempDir(), t.TempDir()
	content := strings.Repeat("x", (2<<20)+1) + "'/home/old/application'"
	file := filepath.Join(root, "config.php")
	if err := os.WriteFile(file, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	err := relocateMigrationHomePaths([]string{root}, "/home/old", "/home/litecp/example.test", recovery, os.Getuid(), os.Getgid())
	if err == nil || !strings.Contains(err.Error(), "config.php") {
		t.Fatal("oversized required rewrite must identify the file for review", err)
	}
	data, readErr := os.ReadFile(file)
	if readErr != nil || string(data) != content {
		t.Fatal("failed inspection modified the source", readErr)
	}
}
