package main

import (
	"log"
	"net"
	"net/http"
)

// Fixed event names and canonical addresses are safe for journald and Fail2ban.
// Never log request bodies, submitted usernames, cookies or authentication data.
func (a *App) authenticationEvent(event string, r *http.Request) {
	switch event {
	case "failed", "throttled", "password_verified":
	default:
		return
	}
	ip := net.ParseIP(clientIP(r))
	if ip == nil {
		return
	}
	log.Printf("LITECP_AUTH event=%s remote=%s", event, ip.String())
}
