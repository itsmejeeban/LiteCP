package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cpanelServicesFixture(t *testing.T) (string, LiveMigrationJob) {
	t.Helper()
	root := t.TempDir()
	write := func(name, value string) {
		t.Helper()
		name = filepath.Join(root, "cpanel_account", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("format.json", `{"version":1,"sourceHome":"/home/example"}`)
	write("etc/example.test/shadow", "sales:$6$salt$"+strings.Repeat("A", 86)+":0:0:99999:7:::\n")
	write("etc/example.test/quota", "sales:2097152\n")
	write("mail/example.test/sales/cur/message", "Subject: preserved\n\nMail body")
	write("cron/example", "MAILTO=owner@example.test\nAPP_ENV=production\n*/5 * * * * /opt/cpanel/ea-php84/root/usr/bin/php /home/example/public_html/task.php\n@daily\techo report\n@reboot echo review\n")
	return root, LiveMigrationJob{ID: "cpanel_fixture_123456", Account: "example", Source: LiveSource{Platform: "cpanel"}}
}

func TestCPanelServicesPlansMailAndCronWithoutExecutingSource(t *testing.T) {
	root, job := cpanelServicesFixture(t)
	plan, err := planCPanelServices(root, job)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Mailboxes) != 1 || plan.Mailboxes[0].Mailbox.QuotaMB != 2 || len(plan.Cron) != 3 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if !strings.HasPrefix(plan.Mailboxes[0].Mailbox.PasswordHash, "{SHA512-CRYPT}$6$") {
		t.Fatal("password hash not preserved")
	}
	if !plan.Cron[0].Enabled || plan.Cron[1].Schedule != "0 0 * * *" || plan.Cron[2].Enabled {
		t.Fatal("cron compatibility policy not applied")
	}
	system := &System{demo: true, dataDir: t.TempDir()}
	site := Site{ID: "destination", Domain: "example.test", Root: "/home/litecp/example.test/public_html", PHPVersion: "8.4"}
	jobs := system.prepareCPanelCron(plan, site)
	if strings.Contains(jobs[0].Command, "/home/example/") || !strings.Contains(jobs[0].Command, "/home/litecp/example.test/public_html/task.php") || strings.Contains(jobs[0].Command, "/opt/cpanel/") {
		t.Fatal("source paths not translated:", jobs[0].Command)
	}
	if isMigrationDatabaseDump("cpanel_account/mail/example.test/sales/mysql.sql", "cpanel") {
		t.Fatal("mail attachment treated as database dump")
	}
	if len(plan.Warnings) < 2 {
		t.Fatal("missing unsupported cron/notification warnings")
	}
}

func TestCPanelServiceImportIsAdditiveAndRetryDoesNotReplaceMail(t *testing.T) {
	root, job := cpanelServicesFixture(t)
	plan, err := planCPanelServices(root, job)
	if err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	store, err := OpenStore(data)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, system: &System{demo: true, dataDir: data}}
	store.cfg.Settings.EmailHostingEnabled = true
	site := Site{ID: "cpanel_target", Domain: "example.test", Root: filepath.Join(data, "website"), PHPVersion: "8.4", RuntimeUID: 1000, RuntimeGID: 1000}
	if err = os.MkdirAll(site.Root, 0750); err != nil {
		t.Fatal(err)
	}
	if err = app.importCPanelServices(context.Background(), plan, site); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(app.system.migrationMailRoot(), "example.test", "sales", "Maildir", "cur", "message")
	content, err := os.ReadFile(target)
	if err != nil || !strings.Contains(string(content), "Mail body") {
		t.Fatal("message not imported", err)
	}
	if err = os.WriteFile(target, []byte("mail changed after import"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = app.importCPanelServices(context.Background(), plan, site); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(target)
	if err != nil || string(content) != "mail changed after import" {
		t.Fatal("retry overwrote existing mail")
	}
	_, boxes, _ := store.MailConfig()
	if len(boxes) != 1 {
		t.Fatal("duplicate mailbox on retry")
	}
	jobs, err := app.system.websiteCronJobs(site.ID)
	if err != nil || len(jobs) != 3 {
		t.Fatal("duplicate/missing cron on retry", err)
	}
	plan.Mailboxes[0].Mailbox.ID = "conflicting_other_job"
	if err = app.preflightCPanelServices(plan); err == nil {
		t.Fatal("existing mailbox conflict accepted")
	}
}

func TestCPanelServicesRejectsUnsupportedMetadataAndHonorsCancellation(t *testing.T) {
	root, job := cpanelServicesFixture(t)
	shadow := filepath.Join(root, "cpanel_account", "etc", "example.test", "shadow")
	if err := os.WriteFile(shadow, []byte("sales:!locked:\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := planCPanelServices(root, job); err == nil || strings.Contains(err.Error(), "!locked") {
		t.Fatal("unsupported hash was accepted or leaked")
	}
	if _, err := planCPanelServices(t.TempDir(), job); err == nil {
		t.Fatal("old capture without metadata accepted")
	}
	data := t.TempDir()
	store, err := OpenStore(data)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, system: &System{demo: true, dataDir: data}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = app.importCPanelServices(ctx, cpanelServicesPlan{}, Site{}); err != context.Canceled {
		t.Fatalf("ignored cancellation: %v", err)
	}
	if _, err = os.Stat(app.system.migrationMailRoot()); !os.IsNotExist(err) {
		t.Fatal("cancelled import created mail data")
	}
}

func TestCPanelCronKeepsQuotedExecutableAndDisablesUnsupportedSyntax(t *testing.T) {
	jobs, _, err := parseCPanelCron("* * * * * \"/usr/bin/php\" '/home/example/public_html/task.php'\n0 0 * * * printf 'hello%world'\n", "cron_fixture_123456")
	if err != nil {
		t.Fatal(err)
	}
	system := &System{demo: true, dataDir: t.TempDir()}
	site := Site{ID: "cron_target", Root: "/home/litecp/example.test/public_html", PHPVersion: "8.4"}
	result := system.prepareCPanelCron(cpanelServicesPlan{Cron: jobs, SourceHome: "/home/example"}, site)
	if !strings.Contains(result[0].Command, `"/opt/remi/php84/root/usr/bin/php"`) || !result[0].Enabled || result[1].Enabled {
		t.Fatalf("invalid command translation: %+v", result)
	}
}
