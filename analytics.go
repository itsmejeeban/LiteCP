package main

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type usageSample struct {
	Time   time.Time `json:"time"`
	CPU    float64   `json:"cpu"`
	Memory float64   `json:"memory"`
	Disk   float64   `json:"disk"`
}

var usageMonitor struct {
	sync.RWMutex
	latest  usageSample
	live    []usageSample
	problem string
}

func (a *App) startUsageMonitor() {
	go func() {
		system := NewSystem(a.system.dataDir) // Keep the sampling CPU delta independent of dashboard requests.
		var lastSave time.Time
		sample := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			point := usageSample{Time: time.Now().UTC(), CPU: system.cpuUsage(), Memory: memoryUsage().Percent, Disk: diskUsage(ctx).Percent}
			usageMonitor.Lock()
			usageMonitor.latest = point
			usageMonitor.live = append(usageMonitor.live, point)
			if len(usageMonitor.live) > 240 {
				usageMonitor.live = usageMonitor.live[1:]
			}
			usageMonitor.Unlock()
			if point.Time.Sub(lastSave) < time.Minute {
				return
			}
			data, err := json.Marshal(point)
			if err == nil {
				var file *os.File
				file, err = os.OpenFile(filepath.Join(system.dataDir, "usage-history.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
				if err == nil {
					_, err = file.Write(append(data, '\n'))
					closeErr := file.Close()
					if err == nil {
						err = closeErr
					}
				}
			}
			usageMonitor.Lock()
			if err != nil {
				usageMonitor.problem = "Usage history could not be saved"
				log.Printf("usage history: %v", err)
			} else {
				usageMonitor.problem = ""
				lastSave = point.Time
			}
			usageMonitor.Unlock()
		}
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		// Allow the CPU delta to accumulate before storing the first sample.
		for range ticker.C {
			sample()
		}
	}()
}
func usageBucket(at time.Time, view string) time.Time {
	at = at.UTC()
	switch view {
	case "hourly":
		return at.Truncate(time.Hour)
	case "daily":
		return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	case "weekly":
		day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
		return day.AddDate(0, 0, -(int(day.Weekday())+6)%7)
	case "monthly":
		return time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	return at
}
func aggregateUsage(samples []usageSample, view string, since time.Time) []usageSample {
	type bucket struct {
		point usageSample
		count int
	}
	bins := map[time.Time]*bucket{}
	for _, point := range samples {
		if point.Time.Before(since) {
			continue
		}
		at := usageBucket(point.Time, view)
		b := bins[at]
		if b == nil {
			b = &bucket{point: usageSample{Time: at}}
			bins[at] = b
		}
		b.count++
		b.point.CPU += point.CPU
		b.point.Memory += point.Memory
		b.point.Disk += point.Disk
	}
	result := make([]usageSample, 0, len(bins))
	for _, b := range bins {
		b.point.CPU /= float64(b.count)
		b.point.Memory /= float64(b.count)
		b.point.Disk /= float64(b.count)
		result = append(result, b.point)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Time.Before(result[j].Time) })
	return result
}
func (a *App) usageAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "Use GET")
		return
	}
	view := r.URL.Query().Get("view")
	durations := map[string]time.Duration{"live": time.Hour, "hourly": 24 * time.Hour, "daily": 30 * 24 * time.Hour, "weekly": 12 * 7 * 24 * time.Hour, "monthly": 366 * 24 * time.Hour}
	duration, ok := durations[view]
	if !ok {
		writeError(w, 400, "Choose live, hourly, daily, weekly or monthly")
		return
	}
	usageMonitor.RLock()
	latest, problem := usageMonitor.latest, usageMonitor.problem
	samples := append([]usageSample{}, usageMonitor.live...)
	usageMonitor.RUnlock()
	if view != "live" {
		samples = nil
		file, err := os.Open(filepath.Join(a.system.dataDir, "usage-history.jsonl"))
		if err == nil {
			defer file.Close()
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				var point usageSample
				if json.Unmarshal(scanner.Bytes(), &point) == nil {
					if !point.Time.Before(time.Now().Add(-duration)) {
						samples = append(samples, point)
					}
				}
			}
			if scanner.Err() != nil {
				writeError(w, 500, "Cannot read usage history")
				return
			}
		} else if !os.IsNotExist(err) {
			writeError(w, 500, "Cannot read usage history")
			return
		}
	}
	points := aggregateUsage(samples, view, time.Now().Add(-duration))
	writeJSON(w, 200, map[string]any{"points": points, "latest": latest, "view": view, "warning": problem, "intervalSeconds": 15})
}
