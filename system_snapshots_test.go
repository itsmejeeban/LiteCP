package main

import (
	"net/http/httptest"
	"testing"
)

func TestSystemSnapshotsRequireAuthentication(t *testing.T) {
	a := &App{}
	for _, path := range []string{"/api/system-snapshots", "/api/system-snapshots/upload", "/api/system-snapshots/inspect", "/api/system-snapshots/restore"} {
		w := httptest.NewRecorder()
		a.routes().ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		if w.Code != 401 {
			t.Fatalf("%s got %d", path, w.Code)
		}
	}
}
func TestSnapshotPathsCannotEscape(t *testing.T) {
	for _, name := range []string{"../x.zip", "/etc/secret.zip", "x\\y.zip", "", "x.tar", "x.zip\n"} {
		if _, err := snapshotPath(name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	if _, err := snapshotPath("uploaded-abc.zip"); err != nil {
		t.Fatal(err)
	}
}
