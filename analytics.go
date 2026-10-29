package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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

type directoryUsageEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	IsDir bool   `json:"isDir"`
}

func safeDiskUsagePath(base, relative string) (string, string, error) {
	base = filepath.Clean(base)
	relative = strings.TrimSpace(strings.ReplaceAll(relative, "\\", "/"))
	if strings.HasPrefix(relative, "/") || filepath.IsAbs(filepath.FromSlash(relative)) {
		return "", "", errors.New("absolute paths are not allowed")
	}
	relative = strings.TrimPrefix(relative, "/")
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." {
		clean = ""
	}
	target := filepath.Clean(filepath.Join(base, clean))
	within, relErr := filepath.Rel(base, target)
	if relErr != nil || within == ".." || strings.HasPrefix(within, ".."+string(os.PathSeparator)) {
		return "", "", errors.New("path is outside managed website storage")
	}
	return target, filepath.ToSlash(clean), nil
}

func analyticsDirectorySize(ctx context.Context, path string) (int64, error) {
	var size int64
	err := filepath.WalkDir(path, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// managedDiskUsageRootNames returns the first-level storage directories that
// belong to websites still present in LiteCP's registry. Website removal may
// intentionally retain files for recovery, but retained directories must not
// be presented as active website usage.
func managedDiskUsageRootNames(base string, sites []Site) map[string]struct{} {
	base = filepath.Clean(base)
	allowed := make(map[string]struct{}, len(sites))
	for _, site := range sites {
		root := filepath.Clean(managedSiteRoot(site))
		relative, err := filepath.Rel(base, root)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) > 0 && parts[0] != "" && parts[0] != "." {
			allowed[parts[0]] = struct{}{}
		}
	}
	return allowed
}

func (a *App) directoryDiskUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Use GET")
		return
	}
	const base = "/"
	target, relative, err := safeDiskUsagePath(base, r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if target == "/proc" || strings.HasPrefix(target, "/proc/") || target == "/sys" || strings.HasPrefix(target, "/sys/") || target == "/dev" || strings.HasPrefix(target, "/dev/") || target == "/run" || strings.HasPrefix(target, "/run/") {
		writeError(w, http.StatusBadRequest, "Virtual runtime filesystems do not consume persistent disk storage")
		return
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		writeError(w, http.StatusNotFound, "Directory not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	// du counts allocated blocks and hard links once, including hidden files.
	// Kernel/runtime pseudo-filesystems are excluded from persistent storage.
	cmd := exec.CommandContext(ctx, "du", "-x", "-B1", "--max-depth=1", "--null", "--no-dereference", "--exclude=/proc", "--exclude=/sys", "--exclude=/dev", "--exclude=/run", "--", target)
	output, scanErr := cmd.Output()
	if ctx.Err() != nil {
		writeError(w, http.StatusRequestTimeout, "Disk scan timed out; open a smaller directory and retry")
		return
	}
	entries := make([]directoryUsageEntry, 0)
	var measuredTotal, directoriesBytes int64
	for _, record := range strings.Split(string(output), "\x00") {
		fields := strings.SplitN(record, "\t", 2)
		if len(fields) != 2 { continue }
		size, parseErr := strconv.ParseInt(fields[0], 10, 64)
		if parseErr != nil { continue }
		path := filepath.Clean(fields[1])
		if path == target { measuredTotal = size; continue }
		childRelative, pathErr := filepath.Rel(base, path)
		if pathErr != nil { continue }
		entries = append(entries, directoryUsageEntry{Name: path, Path: filepath.ToSlash(childRelative), Bytes: size, IsDir: true})
		directoriesBytes += size
	}
	if scanErr != nil && len(output) == 0 {
		writeError(w, http.StatusInternalServerError, "Cannot read disk usage for this directory")
		return
	}
	filesBytes := measuredTotal - directoriesBytes
	if filesBytes > 0 {
		entries = append(entries, directoryUsageEntry{Name: "Files in this folder", Path: relative, Bytes: filesBytes})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Bytes > entries[j].Bytes })
	var total int64
	for _, entry := range entries {
		total += entry.Bytes
	}
	parent := ""
	if relative != "" {
		parent = filepath.ToSlash(filepath.Dir(filepath.FromSlash(relative)))
		if parent == "." {
			parent = ""
		}
	}
	var filesystem map[string]int64
	if output, err := exec.CommandContext(ctx, "df", "-B1", "--output=size,used,avail", "--", target).Output(); err == nil {
		fields := strings.Fields(string(output))
		if len(fields) == 6 {
			capacity, e1 := strconv.ParseInt(fields[3], 10, 64)
			used, e2 := strconv.ParseInt(fields[4], 10, 64)
			free, e3 := strconv.ParseInt(fields[5], 10, 64)
			if e1 == nil && e2 == nil && e3 == nil && capacity > 0 && used >= 0 && free >= 0 {
				filesystem = map[string]int64{"capacityBytes": capacity, "usedBytes": used, "freeBytes": free}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"root": base, "path": relative, "parent": parent, "totalBytes": total, "entries": entries, "partial": scanErr != nil, "filesystem": filesystem})
}
