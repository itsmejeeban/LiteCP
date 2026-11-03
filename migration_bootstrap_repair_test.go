package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationLocalBootstrapRepair(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	original := "require dirname(__DIR__) . '/app/bootstrap.php';"
	if got := repairMigrationLocalBootstrap(original, root, home); got != original {
		t.Fatal("changed missing bootstrap")
	}
	if err := os.MkdirAll(filepath.Join(root, "app"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app", "bootstrap.php"), []byte("<?php"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := repairMigrationLocalBootstrap(original, root, home); got != "require __DIR__ . '/app/bootstrap.php';" {
		t.Fatal(got)
	}
	if err := os.MkdirAll(filepath.Join(home, "app"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "app", "bootstrap.php"), []byte("<?php"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := repairMigrationLocalBootstrap(original, root, home); got != original {
		t.Fatal("overrode valid private bootstrap")
	}
}
