package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := hashPassword("a-correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword(hash, "a-correct-horse-battery-staple") {
		t.Fatal("valid password was rejected")
	}
	if verifyPassword(hash, "a-different-password") {
		t.Fatal("invalid password was accepted")
	}
}

func TestDomainValidation(t *testing.T) {
	valid := []string{"billing.example.com", "clients.hosting-test.co.uk", "xn--bcher-kva.example"}
	invalid := []string{"example", "https://example.com", "../../etc/passwd", "example.com;reboot", "-bad.example.com"}
	for _, domain := range valid {
		if !domainPattern.MatchString(domain) {
			t.Errorf("valid domain rejected: %s", domain)
		}
	}
	for _, domain := range invalid {
		if domainPattern.MatchString(domain) {
			t.Errorf("invalid domain accepted: %s", domain)
		}
	}
}

func TestStorePersistsConfiguration(t *testing.T) {
	directory := t.TempDir()
	store, err := OpenStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize("admin", "a-correct-horse-battery-staple"); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.CheckPassword("admin", "a-correct-horse-battery-staple") {
		t.Fatal("persisted administrator credentials could not be verified")
	}
}

func TestFreshPublicStateUsesEmptyArrays(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize("admin", "a-correct-horse-battery-staple"); err != nil {
		t.Fatal(err)
	}
	state := store.PublicState()
	if state.Sites == nil || state.Backups == nil || state.BackupDestinations == nil || state.Databases == nil || state.FirewallRules == nil || state.MailDomains == nil || state.Mailboxes == nil || state.MailAliases == nil || state.Activities == nil {
		t.Fatal("fresh public state must use JSON arrays instead of null collections")
	}
}

func TestPublicStateHidesStoredSecrets(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize("admin", "a-correct-horse-battery-staple"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddBackupDestination(BackupDestination{ID: "remote", Name: "Remote", PasswordCipher: "ciphertext"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddMailbox(Mailbox{ID: "mailbox", Address: "admin@example.com", Domain: "example.com", PasswordHash: "{SHA512-CRYPT}secret"}); err != nil {
		t.Fatal(err)
	}
	state := store.PublicState()
	if state.BackupDestinations[0].PasswordCipher != "" || !state.BackupDestinations[0].HasPassword {
		t.Fatal("backup credential leaked or password flag was lost")
	}
	if state.Mailboxes[0].PasswordHash != "" {
		t.Fatal("mailbox password hash leaked through public state")
	}
}

func TestSecretEncryptionRoundTrip(t *testing.T) {
	system := &System{dataDir: t.TempDir(), demo: true}
	encrypted, err := system.EncryptSecret("a remote backup password")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, "remote") {
		t.Fatal("encrypted secret contains plaintext")
	}
	decrypted, err := system.DecryptSecret(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "a remote backup password" {
		t.Fatal("secret encryption round trip failed")
	}
}

func TestManagedPathRejectsTraversalAndEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	if _, _, err := resolveManagedPath(root, "../../etc/passwd", false); err == nil {
		t.Fatal("path traversal was accepted")
	}
	outside := t.TempDir()
	link := filepath.Join(root, "outside")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, _, err := resolveManagedPath(root, "outside/secret.txt", true); err == nil {
		t.Fatal("escaping symbolic link was accepted")
	}
}

func TestManagedZIPCreateAndExtract(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "source"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source", "hello.txt"), []byte("hello LiteCP"), 0640); err != nil {
		t.Fatal(err)
	}
	system := &System{dataDir: t.TempDir(), demo: true}
	site := Site{ID: "site", Domain: "example.com", Root: root}
	entry, err := system.ArchiveManagedPaths(context.Background(), site, []string{"source"}, "bundle.zip")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Path != "bundle.zip" || entry.Size == 0 {
		t.Fatalf("unexpected ZIP metadata: %#v", entry)
	}
	if err := os.RemoveAll(filepath.Join(root, "source")); err != nil {
		t.Fatal(err)
	}
	if err := system.ExtractManagedArchive(context.Background(), site, "bundle.zip", ""); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, "source", "hello.txt"))
	if err != nil || string(content) != "hello LiteCP" {
		t.Fatalf("ZIP round trip failed: %q, %v", content, err)
	}
}

