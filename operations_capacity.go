package main

import (
	"context"
	"encoding/json"
	"log"
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
	sync.RWMutex
	dataDir string
	latest  []operationsSiteUsage
}{}

func (a *App) recordOperationsCapacity(ctx context.Context) {
	if a.system.demo {
		return
	}
	previous, sample, err := sampleWebsiteProcesses(ctx, a.system.dataDir)
	if err != nil {
		return
	}
	now := sample.at
	sites := a.store.Sites()
	owners := map[int]int{}
	for _, site := range sites {
		owners[site.RuntimeUID]++
	}
	latest := make([]operationsSiteUsage, 0, len(sites))
	var lines []byte
	for _, site := range sites {
		v := operationsSiteUsage{At: now, ID: site.ID, Domain: site.Domain, Coverage: "Website UID observation; shared services and remapped container users excluded"}
		if site.RuntimeUID > 0 && owners[site.RuntimeUID] == 1 {
			u := sample.usage(site.RuntimeUID, previous)
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
		latest = append(latest, v)
		if data, err := json.Marshal(v); err == nil {
			lines = append(lines, data...)
			lines = append(lines, '\n')
		}
	}
	operationsCapacity.Lock()
	operationsCapacity.dataDir = a.system.dataDir
	operationsCapacity.latest = latest
	operationsCapacity.Unlock()
	if len(lines) > 0 {
		if err := appendHistoryLines(a.system.dataDir, "website-usage", now, lines); err != nil {
			log.Printf("website usage history: %v", err)
		}
	}
}

func (a *App) operationsSiteHistory(id string) []operationsSiteUsage {
	points := []operationsSiteUsage{}
	if id == "" || a.store.SiteByID(id) == nil {
		return points
	}
	hours := map[time.Time]operationsSiteUsage{}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	_ = scanHistoryRecords(a.system.dataDir, "website-usage", "website-usage-history.jsonl", cutoff, 16<<20, func(line []byte) {
		var point operationsSiteUsage
		if json.Unmarshal(line, &point) != nil || point.ID != id || point.At.Before(cutoff) {
			return
		}
		hour := point.At.UTC().Truncate(time.Hour)
		if previous, ok := hours[hour]; !ok || point.At.After(previous.At) {
			hours[hour] = point
		}
	})
	for _, point := range hours {
		points = append(points, point)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].At.Before(points[j].At) })
	return points
}
func (a *App) readOperationsSiteUsage() []operationsSiteUsage {
	items := []operationsSiteUsage{}
	latest := map[string]operationsSiteUsage{}
	operationsCapacity.RLock()
	hasSample := operationsCapacity.dataDir == a.system.dataDir && operationsCapacity.latest != nil
	if hasSample {
		for _, v := range operationsCapacity.latest {
			if time.Since(v.At) < 10*time.Minute {
				latest[v.ID] = v
			}
		}
	}
	operationsCapacity.RUnlock()
	if !hasSample {
		_ = scanHistoryRecords(a.system.dataDir, "website-usage", "website-usage-history.jsonl", time.Now().Add(-10*time.Minute), 2<<20, func(line []byte) {
			var v operationsSiteUsage
			if json.Unmarshal(line, &v) == nil && time.Since(v.At) < 10*time.Minute {
				latest[v.ID] = v
			}
		})
	}
	for _, site := range a.store.Sites() {
		if v, ok := latest[site.ID]; ok {
			items = append(items, v)
		}
	}
	return items
}
