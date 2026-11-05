package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestTerminalCapabilitiesSeparatedMode(t *testing.T) {
	previous := os.Args
	os.Args = []string{"litecp", "broker"}
	defer func() { os.Args = previous }()
	response := httptest.NewRecorder()
	app := &App{}
	app.terminalCapabilities(response, httptest.NewRequest(http.MethodGet, "/api/shell-capabilities", nil))
	var result struct {
		Available bool
		Reason    string
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || result.Available || result.Reason == "" {
		t.Fatalf("unexpected capabilities: %s", response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("capabilities must not be cached")
	}
}

func TestTerminalSessionBoundToBrowser(t *testing.T) {
	ownerRequest := httptest.NewRequest(http.MethodGet, "/api/terminal/session", nil)
	ownerRequest.AddCookie(&http.Cookie{Name: "litecp_session", Value: "browser-a"})
	session := &interactiveTerminal{owner: terminalOwner(ownerRequest)}
	interactiveTerminals.Lock()
	interactiveTerminals.items["test-browser-ownership"] = session
	interactiveTerminals.Unlock()
	defer func() {
		interactiveTerminals.Lock()
		delete(interactiveTerminals.items, "test-browser-ownership")
		interactiveTerminals.Unlock()
	}()
	request := httptest.NewRequest(http.MethodGet, "/api/terminal/session?id=test-browser-ownership", nil)
	request.AddCookie(&http.Cookie{Name: "litecp_session", Value: "browser-b"})
	response := httptest.NewRecorder()
	app := &App{}
	app.interactiveTerminal(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("another browser accessed terminal: %d", response.Code)
	}
}

func TestTerminalRevocationPreservesOtherBrowsers(t *testing.T) {
	owner := terminalOwner(httptest.NewRequest("GET", "/", nil))
	other := owner
	other[0] ^= 1
	mine := &interactiveTerminal{owner: owner}
	theirs := &interactiveTerminal{owner: other}
	interactiveTerminals.Lock()
	interactiveTerminals.items["revoke-mine"] = mine
	interactiveTerminals.items["revoke-other"] = theirs
	interactiveTerminals.Unlock()
	defer func() {
		interactiveTerminals.Lock()
		delete(interactiveTerminals.items, "revoke-mine")
		delete(interactiveTerminals.items, "revoke-other")
		interactiveTerminals.Unlock()
	}()
	closeOwnedTerminals(owner)
	interactiveTerminals.Lock()
	_, remains := interactiveTerminals.items["revoke-mine"]
	preserved := interactiveTerminals.items["revoke-other"] == theirs
	interactiveTerminals.Unlock()
	if remains || !preserved || !mine.closed || theirs.closed {
		t.Fatal("terminal revocation crossed browser boundary or retained shell")
	}
	closeOwnedTerminals(owner) // Revocation is idempotent.
}
