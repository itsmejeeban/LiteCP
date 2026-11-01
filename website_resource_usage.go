package main

import (
	"context"
	"math"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type websiteProcessCounter struct {
	UID, PID     int
	Age, Seconds float64
	RSS          int64
}
type websiteObservedUsage struct {
	Available bool      `json:"available"`
	CPU       *float64  `json:"cpuPercent"`
	Memory    int64     `json:"memoryBytes"`
	Processes int       `json:"processes"`
	Disk      *int64    `json:"diskBytes"`
	DiskAt    time.Time `json:"diskUpdatedAt,omitempty"`
}
type websiteResourceRow struct {
	ID       string                 `json:"id"`
	Domain   string                 `json:"domain"`
	Settings *WebsiteResourceLimits `json:"settings"`
	State    string                 `json:"state"`
	Enforced bool                   `json:"enforced"`
	Usage    websiteObservedUsage   `json:"usage"`
}

var websiteUsageSamples = struct {
	sync.Mutex
	At       time.Time
	Counters map[int]websiteProcessCounter
}{Counters: map[int]websiteProcessCounter{}}
var websiteDiskSamples = struct {
	sync.Mutex
	Busy  bool
	At    time.Time
	Sizes map[string]int64
}{Sizes: map[string]int64{}}

func parseWebsiteProcesses(output string) map[int]websiteProcessCounter {
	result := map[int]websiteProcessCounter{}
	for _, line := range strings.Split(output, "\n") {
		f := strings.Fields(line)
		if len(f) != 5 {
			continue
		}
		uid, e1 := strconv.Atoi(f[0])
		pid, e2 := strconv.Atoi(f[1])
		age, e3 := strconv.ParseFloat(f[2], 64)
		seconds, e4 := strconv.ParseFloat(f[3], 64)
		rss, e5 := strconv.ParseInt(f[4], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || uid <= 0 || pid <= 0 || age < 0 || seconds < 0 || rss < 0 || rss > math.MaxInt64/1024 || math.IsNaN(age) || math.IsInf(age, 0) || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
			continue
		}
		result[pid] = websiteProcessCounter{uid, pid, age, seconds, rss * 1024}
	}
	return result
}

func aggregateWebsiteProcesses(uid int, before, now map[int]websiteProcessCounter, elapsed float64) websiteObservedUsage {
	u := websiteObservedUsage{Available: uid > 0}
	if uid <= 0 {
		return u
	}
	cpu := 0.0
	for pid, p := range now {
		if p.UID != uid {
			continue
		}
		u.Memory += p.RSS
		u.Processes++
		old, ok := before[pid]
		// Process IDs may be reused. Require a matching UID, age progression
		// and increasing CPU counter before computing an interval percentage.
		if ok && old.UID == uid && elapsed > 0 && elapsed <= 30 && p.Seconds >= old.Seconds && math.Abs(p.Age-old.Age-elapsed) <= 2.1 {
			cpu += (p.Seconds - old.Seconds) / elapsed * 100
		}
	}
	if elapsed > 0 && elapsed <= 30 {
		u.CPU = &cpu
	}
	return u
}

func websiteDiskKey(data string, site Site) string {
	return data + "\x00" + site.ID + "\x00" + managedSiteRoot(site)
}

// Disk scans are bounded and cached for one minute, independently of the
// three-second process polling. No files or quotas are modified.
func refreshWebsiteDiskSamples(data string, sites []Site) {
	websiteDiskSamples.Lock()
	if websiteDiskSamples.Busy || time.Since(websiteDiskSamples.At) < time.Minute {
		websiteDiskSamples.Unlock()
		return
	}
	websiteDiskSamples.Busy = true
	websiteDiskSamples.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		args := []string{"-sx", "-B1", "--null", "--"}
		roots := map[string][]string{}
		for _, site := range sites {
			root := managedSiteRoot(site)
			if root == "" {
				continue
			}
			if _, exists := roots[root]; !exists {
				args = append(args, root)
			}
			roots[root] = append(roots[root], websiteDiskKey(data, site))
		}
		sizes := map[string]int64{}
		if len(args) > 4 {
			output, err := exec.CommandContext(ctx, "du", args...).Output()
			// Do not report a partial scan as a complete directory measurement.
			if err != nil {
				output = nil
			}
			for _, line := range strings.Split(string(output), "\x00") {
				f := strings.SplitN(line, "\t", 2)
				if len(f) != 2 {
					continue
				}
				n, e := strconv.ParseInt(f[0], 10, 64)
				if e != nil || n < 0 {
					continue
				}
				for _, key := range roots[f[1]] {
					sizes[key] = n
				}
			}
		}
		websiteDiskSamples.Lock()
		websiteDiskSamples.Sizes = sizes
		websiteDiskSamples.At = time.Now().UTC()
		websiteDiskSamples.Busy = false
		websiteDiskSamples.Unlock()
	}()
}

func (a *App) websiteResourceList(w http.ResponseWriter, r *http.Request) {
	sites := []Site{}
	for _, site := range a.store.Sites() {
		if site.ResourceLimits != nil {
			sites = append(sites, site)
		}
	}
	rows := []websiteResourceRow{}
	now := time.Now().UTC()
	note := ""
	var counters map[int]websiteProcessCounter
	if len(sites) > 0 && runtime.GOOS == "linux" && !a.system.demo {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if output, err := exec.CommandContext(ctx, "ps", "-eo", "uid=,pid=,etimes=,cputimes=,rss=").Output(); err == nil {
			counters = parseWebsiteProcesses(string(output))
		} else {
			note = "Process measurements are unavailable."
		}
		refreshWebsiteDiskSamples(a.system.dataDir, sites)
	} else if len(sites) > 0 {
		note = "Live measurements require the installed Linux server."
	}
	websiteUsageSamples.Lock()
	elapsed := 0.0
	if !websiteUsageSamples.At.IsZero() {
		elapsed = now.Sub(websiteUsageSamples.At).Seconds()
	}
	owners := map[int]int{}
	for _, site := range a.store.Sites() {
		owners[site.RuntimeUID]++
	}
	for _, site := range sites {
		u := websiteObservedUsage{}
		if counters != nil && owners[site.RuntimeUID] == 1 {
			u = aggregateWebsiteProcesses(site.RuntimeUID, websiteUsageSamples.Counters, counters, elapsed)
		}
		websiteDiskSamples.Lock()
		if n, ok := websiteDiskSamples.Sizes[websiteDiskKey(a.system.dataDir, site)]; ok {
			u.Disk = &n
			u.DiskAt = websiteDiskSamples.At
		}
		websiteDiskSamples.Unlock()
		rows = append(rows, websiteResourceRow{ID: site.ID, Domain: site.Domain, Settings: site.ResourceLimits, State: "Disabled", Usage: u})
	}
	if counters != nil {
		websiteUsageSamples.At = now
		websiteUsageSamples.Counters = counters
	}
	websiteUsageSamples.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"items": rows, "updatedAt": now, "note": note, "coverage": "Observed website-UID processes only. Shared Nginx/MySQL/mail and container processes with remapped UIDs are excluded. Memory is summed RSS. CPU needs two samples; disk refreshes every 60 seconds. Saved settings are not enforced."})
}
