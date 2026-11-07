package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type operationsSiteUsage struct {
	At        time.Time `json:"at"`
	ID        string    `json:"id"`
	Domain    string    `json:"domain"`
	CPU       *float64  `json:"cpu"`
	Memory    int64     `json:"memory"`
	Processes int       `json:"processes"`
	Disk      *int64    `json:"disk,omitempty"`
	Coverage  string    `json:"coverage"`
}

var operationsCapacity = struct {
	sync.Mutex
	At       time.Time
	Counters map[int]websiteProcessCounter
}{}

func (a *App) recordOperationsCapacity(ctx context.Context) {
	if a.system.demo {
		return
	}
	output, e := runCommand(ctx, 3*time.Second, "ps", "-eo", "uid=,pid=,etimes=,cputimes=,rss=")
	if e != nil {
		return
	}
	current := parseWebsiteProcesses(output)
	now := time.Now().UTC()
	operationsCapacity.Lock()
	defer operationsCapacity.Unlock()
	elapsed := now.Sub(operationsCapacity.At).Seconds()
	if operationsCapacity.At.IsZero() {
		elapsed = 0
	}
	sites := a.store.Sites()
	owners := map[int]int{}
	for _, site := range sites {
		owners[site.RuntimeUID]++
	}
	file, e := os.OpenFile(filepath.Join(a.system.dataDir, "website-usage-history.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil || info.Size() >= 64<<20 {
		return
	}
	for _, site := range sites {
		v := operationsSiteUsage{At: now, ID: site.ID, Domain: site.Domain, Coverage: "Website UID observation; shared services and remapped container users excluded"}
		if site.RuntimeUID > 0 && owners[site.RuntimeUID] == 1 {
			u := aggregateWebsiteProcesses(site.RuntimeUID, operationsCapacity.Counters, current, elapsed)
			v.CPU = u.CPU
			v.Memory = u.Memory
			v.Processes = u.Processes
		}
		if resourceEnabled(site) {
			s := resourceStatusForSite(site)
			if s.Enforced && time.Since(s.UpdatedAt) < 10*time.Second {
				v.CPU = s.CPUPercent
				v.Memory = s.MemoryBytes
				v.Processes = s.Processes
				disk := s.Sample.DiskBytes
				v.Disk = &disk
				v.Coverage = "Kernel counters for enforced website workloads"
			}
		}
		b, e := json.Marshal(v)
		if e == nil {
			_, _ = file.Write(append(b, '\n'))
		}
	}
	operationsCapacity.At = now
	operationsCapacity.Counters = current
}

func (a *App) operationsSiteHistory(id string) []operationsSiteUsage {
	points := []operationsSiteUsage{}
	if id == "" || a.store.SiteByID(id) == nil {
		return points
	}
	file, err := os.Open(filepath.Join(a.system.dataDir, "website-usage-history.jsonl"))
	if err != nil {
		return points
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() > 8<<20 {
		_, _ = file.Seek(-(8 << 20), 2)
	}
	hours := map[time.Time]operationsSiteUsage{}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var point operationsSiteUsage
		if json.Unmarshal(scanner.Bytes(), &point) != nil || point.ID != id || point.At.Before(cutoff) {
			continue
		}
		hour := point.At.UTC().Truncate(time.Hour)
		if previous, ok := hours[hour]; !ok || point.At.After(previous.At) {
			hours[hour] = point
		}
	}
	for _, point := range hours {
		points = append(points, point)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].At.Before(points[j].At) })
	return points
}
func (a *App) readOperationsSiteUsage() []operationsSiteUsage {
	items := []operationsSiteUsage{}
	f, e := os.Open(filepath.Join(a.system.dataDir, "website-usage-history.jsonl"))
	if e != nil {
		return items
	}
	defer f.Close()
	if info, e := f.Stat(); e == nil && info.Size() > 2<<20 {
		_, _ = f.Seek(-(2 << 20), 2)
	}
	scan := bufio.NewScanner(f)
	latest := map[string]operationsSiteUsage{}
	for scan.Scan() {
		var v operationsSiteUsage
		if json.Unmarshal(scan.Bytes(), &v) == nil && time.Since(v.At) < 10*time.Minute {
			latest[v.ID] = v
		}
	}
	for _, site := range a.store.Sites() {
		if v, ok := latest[site.ID]; ok {
			items = append(items, v)
		}
	}
	return items
}
