package main

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"
)

type securityCheck struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type securityPosture struct {
	CheckedAt time.Time       `json:"checkedAt"`
	Checks    []securityCheck `json:"checks"`
}

type securityProbe func(string, ...string) (string, error)

// Read-only probes use fixed commands and arguments, never user input.
func collectSecurityPosture(probe securityProbe, uid int) securityPosture {
	result := securityPosture{CheckedAt: time.Now().UTC(), Checks: []securityCheck{}}
	add := func(id, label, status, detail string) {
		result.Checks = append(result.Checks, securityCheck{id, label, status, detail})
	}
	if uid == 0 {
		add("privilege", "Panel privileges", "critical", "The web application runs as root. A staged privilege split is required.")
	} else {
		add("privilege", "Panel privileges", "info", "The web process is not root. Privileged helper boundaries still require verification.")
	}
	value, err := probe("getenforce")
	if err != nil {
		add("selinux", "SELinux", "unknown", "Unable to read enforcement status.")
	} else if strings.TrimSpace(value) == "Enforcing" {
		add("selinux", "SELinux", "good", "Enforcing. Application-specific policy coverage has not been audited.")
	} else {
		add("selinux", "SELinux", "critical", "Not enforcing. Review policy compatibility before changing this setting.")
	}
	for _, item := range []struct{ id, label, unit string }{{"firewall", "Firewall", "firewalld"}, {"fail2ban", "Brute-force service", "fail2ban"}, {"postfix", "SMTP service", "postfix"}, {"dovecot", "Mailbox service", "dovecot"}, {"opendkim", "DKIM signer", "opendkim"}} {
		output, e := probe("systemctl", "is-active", item.unit)
		if item.id == "firewall" && (e != nil || strings.TrimSpace(output) != "active") {
			// Ubuntu installations can use UFW instead of firewalld. Its
			// effective status, not its oneshot systemd unit, is authoritative.
			ufw, ufwErr := probe("ufw", "status")
			if ufwErr == nil && strings.HasPrefix(strings.TrimSpace(ufw), "Status: active") {
				add(item.id, item.label, "good", "UFW is active; this does not verify every firewall rule.")
				continue
			}
			if ufwErr == nil && strings.HasPrefix(strings.TrimSpace(ufw), "Status: inactive") {
				add(item.id, item.label, "warning", "UFW is inactive and firewalld is not active.")
				continue
			}
		}
		if strings.TrimSpace(output) == "active" && e == nil {
			add(item.id, item.label, "good", "Service is active; this does not verify every security setting.")
		} else if strings.TrimSpace(output) == "inactive" || strings.TrimSpace(output) == "failed" {
			add(item.id, item.label, "warning", "Service is not active.")
		} else {
			add(item.id, item.label, "unknown", "Service state could not be verified.")
		}
	}
	output, err := probe("fail2ban-client", "status")
	if err != nil {
		add("ssh-jail", "SSH protection", "unknown", "Unable to inspect Fail2ban jails.")
	} else {
		protected := false
		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(line, "Jail list:") {
				for _, jail := range strings.Split(strings.SplitN(line, "Jail list:", 2)[1], ",") {
					if strings.TrimSpace(jail) == "sshd" {
						protected = true
					}
				}
			}
		}
		if protected {
			add("ssh-jail", "SSH protection", "good", "An sshd jail is listed. Its effective rules and recent events need separate review.")
		} else {
			add("ssh-jail", "SSH protection", "warning", "An sshd jail was not found in the available jail list.")
		}
	}
	output, err = probe("sshd", "-T")
	if err != nil {
		add("ssh-auth", "SSH authentication", "unknown", "Effective SSH configuration could not be read.")
	} else if strings.Contains("\n"+output, "\npermitrootlogin yes") && strings.Contains("\n"+output, "\npasswordauthentication yes") {
		add("ssh-auth", "SSH authentication", "warning", "Root and password login are permitted. Confirm an alternative access method before restricting them.")
	} else {
		add("ssh-auth", "SSH authentication", "info", "Effective global SSH settings were read. Per-user Match rules and alternate access remain to be checked.")
	}
	add("mfa", "Administrator 2FA", "warning", "TOTP enrollment is not implemented in this version.")
	add("integrity", "File integrity monitoring", "warning", "Trusted baselines and change monitoring are not implemented.")
	add("ddos", "DDoS protection", "unknown", "No application-level attack monitor is configured. Large attacks require upstream protection.")
	add("backup", "Backup security", "warning", "Private storage and archive validation exist. Archive encryption and off-server restore acceptance need verification.")
	add("audit", "Security events", "info", "Authentication events go to the service journal. Independent, tamper-resistant log retention is not verified.")
	return result
}

func (a *App) securityPosture(w http.ResponseWriter, r *http.Request) {
	if a.system.demo {
		writeJSON(w, 200, securityPosture{CheckedAt: time.Now().UTC(), Checks: []securityCheck{{"demo", "Security status", "unknown", "Live security status is unavailable in demo mode."}}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result := collectSecurityPosture(func(command string, args ...string) (string, error) {
		return runCommand(ctx, 2*time.Second, command, args...)
	}, os.Geteuid())
	for i := range result.Checks {
		if result.Checks[i].ID == "mfa" {
			if a.store.twoFactor().Enabled {
				result.Checks[i].Status = "good"
				result.Checks[i].Detail = "Authenticator verification is enabled. Keep recovery codes offline."
			} else {
				result.Checks[i].Detail = "Authenticator verification is not enabled. Complete enrollment in Security > 2FA."
			}
		}
	}
	if a.system.AntivirusAvailable() {
		result.Checks = append(result.Checks, securityCheck{"malware", "Malware scanner", "info", "ClamAV is installed. A completed scan and signature freshness have not been verified."})
	} else {
		result.Checks = append(result.Checks, securityCheck{"malware", "Malware scanner", "warning", "ClamAV is not installed."})
	}
	engine := a.system.WAFEngine()
	if engine == "" || engine == "not installed" {
		result.Checks = append(result.Checks, securityCheck{"waf", "WAF", "warning", "A compatible WAF engine was not detected."})
	} else {
		result.Checks = append(result.Checks, securityCheck{"waf", "WAF", "info", "An engine was detected. Website modes, rules and false positives still need review."})
	}
	writeJSON(w, 200, result)
}
