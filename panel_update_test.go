package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPanelUpdateRejectsDemoAndInvalidRequests(t *testing.T) {
	app := &App{system: &System{demo: true}}
	for _, body := range []string{`{broken`, `{"version":"v999.0.0"}`} {
		response := httptest.NewRecorder()
		app.startPanelUpdate(response, httptest.NewRequest("POST", "/api/updates/install", strings.NewReader(body)))
		if response.Code != 400 {
			t.Fatalf("expected rejection, got %d", response.Code)
		}
	}
}

func TestPanelUpdateRejectsUnadvertisedRelease(t *testing.T) {
	releaseCache.Lock()
	oldUntil, oldTag, oldChecked := releaseCache.until, releaseCache.tag, releaseCache.checked
	releaseCache.until = time.Now().Add(time.Hour)
	releaseCache.tag = "v999.0.0"
	releaseCache.checked = true
	releaseCache.Unlock()
	defer func() {
		releaseCache.Lock()
		releaseCache.until, releaseCache.tag, releaseCache.checked = oldUntil, oldTag, oldChecked
		releaseCache.Unlock()
	}()
	app := &App{system: &System{}}
	response := httptest.NewRecorder()
	app.startPanelUpdate(response, httptest.NewRequest("POST", "/api/updates/install", strings.NewReader(`{"version":"v998.0.0"}`)))
	if response.Code != 409 {
		t.Fatalf("unadvertised release accepted: %d", response.Code)
	}
}

func TestPanelRestorePointsRejectInvalidMutation(t *testing.T) {
	app := &App{system: &System{demo: true}}
	for _, body := range []string{`{}`, `{"limit":null}`, `{"limit":-1}`, `{"limit":11}`, `{"limit":1.5}`} {
		response := httptest.NewRecorder()
		app.panelRestorePoints(response, httptest.NewRequest("PUT", "/api/updates/restore-points", strings.NewReader(body)))
		if response.Code != 400 {
			t.Fatalf("invalid retention %s returned %d", body, response.Code)
		}
	}
	for _, id := range []string{"../update-aBcD1234", "update-aBcD1234/source", "rollback-aBcD1234"} {
		response := httptest.NewRecorder()
		app.deletePanelRestorePoint(response, httptest.NewRequest("DELETE", "/api/updates/restore-points/"+id, nil))
		if response.Code != 400 {
			t.Fatalf("invalid restore point %q returned %d", id, response.Code)
		}
	}
}
