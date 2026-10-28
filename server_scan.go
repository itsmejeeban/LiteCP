package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

//go:embed scripts/server-malware-scan.py
var serverScanWorker string

func scanCalendar(frequency, at string) (string, error) {
	parsed, err := time.Parse("15:04", at)
	if err != nil {
		return "", fmt.Errorf("Choose a valid scan time")
	}
	prefix := ""
	switch frequency {
	case "daily":
		prefix = "* * *"
	case "weekly":
		prefix = "* * 0"
	case "monthly":
		prefix = "1 * *"
	case "disabled":
		return "", nil
	default:
		return "", fmt.Errorf("Choose daily, weekly, monthly or disabled")
	}
	return fmt.Sprintf("%d %d %s root /usr/bin/nice -n 19 /usr/local/bin/litecp scan-server\n", parsed.Minute(), parsed.Hour(), prefix), nil
}

func (a *App) serverScan(w http.ResponseWriter, r *http.Request) {
	if a.system.demo {
		writeError(w, 400, "Server scans require the installed server.")
		return
	}
	root := filepath.Join(a.system.dataDir, "server-scan")
	if err := os.MkdirAll(root, 0700); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		status := json.RawMessage(`{"state":"idle"}`)
		if content, err := os.ReadFile(filepath.Join(root, "status.json")); err == nil && json.Valid(content) {
			status = content
		}
		schedule := json.RawMessage(`{"frequency":"disabled","time":"02:00"}`)
		if content, err := os.ReadFile(filepath.Join(root, "schedule.json")); err == nil && json.Valid(content) {
			schedule = content
		}
		writeJSON(w, 200, map[string]any{"available": a.system.AntivirusAvailable(), "status": status, "schedule": schedule})
	case http.MethodPost:
		if !a.system.AntivirusAvailable() {
			writeError(w, 400, "Install ClamAV antivirus support before scanning.")
			return
		}
		_, err := runCommand(r.Context(), 10*time.Second, "systemd-run", "--unit=litecp-server-malware-scan", "--collect", "--property=Nice=19", "--property=IOSchedulingClass=idle", "--property=RuntimeMaxSec=86400", "/usr/local/bin/litecp", "scan-server")
		if err != nil {
			writeError(w, 502, "Cannot start scan (another scan may already be running): "+err.Error())
			return
		}
		a.store.AddActivity("Server malware scan started", "Report-only scan; files are not deleted.", "info")
		writeJSON(w, 202, map[string]string{"state": "starting"})
	case http.MethodPut:
		var input struct {
			Frequency string `json:"frequency"`
			Time      string `json:"time"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		line, err := scanCalendar(input.Frequency, input.Time)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if input.Frequency != "disabled" && !a.system.AntivirusAvailable() {
			writeError(w, 400, "Install ClamAV before scheduling scans.")
			return
		}
		if err = os.WriteFile("/etc/cron.d/litecp-malware-scan", []byte("# LiteCP server malware scan (server time zone)\n"+line), 0644); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		content, _ := json.Marshal(input)
		if err = os.WriteFile(filepath.Join(root, "schedule.json"), content, 0600); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		a.store.AddActivity("Malware schedule updated", input.Frequency, "info")
		writeJSON(w, 200, input)
	default:
		w.Header().Set("Allow", "GET, POST, PUT")
		writeError(w, 405, "Method not allowed.")
	}
}

func runServerScan() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("Server scans require root")
	}
	command := exec.Command("/usr/bin/python3", "-c", serverScanWorker)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}
