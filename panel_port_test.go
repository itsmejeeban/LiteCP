package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPanelPortRejectsInvalidChanges(t *testing.T) {
	app := twoFactorTestApp(t)
	app.system.demo = false
	for _, body := range []string{`{"port":22,"action":"apply","confirm":true}`, `{"port":65536,"action":"apply","confirm":true}`, `{"port":5432,"action":"unknown","confirm":true}`, `{"port":5432,"action":"apply"}`, `{"port":5432,"action":"confirm","confirm":true}`} {
		req := httptest.NewRequest(http.MethodPut, "https://panel.test:4321/api/security/panel-port", strings.NewReader(body))
		w := httptest.NewRecorder()
		app.panelPortSettings(w, req)
		if w.Code != 400 {
			t.Fatalf("invalid change accepted: %d", w.Code)
		}
	}
}
