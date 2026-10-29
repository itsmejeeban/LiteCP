package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTerminalSessionBoundToBrowser(t *testing.T) {
	ownerRequest := httptest.NewRequest(http.MethodGet, "/api/terminal/session", nil)
	ownerRequest.AddCookie(&http.Cookie{Name:"litecp_session", Value:"browser-a"})
	session := &interactiveTerminal{owner:terminalOwner(ownerRequest)}
	interactiveTerminals.Lock()
	interactiveTerminals.items["test-browser-ownership"] = session
	interactiveTerminals.Unlock()
	defer func(){ interactiveTerminals.Lock(); delete(interactiveTerminals.items,"test-browser-ownership"); interactiveTerminals.Unlock() }()
	request := httptest.NewRequest(http.MethodGet,"/api/terminal/session?id=test-browser-ownership",nil)
	request.AddCookie(&http.Cookie{Name:"litecp_session",Value:"browser-b"})
	response := httptest.NewRecorder()
	app := &App{}
	app.interactiveTerminal(response,request)
	if response.Code != http.StatusNotFound { t.Fatalf("another browser accessed terminal: %d",response.Code) }
}
