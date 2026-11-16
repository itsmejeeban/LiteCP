package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPanelRepairRunRejectsOverlapAndBoundsOutput(t *testing.T) {
	var job panelRepairJob
	if got := job.snapshot().Status; got != "idle" {
		t.Fatalf("initial status = %q", got)
	}
	release := make(chan struct{})
	defer func() {
		if release != nil {
			close(release)
		}
	}()
	gate := release
	status, started := job.start("repair", func(out io.Writer) string {
		<-gate
		_, _ = io.WriteString(out, strings.Repeat("x", panelRepairOutputLimit+5))
		return "issues"
	})
	if !started || status.Status != "running" || status.StartedAt == nil {
		t.Fatalf("unexpected start: %+v", status)
	}
	if _, started := job.start("check", nil); started {
		t.Fatal("a second run was accepted")
	}
	close(release)
	release = nil
	deadline := time.Now().Add(3 * time.Second)
	for {
		value := job.snapshot()
		if value.Status != "running" {
			if value.Status != "issues" || value.Mode != "repair" || value.FinishedAt == nil ||
				!strings.Contains(value.Output, "size limit") || len(value.Output) > panelRepairOutputLimit+100 {
				t.Fatalf("unexpected status %q or output size %d", value.Status, len(value.Output))
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("job did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPanelRepairRouteRequiresPrimarySameOriginSession(t *testing.T) {
	a := apiTestApp(t)
	a.store.cfg.Operators = []PanelOperator{{ID: "site-admin", Role: "websites", Enabled: true, Revision: "rev"}}
	a.sessions["operator-session"] = session{Expires: time.Now().Add(time.Hour), OperatorID: "site-admin", OperatorRevision: "rev"}
	handler := a.routes()
	tests := []struct {
		method, session, origin, body string
		want                          int
	}{
		{http.MethodGet, "", "", "", http.StatusUnauthorized},
		{http.MethodGet, "operator-session", "", "", http.StatusForbidden},
		{http.MethodPost, "operator-session", "https://panel.example", `{"mode":"repair"}`, http.StatusForbidden},
		{http.MethodPost, "test-session", "https://other.example", `{"mode":"repair"}`, http.StatusForbidden},
		{http.MethodGet, "test-session", "", "", http.StatusOK},
		{http.MethodPost, "test-session", "https://panel.example", `{"mode":"other"}`, http.StatusBadRequest},
		{http.MethodPost, "test-session", "https://panel.example", `{"mode":"repair"}`, http.StatusBadRequest},
	}
	for _, tc := range tests {
		r := httptest.NewRequest(tc.method, "https://panel.example/api/panel-repair", strings.NewReader(tc.body))
		if tc.session != "" {
			r.AddCookie(&http.Cookie{Name: "litecp_session", Value: tc.session})
		}
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s session %q origin %q: got %d, want %d (%s)", tc.method, tc.session, tc.origin, w.Code, tc.want, w.Body.String())
		}
	}
}
