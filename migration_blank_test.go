package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBlankWebsiteMigrationWithDatabase(t *testing.T) {
	public := t.TempDir()
	private := filepath.Join(public, "missing-private")
	for _, name := range []string{".htaccess", "php.ini", ".user.ini"} {
		if err := os.WriteFile(filepath.Join(public, name), []byte("settings"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	plans, databases, err := genericMigrationDatabasePlan(public, private, []string{"mysql/unused.sql"})
	if err != nil || len(plans) != 0 || databases["unused"] != "mysql/unused.sql" {
		t.Fatalf("blank site rejected or dump lost: %v %v %v", plans, databases, err)
	}
	if err := os.WriteFile(filepath.Join(public, "index.php"), []byte("<?php require 'custom.php';"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := genericMigrationDatabasePlan(public, private, []string{"mysql/unused.sql"}); err == nil {
		t.Fatal("unrecognized application was accepted as blank")
	}
}
