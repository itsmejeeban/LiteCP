package main

import (
	"context"
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

func TestOverviewMetricsBackupScope(t *testing.T) {
	a := &App{store: &Store{cfg: Config{Backups: []Backup{{ID: "backup", Path: "private-archive"}}}}}
	for _, role := range []string{"viewer", "websites", "email", "backups", ""} {
		request := httptest.NewRequest("GET", "/api/overview/metrics?backups=1", nil)
		if role != "" {
			request = request.WithContext(context.WithValue(request.Context(), operatorContextKey{}, &PanelOperator{Role: role}))
		}
		response := httptest.NewRecorder()
		a.overviewMetrics(response, request)
		var result struct{ Backups []Backup }
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if got, want := len(result.Backups) == 1, role == "backups" || role == ""; got != want {
			t.Errorf("backup history access for role %q = %v, want %v", role, got, want)
		}
	}
	request := httptest.NewRequest("GET", "/api/overview/metrics?backups=1", nil)
	request = request.WithContext(context.WithValue(request.Context(), externalAPIGrantKey{}, externalAPIGrant{Path: request.URL.Path, Method: "GET"}))
	response := httptest.NewRecorder()
	a.overviewMetrics(response, request)
	if response.Code != 403 {
		t.Fatalf("metrics API grant returned backup history, status %d", response.Code)
	}
}