func TestZIPExtractionRejectsZipSlip(t *testing.T) {
	root := t.TempDir()
	archive, err := os.Create(filepath.Join(root, "unsafe.zip"))
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(archive)
	member, err := writer.Create("../escaped.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = member.Write([]byte("unsafe"))
	_ = writer.Close()
	_ = archive.Close()
	system := &System{dataDir: t.TempDir(), demo: true}
	if err := system.ExtractManagedArchive(context.Background(), Site{Root: root}, "unsafe.zip", ""); err == nil {
		t.Fatal("ZIP path traversal was accepted")
	}
}

func TestZIPExtractionOverwritesTransactionallyAndRemovesBackup(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "replace.txt"), []byte("old"), 0640); err != nil {
		t.Fatal(err)
	}
	archive, err := os.Create(filepath.Join(root, "update.zip"))
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(archive)
	member, err := writer.Create("replace.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = member.Write([]byte("new"))
	_ = writer.Close()
	_ = archive.Close()
	system := &System{dataDir: t.TempDir(), demo: true}
	if err := system.ExtractManagedArchive(context.Background(), Site{Root: root}, "update.zip", "", true); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, "replace.txt"))
	if err != nil || string(content) != "new" {
		t.Fatalf("existing file was not replaced: %q, %v", content, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".litecp-extract-backup-") {
			t.Fatal("successful extraction left recovery data behind")
		}
	}
}

