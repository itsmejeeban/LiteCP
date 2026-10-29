package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const terminalOutputLimit = 1 << 20

type terminalRequest struct {
	Command string `json:"command"`
	CWD     string `json:"cwd"`
}

type cappedBuffer struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	remaining int
	truncated bool
}

func (w *cappedBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	original := len(p)
	if len(p) > w.remaining {
		p = p[:w.remaining]
		w.truncated = true
	}
	_, _ = w.buffer.Write(p)
	w.remaining -= len(p)
	return original, nil
}

func (a *App) terminalCommand(w http.ResponseWriter, r *http.Request) {
	var request terminalRequest
	if err := decodeJSONWithLimit(r, &request, 16<<10); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid terminal request.")
		return
	}
	request.Command = strings.TrimSpace(request.Command)
	request.CWD = strings.TrimSpace(request.CWD)
	if request.Command == "" || len(request.Command) > 4096 {
		writeError(w, http.StatusBadRequest, "Enter a command up to 4096 characters.")
		return
	}
	if request.CWD == "" {
		request.CWD = "/root"
	}
	if !filepath.IsAbs(request.CWD) {
		writeError(w, http.StatusBadRequest, "Working directory must be an absolute path.")
		return
	}
	info, err := os.Stat(request.CWD)
	if err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "Working directory does not exist.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/bash", "--noprofile", "--norc", "-lc", request.Command)
	command.Dir = request.CWD
	command.Env = []string{"HOME=/root", "LANG=C.UTF-8", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "TERM=xterm-256color"}
	command.Stdin = nil
	output := &cappedBuffer{remaining: terminalOutputLimit}
	command.Stdout = output
	command.Stderr = output
	err = command.Run()
	exitCode := 0
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		} else if ctx.Err() == nil {
			writeError(w, http.StatusInternalServerError, "Command could not be started.")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"output": output.buffer.String(), "exitCode": exitCode, "cwd": request.CWD,
		"timedOut": ctx.Err() == context.DeadlineExceeded, "truncated": output.truncated,
	})
}
