package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWebmailTicketBoundToSessionAndOneUse(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, sessions: map[string]session{"active": {Expires: time.Now().Add(time.Hour)}}}
	consume := func(forwarded bool) int {
		r := httptest.NewRequest("POST", "http://127.0.0.1:9090/internal/webmail-ticket", strings.NewReader(`{"ticket":"test"}`))
		r.RemoteAddr = "127.0.0.1:1234"
		if forwarded {
			r.Header.Set("X-Forwarded-For", "203.0.113.1")
		}
		w := httptest.NewRecorder()
		app.consumeWebmailTicket(w, r)
		return w.Code
	}
	webmailTicketMu.Lock()
	webmailTickets["test"] = webmailLaunch{Session: "active", Expires: time.Now().Add(time.Minute)}
	webmailTicketMu.Unlock()
	if consume(true) != 403 {
		t.Fatal("forwarded request accepted")
	}
	if consume(false) != 403 {
		t.Fatal("missing mailbox accepted")
	}
	webmailTicketMu.Lock()
	_, ok := webmailTickets["test"]
	webmailTicketMu.Unlock()
	if ok {
		t.Fatal("ticket not consumed")
	}
	webmailTicketMu.Lock()
	webmailTickets["test"] = webmailLaunch{Session: "expired", Expires: time.Now().Add(time.Minute)}
	webmailTicketMu.Unlock()
	if consume(false) != 403 {
		t.Fatal("expired session accepted")
	}
	if consume(false) != 403 {
		t.Fatal("ticket replay accepted")
	}
}
func TestWebmailLaunchRequiresAuthentication(t *testing.T) {
	a := &App{}
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, httptest.NewRequest("POST", "/api/mail/webmail-login", strings.NewReader(`{}`)))
	if w.Code != 401 {
		t.Fatalf("got %d", w.Code)
	}
}

func TestWebmailTicketSuccessfulRedemption(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.UpdateSettings(func(settings *Settings) { settings.EmailHostingEnabled = true }); err != nil {
		t.Fatal(err)
	}
	if err = store.AddMailbox(Mailbox{ID: "mail", Address: "qa@example.test", Domain: "example.test"}); err != nil {
		t.Fatal(err)
	}
	system := &System{dataDir: dir}
	cipher, err := system.EncryptSecret("test-service-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "webmail-master.enc"), []byte(cipher), 0600); err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, system: system, sessions: map[string]session{"active": {Expires: time.Now().Add(time.Hour)}}}
	redeem := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://127.0.0.1:9090/internal/webmail-ticket", strings.NewReader(`{"ticket":"success"}`))
		r.RemoteAddr = "127.0.0.1:2345"
		w := httptest.NewRecorder()
		app.consumeWebmailTicket(w, r)
		return w
	}
	webmailTicketMu.Lock()
	webmailTickets["success"] = webmailLaunch{MailboxID: "mail", Address: "qa@example.test", Session: "active", Expires: time.Now().Add(time.Minute)}
	webmailTicketMu.Unlock()
	w := redeem()
	if w.Code != 200 || !strings.Contains(w.Body.String(), "qa@example.test") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("redemption failed: %d", w.Code)
	}
	if redeem().Code != 403 {
		t.Fatal("ticket replay accepted")
	}
	webmailTicketMu.Lock()
	webmailTickets["success"] = webmailLaunch{MailboxID: "mail", Address: "qa@example.test", Session: "active", Expires: time.Now().Add(-time.Second)}
	webmailTicketMu.Unlock()
	if redeem().Code != 403 {
		t.Fatal("expired ticket accepted")
	}
}