func TestCappedArchiveWriterRejectsOverflowBeforeWriting(t *testing.T) {
	var destination strings.Builder
	writer := &cappedArchiveWriter{destination: &destination, maximum: 4}
	if _, err := writer.Write([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("5")); err == nil {
		t.Fatal("archive size limit was not enforced")
	}
	if destination.String() != "1234" {
		t.Fatal("overflow bytes reached the destination")
	}
}

func TestDemoDatabaseAndCronBehaviorMatchesProduction(t *testing.T) {
	system := &System{demo: true}
	if system.DatabaseEngine() != "MySQL" {
		t.Fatalf("unexpected demo database engine: %s", system.DatabaseEngine())
	}
	first := demoCredentials("billing-one.example.com")
	second := demoCredentials("billing-two.example.com")
	if first.Name == second.Name || first.Username == second.Username {
		t.Fatal("demo database credentials collide between domains")
	}
	if err := system.SetCron(Site{Domain: "static.example.com", AppType: "regular"}, true); err == nil {
		t.Fatal("demo accepted application cron for a regular website")
	}
}

func TestInfrastructureValidation(t *testing.T) {
	validDestination := BackupDestination{Name: "SFTP", Protocol: "sftp", Host: "backup.example.com", Port: 22, Username: "backup", RemotePath: "/litecp", HostKeySHA256: "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", PasswordCipher: "cipher"}
	if err := validateBackupDestination(validDestination, true); err != nil {
		t.Fatalf("valid destination rejected: %v", err)
	}
	invalidDestination := validDestination
	invalidDestination.HostKeySHA256 = ""
	if err := validateBackupDestination(invalidDestination, true); err == nil {
		t.Fatal("SFTP destination without host-key fingerprint was accepted")
	}
	if err := validateDatabaseObjects("client_db", "client_user"); err != nil {
		t.Fatal(err)
	}
	if err := validateDatabaseObjects("client-db", "client_user"); err == nil {
		t.Fatal("unsafe database name was accepted")
	}
	if err := validateDatabasePassword("too-short"); err == nil {
		t.Fatal("short database password was accepted")
	}
	if err := validateFirewallRule(FirewallRule{Port: 8448, Protocol: "tcp", Source: "203.0.113.0/24"}); err != nil {
		t.Fatal(err)
	}
	if err := validateFirewallRule(FirewallRule{Port: 70000, Protocol: "tcp", Source: "any"}); err == nil {
		t.Fatal("invalid firewall port was accepted")
	}
	if err := validateFirewallRule(FirewallRule{Port: 8443, Protocol: "tcp", Source: "any"}); err == nil {
		t.Fatal("duplicate management of an essential LiteCP port was accepted")
	}
	if err := validateMailboxAddress("billing@example.com", "example.com"); err != nil {
		t.Fatal(err)
	}
	if err := validateAliasAddress("sales@example.com", "owner@example.net"); err != nil {
		t.Fatal(err)
	}
}

func TestStoreMigratesSiteDatabase(t *testing.T) {
	directory := t.TempDir()
	created := time.Now().UTC()
	legacy := Config{Sites: []Site{{ID: "legacy", Domain: "billing.example.com", Root: "/var/www/example", AppType: "whmcs", PHPVersion: "8.3", Database: "legacy_db", DBUser: "legacy_user", CreatedAt: created}}}
	content, _ := json.Marshal(legacy)
	if err := os.WriteFile(filepath.Join(directory, "config.json"), content, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	state := store.PublicState()
	if len(state.Databases) != 1 || state.Databases[0].Name != "legacy_db" || state.Databases[0].SiteID != "legacy" {
		t.Fatalf("site database was not migrated: %#v", state.Databases)
	}
}

func TestOpenStoreMigratesLegacySiteDefaults(t *testing.T) {
	directory := t.TempDir()
	legacy := Config{Sites: []Site{{ID: "legacy", Domain: "billing.example.com", Root: "/var/www/example", PHPVersion: ""}}}
	content, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "config.json"), content, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	site := store.SiteByID("legacy")
	if site == nil || site.AppType != "whmcs" || site.PHPVersion != "8.3" {
		t.Fatalf("legacy site was not migrated: %#v", site)
	}
}

func TestNginxConfigProtectsWHMCSFiles(t *testing.T) {
	system := &System{os: OSInfo{ID: "ubuntu"}}
	config := system.nginxConfig(Site{ID: "billing", Domain: "billing.example.com", Root: "/var/www/litecp/billing.example.com/public", AppType: "whmcs", AppVersion: "8.13", PHPVersion: "8.1"})
	for _, required := range []string{"server_name billing.example.com", "location = /configuration.php", "location ^~ /crons/", "php8.1-litecp-billing.sock"} {
		if !strings.Contains(config, required) {
			t.Errorf("nginx config is missing %q", required)
		}
	}
}

func TestNginxConfigProtectsWordPressFiles(t *testing.T) {
	system := &System{os: OSInfo{ID: "almalinux"}}
	config := system.nginxConfig(Site{ID: "blog", Domain: "blog.example.com", Root: "/var/www/litecp/blog.example.com/public", AppType: "wordpress", PHPVersion: "8.4"})
	for _, required := range []string{"server_name blog.example.com", "location = /wp-config.php", "wp-content/uploads/.*\\.php", "/var/opt/remi/php84/run/php-fpm/litecp-blog.sock"} {
		if !strings.Contains(config, required) {
			t.Errorf("nginx config is missing %q", required)
		}
	}
	if strings.Contains(config, "location = /configuration.php") {
		t.Fatal("WordPress vhost contains WHMCS-only rules")
	}
	uploadRule := strings.Index(config, "wp-content/uploads")
	genericPHP := strings.Index(config, "location ~ \\.php$")
	if uploadRule < 0 || genericPHP < 0 || uploadRule > genericPHP {
		t.Fatal("WordPress upload protection must be evaluated before the generic PHP handler")
	}
}

func TestNginxConfigProtectsRegularWebsiteSecrets(t *testing.T) {
	system := &System{os: OSInfo{ID: "ubuntu"}}
	config := system.nginxConfig(Site{ID: "regular", Domain: "www.example.com", Root: "/var/www/litecp/www.example.com/public", AppType: "regular", PHPVersion: "8.4"})
	for _, required := range []string{"server_name www.example.com", "composer\\.(?:json|lock)", "php8.4-litecp-regular.sock"} {
		if !strings.Contains(config, required) {
			t.Errorf("regular website config is missing %q", required)
		}
	}
	if strings.Contains(config, "configuration.php") || strings.Contains(config, "wp-config.php") {
		t.Fatal("regular website config contains application-specific rules")
	}
}

func TestApplicationPHPCompatibility(t *testing.T) {
	valid := [][3]string{{"whmcs", "8.13", "7.3"}, {"whmcs", "9.0", "8.3"}, {"wordpress", "", "7.4"}, {"wordpress", "", "8.4"}, {"regular", "", "7.3"}}
	for _, item := range valid {
		if err := validateSiteOptions(item[0], item[1], item[2]); err != nil {
			t.Errorf("valid combination %v rejected: %v", item, err)
		}
	}
	invalid := [][3]string{{"whmcs", "8.13", "8.4"}, {"whmcs", "9.0", "8.1"}, {"wordpress", "", "7.3"}, {"unknown", "", "8.3"}, {"whmcs", "8.13", "8.0"}}
	for _, item := range invalid {
		if err := validateSiteOptions(item[0], item[1], item[2]); err == nil {
			t.Errorf("invalid combination accepted: %v", item)
		}
	}
}

func TestReplaceFastCGISocketPreservesTLSConfig(t *testing.T) {
	original := "listen 443 ssl;\n    fastcgi_pass unix:/run/php/php8.3-fpm.sock;\nssl_certificate /etc/letsencrypt/live/example/fullchain.pem;\n"
	updated, err := replaceFastCGISocket(original, "/run/php/php8.2-fpm.sock")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated, "fastcgi_pass unix:/run/php/php8.2-fpm.sock;") || !strings.Contains(updated, "ssl_certificate /etc/letsencrypt") {
		t.Fatalf("unexpected updated config: %s", updated)
	}
}
