package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestOverviewShowsOnlyActiveBackupRequests(t *testing.T) {
	a := &App{store: &Store{cfg: Config{Backups: []Backup{{ID: "queued", Domain: "example.com", Status: "queued"}, {ID: "finished", Status: "complete"}, {ID: "failed", Status: "remote failed"}}}}, system: &System{dataDir: t.TempDir()}}
	response := httptest.NewRecorder()
	a.overviewJobs(response, httptest.NewRequest("GET", "/api/overview/jobs", nil))
	var result struct {
		Items []overviewJob `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].ID != "backup-queued" || result.Items[0].Percent != nil {
		t.Fatalf("unexpected active jobs: %+v", result.Items)
	}
}
