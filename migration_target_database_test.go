package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationTargetDatabaseSelection(t *testing.T) {
	databases := []ManagedDatabase{
		{ID: "primary", SiteID: "site", Name: "primary_db", Username: "primary_user"},
		{ID: "secondary", SiteID: "site", Name: "selected_db", Username: "selected_user"},
		{ID: "foreign", SiteID: "other", Name: "other_db", Username: "other_user"},
	}
	for _, tc := range []struct {
		name, id  string
		databases []ManagedDatabase
		want      string
		wantError bool
	}{
		{name: "explicit secondary overrides primary", id: "secondary", databases: databases, want: "secondary"},
		{name: "explicit primary", id: "primary", databases: databases, want: "primary"},
		{name: "foreign target rejected", id: "foreign", databases: databases, wantError: true},
		{name: "missing target rejected", id: "missing", databases: databases, wantError: true},
		{name: "ambiguous legacy request rejected", databases: databases, wantError: true},
		{name: "single legacy target retained", databases: []ManagedDatabase{databases[1], databases[2]}, want: "secondary"},
		{name: "no linked database", databases: databases[2:], wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, err := migrationTargetDatabase(tc.databases, "site", tc.id)
			if tc.wantError {
				if err == nil || target != nil {
					t.Fatalf("unsafe target accepted: %+v %v", target, err)
				}
				return
			}
			if err != nil || target == nil || target.ID != tc.want {
				t.Fatalf("selected target %+v, error %v; want %s", target, err, tc.want)
			}
			original := target.Name
			target.Name = "fixture-only-change"
			for _, item := range tc.databases {
				if item.ID == tc.want && item.Name != original {
					t.Fatal("selection exposed mutable stored database")
				}
			}
		})
	}
}

func TestMigrationDatabaseValidationPrecedesFileStaging(t *testing.T) {
	databases := []ManagedDatabase{
		{ID: "primary", SiteID: "site", Name: "primary_db", Username: "primary_user"},
		{ID: "secondary", SiteID: "site", Name: "selected_db", Username: "selected_user"},
		{ID: "foreign", SiteID: "other", Name: "other_db", Username: "other_user"},
	}
	for _, tc := range []struct {
		name, target, password, sql string
		wantStatus                  int
		wantMessage                 string
	}{
		{name: "foreign target", target: "foreign", password: "current-passphrase", sql: "database.sql", wantStatus: 400, wantMessage: "linked to this website"},
		{name: "missing target", target: "missing", password: "current-passphrase", sql: "database.sql", wantStatus: 400, wantMessage: "linked to this website"},
		{name: "ambiguous legacy target", password: "current-passphrase", sql: "database.sql", wantStatus: 400, wantMessage: "multiple databases"},
		{name: "explicit secondary accepted with UI field casing", target: "secondary", sql: "database.sql", wantStatus: 409, wantMessage: "Destination website must be empty"},
		{name: "files only ignores stale database selection", target: "foreign", wantStatus: 409, wantMessage: "Destination website must be empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := OpenStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			data := t.TempDir()
			stage := filepath.Join(data, "migrations", "fixturestage")
			if err := os.MkdirAll(stage, 0700); err != nil {
				t.Fatal(err)
			}
			manifest, _ := json.Marshal(MigrationPreview{ID: "fixturestage", Roots: []string{"public_html"}, Databases: []string{"database.sql"}})
			if err := os.WriteFile(filepath.Join(stage, "manifest.json"), manifest, 0600); err != nil {
				t.Fatal(err)
			}
			home := t.TempDir()
			root := filepath.Join(home, "public_html")
			if err := os.Mkdir(root, 0750); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(root, "existing.txt")
			if err := os.WriteFile(marker, []byte("preserve existing website"), 0600); err != nil {
				t.Fatal(err)
			}
			store.cfg.Sites = []Site{{ID: "site", Domain: "target.example.test", Root: root}}
			store.cfg.Databases = databases
			app := &App{store: store, system: &System{dataDir: data, demo: true}}
			body, _ := json.Marshal(map[string]string{"id": "fixturestage", "siteId": "site", "root": "public_html", "sql": tc.sql, "databaseID": tc.target, "password": tc.password})
			response := httptest.NewRecorder()
			probeCalls := 0
			app.migrationImportWithSpaceProbe(response, httptest.NewRequest("POST", "/api/migrations/import", bytes.NewReader(body)), func(context.Context, string) (int64, error) { probeCalls++; return 1 << 40, nil })
			if response.Code != tc.wantStatus || !strings.Contains(response.Body.String(), tc.wantMessage) {
				t.Fatalf("HTTP %d %s", response.Code, response.Body.String())
			}
			if probeCalls != 0 {
				t.Fatal("invalid database request reached file-staging space check")
			}
			if contents, err := os.ReadFile(marker); err != nil || string(contents) != "preserve existing website" {
				t.Fatal("existing website changed", err)
			}
			entries, err := os.ReadDir(home)
			if err != nil || len(entries) != 1 || entries[0].Name() != "public_html" {
				t.Fatal("invalid selection created staging files", err)
			}
			if len(store.cfg.Databases) != len(databases) || store.cfg.Databases[1].Name != "selected_db" {
				t.Fatal("database metadata changed")
			}
		})
	}
}

func TestMigrationLaravelCore(t *testing.T) {
	home := t.TempDir()
	public := filepath.Join(home, "public_html")
	core := filepath.Join(home, "email-marketing-app")
	for _, dir := range []string{public, filepath.Join(core, "vendor"), filepath.Join(core, "bootstrap")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(public, "index.php"), []byte("<?php is_file(__DIR__.'/../email-marketing-app/vendor/autoload.php'); require 'bootstrap/app.php';"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := migrationLaravelCore("cpanel", public); err == nil {
		t.Fatal("missing Laravel core was accepted")
	}
	for _, name := range []string{"vendor/autoload.php", "bootstrap/app.php"} {
		if err := os.WriteFile(filepath.Join(core, filepath.FromSlash(name)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := migrationLaravelCore("cpanel", public); err != nil || got != core {
		t.Fatalf("got %q, %v", got, err)
	}
}
