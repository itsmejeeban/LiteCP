package main

import (
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed scripts/enable-selinux.sh
var selinuxEnableScript string

//go:embed scripts/repair-waf-labels.sh
var selinuxWAFScript string

//go:embed scripts/repair-php73-selinux.sh
var selinuxPHP73Script string

//go:embed scripts/selinux-repair-job.sh
var selinuxJobScript string

var selinuxRepairMu sync.Mutex

const selinuxRepairUnit = "litecp-selinux-repair.service"

type selinuxRepairStatus struct {
	State     string     `json:"state"`
	Message   string     `json:"message"`
	StartedAt time.Time  `json:"startedAt,omitempty"`
	Mode      string     `json:"mode"`
	CanEnable bool       `json:"canEnable"`
	Log       string     `json:"log,omitempty"`
	Job       string     `json:"job,omitempty"`
	RetryAt   *time.Time `json:"retryAt,omitempty"`
}

func selinuxRepairAllowed(mode string) bool { return mode == "Permissive" }

func readSELinuxRepairStatus(base string) selinuxRepairStatus {
	value := selinuxRepairStatus{State: "idle"}
	if data, err := os.ReadFile(filepath.Join(base, "status.json")); err == nil {
		if json.Unmarshal(data, &value) != nil {
			return selinuxRepairStatus{State: "error", Message: "The previous repair status could not be read."}
		}
	}
	return value
}

func stageSELinuxRepairScripts(job string) error {
	scripts := map[string]string{
		"enable-selinux.sh":       selinuxEnableScript,
		"repair-waf-labels.sh":    selinuxWAFScript,
		"repair-php73-selinux.sh": selinuxPHP73Script,
		"job.sh":                  selinuxJobScript,
	}
	for name, content := range scripts {
		if err := os.WriteFile(filepath.Join(job, name), []byte(strings.ReplaceAll(content, "\r\n", "\n")), 0700); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) selinuxRepair(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, 405, "Use GET or POST.")
		return
	}
	if a.system.demo {
		writeError(w, 400, "SELinux management requires an installed Linux server.")
		return
	}
	selinuxRepairMu.Lock()
	defer selinuxRepairMu.Unlock()
	base := filepath.Join(a.system.dataDir, "selinux-repair")
	value := readSELinuxRepairStatus(base)
	mode, modeErr := runCommand(r.Context(), 3*time.Second, "getenforce")
	value.Mode = strings.TrimSpace(mode)
	if modeErr != nil {
		value.Mode = "Unknown"
	}
	active, _ := runCommand(r.Context(), 3*time.Second, "systemctl", "is-active", selinuxRepairUnit)
	running := strings.TrimSpace(active) == "active" || strings.TrimSpace(active) == "activating"
	if running {
		value.State = "running"
	}
	if value.State == "running" && !running && time.Since(value.StartedAt) > 15*time.Second {
		value.State = "failed"
		value.Message = "The repair worker stopped before completion. Its independent rollback may still be pending; review the saved log."
	}
	value.CanEnable = selinuxRepairAllowed(value.Mode) && value.State != "running" && (value.State != "failed" || time.Since(value.StartedAt) > 8*time.Minute)
	value.RetryAt = nil
	if value.Mode == "Permissive" && value.State == "failed" && !value.CanEnable {
		retryAt := value.StartedAt.Add(8 * time.Minute)
		value.RetryAt = &retryAt
	}
	if r.Method == http.MethodGet {
		if value.Job != "" && filepath.Base(value.Job) == value.Job && strings.HasPrefix(value.Job, "job-") {
			if log, err := os.Open(filepath.Join(base, value.Job, "output.log")); err == nil {
				defer log.Close()
				if info, e := log.Stat(); e == nil && info.Size() > 16<<10 {
					_, _ = log.Seek(-(16 << 10), io.SeekEnd)
				}
				data, _ := io.ReadAll(io.LimitReader(log, 16<<10))
				value.Log = string(data)
			}
		}
		writeJSON(w, 200, value)
		return
	}
	var input struct {
		Confirm bool `json:"confirm"`
	}
	if decodeJSONWithLimit(r, &input, 1024) != nil || !input.Confirm {
		writeError(w, 400, "Confirm guarded SELinux enablement first.")
		return
	}
	if value.State == "running" {
		writeError(w, 409, "SELinux enablement is already running.")
		return
	}
	if !value.CanEnable {
		message := "SELinux status is unknown. Inspect the server before enabling enforcement."
		if value.Mode == "Permissive" {
			message = "Wait eight minutes after the previous attempt so its rollback guard can finish before retrying."
		}
		if value.Mode == "Disabled" {
			message = "SELinux is disabled at boot. OS-level enablement and relabeling are required; the panel will not reboot the server automatically."
		}
		if value.Mode == "Enforcing" {
			message = "SELinux is already enforcing."
		}
		writeError(w, 409, message)
		return
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		writeError(w, 500, "Cannot prepare the SELinux repair directory.")
		return
	}
	job, err := os.MkdirTemp(base, "job-")
	if err != nil {
		writeError(w, 500, "Cannot prepare the SELinux repair worker.")
		return
	}
	if err := stageSELinuxRepairScripts(job); err != nil {
		writeError(w, 500, "Cannot write the SELinux repair worker.")
		return
	}
	value = selinuxRepairStatus{State: "running", Message: "Preparing a configuration backup and guarded enforcement checks.", Mode: value.Mode, StartedAt: time.Now().UTC(), Job: filepath.Base(job)}
	encoded, _ := json.Marshal(value)
	if err = writeAtomic(filepath.Join(base, "status.json"), encoded, 0600); err != nil {
		writeError(w, 500, "Cannot save SELinux repair status.")
		return
	}
	_, err = runCommand(r.Context(), 10*time.Second, "systemd-run", "--quiet", "--collect", "--unit=litecp-selinux-repair", "--property=RuntimeMaxSec=180", "--property=UMask=0077", "--", "/bin/bash", filepath.Join(job, "job.sh"), job)
	if err != nil {
		value.State = "failed"
		value.Message = "Could not start the guarded SELinux worker. No enablement was confirmed."
		encoded, _ = json.Marshal(value)
		_ = writeAtomic(filepath.Join(base, "status.json"), encoded, 0600)
		writeError(w, 500, value.Message)
		return
	}
	_ = a.store.AddActivity("SELinux enablement started", "Guarded enforcement requested from Security Dashboard", "warning")
	writeJSON(w, http.StatusAccepted, value)
}
