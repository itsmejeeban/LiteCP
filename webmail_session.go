package main

import (
	"errors"
	"net/http"
	"time"
)

// A trusted browser may outlive the in-memory session after a panel restart.
// Issue a normal revocable session before binding a one-use webmail ticket.
func (a *App) webmailSession(w http.ResponseWriter, r *http.Request) (string, error) {
	if cookie, err := r.Cookie("litecp_session"); err == nil && a.validSession(cookie.Value) {
		return cookie.Value, nil
	}
	if !requestIsHTTPS(r) || !a.trustedRequest(r) {
		return "", errors.New("administrator authentication required")
	}
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(12 * time.Hour)
	a.rememberSession(token, expires)
	http.SetCookie(w, &http.Cookie{Name: "litecp_session", Value: token, Path: "/api/", Expires: expires, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	return token, nil
}
