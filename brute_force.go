package main

import (
	_ "embed"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed scripts/configure-brute-force.sh
var bruteForceScript string
var bruteForceChange sync.Mutex
var bruteForceJails = []string{"sshd", "postfix", "postfix-sasl", "dovecot"}

type bruteForceRule struct {
	Action   string `json:"action"`
	Jail     string `json:"jail"`
	MaxRetry int    `json:"maxRetry"`
	FindTime int    `json:"findTime"`
	BanTime  int    `json:"banTime"`
	Confirm  bool   `json:"confirm"`
}

func validBruteForceRule(v bruteForceRule) bool {
	allowed := false
	for _, jail := range bruteForceJails {
		if v.Jail == jail {
			allowed = true
		}
	}
	return allowed && v.MaxRetry >= 1 && v.MaxRetry <= 100 && v.FindTime >= 60 && v.FindTime <= 86400 && v.BanTime >= 60 && v.BanTime <= 2592000
}
func (a *App) bruteForceProtection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if a.system.demo {
		writeError(w, 400, "Brute-force protection requires an installed server.")
		return
	}
	if r.Method == http.MethodGet {
		probe := func(name string, args ...string) (string, error) {
			return runCommand(r.Context(), 4*time.Second, name, args...)
		}
		load, _ := probe("systemctl", "show", "fail2ban", "--property=LoadState", "--value")
		active, _ := probe("systemctl", "is-active", "fail2ban")
		installed := strings.TrimSpace(load) == "loaded"
		running := strings.TrimSpace(active) == "active"
		jails := []map[string]any{}
		if running {
			for _, jail := range bruteForceJails {
				status, err := probe("fail2ban-client", "status", jail)
				if err != nil {
					continue
				}
				values := map[string]any{"name": jail, "status": status}
				for _, setting := range []string{"maxretry", "findtime", "bantime"} {
					value, err := probe("fail2ban-client", "get", jail, setting)
					number, parseErr := strconv.Atoi(strings.TrimSpace(value))
					if err == nil && parseErr == nil {
						values[setting] = number
					}
				}
				jails = append(jails, values)
			}
		}
		logs, logErr := probe("journalctl", "-u", "fail2ban", "--since", "24 hours ago", "-n", "500", "--no-pager", "-o", "short-iso")
		fileLogs, fileErr := probe("tail", "-n", "500", "/var/log/fail2ban.log")
		// Fail2ban commonly logs to its file rather than the system journal.
		if fileErr == nil && strings.TrimSpace(fileLogs) != "" {
			logs = fileLogs
			logErr = nil
		}
		events := []string{}
		for _, line := range strings.Split(logs, "\n") {
			if strings.Contains(line, "Found ") || strings.Contains(line, "Ban ") || strings.Contains(line, "Unban ") {
				events = append(events, line)
			}
		}
		if len(events) > 200 {
			events = events[len(events)-200:]
		}
		writeJSON(w, 200, map[string]any{"installed": installed, "running": running, "jails": jails, "events": events, "logsAvailable": logErr == nil})
		return
	}
	if r.Method != http.MethodPut {
		writeError(w, 405, "Use GET or PUT.")
		return
	}
	var input bruteForceRule
	if decodeJSONWithLimit(r, &input, 2048) != nil || (input.Action != "enable" && input.Action != "disable" && input.Action != "rules") || (input.Action == "disable" && !input.Confirm) || (input.Action == "rules" && !validBruteForceRule(input)) {
		writeError(w, 400, "Choose a valid action and rule limits. Confirm before disabling protection.")
		return
	}
	if !bruteForceChange.TryLock() {
		writeError(w, 409, "A protection update is already running.")
		return
	}
	defer bruteForceChange.Unlock()
	_, err := runCommand(r.Context(), 60*time.Second, "systemd-run", "--quiet", "--wait", "--pipe", "--collect", "--property=RuntimeMaxSec=50", "/bin/bash", "-c", strings.TrimPrefix(bruteForceScript, "\ufeff"), "litecp-brute-force", input.Action, input.Jail, strconv.Itoa(input.MaxRetry), strconv.Itoa(input.FindTime), strconv.Itoa(input.BanTime))
	if err != nil {
		writeError(w, 500, "Protection update failed: "+err.Error())
		return
	}
	_ = a.store.AddActivity("Brute-force protection updated", input.Action+" "+input.Jail, "warning")
	writeJSON(w, 200, map[string]string{"message": "Brute-force protection updated."})
}
