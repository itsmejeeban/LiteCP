package main

import (
	"archive/zip"
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationParentConflictNamesEntry(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "source.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for _, name := range []string{"public_html/assets/icon.txt", "public_html/assets"} {
		entry, e := w.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = io.WriteString(entry, "retained"); e != nil {
			t.Fatal(e)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(root, "stage")
	if err = os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	_, err = extractMigration(context.Background(), archive, stage, "cpanel")
	if err == nil || !strings.Contains(err.Error(), "public_html/assets") {
		t.Fatal("conflicting entry was accepted or not identified", err)
	}
	data, err := os.ReadFile(filepath.Join(stage, "public_html", "assets", "icon.txt"))
	if err != nil || string(data) != "retained" {
		t.Fatal("conflict damaged an extracted file", err)
	}
}

func TestGenericMigrationMySQLDSN(t *testing.T) {
	input := `<?php $base=['url'=>'https://example.test','database'=>['dsn'=>'mysql:host=127.0.0.1;dbname=source; charset=utf8mb4','user'=>'source_user','password'=>'secret'],'oidc'=>['client_id'=>'keep']]; return $base;`
	plan, err := inspectMigrationConfig(input, "php")
	if err != nil || plan.Database != "source" {
		t.Fatalf("DSN not mapped: %v", err)
	}
	got := rewriteGenericMigrationConfig(plan, DBCredentials{Name: "target", Username: "new_user", Password: "new_secret"})
	for _, want := range []string{"mysql:host=localhost;port=3306;dbname=target;charset=utf8mb4", "'user'=>'new_user'", "'password'=>'new_secret'", "'client_id'=>'keep'"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s", want)
		}
	}
	for _, dsn := range []string{"mysql:host=external.test;dbname=source", "mysql:host=localhost;dbname=source;dbname=other", "mysql:host=localhost;dbname=source;port=3307", "mysql:unix_socket=/source.sock;dbname=source", "sqlite:/source.db"} {
		if _, err := inspectMigrationConfig(strings.Replace(input, "mysql:host=127.0.0.1;dbname=source; charset=utf8mb4", dsn, 1), "php"); err == nil {
			t.Fatalf("unsafe DSN accepted: %s", dsn)
		}
	}
}

func TestGenericMigrationFileBasedSiteKeepsUnrelatedDump(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "index.php")
	if err := os.WriteFile(file, []byte("<?php session_start(); echo 'Website';"), 0600); err != nil {
		t.Fatal(err)
	}
	plans, dbs, err := genericMigrationDatabasePlan(root, filepath.Join(root, "missing"), []string{"mysql/unused.sql"})
	if err != nil || len(plans) != 0 || dbs["unused"] == "" {
		t.Fatalf("file based site rejected or dump lost: %v", err)
	}
	for _, content := range []string{"<?php new PDO($connection);", "<?php mysqli_connect($host,$user,$password);", "DB_DATABASE=unmapped\n"} {
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err = genericMigrationDatabasePlan(root, filepath.Join(root, "missing"), []string{"mysql/unused.sql"}); err == nil {
			t.Fatal("unmapped database reference accepted")
		}
	}
}

func TestMigrationHomePathRelocation(t *testing.T) {
	input := `<?php /* '/home/old/comment' */ $root='/home/old/workspace-app'; $other='/home/older/app'; $url='https://example.test/home/old/a';`
	got := relocateMigrationHomeText(input, ".php", "/home/old", "/home/litecp/example.test")
	if !strings.Contains(got, "'/home/litecp/example.test/workspace-app'") || !strings.Contains(got, "'/home/older/app'") || !strings.Contains(got, "/* '/home/old/comment' */") {
		t.Fatal("incorrect literal path relocation")
	}
	ini := "error_log = \"/home/old/logs/error.log\"\n; disabled=/home/old/test\n"
	if result := relocateMigrationHomeText(ini, ".ini", "/home/old", "/home/litecp/example.test"); result != "error_log = \"/home/litecp/example.test/logs/error.log\"\n; disabled=/home/old/test\n" {
		t.Fatal(result)
	}
	root, recovery := t.TempDir(), t.TempDir()
	file := filepath.Join(root, "index.php")
	if err := os.WriteFile(file, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	if err := relocateMigrationHomePaths([]string{root}, "/home/old", "/home/litecp/example.test", recovery, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != got {
		t.Fatal("path repair failed", err)
	}
	originals, _ := filepath.Glob(filepath.Join(recovery, ".path-recovery-*", "*.original"))
	if len(originals) != 1 {
		t.Fatal("original not preserved")
	}
	data, err = os.ReadFile(originals[0])
	if err != nil || string(data) != input {
		t.Fatal("original changed", err)
	}
	if err = relocateMigrationHomePaths([]string{root}, "/home/old", "/home/litecp/example.test", recovery, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	after, _ := filepath.Glob(filepath.Join(recovery, ".path-recovery-*", "*.original"))
	if len(after) != 1 {
		t.Fatal("retry created duplicate repairs")
	}
}

func TestHealthRetryRejectsUnrelatedWebsite(t *testing.T) {
	a := &App{system: &System{dataDir: t.TempDir()}, store: &Store{}}
	id := "abcdefghijklmnop1234"
	root, _ := a.liveJobRoot(id)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	job := LiveMigrationJob{ID: id, Status: "restored-review", NativeReady: true, Source: LiveSource{Platform: "cpanel"}}
	if err := a.saveLiveJob(&job); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	a.autoMigrationAction(r, httptest.NewRequest("POST", "/", strings.NewReader(`{"id":"`+id+`","background":true}`)))
	if r.Code != 409 {
		t.Fatalf("unrelated health retry accepted: %d", r.Code)
	}
}

func TestHealthRetryQueuesOnlyCommittedWebsite(t *testing.T) {
	a := &App{system: &System{dataDir: t.TempDir()}, store: &Store{cfg: Config{Sites: []Site{{ID: "original-site", Domain: "example.test"}}}}}
	id := "abcdefghijklmnop1234"
	root, _ := a.liveJobRoot(id)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	job := LiveMigrationJob{ID: id, Status: "restored-review", NativeReady: true, Source: LiveSource{Platform: "cpanel"}, Account: "source", Accounts: []LiveAccount{{ID: "source", Domain: "example.test"}}, CPanelWebsiteID: "original-site"}
	if err := a.saveLiveJob(&job); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	a.autoMigrationAction(r, httptest.NewRequest("POST", "/", strings.NewReader(`{"id":"`+id+`","background":true}`)))
	if r.Code != 202 {
		t.Fatalf("committed health retry rejected: %d %s", r.Code, r.Body.String())
	}
	saved, err := a.loadLiveJob(id)
	if err != nil || saved.Status != "review" || saved.CPanelWebsiteID != "original-site" {
		t.Fatal("health retry lost committed-site marker", err)
	}
}
