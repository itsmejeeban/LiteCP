package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func featureSite(t *testing.T) (*System, Site) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "public")
	if err := os.Mkdir(root, 0750); err != nil {
		t.Fatal(err)
	}
	return &System{demo: true, dataDir: t.TempDir()}, Site{ID: "feature-site", Domain: "example.test", Root: root, RuntimeUID: os.Getuid(), RuntimeGID: os.Getgid()}
}

func TestTrashRestoresWithoutOverwriting(t *testing.T) {
	system, site := featureSite(t)
	file := filepath.Join(site.Root, "important.php")
	if err := os.WriteFile(file, []byte("original"), 0640); err != nil {
		t.Fatal(err)
	}
	entry, err := system.MoveManagedPathToTrash(site, "important.php")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("deleted file remains live")
	}
	entries, err := system.ListTrash(site)
	if err != nil || len(entries) != 1 {
		t.Fatalf("trash: %v %v", entries, err)
	}
	if err := os.WriteFile(file, []byte("new"), 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := system.RestoreTrashEntry(site, entry.ID, ""); err == nil {
		t.Fatal("restore overwrote live file")
	}
	if _, err := system.RestoreTrashEntry(site, entry.ID, "../escape"); err == nil {
		t.Fatal("restore escaped website")
	}
	if _, err := system.RestoreTrashEntry(site, entry.ID, "restored.php"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(site.Root, "restored.php"))
	if err != nil || string(content) != "original" {
		t.Fatal("payload changed")
	}
	entries, err = system.ListTrash(site)
	if err != nil || len(entries) != 0 {
		t.Fatal("restored payload remains in trash")
	}
	if _, err := system.MoveManagedPathToTrash(site, ""); err == nil {
		t.Fatal("website root was deleted")
	}
	if err := system.DeleteTrashEntry(site, "../outside"); err == nil {
		t.Fatal("unsafe trash ID accepted")
	}
}

func TestTrashPreservesDirectoryContents(t *testing.T) {
	system, site := featureSite(t)
	if err := os.Mkdir(filepath.Join(site.Root, "folder"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site.Root, "folder", "child.txt"), []byte("keep"), 0640); err != nil {
		t.Fatal(err)
	}
	entry, err := system.MoveManagedPathToTrash(site, "folder")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Type != "directory" || entry.Size != 4 {
		t.Fatalf("wrong trash metadata: %+v", entry)
	}
	if _, err := system.RestoreTrashEntry(site, entry.ID, ""); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(site.Root, "folder", "child.txt")); err != nil || string(content) != "keep" {
		t.Fatal("lost nested file")
	}
}

func TestBulkSelectionPreflight(t *testing.T) {
	for _, paths := range [][]string{{"a", "a"}, {"a", "a/file"}, {"a/file", "a"}, {"one/a", "two/a"}} {
		if err := validateBulkSelection(paths); err == nil {
			t.Errorf("accepted overlapping selection %v", paths)
		}
	}
	system, site := featureSite(t)
	if err := os.Mkdir(filepath.Join(site.Root, "dest"), 0750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first.txt", "second.txt", "dest/second.txt"} {
		if err := os.WriteFile(filepath.Join(site.Root, name), []byte("data"), 0640); err != nil {
			t.Fatal(err)
		}
	}
	if err := system.CopyManagedPaths(context.Background(), site, []string{"first.txt", "second.txt"}, "dest"); err == nil {
		t.Fatal("copy overwrote destination")
	}
	if _, err := os.Stat(filepath.Join(site.Root, "dest", "first.txt")); !os.IsNotExist(err) {
		t.Fatal("preflight failed after starting the copy")
	}
	if err := system.MoveManagedPaths(site, []string{"first.txt", "second.txt"}, "dest"); err == nil {
		t.Fatal("move overwrote destination")
	}
	if _, err := os.Stat(filepath.Join(site.Root, "first.txt")); err != nil {
		t.Fatal("preflight moved a file")
	}
}

