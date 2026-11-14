package main

import (
	"net/url"
	"strconv"
	"strings"
)

func requiredFirewallRemark(rule profileRule, panelPort int, sshPorts []string, webmailPort string) string {
	port := strings.ReplaceAll(rule.Port, ":", "-")
	if rule.Direction == "inbound" {
		if port == strconv.Itoa(panelPort)+"/tcp" {
			return "LiteCP administrator panel: encrypted access to server management."
		}
		if containsString(sshPorts, port) || port == "22/tcp" {
			return "SSH: remote server administration and secure file transfers."
		}
		if webmailPort != "" && port == webmailPort+"/tcp" {
			return "Webmail: encrypted browser access to mailboxes."
		}
		switch port {
		case "80/tcp":
			return "HTTP: website access and certificate validation."
		case "443/tcp":
			return "HTTPS: encrypted website access."
		case "4321/tcp":
			return "LiteCP panel: default administrator HTTPS port retained in the profile."
		case "25/tcp":
			return "SMTP: receives email from other mail servers."
		case "465/tcp":
			return "SMTP over TLS: encrypted outgoing mail submission by mail clients."
		case "587/tcp":
			return "SMTP submission: authenticated outgoing mail with STARTTLS."
		case "993/tcp":
			return "IMAP over TLS: encrypted mailbox access by mail clients."
		case "21/tcp":
			return "FTPS control: file-transfer login and commands."
		case "50000-50049/tcp":
			return "Passive FTPS data: website file uploads and downloads."
		case "53/tcp", "53/udp":
			return "Authoritative DNS: answers queries for hosted DNS zones."
		}
	} else {
		switch port {
		case "53/tcp", "53/udp":
			return "DNS resolution: looks up remote server and domain addresses."
		case "123/udp":
			return "NTP: keeps the server clock synchronized."
		case "80/tcp":
			return "HTTP: downloads and service requests to remote servers."
		case "443/tcp":
			return "HTTPS: encrypted updates, downloads and remote API requests."
		case "22/tcp":
			return "SSH: migrations and secure transfers to remote servers."
		case "25/tcp":
			return "SMTP: delivers outgoing email to remote mail servers."
		case "465/tcp":
			return "SMTP over TLS: connects to encrypted mail submission services."
		case "587/tcp":
			return "SMTP submission: connects to authenticated mail relays."
		case "993/tcp":
			return "IMAP over TLS: connects to remote mailboxes."
		}
	}
	return "Retained LiteCP required-port rule: review the current service configuration."
}
func (a *App) annotateFirewallProfile(state *profileState) {
	webmailPort := ""
	if parsed, err := url.Parse(a.system.WebmailURL()); err == nil {
		webmailPort = parsed.Port()
	}
	sshPorts, panelPort := savedFirewallSSHPorts(a.system.dataDir), currentPanelPort()
	notes, _ := a.readProfileRemarks()
	for i := range state.Rules {
		state.Rules[i].Remarks = requiredFirewallRemark(state.Rules[i], panelPort, sshPorts, webmailPort)
		if note, ok := notes[profileRuleKey(state.Rules[i])]; ok {
			state.Rules[i].Remarks = note
		}
	}
}
