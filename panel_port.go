package main

import (
	_ "embed"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

//go:embed scripts/configure-panel-port.sh
var configurePanelPort string

func currentPanelPort() int {
	for _, path := range []string{"/etc/nginx/sites-available/litecp-panel.conf", "/etc/nginx/conf.d/litecp-panel.conf"} {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		m := regexp.MustCompile(`listen\s+(\d+)\s+ssl;`).FindSubmatch(b)
		if len(m) == 2 {
			p, _ := strconv.Atoi(string(m[1]))
			return p
		}
	}
	return 4321
}

func (a *App) panelPortSettings(w http.ResponseWriter, r *http.Request) {
	if a.system.demo {
		writeError(w, 400, "Panel port settings require an installed server")
		return
	}
	if r.Method == http.MethodGet {
		b, _ := os.ReadFile("/var/lib/litecp/panel-port-pending")
		fields := strings.Fields(string(b))
		pending := 0
		if len(fields) == 3 {
			pending, _ = strconv.Atoi(fields[1])
		}
		writeJSON(w, 200, map[string]int{"port": currentPanelPort(), "pendingPort": pending})
		return
	}
	if r.Method != http.MethodPut {
		writeError(w, 405, "Use GET or PUT")
		return
	}
	var input struct {
		Port    int    `json:"port"`
		Action  string `json:"action"`
		Confirm bool   `json:"confirm"`
	}
	if decodeJSONWithLimit(r, &input, 1024) != nil || !input.Confirm || input.Port < 1024 || input.Port > 65535 || (input.Action != "apply" && input.Action != "confirm") {
		writeError(w, 400, "Confirm a panel port between 1024 and 65535")
		return
	}
	if input.Action == "confirm" {
		_, port, err := net.SplitHostPort(r.Host)
		if err != nil || port != strconv.Itoa(input.Port) {
			writeError(w, 400, "Open the new panel URL before confirming the port")
			return
		}
	}
	// The privileged unit executes outside the panel service filesystem sandbox.
	// A saved helper is required because the rollback timer must survive this call.
	output, err := runCommand(r.Context(), 90*time.Second, "systemd-run", "--quiet", "--wait", "--pipe", "--collect", "--property=RuntimeMaxSec=80", "/bin/bash", "-c", `helper=$(mktemp /var/lib/litecp/panel-port-run.XXXXXXXX); printf '%s' "$1" > "$helper"; chmod 700 "$helper"; exec /bin/bash "$helper" "$2" "$3"`, "litecp-panel-port", configurePanelPort, input.Action, strconv.Itoa(input.Port))
	if err != nil {
		writeError(w, 500, "Panel port change failed: "+err.Error())
		return
	}
	_ = a.store.AddActivity("Panel port updated", input.Action+" port "+strconv.Itoa(input.Port), "warning")
	writeJSON(w, 200, map[string]any{"port": input.Port, "message": strings.TrimSpace(output)})
}
