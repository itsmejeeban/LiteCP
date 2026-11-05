package main

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotRemoteListing(t *testing.T) {
	names := snapshotNamesFromListing("sftp> ls -1 \"/snapshots/\"\n/snapshots/litecp-system-one.zip\nother.txt\n/snapshots/litecp-system-one.zip\nremote-two.zip\n")
	if len(names) != 2 || names[0] != "litecp-system-one.zip" || names[1] != "remote-two.zip" {
		t.Fatalf("unexpected names: %v", names)
	}
}
func TestSnapshotSettingsHideSecretsAndStaySeparate(t *testing.T) {
	dir := t.TempDir()
	a := &App{system: &System{dataDir: dir}}
	content := `{"destination":{"host":"backup.test","passwordCipher":"encrypted-secret"},"enabled":false}`
	if err := os.WriteFile(filepath.Join(dir, "snapshot-settings.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	a.snapshotSettingsAPI(response, httptest.NewRequest("GET", "/api/system-snapshots/settings", nil))
	if response.Code != 200 || strings.Contains(response.Body.String(), "encrypted-secret") || !strings.Contains(response.Body.String(), `"hasPassword":true`) {
		t.Fatal(response.Body.String())
	}
}
func TestSnapshotScheduleRejectsInvalidCron(t *testing.T) {
	a := &App{system: &System{dataDir: t.TempDir()}}
	response := httptest.NewRecorder()
	a.snapshotSettingsAPI(response, httptest.NewRequest("POST", "/api/system-snapshots/settings", strings.NewReader(`{"enabled":true,"cron":"invalid"}`)))
	if response.Code != 400 {
		t.Fatal(response.Code)
	}
}

func TestSnapshotFingerprintDetection(t *testing.T) {
	scan := "host ssh-ed25519 AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n"
	value, err := snapshotDetectedFingerprint(scan)
	if err != nil || !strings.HasPrefix(value, "SHA256:") {
		t.Fatalf("%s %v", value, err)
	}
	if _, err = snapshotDetectedFingerprint("host ssh-rsa invalid"); err == nil {
		t.Fatal("invalid key accepted")
	}
}

func TestSnapshotRemoteOnlyCleanup(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		remoteOnly             bool
		transferErr, removeErr error
		wantRemove             bool
	}{
		{"local copy", false, nil, nil, false},
		{"failed upload", true, errors.New("transfer failed"), nil, false},
		{"remote only", true, nil, nil, true},
		{"cleanup failed", true, nil, errors.New("permission denied"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			status := finishSnapshotUpload("snapshot.zip", tc.remoteOnly, tc.transferErr, func(name string) error {
				called = true
				if name != "snapshot.zip" {
					t.Fatal(name)
				}
				return tc.removeErr
			})
			if called != tc.wantRemove {
				t.Fatalf("unexpected cleanup: %v", called)
			}
			if tc.removeErr != nil && !strings.Contains(status, "could not be removed") {
				t.Fatal(status)
			}
			if tc.transferErr != nil && !strings.Contains(status, "retained") {
				t.Fatal(status)
			}
		})
	}
}

func TestSnapshotRemoteRequiresConfiguredDestination(t *testing.T) {
	a := &App{system: &System{dataDir: t.TempDir()}}
	for _, method := range []string{"GET", "POST"} {
		response := httptest.NewRecorder()
		a.snapshotRemoteAPI(response, httptest.NewRequest(method, "/api/system-snapshots/remote", nil))
		if response.Code != 400 || !strings.Contains(response.Body.String(), "Configure and save snapshot remote storage") {
			t.Fatal(response.Code, response.Body.String())
		}
	}
}
