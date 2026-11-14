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
	ID           string                 `json:"id"`
	Domain       string                 `json:"domain"`
	Settings     *WebsiteResourceLimits `json:"settings"`
	State        string                 `json:"state"`
	Reason       string                 `json:"reason,omitempty"`
	Enforced     bool                   `json:"enforced"`
	Usage        websiteObservedUsage   `json:"usage"`
	NextRecovery time.Time              `json:"nextRecovery,omitempty"`
	Error        string                 `json:"error,omitempty"`
}

var websiteUsageSamples = struct {
	sync.Mutex
	dataDir  string
	previous websiteProcessSample
	current  websiteProcessSample
}{}

type websiteProcessSample struct {
	at       time.Time
	counters map[int]websiteProcessCounter
}

// Measure across published cgroup samples. The watcher checks enforcement
// every second, but only publishes routine telemetry every three seconds.
type resourceCPUReading struct {
	sample  resourceSample
	percent *float64
}

func (previous resourceCPUReading) update(sample resourceSample, now time.Time) resourceCPUReading {
	if sample.At.IsZero() || now.Sub(sample.At) >= 10*time.Second {
		return resourceCPUReading{}
	}
	elapsed := sample.At.Sub(previous.sample.At)
	if elapsed == 0 {
		return previous
	}
	reading := resourceCPUReading{sample: sample}
	if !previous.sample.At.IsZero() && elapsed > 0 && elapsed < 10*time.Second && sample.UsageUsec >= previous.sample.UsageUsec {
		percent := float64(sample.UsageUsec-previous.sample.UsageUsec) / (elapsed.Seconds() * 1e6) * 100
		reading.percent = &percent
	}
	return reading
}

func sampleWebsiteProcesses(parent context.Context, dataDir string) (websiteProcessSample, websiteProcessSample, error) {
	websiteUsageSamples.Lock()
	defer websiteUsageSamples.Unlock()
	if websiteUsageSamples.dataDir == dataDir && time.Since(websiteUsageSamples.current.at) < 3*time.Second {
		return websiteUsageSamples.previous, websiteUsageSamples.current, nil
	}
	output, err := runCommand(parent, 3*time.Second, "ps", "-eo", "uid=,pid=,etimes=,cputimes=,rss=")
	if err != nil {
		return websiteProcessSample{}, websiteProcessSample{}, err
	}
	previous := websiteProcessSample{}
	if websiteUsageSamples.dataDir == dataDir {
		previous = websiteUsageSamples.current
	}
	current := websiteProcessSample{at: time.Now().UTC(), counters: parseWebsiteProcesses(output)}
	websiteUsageSamples.dataDir = dataDir
	websiteUsageSamples.previous = previous
	websiteUsageSamples.current = current
	return previous, current, nil
}

func (current websiteProcessSample) usage(uid int, previous websiteProcessSample) websiteObservedUsage {
	return aggregateWebsiteProcesses(uid, previous.counters, current.counters, current.at.Sub(previous.at).Seconds())
}

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
	rows, now, note := a.collectWebsiteResourceUsage(r.Context())
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"items": rows, "updatedAt": now, "note": note, "coverage": "Observed website-UID processes only. Shared Nginx/MySQL/mail and container processes with remapped UIDs are excluded. Memory is summed RSS. CPU needs two samples; disk refreshes every 60 seconds. Enabled rows use verified kernel counters; disabled rows are observations only."})
}

func (a *App) collectWebsiteResourceUsage(parent context.Context) ([]websiteResourceRow, time.Time, string) {
	sites := []Site{}
	allSites := a.store.Sites()
	for _, site := range allSites {
		if site.ResourceLimits != nil {
			sites = append(sites, site)
		}
	}
	observedSites := []Site{}
	for _, site := range sites {
		if !resourceEnabled(site) {
			observedSites = append(observedSites, site)
		}
	}
	rows := []websiteResourceRow{}
	now := time.Now().UTC()
	note := ""
	var previous, sample websiteProcessSample
	if len(observedSites) > 0 && runtime.GOOS == "linux" && !a.system.demo {
		var err error
		if previous, sample, err = sampleWebsiteProcesses(parent, a.system.dataDir); err == nil {
			now = sample.at
		} else {
			note = "Process measurements are unavailable."
		}
		refreshWebsiteDiskSamples(a.system.dataDir, observedSites)
	} else if len(observedSites) > 0 {
		note = "Live measurements require the installed Linux server."
	}
	owners := map[int]int{}
	for _, site := range allSites {
		owners[site.RuntimeUID]++
	}
	a.resourceUsageMu.Lock()
	defer a.resourceUsageMu.Unlock()
	cpuReadings := make(map[string]resourceCPUReading, len(a.resourceCPU))
	for _, site := range sites {
		u := websiteObservedUsage{}
		if sample.counters != nil && owners[site.RuntimeUID] == 1 {
			u = sample.usage(site.RuntimeUID, previous)
		}
		websiteDiskSamples.Lock()
		if n, ok := websiteDiskSamples.Sizes[websiteDiskKey(a.system.dataDir, site)]; ok {
			u.Disk = &n
			u.DiskAt = websiteDiskSamples.At
		}
		websiteDiskSamples.Unlock()
		row := websiteResourceRow{ID: site.ID, Domain: site.Domain, Settings: site.ResourceLimits, State: "Disabled", Usage: u}
		if resourceEnabled(site) {
			st := resourceStatusForSite(site)
			row.State = st.State
			row.Reason = st.Reason
			row.NextRecovery = st.NextRecovery
			row.Error = st.Error
			row.Enforced = st.Enforced && time.Since(st.UpdatedAt) < 10*time.Second
			row.Usage = websiteObservedUsage{Available: row.Enforced, Memory: st.MemoryBytes, Processes: st.Processes}
			if row.Enforced {
				if site.Suspended || st.State == "Resource blocked" {
					idle := 0.0
					row.Usage.CPU = &idle
				} else if st.State == "Active" {
					reading := a.resourceCPU[site.ID].update(st.Sample, time.Now())
					cpuReadings[site.ID] = reading
					row.Usage.CPU = reading.percent
				}
				disk := st.Sample.DiskBytes
				row.Usage.Disk = &disk
				row.Usage.DiskAt = st.UpdatedAt
			} else {
				row.State = "Error: resource monitor unavailable"
			}
		}
		rows = append(rows, row)
	}
	a.resourceCPU = cpuReadings
	return rows, now, note
}
