package main

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCPanelBlankDocumentRootExtractsWithoutWebsiteFiles(t *testing.T) {
	for _, tc := range []struct {
		name      string
		directory string
		file      string
		content   string
		wantRoot  string
		wantSQL   string
	}{
		{name: "portable directory only", directory: "public_html", wantRoot: "public_html"},
		{name: "portable with empty schema dump", directory: "public_html", file: "mysql/account_empty.sql", content: "-- Empty database dump\n", wantRoot: "public_html", wantSQL: "mysql/account_empty.sql"},
		{name: "native directory only", directory: "cpmove-account/homedir/public_html", wantRoot: "cpmove-account/homedir/public_html"},
		{name: "nested private root remains private", directory: "public_html", file: "private_home/app/public_html/index.php", content: "<?php echo 'private';", wantRoot: "public_html"},
		{name: "private root cannot become public", directory: "private_home/homedir/public_html", file: "private_home/homedir/public_html/index.php", content: "<?php echo 'private';"},
		{name: "root must be a directory", file: "public_html", content: "not a website directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archivePath := filepath.Join(t.TempDir(), "source.tar")
			archive, err := os.Create(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			writer := tar.NewWriter(archive)
			if tc.directory != "" {
				if err := writer.WriteHeader(&tar.Header{Name: tc.directory, Mode: 0700, Typeflag: tar.TypeDir}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.file != "" {
				if err := writer.WriteHeader(&tar.Header{Name: tc.file, Mode: 0600, Typeflag: tar.TypeReg, Size: int64(len(tc.content))}); err != nil {
					t.Fatal(err)
				}
				if _, err := io.WriteString(writer, tc.content); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}

			stage := t.TempDir()
			preview, err := extractMigrationWithLimit(context.Background(), archivePath, stage, "cpanel", liveMigrationExtractLimit)
			if tc.wantRoot == "" {
				if err == nil || !strings.Contains(err.Error(), "no website root found") {
					t.Fatalf("expected missing website root, got %+v, %v", preview, err)
				}
				return
			}
			if err != nil || len(preview.Roots) != 1 || preview.Roots[0] != tc.wantRoot {
				t.Fatalf("unexpected blank site preview: %+v, %v", preview, err)
			}
			public := filepath.Join(stage, filepath.FromSlash(tc.wantRoot))
			if !blankMigrationWebsite(public) {
				t.Fatal("empty document root is not recognized as blank")
			}
			if tc.wantSQL != "" {
				plans, databases, err := genericMigrationDatabasePlan(public, filepath.Join(stage, "private_home"), preview.Databases)
				if err != nil || len(plans) != 0 || len(databases) != 1 || databases["account_empty"] != tc.wantSQL {
					t.Fatalf("blank site's empty schema did not reach restore plan: %v, %v, %v", plans, databases, err)
				}
			} else if len(preview.Databases) != 0 {
				t.Fatalf("unexpected database in preview: %+v", preview)
			}
		})
	}
}

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
