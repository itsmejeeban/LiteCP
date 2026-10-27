package main

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type cronTestOutput struct {
	sync.Mutex
	data      []byte
	truncated bool
}

func (o *cronTestOutput) Write(p []byte) (int, error) {
	o.Lock()
	defer o.Unlock()
	n := len(p)
	remaining := 64*1024 - len(o.data)
	if len(p) > remaining {
		o.truncated = true
		p = p[:remaining]
	}
	o.data = append(o.data, p...)
	return n, nil
}

func (a *App) runWebsiteCron(w http.ResponseWriter, r *http.Request) {
	site := a.store.SiteByID(r.URL.Query().Get("siteId"))
	if site == nil {
		writeError(w, 404, "Website not found")
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if decodeJSONWithLimit(r, &body, 1024) != nil {
		writeError(w, 400, "Invalid cron job request")
		return
	}
	jobs, err := a.system.websiteCronJobs(site.ID)
	if err != nil {
		writeError(w, 500, "Cannot read cron jobs")
		return
	}
	var selected *WebsiteCronJob
	for i := range jobs {
		if jobs[i].ID == body.ID {
			selected = &jobs[i]
			break
		}
	}
	if selected == nil {
		writeError(w, 404, "Cron job not found for this website")
		return
	}
	if a.system.demo {
		writeError(w, 400, "Cron testing requires the installed Linux server")
		return
	}
	if _, err = a.system.renderWebsiteCron(*site, []WebsiteCronJob{*selected}); err != nil {
		writeError(w, 400, "This website does not have a valid isolated runtime")
		return
	}
	_, script, err := a.system.websiteCronScript(*site, *selected)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	args := []string{"--quiet", "--wait", "--pipe", "--collect", "--service-type=exec", "--property=RuntimeMaxSec=30", "--property=TimeoutStopSec=2", "--property=KillMode=control-group", "--property=NoNewPrivileges=yes", "--property=MemoryMax=256M", "--property=TasksMax=32", fmt.Sprintf("--property=User=%d", site.RuntimeUID), fmt.Sprintf("--property=Group=%d", site.RuntimeGID), "--setenv=PATH=/usr/local/bin:/usr/bin:/bin", "--", "/bin/sh"}
	command := exec.CommandContext(ctx, "systemd-run", args...)
	command.Stdin = strings.NewReader(string(script))
	output := &cronTestOutput{}
	command.Stdout = output
	command.Stderr = output
	started := time.Now()
	err = command.Run()
	message := "Completed successfully"
	if err != nil {
		message = "Command failed or reached the 30-second limit"
	}
	text := strings.TrimSpace(string(output.data))
	if output.truncated {
		text += "\n[Output truncated at 64 KiB]"
	}
	a.store.AddActivity("Cron job tested", site.Domain+": "+message, "info")
	writeJSON(w, 200, map[string]any{"success": err == nil, "message": message, "output": text, "durationMs": time.Since(started).Milliseconds()})
}
