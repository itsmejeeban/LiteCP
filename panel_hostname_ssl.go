package main

import (
	"context"
	"crypto/tls"
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"
)

//go:embed scripts/configure-panel-hostname.sh
var configurePanelHostname string

func validPanelSSLHostname(host string) bool {
	return len(host) <= 253 && domainPattern.MatchString(host) && net.ParseIP(host) == nil
}

func (a *App) panelHostnameSSL(w http.ResponseWriter, r *http.Request) {
	if a.system == nil || a.system.demo {
		writeError(w, http.StatusConflict, "Hostname SSL requires an installed LiteCP server.")
		return
	}
	host, err := os.Hostname()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read the current server hostname.")
		return
	}
	host = strings.ToLower(host)
	port := currentPanelPort()
	url := "https://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/"
	if r.Method == http.MethodGet {
		valid := validPanelSSLHostname(host)
		status := map[string]any{"hostname": host, "validHostname": valid, "trusted": false}
		if valid {
			status["url"] = url
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			dialer := tls.Dialer{Config: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}}
			if connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))); err == nil {
				defer connection.Close()
				state := connection.(*tls.Conn).ConnectionState()
				status["trusted"] = true
				status["expiresAt"] = state.PeerCertificates[0].NotAfter
			}
		}
		writeJSON(w, http.StatusOK, status)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Use GET or POST.")
		return
	}
	var input struct {
		Hostname string `json:"hostname"`
	}
	if decodeJSONWithLimit(r, &input, 4096) != nil || input.Hostname != host || !validPanelSSLHostname(host) {
		writeError(w, http.StatusBadRequest, "Save and confirm a public server hostname before installing SSL.")
		return
	}
	if a.store.SiteByDomain(host) != nil {
		writeError(w, http.StatusConflict, "The server hostname is already a managed website. Use a dedicated panel hostname.")
		return
	}
	email := a.store.Settings().Email
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		writeError(w, http.StatusBadRequest, "Set a valid Administrator email before installing hostname SSL.")
		return
	}
	// The transient service finishes its verification or rollback even if the
	// browser disconnects when Nginx switches certificates.
	_, err = runCommand(context.Background(), 5*time.Minute, "systemd-run", "--quiet", "--wait", "--pipe", "--collect", "--unit=litecp-panel-hostname-ssl", "--property=RuntimeMaxSec=270", "--", "/bin/bash", "-c", configurePanelHostname, "litecp-panel-hostname", host, strconv.Itoa(port), email)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Hostname SSL setup failed. "+err.Error())
		return
	}
	_ = a.store.AddActivity("Hostname SSL installed", fmt.Sprintf("%s on port %d", host, port), "success")
	writeJSON(w, http.StatusOK, map[string]string{"hostname": host, "url": url})
}
