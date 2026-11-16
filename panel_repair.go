package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

const panelRepairOutputLimit = 32 << 10

type panelRepairStatus struct {
	Status     string     `json:"status"`
	Mode       string     `json:"mode,omitempty"`
	Output     string     `json:"output"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// The broker owns the current run. A browser disconnect does not cancel it.
type panelRepairJob struct {
	mu        sync.Mutex
	status    panelRepairStatus
	output    bytes.Buffer
	truncated bool
}

func (j *panelRepairJob) snapshot() panelRepairStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	status := j.status
	if status.Status == "" {
		status.Status = "idle"
	}
	status.Output = j.output.String()
	if j.truncated {
		status.Output += "\n[INFO] Diagnostic output reached its size limit.\n"
	}
	return status
}

func (j *panelRepairJob) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	remaining := panelRepairOutputLimit - j.output.Len()
	if len(p) > remaining {
		j.output.Write(p[:remaining])
		j.truncated = true
	} else {
		j.output.Write(p)
	}
	return len(p), nil
}

func (j *panelRepairJob) start(mode string, run func(io.Writer) string) (panelRepairStatus, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.status.Status == "running" {
		return panelRepairStatus{}, false
	}
	now := time.Now().UTC()
	j.status = panelRepairStatus{Status: "running", Mode: mode, StartedAt: &now}
	j.output.Reset()
	j.truncated = false
	go func() {
		status := run(j)
		finished := time.Now().UTC()
		j.mu.Lock()
		j.status.Status = status
		j.status.FinishedAt = &finished
		j.mu.Unlock()
	}()
	return j.status, true
}

func runPanelDoctor(dataDir, mode string, output io.Writer) string {
	flag := "--panel-check"
	if mode == "repair" {
		flag = "--panel-repair"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/bash", "--noprofile", "--norc", "-s", "--", flag)
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"LC_ALL=C",
		"LITECP_DATA_DIR=" + dataDir,
	}
	cmd.Stdin = strings.NewReader(doctorScript)
	cmd.Stdout, cmd.Stderr = output, output
	cmd.WaitDelay = 3 * time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		io.WriteString(output, "\n[ATTENTION] Diagnostics reached the time limit. Review the results and service status before retrying.\n")
		return "failed"
	}
	if err == nil {
		return "healthy"
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return "issues"
	}
	io.WriteString(output, "\n[ATTENTION] Diagnostics could not finish. Check the LiteCP broker service and try again.\n")
	return "failed"
}

func (a *App) panelRepair(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if validExternalAPIGrant(r) || a.requestOperator(r) != nil {
		writeError(w, http.StatusForbidden, "Repair Panel requires the primary administrator.")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, a.repairJob.snapshot())
	case http.MethodPost:
		var input struct {
			Mode string `json:"mode"`
		}
		if decodeJSONWithLimit(r, &input, 1024) != nil || (input.Mode != "check" && input.Mode != "repair") {
			writeError(w, http.StatusBadRequest, "Select Check only or Check and repair.")
			return
		}
		if a.system.demo || runtime.GOOS != "linux" || os.Geteuid() != 0 {
			writeError(w, http.StatusBadRequest, "Repair Panel requires an installed Linux server.")
			return
		}
		status, ok := a.repairJob.start(input.Mode, func(output io.Writer) string {
			return runPanelDoctor(a.system.dataDir, input.Mode, output)
		})
		if !ok {
			writeError(w, http.StatusConflict, "A panel check is already running.")
			return
		}
		writeJSON(w, http.StatusAccepted, status)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "Use GET or POST.")
	}
}
