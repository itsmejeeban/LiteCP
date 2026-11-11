package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const deleteZIPFixtureID = "backup-delete-fixture-123"

type websiteZIPDeleteFixture struct {
	app       *App
	site      Site
	item      websiteZIPBackup
	directory string
}

func newWebsiteZIPDeleteFixture(t *testing.T) websiteZIPDeleteFixture {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	site := Site{ID: "delete-site", Domain: "delete.example.test", Root: filepath.Join(t.TempDir(), "public_html")}
	if err := os.Mkdir(site.Root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site.Root, "live.txt"), []byte("keep website"), 0600); err != nil {
		t.Fatal(err)
	}
	store.cfg.Sites = []Site{site}
	app := &App{store: store, system: &System{dataDir: t.TempDir(), demo: true}}
	directory := filepath.Join(app.system.websiteBackupsRoot(site), deleteZIPFixtureID)
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	item := websiteZIPBackup{ID: deleteZIPFixtureID, SiteID: site.ID, Domain: site.Domain, Size: 7}
	if err := os.WriteFile(filepath.Join(directory, "website.zip"), []byte("archive"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "temporary.sql"), []byte("generated fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(directory), "other-backup.txt"), []byte("keep sibling"), 0600); err != nil {
		t.Fatal(err)
	}
	fixture := websiteZIPDeleteFixture{app: app, site: site, item: item, directory: directory}
	fixture.save(t)
	return fixture
}
func (f websiteZIPDeleteFixture) save(t *testing.T) {
	t.Helper()
	body, err := json.Marshal(f.item)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.directory, "manifest.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
}
func (f websiteZIPDeleteFixture) request(body map[string]any, siteID string) *http.Request {
	encoded, _ := json.Marshal(body)
	return httptest.NewRequest(http.MethodDelete, "/api/website-backups?siteId="+siteID, bytes.NewReader(encoded))
}
func (f websiteZIPDeleteFixture) preserved(t *testing.T) {
	t.Helper()
	for name, want := range map[string]string{filepath.Join(f.site.Root, "live.txt"): "keep website", filepath.Join(filepath.Dir(f.directory), "other-backup.txt"): "keep sibling"} {
		got, err := os.ReadFile(name)
		if err != nil || string(got) != want {
			t.Fatalf("unrelated data changed: %s %v", name, err)
		}
	}
	if len(f.app.store.PublicState().Sites) != 1 {
		t.Fatal("website metadata changed")
	}
}
func TestWebsiteZIPDeleteSelectedLocalBackup(t *testing.T) {
	f := newWebsiteZIPDeleteFixture(t)
	response := httptest.NewRecorder()
	f.app.websiteBackups(response, f.request(map[string]any{"id": f.item.ID, "confirm": true}, f.site.ID))
	if response.Code != 200 {
		t.Fatalf("HTTP %d %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(f.directory); !os.IsNotExist(err) {
		t.Fatalf("selected generated files remain: %v", err)
	}
	f.preserved(t)
}
func TestWebsiteZIPDeleteRemoteOrderingAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name               string
		removeRemote, fail bool
	}{
		{name: "local deletion preserves remote"},
		{name: "explicit remote deletion", removeRemote: true},
		{name: "remote failure retains all local files", removeRemote: true, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWebsiteZIPDeleteFixture(t)
			f.item.RemotePath = "sftp://storage.example.test:22/backups/" + f.item.Domain + "-" + f.item.ID + ".zip"
			f.item.DestinationID = "destination"
			f.app.store.cfg.BackupDestinations = []BackupDestination{{ID: "destination", Host: "storage.example.test", Protocol: "sftp", RemotePath: "/backups", Port: 22}}
			f.save(t)
			calls := 0
			remote := func(ctx context.Context, d *BackupDestination, b Backup, operation, local string) error {
				calls++
				if _, err := os.Stat(filepath.Join(f.directory, "manifest.json")); err != nil {
					t.Fatal("local metadata removed before remote operation")
				}
				if d == nil || d.ID != f.item.DestinationID || b.Path != f.item.Domain+"-"+f.item.ID+".zip" || b.RemotePath != f.item.RemotePath || operation != "remove" || local != "" {
					t.Fatalf("wrong remote object: %+v %+v %q %q", d, b, operation, local)
				}
				if tc.fail {
					return errors.New("fixture host key mismatch")
				}
				return nil
			}
			response := httptest.NewRecorder()
			f.app.deleteWebsiteZIPBackupWithOperations(response, f.request(map[string]any{"id": f.item.ID, "confirm": true, "removeRemote": tc.removeRemote}, f.site.ID), retainedWebsite, remote)
			want := 200
			if tc.fail {
				want = 502
			}
			if response.Code != want {
				t.Fatalf("HTTP %d %s", response.Code, response.Body.String())
			}
			if (calls == 1) != tc.removeRemote {
				t.Fatalf("remote calls=%d", calls)
			}
			if tc.fail {
				for _, name := range []string{"manifest.json", "website.zip", "temporary.sql"} {
					if _, err := os.Stat(filepath.Join(f.directory, name)); err != nil {
						t.Fatal("failed remote operation lost local data", err)
					}
				}
			} else if _, err := os.Stat(f.directory); !os.IsNotExist(err) {
				t.Fatal("selected local directory remains", err)
			}
			f.preserved(t)
		})
	}
}
func TestWebsiteZIPDeleteRetainedOwnerWithoutCreatingWebsite(t *testing.T) {
	f := newWebsiteZIPDeleteFixture(t)
	f.app.store.cfg.Sites = nil
	resolved := false
	retained := func(id string) (Site, error) {
		resolved = true
		if id != f.site.ID {
			t.Fatal("wrong retained owner")
		}
		return f.site, nil
	}
	response := httptest.NewRecorder()
	f.app.deleteWebsiteZIPBackupWithOperations(response, f.request(map[string]any{"id": f.item.ID, "confirm": true}, f.site.ID), retained, func(context.Context, *BackupDestination, Backup, string, string) error {
		t.Fatal("unexpected remote operation")
		return nil
	})
	if response.Code != 200 || !resolved {
		t.Fatalf("HTTP %d %s", response.Code, response.Body.String())
	}
	if len(f.app.store.PublicState().Sites) != 0 {
		t.Fatal("deleted website was recreated")
	}
	if _, err := os.Stat(filepath.Join(f.site.Root, "live.txt")); err != nil {
		t.Fatal("website files touched", err)
	}
}
func TestWebsiteZIPDeleteRejectsUnsafeOrUnconfirmedRequests(t *testing.T) {
	for _, tc := range []struct {
		name, id, site              string
		confirm, wrongOwner, active bool
		want                        int
	}{
		{name: "confirmation required", id: deleteZIPFixtureID, site: "delete-site", want: 400},
		{name: "archive path traversal", id: "../../outside", site: "delete-site", confirm: true, want: 400},
		{name: "owner path traversal", id: deleteZIPFixtureID, site: "../outside", confirm: true, want: 400},
		{name: "wrong manifest owner", id: deleteZIPFixtureID, site: "delete-site", confirm: true, wrongOwner: true, want: 404},
		{name: "generation is active", id: deleteZIPFixtureID, site: "delete-site", confirm: true, active: true, want: 409},
		{name: "unknown owner", id: deleteZIPFixtureID, site: "unknown-site", confirm: true, want: 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWebsiteZIPDeleteFixture(t)
			if tc.wrongOwner {
				f.item.SiteID = "other-site"
				f.save(t)
			}
			if tc.active {
				websiteBackupWorkers.Lock()
				websiteBackupWorkers.active[f.directory] = true
				websiteBackupWorkers.Unlock()
				t.Cleanup(func() {
					websiteBackupWorkers.Lock()
					delete(websiteBackupWorkers.active, f.directory)
					websiteBackupWorkers.Unlock()
				})
			}
			response := httptest.NewRecorder()
			f.app.websiteBackups(response, f.request(map[string]any{"id": tc.id, "confirm": tc.confirm}, tc.site))
			if response.Code != tc.want {
				t.Fatalf("HTTP %d %s", response.Code, response.Body.String())
			}
			if _, err := os.Stat(filepath.Join(f.directory, "manifest.json")); err != nil {
				t.Fatal("rejected request removed backup", err)
			}
			f.preserved(t)
		})
	}
}
func TestWebsiteZIPDeleteRejectsSymlinkedMetadataAndAncestors(t *testing.T) {
	for _, kind := range []string{"manifest", "ancestor"} {
		t.Run(kind, func(t *testing.T) {
			f := newWebsiteZIPDeleteFixture(t)
			if kind == "manifest" {
				moved := filepath.Join(t.TempDir(), "metadata.json")
				if err := os.Rename(filepath.Join(f.directory, "manifest.json"), moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, filepath.Join(f.directory, "manifest.json")); err != nil {
					t.Skip("symlinks unavailable")
				}
			} else {
				root := filepath.Dir(f.directory)
				moved := root + "-retained"
				if err := os.Rename(root, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, root); err != nil {
					t.Skip("symlinks unavailable")
				}
			}
			response := httptest.NewRecorder()
			f.app.websiteBackups(response, f.request(map[string]any{"id": f.item.ID, "confirm": true}, f.site.ID))
			if response.Code != 400 || !strings.Contains(response.Body.String(), "safely verified") {
				t.Fatalf("HTTP %d %s", response.Code, response.Body.String())
			}
			if _, err := os.Stat(filepath.Join(f.directory, "website.zip")); err != nil {
				t.Fatal("unsafe path deletion affected backup", err)
			}
		})
	}
}
func TestWebsiteZIPDeleteRequiresAuthentication(t *testing.T) {
	response := httptest.NewRecorder()
	(&App{}).routes().ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/api/website-backups?siteId=delete-site", strings.NewReader(`{"id":"backup-delete-fixture-123","confirm":true}`)))
	if response.Code != 401 {
		t.Fatalf("HTTP %d", response.Code)
	}
}

