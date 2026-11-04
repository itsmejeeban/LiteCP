package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegrityChangesAndReadOnly(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := integrityCollect(context.Background(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(integrityDiff(before, before)) != 0 {
		t.Fatal("unchanged files reported")
	}
	if err = os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	added := filepath.Join(root, "added")
	if err = os.WriteFile(added, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := integrityCollect(context.Background(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	changes := integrityDiff(before, after)
	if len(changes) != 2 || changes[0].Kind != "added" || changes[1].Kind != "modified" {
		t.Fatal(changes)
	}
	missing := integrityDiff(before, map[string]integrityEntry{})
	if len(missing) != 1 || missing[0].Kind != "missing" {
		t.Fatal(missing)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "changed" {
		t.Fatal("scanner modified file")
	}
}
func TestIntegrityCancellation(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := integrityCollect(ctx, []string{root}); err == nil {
		t.Fatal("canceled scan succeeded")
	}
}
func TestIntegritySymlinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside")
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlink privilege unavailable")
	}
	result, err := integrityCollect(context.Background(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if result[link].Link != target || result[link].Hash != "" {
		t.Fatal(result)
	}
}
func TestIntegritySettingsAndConfirmation(t *testing.T) {
	app := &App{system: &System{dataDir: t.TempDir()}}
	for _, body := range []string{`{"action":"baseline"}`, `{"action":"scan"}`, `{"action":"settings","enabled":true,"interval":60}`, `{"action":"settings","interval":1}`, `{"action":"unknown"}`} {
		response := httptest.NewRecorder()
		app.integrityAPI(response, httptest.NewRequest("POST", "/api/security/integrity", strings.NewReader(body)))
		if response.Code != 400 {
			t.Fatalf("%s: %d %s", body, response.Code, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	app.integrityAPI(response, httptest.NewRequest("POST", "/api/security/integrity", strings.NewReader(`{"action":"settings","enabled":false,"interval":15}`)))
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	state, err := app.integrityRead()
	if err != nil || state.Interval != 15 || state.Enabled {
		t.Fatal(state, err)
	}
	response = httptest.NewRecorder()
	app.integrityAPI(response, httptest.NewRequest("GET", "/api/security/integrity", nil))
	var payload map[string]json.RawMessage
	if json.Unmarshal(response.Body.Bytes(), &payload) != nil || payload["roots"] == nil {
		t.Fatal(response.Body.String())
	}
}

func TestIntegrityBaselineApprovalAndReports(t *testing.T) {
	dataDir, root := t.TempDir(), t.TempDir()
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{system: &System{dataDir: dataDir}, store: store}
	previousRoots := integrityRoots
	integrityRoots = []string{root}
	defer func() { integrityRoots = previousRoots }()
	path := filepath.Join(root, "config")
	if err = os.WriteFile(path, []byte("trusted"), 0600); err != nil {
		t.Fatal(err)
	}
	app.integrityRun(true)
	initial, err := app.integrityRead()
	if err != nil || initial.Baseline == "" || !initial.Enabled || initial.Error != "" {
		t.Fatal(initial, err)
	}
	if err = os.WriteFile(path, []byte("unexpected"), 0600); err != nil {
		t.Fatal(err)
	}
	app.integrityRun(false)
	changed, _ := app.integrityRead()
	if len(changed.Changes) != 1 || changed.Changes[0].Kind != "modified" || changed.Baseline != initial.Baseline {
		t.Fatal(changed)
	}
	app.integrityRun(true)
	approved, _ := app.integrityRead()
	if approved.Baseline == initial.Baseline || len(approved.Changes) != 0 {
		t.Fatal(approved)
	}
	if _, err = os.Stat(filepath.Join(app.integrityDir(), initial.Baseline)); err != nil {
		t.Fatal("previous baseline was not preserved", err)
	}
	reports, _ := filepath.Glob(filepath.Join(app.integrityDir(), "report-*.json"))
	if len(reports) != 3 {
		t.Fatal(reports)
	}
}
