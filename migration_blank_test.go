package main

import (
	"errors"
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

func TestMigrationCannotOmitConfiguredDatabaseDump(t *testing.T) {
	public, private := t.TempDir(), t.TempDir()
	app := filepath.Join(private, "app")
	if err := os.Mkdir(app, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, ".env"), []byte("DB_DATABASE=source_db\nDB_USERNAME=source_user\nDB_PASSWORD=example\nDB_HOST=localhost\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err := genericMigrationDatabasePlan(public, private, nil)
	if !errors.Is(err, errMigrationDatabaseDumpMissing) || err.Error() != `configuration "private_home/app/.env" has no matching database dump` {
		t.Fatalf("missing dump did not identify the staged configuration: %v", err)
	}
}
