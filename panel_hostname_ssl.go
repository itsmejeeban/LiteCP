package main

import (
	"context"
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

//go:embed scripts/install-services-tls.sh
var installServicesTLS string

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
		}
		services := hostnameTLSServiceStatuses(r.Context(), host, port)
		status["services"] = services
		if valid {
			if services[0].Status == "trusted" {
				status["trusted"] = true
				status["expiresAt"] = services[0].ExpiresAt
			} else {
				// The public listener may be bound to a specific address rather
				// than loopback. Verify the configured chain independently.
				level, detail := a.operationsPanelTLS(host)
				status["configured"] = level == "pass"
				status["detail"] = detail
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
		Hostname    string `json:"hostname"`
		AllServices bool   `json:"allServices"`
	}
	if decodeJSONWithLimit(r, &input, 4096) != nil || input.Hostname != host || !validPanelSSLHostname(host) {
		writeError(w, http.StatusBadRequest, "Save and confirm a public server hostname before installing SSL.")
		return
	}
	if !input.AllServices || hostnameTLSServiceStatuses(r.Context(), host, port)[0].Status != "trusted" {
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
		// The host service can finish even if the browser disconnects during reload.
		emailArg := strings.NewReplacer("$", "$$", "%", "%%").Replace(email)
		_, err = runCommandInput(context.Background(), 5*time.Minute, configurePanelHostname, "systemd-run", "--quiet", "--wait", "--pipe", "--collect", "--unit=litecp-panel-hostname-ssl", "--property=RuntimeMaxSec=270", "--", "/bin/bash", "-s", "--", host, strconv.Itoa(port), emailArg)
		if err != nil {
			writeError(w, http.StatusBadGateway, "Hostname SSL setup failed. "+err.Error())
			return
		}
		_ = a.store.AddActivity("Hostname SSL installed", fmt.Sprintf("%s on port %d", host, port), "success")
	}
	if input.AllServices {
		services := hostnameTLSServiceStatuses(r.Context(), host, port)
		ftps, mailTLS := "skip", "skip"
		for _, service := range services[1:] {
			if service.Status == "untrusted" {
				if service.Name == "FTPS" {
					ftps = "sync"
				} else {
					mailTLS = "sync"
				}
			}
		}
		var warnings []string
		if ftps == "sync" || mailTLS == "sync" {
			_, err := runCommandInput(context.Background(), 3*time.Minute, installServicesTLS, "systemd-run", "--quiet", "--wait", "--pipe", "--collect", "--unit=litecp-services-ssl", "--property=RuntimeMaxSec=165", "--", "/bin/bash", "-s", "--", ftps, mailTLS)
			if err != nil {
				warnings = append(warnings, "Some service certificates could not be installed. Check the service statuses and the litecp-services-ssl journal.")
				a.recordOperationsEvent("ssl", host+"-"+time.Now().UTC().Format("2006-01-02T15"), "Service SSL installation needs attention", "One or more service certificates could not be installed. Review Hostname SSL service statuses and the installer journal.", "warning", "settings/hostname")
			}
			services = hostnameTLSServiceStatuses(context.Background(), host, port)
		}
		writeJSON(w, http.StatusOK, map[string]any{"hostname": host, "url": url, "services": services, "warnings": warnings})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"hostname": host, "url": url})
}
