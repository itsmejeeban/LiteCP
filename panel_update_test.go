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
