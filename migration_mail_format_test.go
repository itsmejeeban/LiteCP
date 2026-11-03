package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCPanelMailFormatMetadataPreservesNamedMailbox(t *testing.T) {
	root, job := cpanelServicesFixture(t)
	marker := filepath.Join(root, "cpanel_account", "mail", "mailbox_format.cpanel")
	if err := os.WriteFile(marker, []byte("maildir\n"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := planCPanelServices(root, job)
	if err != nil {
		t.Fatalf("regular mail format metadata blocked migration: %v", err)
	}
	if len(plan.Mailboxes) != 1 || plan.Mailboxes[0].Mailbox.Address != "sales@example.test" || plan.Mailboxes[0].DefaultAccount {
		t.Fatalf("format metadata changed the named mailbox plan: %+v", plan.Mailboxes)
	}
	if len(plan.Cron) != 3 {
		t.Fatalf("mail metadata changed cron discovery: %d jobs", len(plan.Cron))
	}
}

func TestCPanelMailFormatMetadataDoesNotCreateDefaultMailbox(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "cpanel_account")
	for _, directory := range []string{"etc/example.test", "mail/example.test", "mail/cur", "mail/new", "mail/tmp", "mail/.Junk/cur"} {
		if err := os.MkdirAll(filepath.Join(base, filepath.FromSlash(directory)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "format.json"), []byte(`{"version":1,"sourceHome":"/home/example"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "mail", "mailbox_format.cpanel"), []byte("maildir\n"), 0600); err != nil {
		t.Fatal(err)
	}
	job := LiveMigrationJob{ID: "cpanel_metadata_only_123", Account: "example", Source: LiveSource{Platform: "cpanel"}, Accounts: []LiveAccount{{ID: "example", Domain: "example.test"}}}
	plan, err := planCPanelServices(root, job)
	if err != nil {
		t.Fatalf("empty mail directories and regular metadata blocked migration: %v", err)
	}
	if len(plan.Mailboxes) != 0 {
		t.Fatalf("metadata created a phantom default mailbox: %+v", plan.Mailboxes)
	}
}

func TestCPanelMailFormatMetadataRejectsUnsafeOrUnsupportedEntries(t *testing.T) {
	for _, kind := range []string{"directory", "symlink", "unsupported"} {
		t.Run(kind, func(t *testing.T) {
			root, job := cpanelServicesFixture(t)
			marker := filepath.Join(root, "cpanel_account", "mail", "mailbox_format.cpanel")
			switch kind {
			case "directory":
				if err := os.Mkdir(marker, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(t.TempDir(), "format")
				if err := os.WriteFile(target, []byte("maildir\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, marker); err != nil {
					t.Skipf("symbolic links unavailable in this environment: %v", err)
				}
			case "unsupported":
				if err := os.WriteFile(marker, []byte("mdbox\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := planCPanelServices(root, job); err == nil {
				t.Fatalf("%s mail format metadata was accepted", kind)
			}
		})
	}
}

func TestCPanelMailFormatMetadataStillRejectsOrphanMailbox(t *testing.T) {
	root, job := cpanelServicesFixture(t)
	mailRoot := filepath.Join(root, "cpanel_account", "mail")
	if err := os.WriteFile(filepath.Join(mailRoot, "mailbox_format.cpanel"), []byte("maildir\n"), 0600); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(mailRoot, "unknown.test", "orphan", "cur")
	if err := os.MkdirAll(orphan, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphan, "message"), []byte("retained orphan mail"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := planCPanelServices(root, job); err == nil {
		t.Fatal("regular metadata caused unmatched mailbox data to be silently ignored")
	}
}