func TestWebsiteZIPDeleteRemoteOnlyWithoutLocalArchive(t *testing.T) {
	f := newWebsiteZIPDeleteFixture(t)
	f.item.RemoteOnly = true
	f.item.RemotePath = "sftp://storage.example.test:22/backups/" + f.item.Domain + "-" + f.item.ID + ".zip"
	f.item.DestinationID = "remote-only-destination"
	f.save(t)
	if err := os.Remove(filepath.Join(f.directory, "website.zip")); err != nil {
		t.Fatal(err)
	}
	calls := 0
	response := httptest.NewRecorder()
	f.app.deleteWebsiteZIPBackupWithOperations(response, f.request(map[string]any{"id": f.item.ID, "confirm": true, "removeRemote": true}, f.site.ID), retainedWebsite, func(_ context.Context, _ *BackupDestination, b Backup, operation, _ string) error {
		calls++
		if b.RemotePath != f.item.RemotePath || operation != "remove" {
			t.Fatal("wrong remote-only object")
		}
		return nil
	})
	if response.Code != 200 || calls != 1 {
		t.Fatalf("HTTP %d %s; calls=%d", response.Code, response.Body.String(), calls)
	}
	if _, err := os.Stat(f.directory); !os.IsNotExist(err) {
		t.Fatal("remote-only metadata remains", err)
	}
	f.preserved(t)
}
func TestWebsiteZIPDeleteRejectsIncompleteLocalArchive(t *testing.T) {
	f := newWebsiteZIPDeleteFixture(t)
	if err := os.Remove(filepath.Join(f.directory, "website.zip")); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	f.app.deleteWebsiteZIPBackupWithOperations(response, f.request(map[string]any{"id": f.item.ID, "confirm": true, "removeRemote": true}, f.site.ID), retainedWebsite, func(context.Context, *BackupDestination, Backup, string, string) error {
		t.Fatal("invalid local backup reached remote operation")
		return nil
	})
	if response.Code != 404 {
		t.Fatalf("HTTP %d %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(f.directory, "manifest.json")); err != nil {
		t.Fatal("incomplete backup metadata removed", err)
	}
	f.preserved(t)
}
