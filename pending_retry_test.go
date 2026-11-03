package main

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestPendingRetryQueuesRestore(t *testing.T) {
	app := &App{system: &System{dataDir: t.TempDir()}}
	id := "abcdefghijklmnop1234"
	root, _ := app.liveJobRoot(id)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	job := LiveMigrationJob{ID: id, Status: "restore-review", NativeReady: true}
	if err := app.saveLiveJob(&job); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	app.autoMigrationAction(response, httptest.NewRequest("POST", "/", strings.NewReader(`{"id":"`+id+`","background":true}`)))
	if response.Code != 202 {
		t.Fatalf("%d: %s", response.Code, response.Body.String())
	}
	saved, err := app.loadLiveJob(id)
	if err != nil || saved.Status != "review" || !saved.AutoRestore {
		t.Fatalf("not queued: %+v %v", saved, err)
	}
}
