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

func TestMigrationRecreatesEmptyDatabasesOnlyWithVerifiedOptIn(t *testing.T) {
	job := LiveMigrationJob{
		Source:                       LiveSource{Platform: "cpanel"},
		RecreateEmptyDatabases:       true,
		CPanelDatabaseInventoryEmpty: true,
	}
	if !migrationCanRecreateEmptyDatabases(job, nil) {
		t.Fatal("verified cPanel opt-in was ignored")
	}
	if migrationCanRecreateEmptyDatabases(job, []string{"mysql/account.sql"}) {
		t.Fatal("empty mode took precedence over a database dump")
	}
	job.Warnings = []string{"Some account databases could not be exported."}
	if migrationCanRecreateEmptyDatabases(job, nil) {
		t.Fatal("empty mode accepted a known export failure")
	}
	job.Warnings = nil
	job.CPanelDatabaseInventoryEmpty = false
	if migrationCanRecreateEmptyDatabases(job, nil) {
		t.Fatal("empty mode accepted an unverified inventory")
	}
	job.CPanelDatabaseInventoryEmpty = true
	job.RecreateEmptyDatabases = false
	if migrationCanRecreateEmptyDatabases(job, nil) {
		t.Fatal("empty mode was enabled without opt-in")
	}
	job.RecreateEmptyDatabases = true
	job.Source.Platform = "plesk"
	if migrationCanRecreateEmptyDatabases(job, nil) {
		t.Fatal("empty mode was enabled for a different platform")
	}
}

func TestMigrationPlansAnEmptyLocalDatabaseForActiveConfig(t *testing.T) {
	public, private := t.TempDir(), t.TempDir()
	env := "DB_CONNECTION=mysql\nDB_DATABASE=source_db\nDB_USERNAME=source_user\nDB_PASSWORD=secret\nDB_HOST=localhost\n"
	if err := os.WriteFile(filepath.Join(private, ".env"), []byte(env), 0600); err != nil {
		t.Fatal(err)
	}
	sample := `<?php return ['database'=>'example_database','username'=>'user','password'=>'secret','host'=>'localhost'];`
	if err := os.WriteFile(filepath.Join(public, "config.example.php"), []byte(sample), 0600); err != nil {
		t.Fatal(err)
	}
	plans, databases, err := planGenericMigrationDatabases(public, private, nil, true)
	dump, found := databases["source_db"]
	if err != nil || len(plans) != 1 || !plans[0].Private || len(databases) != 1 || !found || dump != "" {
		t.Fatalf("expected one empty database for the active private configuration: %v %v %v", plans, databases, err)
	}
	rewritten := rewriteGenericMigrationConfig(plans[0], DBCredentials{Name: "target_db", Username: "target_user", Password: "target_password"})
	for _, value := range []string{"target_db", "target_user", "target_password"} {
		if !strings.Contains(rewritten, value) {
			t.Fatal("missing destination credentials")
		}
	}
	for _, config := range []string{
		strings.Replace(env, "localhost", "db.example.test", 1),
		strings.Replace(env, "DB_CONNECTION=mysql", "DB_CONNECTION='pgsql'", 1),
		strings.Replace(env, "DB_CONNECTION=mysql", `export DB_CONNECTION="sqlite"`, 1),
		strings.Replace(env, "DB_CONNECTION=mysql", `DB_CONNECTION="${DB_ENGINE}"`, 1),
		env + "DB_CONNECTION=mysql\n",
		env + "DB_PORT=3307\n",
		env + "DB_SOCKET=/run/other-mysql.sock\n",
		env + "export DB_URL=mysql://remote.example.test/db\n",
		"DB_URL=mysql://remote.example.test/db\n",
	} {
		if err := os.WriteFile(filepath.Join(private, ".env"), []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := planGenericMigrationDatabases(public, private, nil, true); err == nil {
			t.Fatal("unsupported database connection was mapped to a new local database")
		}
	}
}

func TestMigrationPlansEmptyWordPressAndWHMCSLocalDatabases(t *testing.T) {
	for _, tc := range []struct {
		filename string
		content  string
	}{
		{"wp-config.php", `<?php define('DB_NAME','source'); define('DB_USER','user'); define('DB_PASSWORD','pass'); define('DB_HOST','localhost');`},
		{"configuration.php", `<?php $db_name='source'; $db_username='user'; $db_password='pass'; $db_host='localhost';`},
	} {
		t.Run(tc.filename, func(t *testing.T) {
			plan, err := planEmptyMigrationAppDatabase(tc.content, "source", tc.filename)
			if err != nil || plan.Path != tc.filename {
				t.Fatal("local literal configuration failed preflight", err)
			}
			if _, err := planEmptyMigrationAppDatabase(strings.Replace(tc.content, "localhost", "db.example.test", 1), "source", tc.filename); err == nil {
				t.Fatal("external connection was mapped as a local empty database")
			}
		})
	}
}
