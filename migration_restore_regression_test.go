package main

import (
	"archive/tar"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationConfigCRLFAndScopedPHP(t *testing.T) {
	env := "DB_CONNECTION=mysql\r\nDB_DATABASE=account_db\r\nDB_USERNAME=account_user\r\nDB_PASSWORD=secret\r\nDB_HOST=localhost\r\n"
	php := `<?php return ['db'=>['host'=>'localhost','name'=>'account_db','user'=>'account_user','pass'=>'secret'], 'oidc'=>['host'=>'identity.example','name'=>'oauth','user'=>'unrelated','password'=>'keep']];`
	for kind, content := range map[string]string{"env": env, "php": php} {
		plan, err := inspectMigrationConfig(content, kind)
		if err != nil || plan.Database != "account_db" {
			t.Fatalf("%s parse: %s, %v", kind, plan.Database, err)
		}
		result := rewriteGenericMigrationConfig(plan, DBCredentials{Name: "target_db", Username: "target_user", Password: "new_secret"})
		if !strings.Contains(result, "target_db") || !strings.Contains(result, "new_secret") {
			t.Fatal("credentials not rewritten")
		}
		if kind == "php" && !strings.Contains(result, "'password'=>'keep'") {
			t.Fatal("unrelated configuration changed")
		}
	}
	for _, content := range []string{
		`<?php return ['db'=>['name'=>'one'],'database'=>['name'=>'two']];`,
		`<?php return ['db'=>['name'=>'account_db'.suffix(),'host'=>'localhost','user'=>'u','pass'=>'p']];`,
	} {
		if _, err := inspectMigrationConfig(content, "php"); err == nil {
			t.Fatal("ambiguous or dynamic config accepted")
		}
	}
}

func TestMigrationDotenvLiteralDollarAndDynamicReference(t *testing.T) {
	base := "DB_DATABASE=account_db\nDB_USERNAME=account_user\nDB_HOST=localhost\nDB_PASSWORD="
	for _, value := range []string{`"literal$part"`, `'literal${part}'`} {
		plan, err := inspectMigrationConfig(base+value+"\n", "env")
		if err != nil || plan.Database != "account_db" || plan.Fields["password"] == nil {
			t.Fatalf("literal dotenv password rejected: %v", err)
		}
	}
	for _, value := range []string{`"${SECRET}"`, `${SECRET}`} {
		if _, err := inspectMigrationConfig(base+value+"\n", "env"); err == nil {
			t.Fatal("dynamic dotenv password accepted")
		}
	}
}

func TestMigrationIgnoresSampleEnvForFileBasedSite(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".env.example", ".env.sample"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("DB_DATABASE=sample\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if !migrationHasNoDatabaseReferences(root) {
		t.Fatal("sample configuration blocked a file-based site")
	}
	if err := os.WriteFile(filepath.Join(root, ".env.local"), []byte("DB_DATABASE=active\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if migrationHasNoDatabaseReferences(root) {
		t.Fatal("active environment configuration was ignored")
	}
}

func TestMigrationRepeatedDirectoriesAndDuplicateFiles(t *testing.T) {
	for _, duplicateFile := range []bool{false, true} {
		dir := t.TempDir()
		archive := filepath.Join(dir, "input.tar")
		f, err := os.Create(archive)
		if err != nil {
			t.Fatal(err)
		}
		w := tar.NewWriter(f)
		for i := 0; i < 3; i++ {
			if err = w.WriteHeader(&tar.Header{Name: "public_html/", Typeflag: tar.TypeDir, Mode: 0750}); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 1+boolInt(duplicateFile); i++ {
			if err = w.WriteHeader(&tar.Header{Name: "public_html/index.php", Typeflag: tar.TypeReg, Mode: 0640, Size: 1}); err != nil {
				t.Fatal(err)
			}
			if _, err = w.Write([]byte("x")); err != nil {
				t.Fatal(err)
			}
		}
		if err = w.Close(); err != nil {
			t.Fatal(err)
		}
		f.Close()
		root := filepath.Join(dir, "output")
		os.Mkdir(root, 0700)
		preview, err := extractMigrationWithLimit(context.Background(), archive, root, "cpanel", liveMigrationExtractLimit)
		if duplicateFile {
			if err == nil {
				t.Fatal("duplicate regular file accepted")
			}
		} else if err != nil || preview.Files != 1 {
			t.Fatalf("repeated directory failed: %+v %v", preview, err)
		}
	}
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func TestCPanelEmptyDefaultFoldersAreNotMailboxes(t *testing.T) {
	root, job := cpanelServicesFixture(t)
	for _, name := range []string{".Junk", ".Trash", ".Drafts", ".Sent"} {
		for _, child := range []string{"cur", "new", "tmp"} {
			if err := os.MkdirAll(filepath.Join(root, "cpanel_account", "mail", name, child), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	plan, err := planCPanelServices(root, job)
	if err != nil || len(plan.Mailboxes) != 1 {
		t.Fatalf("empty default folders blocked import: %+v %v", plan, err)
	}
	job.Accounts = []LiveAccount{{ID: job.Account, Domain: "example.test"}}
	message := filepath.Join(root, "cpanel_account", "mail", ".Junk", "cur", "message")
	if err = os.WriteFile(message, []byte("default mail"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err = planCPanelServices(root, job)
	if err != nil || len(plan.Mailboxes) != 2 || !plan.Mailboxes[1].DefaultAccount {
		t.Fatalf("default account mail not preserved: %+v %v", plan, err)
	}
}

func TestCPanelDefaultMailCopyAndOrphanGuard(t *testing.T) {
	root, job := cpanelServicesFixture(t)
	job.Accounts = []LiveAccount{{ID: job.Account, Domain: "example.test"}}
	mailRoot := filepath.Join(root, "cpanel_account", "mail")
	if err := os.MkdirAll(filepath.Join(mailRoot, ".Junk", "cur"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mailRoot, ".Junk", "cur", "default-message"), []byte("preserved"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := planCPanelServices(root, job)
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err = copyCPanelMaildirFiltered(context.Background(), mailRoot, target, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "Maildir", ".Junk", "cur", "default-message"))
	if err != nil || string(data) != "preserved" {
		t.Fatal("default mail missing", err)
	}
	if _, err = os.Stat(filepath.Join(target, "Maildir", "example.test")); !os.IsNotExist(err) {
		t.Fatal("named mailbox copied into default account")
	}
	if len(plan.Mailboxes) != 2 || plan.Mailboxes[1].Mailbox.PasswordHash != "{CRYPT}*" {
		t.Fatal("default account must remain locked until password reset")
	}
	if err = os.MkdirAll(filepath.Join(mailRoot, "unknown.test", "orphan", "cur"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = planCPanelServices(root, job); err == nil {
		t.Fatal("orphan mailbox silently dropped")
	}
}