func TestArchivePathsAndMigrationPreview(t *testing.T) {
	for _, name := range []string{"../escape", "/etc/passwd", "C:/x", "dir/../../escape", "x\\y", "x\x00y"} {
		if _, err := safeArchiveName(name); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	var payload bytes.Buffer
	writer := zip.NewWriter(&payload)
	for name, content := range map[string]string{"backup/homedir/public_html/index.php": "<?php echo 'ok';", "backup/mysql/site.sql": "CREATE TABLE sample(id int);"} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	link := &zip.FileHeader{Name: "backup/homedir/public_html/escape"}
	link.SetMode(os.ModeSymlink | 0777)
	entry, err := writer.CreateHeader(link)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("/etc/passwd"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "source.zip")
	if err := os.WriteFile(archive, payload.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	preview, err := extractMigration(context.Background(), archive, root, "cpanel")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Files != 2 || preview.Skipped != 1 || len(preview.Roots) != 1 || preview.Roots[0] != "backup/homedir/public_html" || len(preview.Databases) != 1 {
		t.Fatalf("bad preview: %+v", preview)
	}
	if _, err := os.Stat(filepath.Join(root, "backup/homedir/public_html/escape")); !os.IsNotExist(err) {
		t.Fatal("extracted symlink")
	}
}

func TestTarTraversalRejected(t *testing.T) {
	var payload bytes.Buffer
	writer := tar.NewWriter(&payload)
	if err := writer.WriteHeader(&tar.Header{Name: "../outside", Size: 4, Mode: 0644, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = writer.Write([]byte("oops"))
	_ = writer.Close()
	directory := t.TempDir()
	archive := filepath.Join(directory, "bad.tar")
	if err := os.WriteFile(archive, payload.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(directory, "root")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := extractMigration(context.Background(), archive, root, "plesk"); err == nil {
		t.Fatal("unsafe tar accepted")
	}
	if _, err := os.Stat(filepath.Join(directory, "outside")); !os.IsNotExist(err) {
		t.Fatal("archive escaped root")
	}
}

func TestActivityPaginationBounds(t *testing.T) {
	store := &Store{cfg: Config{Activities: make([]Activity, 23)}}
	for _, test := range []struct{ page, size, want int }{{1, 10, 10}, {3, 10, 3}, {4, 10, 0}, {-1, 10, 10}, {int(^uint(0) >> 1), 100, 0}} {
		result, total := store.Activities(test.page, test.size)
		if total != 23 || len(result) != test.want {
			t.Errorf("page %d: %d items of %d", test.page, len(result), total)
		}
	}
}

func TestWAFConfigPreservesTLS(t *testing.T) {
	site := Site{ID: "example", Domain: "example.test"}
	original := "server {\n    listen 443 ssl;\n    ssl_certificate /etc/letsencrypt/live/example/fullchain.pem;\n" + wafBlock(site, "off") + "\n}\nserver { return 301 https://$host$request_uri; }\n"
	updated, err := replaceWAFBlock(original, site, "detect")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"listen 443 ssl", "ssl_certificate /etc/letsencrypt/live/example/fullchain.pem", "server { return 301 https://$host$request_uri; }", "modsecurity on;"} {
		if !strings.Contains(updated, value) {
			t.Errorf("lost %s", value)
		}
	}
	disabled, err := replaceWAFBlock(updated, site, "off")
	if err != nil {
		t.Fatal(err)
	}
	if disabled != original {
		t.Fatal("WAF on/off modified unrelated TLS configuration")
	}
	if _, err := replaceWAFBlock("server {\n    # LITECP_WAF_BEGIN", site, "off"); err == nil {
		t.Fatal("incomplete marker accepted")
	}
}

func TestFirewallDirectionAndProtectedPorts(t *testing.T) {
	for _, port := range []int{22, 25, 80, 443, 465, 587, 993, 8443} {
		if err := validateFirewallRule(normalizeFirewallRule(FirewallRule{Port: port, Protocol: "tcp", Action: "deny"})); err == nil {
			t.Errorf("essential port %d allowed", port)
		}
	}
	valid := normalizeFirewallRule(FirewallRule{Port: 25, Protocol: "tcp", Direction: "outbound", Action: "deny", Destination: "203.0.113.1"})
	if err := validateFirewallRule(valid); err != nil {
		t.Fatal(err)
	}
	valid.Destination = "1.2.3.4;reboot"
	if err := validateFirewallRule(valid); err == nil {
		t.Fatal("unsafe destination accepted")
	}
	rule := FirewallRule{ID: "old", Port: 8080, Protocol: "tcp", Source: "any"}
	store := &Store{cfg: Config{FirewallRules: []FirewallRule{rule}}}
	if store.HasFirewallRule(rule) {
		t.Fatal("editing the same rule is considered a duplicate")
	}
	rule.ID = ""
	if !store.HasFirewallRule(rule) {
		t.Fatal("legacy rule normalization failed")
	}
}

func TestResolverValidation(t *testing.T) {
	for _, values := range [][]string{nil, {"8.8.8.8;reboot"}, {"0.0.0.0"}, {"224.0.0.1"}} {
		if _, err := validateResolvers(values); err == nil {
			t.Errorf("accepted %v", values)
		}
	}
	values, err := validateResolvers([]string{"8.8.8.8", "8.8.8.8", "2001:4860:4860::8888"})
	if err != nil || len(values) != 2 {
		t.Fatal("resolver normalization failed")
	}
}

func TestMigrationListingRequiresAuthentication(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, system: &System{demo: true, dataDir: t.TempDir()}, sessions: map[string]session{"test": {Expires: time.Now().Add(time.Hour)}}, attempts: map[string]loginAttempt{}}
	unauth := httptest.NewRecorder()
	app.routes().ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/api/migrations", nil))
	if unauth.Code != 401 {
		t.Fatalf("unauthenticated response: %d", unauth.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/migrations", nil)
	request.AddCookie(&http.Cookie{Name: "litecp_session", Value: "test"})
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("list migrations: %d %s", response.Code, response.Body.String())
	}
	var result struct{ Items []MigrationPreview }
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Items == nil {
		t.Fatal("empty stages must be an array")
	}
}

func TestExtensionAllowlistProtectsCore(t *testing.T) {
	for _, extension := range []string{"fpm", "cli", "common", "mysqlnd", "xml", "curl", "../../etc"} {
		if extensionAllowed(extension) {
			t.Errorf("unsafe extension allowed: %s", extension)
		}
	}
	if !extensionAllowed("redis") {
		t.Fatal("supported extension unavailable")
	}
}

func TestLimitedCommandOutput(t *testing.T) {
	var buffer limitedBuffer
	data := bytes.Repeat([]byte("a"), 2<<20)
	n, err := buffer.Write(data)
	if err != nil || n != len(data) || buffer.Len() != 1<<20 {
		t.Fatal("output buffer was not bounded")
	}
}

func TestMailFileSnapshotRestoresPreviousState(t *testing.T) {
	directory := t.TempDir()
	existing := filepath.Join(directory, "existing")
	created := filepath.Join(directory, "created")
	if err := os.WriteFile(existing, []byte("before"), 0640); err != nil {
		t.Fatal(err)
	}
	snapshots, err := snapshotMailFiles([]string{existing, created})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := restoreMailFiles(snapshots); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(existing)
	if err != nil || string(content) != "before" {
		t.Fatalf("existing file was not restored: %q, %v", content, err)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatal("new mail file was not removed during rollback")
	}
}

func TestSchemaUpgradePreservesUnmanagedDNSAndMailPreference(t *testing.T) {
	directory := t.TempDir()
	legacy := Config{SchemaVersion: 4}
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
	if len(store.Settings().Resolvers) != 0 {
		t.Fatal("upgrade claims DNS was configured")
	}
	if err := store.UpdateSettings(func(settings *Settings) {
		settings.EmailHostingEnabled = false
		settings.Resolvers = []string{"1.1.1.1"}
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateSettings(func(settings *Settings) { settings.BackupRetention = 7 }); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	settings := reopened.Settings()
	if settings.EmailHostingEnabled || len(settings.Resolvers) != 1 || settings.Resolvers[0] != "1.1.1.1" {
		t.Fatal("settings update lost feature preferences")
	}
}
