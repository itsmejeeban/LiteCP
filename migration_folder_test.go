package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestMigrationFolderScanAndStaleSelection(t *testing.T) {
	app := apiTestApp(t)
	app.system.demo = false
	folder := t.TempDir()
	for _, name := range []string{"older.tar.gz", "newer.ZIP", "unrelated.sql.gz", ".partial.tar.gz"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	older := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(folder, "older.tar.gz"), older, older); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(folder, "subfolder.tar"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "subfolder.tar", "nested.zip"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" {
		if err := os.Symlink("older.tar.gz", filepath.Join(folder, "symlink.zip")); err != nil {
			t.Fatal(err)
		}
	}
	request := func(path string, body migrationFolderRequest, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "https://panel.example/api/migrations/folder/"+path, bytes.NewReader(data))
		req.Header.Set("Origin", "https://panel.example")
		if authenticated {
			req.AddCookie(&http.Cookie{Name: "litecp_session", Value: "test-session"})
		}
		result := httptest.NewRecorder()
		app.routes().ServeHTTP(result, req)
		return result
	}
	input := migrationFolderRequest{Platform: "cpanel", Folder: folder}
	if result := request("scan", input, false); result.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated scan returned %d", result.Code)
	}
	result := request("scan", input, true)
	var output struct {
		Items     []migrationFolderArchive
		Truncated bool
	}
	if err := json.Unmarshal(result.Body.Bytes(), &output); result.Code != http.StatusOK || err != nil || output.Truncated || len(output.Items) != 2 {
		t.Fatalf("unexpected archive list: %d %s", result.Code, result.Body.String())
	}
	if output.Items[0].Name != "newer.ZIP" || output.Items[1].Name != "older.tar.gz" {
		t.Fatalf("archives were not ordered newest first: %v", output.Items)
	}
	selected := output.Items[0]
	input.Name, input.Size, input.ModifiedAt = "../"+selected.Name, selected.Size, selected.ModifiedAt
	if result := request("restore", input, true); result.Code != http.StatusBadRequest {
		t.Fatalf("archive traversal returned %d", result.Code)
	}
	input.Name = selected.Name
	if err := os.WriteFile(filepath.Join(folder, input.Name), []byte("new fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if result := request("restore", input, true); result.Code != http.StatusConflict {
		t.Fatalf("changed archive returned %d: %s", result.Code, result.Body.String())
	}
}

func TestMigrationFolderRejectsUnsafeFolderPaths(t *testing.T) {
	volume := filepath.VolumeName(t.TempDir())
	for _, path := range []string{"relative/backup", volume + "/root/../backup", volume + "/proc/123", volume + "/sys/backup", volume + "/dev/fd", volume + "/run/backup", "//server/share", volume + "/back\x00ups"} {
		if _, _, err := migrationFolderPath(path); err == nil {
			t.Errorf("accepted unsafe backup location %q", path)
		}
	}
}

func TestMigrationFolderAllowsFilesystemRoot(t *testing.T) {
	volume := filepath.VolumeName(t.TempDir())
	root, relative, err := migrationFolderPath(volume + "/")
	if err != nil || root != volume+string(os.PathSeparator) || relative != "." {
		t.Fatalf("root folder was not accepted: %q %q %v", root, relative, err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "account.tar.gz"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	file, _, err := openMigrationFolderArchive(dir, relative, "account.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
}
