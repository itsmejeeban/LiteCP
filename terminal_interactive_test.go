package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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
