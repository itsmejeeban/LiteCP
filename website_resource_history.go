package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type resourceHistoryPoint struct {
	At        time.Time `json:"at"`
	CPU       *float64  `json:"cpuPercent"`
	Memory    *int64    `json:"memoryBytes"`
	Disk      *int64    `json:"diskBytes"`
	Processes *int      `json:"processes"`
}

func resourceHistoryPath(dir, id string, at time.Time) string {
	return filepath.Join(dir, "resource-usage", id, fmt.Sprintf("%02d.jsonl", (at.Unix()/86400)%32))
}

func (a *App) startWebsiteResourceHistory() {
	if a.system.demo || runtime.GOOS != "linux" {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			a.collectWebsiteResourceUsage(context.Background())
			time.Sleep(3 * time.Second)
			rows, now, _ := a.collectWebsiteResourceUsage(context.Background())
			for _, row := range rows {
				if !websiteCronID.MatchString(row.ID) {
					continue
				}
				u := row.Usage
				point := resourceHistoryPoint{At: now}
				if u.Available {
					point.CPU, point.Memory, point.Processes = u.CPU, &u.Memory, &u.Processes
				}
				if u.Disk != nil && now.Sub(u.DiskAt) < 2*time.Minute {
					point.Disk = u.Disk
				}
				if point.Memory == nil && point.Disk == nil {
					continue
				}
				if err := appendResourceHistory(resourceHistoryPath(a.system.dataDir, row.ID, now), point); err != nil {
					log.Printf("Record website resource usage: %v", err)
				}
			}
		}
	}()
}

func appendResourceHistory(path string, point resourceHistoryPoint) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(point)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	info, err := os.Stat(path)
	if os.IsNotExist(err) || (err == nil && !info.ModTime().UTC().Truncate(24*time.Hour).Equal(point.At.Truncate(24*time.Hour))) {
		return writeAtomic(path, data, 0600)
	}
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func historyPeak[T int | int64 | float64](before, sample *T) *T {
	if sample != nil && (before == nil || *sample > *before) {
		return sample
	}
	return before
}

func (a *App) websiteResourceHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "Use GET")
		return
	}
	site := a.store.SiteByID(r.URL.Query().Get("siteId"))
	if site == nil || !websiteCronID.MatchString(site.ID) {
		writeError(w, 404, "Website not found")
		return
	}
	type window struct{ duration, step time.Duration }
	ranges := map[string]window{
		"minute": {time.Hour, time.Minute}, "hour": {24 * time.Hour, time.Hour},
		"day": {24 * time.Hour, time.Minute}, "week": {7 * 24 * time.Hour, 24 * time.Hour},
		"month": {30 * 24 * time.Hour, 24 * time.Hour},
	}
	span, ok := ranges[r.URL.Query().Get("range")]
	if !ok {
		writeError(w, 400, "Unknown history range")
		return
	}
	now := time.Now().UTC()
	start := now.Truncate(span.step).Add(-span.duration + span.step)
	points := make([]resourceHistoryPoint, int(span.duration/span.step))
	for i := range points {
		points[i].At = start.Add(time.Duration(i) * span.step)
	}
	for day := start.Truncate(24 * time.Hour); !day.After(now); day = day.Add(24 * time.Hour) {
		f, err := os.Open(resourceHistoryPath(a.system.dataDir, site.ID, day))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			writeError(w, 500, "Cannot read resource history")
			return
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			var p resourceHistoryPoint
			if json.Unmarshal(scanner.Bytes(), &p) != nil || p.At.Before(start) || p.At.After(now) {
				continue
			}
			i := int(p.At.Sub(start) / span.step)
			row := &points[i]
			row.CPU = historyPeak(row.CPU, p.CPU)
			row.Memory = historyPeak(row.Memory, p.Memory)
			row.Disk = historyPeak(row.Disk, p.Disk)
			row.Processes = historyPeak(row.Processes, p.Processes)
		}
		f.Close()
		if scanner.Err() != nil {
			writeError(w, 500, "Cannot read resource history")
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"points": points})
}
