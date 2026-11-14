package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

type migrationStatusSummary struct {
	ID             string
	Status         string
	Account        string
	MigrationBatch string
	Source         struct{ Platform string }
}

type migrationStatusEntry struct {
	info os.FileInfo
	job  migrationStatusSummary
}

var migrationStatusIndex struct {
	sync.Mutex
	root string
	jobs map[string]migrationStatusEntry
}

// Capture workers run in separate processes and replace status.json atomically.
// Compare file identity as well as size and time so a replacement is visible
// even on filesystems with coarse timestamps.
func (a *App) migrationStatuses() []migrationStatusSummary {
	root := filepath.Join(a.system.dataDir, "live-migrations")
	migrationStatusIndex.Lock()
	defer migrationStatusIndex.Unlock()
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	cache := migrationStatusIndex.jobs
	if root != migrationStatusIndex.root {
		cache = nil
	}
	next := make(map[string]migrationStatusEntry, len(entries))
	result := make([]migrationStatusSummary, 0, len(entries))
	for _, entry := range entries {
		id := entry.Name()
		if !entry.IsDir() || !liveJobIDPattern.MatchString(id) {
			continue
		}
		path := filepath.Join(root, id, "status.json")
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		cached, ok := cache[id]
		// Windows resolves an os.Stat file ID lazily by reopening the path.
		// The installed Linux server caches identity from the original stat.
		if runtime.GOOS != "linux" || !ok || cached.info.Size() != info.Size() || !cached.info.ModTime().Equal(info.ModTime()) || !os.SameFile(cached.info, info) {
			data, err := os.ReadFile(path)
			cached.job = migrationStatusSummary{}
			if err != nil || json.Unmarshal(data, &cached.job) != nil || cached.job.ID != id {
				continue
			}
			cached.info = info
		}
		next[id] = cached
		job := cached.job
		if a.liveMigrationCancelled(id) {
			job.Status = "cancelled"
		}
		result = append(result, job)
	}
	migrationStatusIndex.root = root
	migrationStatusIndex.jobs = next
	return result
}
