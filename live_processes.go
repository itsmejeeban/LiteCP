package main

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type liveProcess struct {
	PID    int     `json:"pid"`
	User   string  `json:"user"`
	State  string  `json:"state"`
	CPU    float64 `json:"cpu"`
	Memory float64 `json:"memory"`
	RSS    int64   `json:"rss"`
	Name   string  `json:"name"`
}

var processSamples struct {
	sync.Mutex
	at    time.Time
	ticks map[int]float64
}

func (a *App) liveProcesses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "Method not allowed")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	// Command names only: never expose arguments or environment credentials.
	output, err := exec.CommandContext(ctx, "ps", "-eo", "pid=,user=,stat=,cputimes=,pmem=,rss=,comm=").Output()
	if err != nil {
		writeError(w, 503, "Unable to read server processes")
		return
	}
	now := time.Now()
	processSamples.Lock()
	defer processSamples.Unlock()
	elapsed := now.Sub(processSamples.at).Seconds()
	next := map[int]float64{}
	entries := []liveProcess{}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 7 {
			continue
		}
		pid, e := strconv.Atoi(fields[0])
		if e != nil {
			continue
		}
		seconds, e := strconv.ParseFloat(fields[3], 64)
		if e != nil {
			continue
		}
		memory, _ := strconv.ParseFloat(fields[4], 64)
		rss, _ := strconv.ParseInt(fields[5], 10, 64)
		cpu := 0.0
		if previous, ok := processSamples.ticks[pid]; ok && elapsed > 0 && seconds >= previous {
			cpu = (seconds - previous) / elapsed * 100
		}
		next[pid] = seconds
		entries = append(entries, liveProcess{pid, fields[1], fields[2], cpu, memory, rss * 1024, strings.Join(fields[6:], " ")})
	}
	processSamples.at = now
	processSamples.ticks = next
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].CPU == entries[j].CPU {
			return entries[i].RSS > entries[j].RSS
		}
		return entries[i].CPU > entries[j].CPU
	})
	usageMonitor.RLock()
	summary := usageMonitor.latest
	usageMonitor.RUnlock()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"processes": entries, "summary": summary, "updatedAt": now.UTC(), "note": fmt.Sprintf("%d processes", len(entries))})
}
