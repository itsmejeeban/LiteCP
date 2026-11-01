package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenericMigrationLaravelEnvironment(t *testing.T) {
	input := "APP_NAME=Example\nDB_CONNECTION=mysql\nDB_HOST=127.0.0.1\nDB_PORT=3306\nDB_DATABASE=source_db\nDB_USERNAME=source_user\nDB_PASSWORD=\"source#secret\"\nDB_SOCKET=/old/socket\n"
	plan, err := inspectMigrationConfig(input, "env")
	if err != nil || plan.Database != "source_db" {
		t.Fatal(plan.Database, err)
	}
	result := rewriteGenericMigrationConfig(plan, DBCredentials{Name: "fresh_db", Username: "fresh_user", Password: "random!pass"})
	for _, want := range []string{`DB_DATABASE="fresh_db"`, `DB_USERNAME="fresh_user"`, `DB_PASSWORD="random!pass"`, `DB_HOST="localhost"`, `DB_PORT="3306"`, `DB_SOCKET=""`, `APP_NAME=Example`} {
		if !strings.Contains(result, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(result, "source#secret") {
		t.Fatal("source password retained in active config")
	}
}
func TestGenericMigrationLiteralPHPConfigurations(t *testing.T) {
	for _, input := range []string{
		`<?php $db_name='source';$db_user='u';$db_pass='p';$db_host='localhost';`,
		`<?php return ['database'=>'source','username'=>'u','password'=>'p','host'=>'localhost'];`,
		`<?php define('DB_NAME','source');define('DB_USER','u');define('DB_PASSWORD','p');define('DB_HOST','localhost');`,
		`<?php $db['default']['database']='source';$db['default']['username']='u';$db['default']['password']='p';$db['default']['hostname']='localhost';`,
	} {
		plan, err := inspectMigrationConfig(input, "php")
		if err != nil || plan.Database != "source" {
			t.Fatalf("%s: %v", input, err)
		}
		got := rewriteGenericMigrationConfig(plan, DBCredentials{Name: "target", Username: "newuser", Password: "quote'$value"})
		if !strings.Contains(got, "target") || strings.Contains(got, "'source'") {
			t.Fatal("rewrite failed")
		}
	}
}
func TestGenericMigrationRejectsAmbiguousOrExternalConfig(t *testing.T) {
	for _, input := range []string{
		"DB_DATABASE=a\nDB_DATABASE=b\nDB_USERNAME=u\nDB_PASSWORD=p\nDB_HOST=localhost\n",
		"DB_DATABASE=a\nDB_USERNAME=u\nDB_PASSWORD=p\nDB_HOST=external.example\n",
		"DB_DATABASE=a\nDB_USERNAME=u\nDB_PASSWORD=\"${SECRET}\"\nDB_HOST=localhost\n",
		"DB_URL=mysql://user:password@host/db\n",
	} {
		if _, err := inspectMigrationConfig(input, "env"); err == nil {
			t.Fatal("unsafe mapping accepted")
		}
	}
}
func TestGenericMigrationPrivateLaravelAndCachePreserved(t *testing.T) {
	root := t.TempDir()
	public := filepath.Join(root, "public_html")
	private := filepath.Join(root, "private_home")
	app := filepath.Join(private, "laravel-app")
	for _, dir := range []string{public, filepath.Join(app, "bootstrap", "cache")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	env := "DB_DATABASE=source\nDB_USERNAME=user\nDB_PASSWORD=secret\nDB_HOST=localhost\n"
	for path, content := range map[string]string{filepath.Join(app, ".env"): env, filepath.Join(app, "artisan"): "<?php", filepath.Join(app, "bootstrap", "cache", "config.php"): "source cache"} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	plans, dbs, err := genericMigrationDatabasePlan(public, private, []string{"mysql/source.sql", "mysql/extra.sql"})
	if err != nil || len(plans) != 1 || !plans[0].Private || len(dbs) != 2 {
		t.Fatal(plans, err)
	}
	if err = preserveLaravelConfigCache(private, plans, true); err != nil {
		t.Fatal(err)
	}
	cached, err := filepath.Glob(filepath.Join(root, ".laravel-cache-recovery-*", "config.php"))
	if err != nil || len(cached) != 1 {
		t.Fatal("source cache not preserved", err)
	}
	data, err := os.ReadFile(cached[0])
	if err != nil || string(data) != "source cache" {
		t.Fatal("source cache changed")
	}
	if _, err = os.Stat(filepath.Join(app, "bootstrap", "cache", "config.php")); !os.IsNotExist(err) {
		t.Fatal("stale active cache retained")
	}
}
func TestMigrationArchiveSeparatorsAndSQLClassification(t *testing.T) {
	got, err := safeMigrationArchiveName(`public_html/wp-content/upgrade/plugin/includes\Products\Parser.php`)
	if err != nil || got != "public_html/wp-content/upgrade/plugin/includes/Products/Parser.php" {
		t.Fatal(got, err)
	}
	for _, name := range []string{`..\escape`, `public_html\..\..\escape`, `C:\secret`, `\\server\share`} {
		if _, err = safeMigrationArchiveName(name); err == nil {
			t.Fatalf("unsafe separator path accepted %q", name)
		}
	}
	for _, name := range []string{"public_html/schema.sql", "public_html/vendor/databases/sample.sql", "private_home/app/schema.sql"} {
		if isMigrationDatabaseDump(name, "cpanel") {
			t.Fatal("application SQL treated as a database")
		}
	}
	if !isMigrationDatabaseDump("backup/mysql/site.sql", "cpanel") {
		t.Fatal("database dump missing")
	}
}
func TestMigrationArchiveInspectionReportsErrorsAndCancellation(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "site.tar.gz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	if err = tw.WriteHeader(&tar.Header{Name: "public_html/index.php", Mode: 0600, Size: 4}); err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(tw, "test")
	_ = tw.Close()
	_ = gz.Close()
	_ = file.Close()
	n, err := migrationArchiveBytes(context.Background(), archive)
	if err != nil || n != 8192 {
		t.Fatal(n, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = migrationArchiveBytes(ctx, archive); err == nil {
		t.Fatal("cancelled inspection accepted")
	}
	if err = os.WriteFile(filepath.Join(root, "bad.tar"), []byte("broken archive"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = migrationArchiveBytes(context.Background(), filepath.Join(root, "bad.tar")); err == nil || !strings.Contains(err.Error(), "archive") {
		t.Fatal("inspection cause missing")
	}
}

func TestMigrationConfigDoesNotRewriteCodeInsideStrings(t *testing.T) {
	input := `<?php $example="\$db_name='source';\$db_user='u';\$db_pass='p';\$db_host='localhost';";`
	plan, err := inspectMigrationConfig(input, "php")
	if err != nil || plan.Database != "" {
		t.Fatal("string contents interpreted as active configuration", err)
	}
}

func TestMigrationNormalizedArchiveDestinations(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		root := t.TempDir()
		archive := filepath.Join(root, "site.zip")
		file, err := os.Create(archive)
		if err != nil {
			t.Fatal(err)
		}
		writer := zip.NewWriter(file)
		names := []string{`public_html/plugin/includes\Parser.php`}
		if duplicate {
			names = append(names, "public_html/plugin/includes/Parser.php")
		}
		for _, name := range names {
			entry, e := writer.Create(name)
			if e != nil {
				t.Fatal(e)
			}
			_, _ = io.WriteString(entry, "test")
		}
		_ = writer.Close()
		_ = file.Close()
		stage := filepath.Join(root, "stage")
		if err = os.Mkdir(stage, 0700); err != nil {
			t.Fatal(err)
		}
		_, err = extractMigration(context.Background(), archive, stage, "cpanel")
		if duplicate {
			if err == nil || !strings.Contains(err.Error(), "duplicate") {
				t.Fatal("normalized collision accepted", err)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			content, e := os.ReadFile(filepath.Join(stage, "public_html", "plugin", "includes", "Parser.php"))
			if e != nil || string(content) != "test" {
				t.Fatal("normalized file not preserved", e)
			}
		}
	}
}

func TestMigrationConfigEmptyDatabaseURLAllowed(t *testing.T) {
	input := "DB_URL=\"\"\nDB_DATABASE=source\nDB_USERNAME=user\nDB_PASSWORD=password\nDB_HOST=localhost\n"
	plan, err := inspectMigrationConfig(input, "env")
	if err != nil || plan.Database != "source" {
		t.Fatal("empty URL blocked normal Laravel connection", err)
	}
}
