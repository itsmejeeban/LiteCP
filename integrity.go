package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var integrityMu sync.Mutex
var integrityBusy bool
var integrityRoots = []string{"/etc/nginx", "/etc/litecp", "/etc/cron.d", "/etc/systemd/system", "/usr/local/bin/litecp"}

type integrityEntry struct {
	Hash string `json:"hash"`
	Mode uint32 `json:"mode"`
	Link string `json:"link,omitempty"`
}
type integrityChange struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}
type integrityState struct {
	Enabled    bool              `json:"enabled"`
	Interval   int               `json:"interval"`
	Baseline   string            `json:"baseline,omitempty"`
	BaselineAt time.Time         `json:"baselineAt"`
	CheckedAt  time.Time         `json:"checkedAt"`
	Changes    []integrityChange `json:"changes"`
	Error      string            `json:"error,omitempty"`
	Running    bool              `json:"running"`
}

func (a *App) integrityDir() string { return filepath.Join(a.system.dataDir, "integrity") }
func (a *App) integrityRead() (integrityState, error) {
	value := integrityState{Interval: 60, Changes: []integrityChange{}}
	data, err := os.ReadFile(filepath.Join(a.integrityDir(), "state.json"))
	if os.IsNotExist(err) {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	err = json.Unmarshal(data, &value)
	if err == nil && (value.Interval != 15 && value.Interval != 60 && value.Interval != 1440) {
		err = fmt.Errorf("invalid monitoring interval")
	}
	if err == nil && value.Baseline != "" && (filepath.Base(value.Baseline) != value.Baseline || !strings.HasPrefix(value.Baseline, "baseline-") || !strings.HasSuffix(value.Baseline, ".json")) {
		err = fmt.Errorf("invalid baseline reference")
	}
	return value, err
}
func (a *App) integritySave(value integrityState) error {
	if err := os.MkdirAll(a.integrityDir(), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(a.integrityDir(), "state.json"), data, 0600)
}

// Never follow symlinks, accept browser-supplied paths or read file contents into reports.
func integrityCollect(ctx context.Context, roots []string) (map[string]integrityEntry, error) {
	entries := map[string]integrityEntry{}
	var total int64
	for _, root := range roots {
		if _, err := os.Lstat(root); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if len(entries) >= 50000 {
				return fmt.Errorf("scan exceeds 50,000 entries")
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			entry := integrityEntry{Mode: uint32(info.Mode())}
			if info.Mode()&os.ModeSymlink != 0 {
				entry.Link, err = os.Readlink(path)
				if err != nil {
					return err
				}
				entries[path] = entry
				return nil
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("unsupported special file: %s", path)
			}
			if info.Size() > 128<<20 {
				return fmt.Errorf("file exceeds 128 MiB: %s", path)
			}
			total += info.Size()
			if total > 512<<20 {
				return fmt.Errorf("scan exceeds 512 MiB")
			}
			f, err := secureOpenManaged(filepath.VolumeName(path)+string(os.PathSeparator), strings.TrimPrefix(path, filepath.VolumeName(path)+string(os.PathSeparator)), os.O_RDONLY, 0)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			hash := sha256.New()
			n, readErr := io.Copy(hash, io.LimitReader(f, 128<<20+1))
			after, statErr := f.Stat()
			f.Close()
			if readErr != nil {
				return readErr
			}
			if statErr != nil {
				return statErr
			}
			if n != info.Size() || !os.SameFile(info, after) || !info.ModTime().Equal(after.ModTime()) {
				return fmt.Errorf("file changed during scan: %s", path)
			}
			entry.Hash = hex.EncodeToString(hash.Sum(nil))
			entries[path] = entry
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no regular files or symbolic links were found in the monitored paths")
	}
	return entries, nil
}
func integrityDiff(before, after map[string]integrityEntry) []integrityChange {
	result := []integrityChange{}
	for path, old := range before {
		if current, ok := after[path]; !ok {
			result = append(result, integrityChange{path, "missing"})
		} else if old != current {
			result = append(result, integrityChange{path, "modified"})
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			result = append(result, integrityChange{path, "added"})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}
func (a *App) integrityRun(baseline bool) {
	defer func() { integrityMu.Lock(); integrityBusy = false; integrityMu.Unlock() }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	entries, scanErr := integrityCollect(ctx, integrityRoots)
	integrityMu.Lock()
	defer integrityMu.Unlock()
	value, err := a.integrityRead()
	if err != nil {
		return
	}
	value.Error = ""
	previousReport, _ := json.Marshal(value.Changes)
	if scanErr == nil {
		if baseline {
			name := "baseline-" + time.Now().UTC().Format("20060102T150405.000000000") + ".json"
			data, _ := json.Marshal(entries)
			if err = a.integritySave(value); err == nil {
				err = os.WriteFile(filepath.Join(a.integrityDir(), name), data, 0600)
			}
			if err == nil {
				value.Baseline = name
				value.BaselineAt = time.Now().UTC()
				value.Changes = []integrityChange{}
				value.Enabled = true
			}
		} else {
			var previous map[string]integrityEntry
			var data []byte
			data, err = os.ReadFile(filepath.Join(a.integrityDir(), value.Baseline))
			if err == nil {
				err = json.Unmarshal(data, &previous)
			}
			if err == nil {
				value.Changes = integrityDiff(previous, entries)
			}
		}
	} else {
		err = scanErr
	}
	value.CheckedAt = time.Now().UTC()
	if err != nil {
		value.Error = err.Error()
	}
	// Retain each completed report and every prior baseline; never silently trust changes.
	report, _ := json.Marshal(value)
	if err := os.WriteFile(filepath.Join(a.integrityDir(), "report-"+value.CheckedAt.Format("20060102T150405.000000000")+".json"), report, 0600); err != nil {
		value.Error = "Cannot retain the integrity report: " + err.Error()
	}
	if err = a.integritySave(value); err != nil {
		log.Printf("File integrity status persistence failed: %v", err)
		return
	}
	currentReport, _ := json.Marshal(value.Changes)
	if value.Error != "" || (len(value.Changes) > 0 && string(previousReport) != string(currentReport)) {
		_ = a.store.AddActivity("File integrity review required", fmt.Sprintf("%d changes. %s", len(value.Changes), value.Error), "warning")
	}
}
func (a *App) integrityAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" && r.Method != "POST" {
		writeError(w, 405, "Use GET or POST")
		return
	}
	integrityMu.Lock()
	defer integrityMu.Unlock()
	value, err := a.integrityRead()
	if err != nil {
		writeError(w, 500, "Cannot read integrity settings")
		return
	}
	if r.Method == "GET" {
		value.Running = integrityBusy
		writeJSON(w, 200, map[string]any{"settings": value, "roots": integrityRoots})
		return
	}
	if a.system.demo {
		writeError(w, 400, "Integrity monitoring requires an installed server")
		return
	}
	var input struct {
		Action   string `json:"action"`
		Confirm  bool   `json:"confirm"`
		Interval int    `json:"interval"`
		Enabled  bool   `json:"enabled"`
	}
	if decodeJSONWithLimit(r, &input, 2048) != nil {
		writeError(w, 400, "Invalid request")
		return
	}
	if integrityBusy {
		writeError(w, 409, "An integrity check is already running")
		return
	}
	switch input.Action {
	case "settings":
		if input.Interval != 15 && input.Interval != 60 && input.Interval != 1440 {
			writeError(w, 400, "Choose 15, 60 or 1440 minutes")
			return
		}
		if input.Enabled && value.Baseline == "" {
			writeError(w, 400, "Create a trusted baseline first")
			return
		}
		value.Enabled = input.Enabled
		value.Interval = input.Interval
		if a.integritySave(value) != nil {
			writeError(w, 500, "Cannot save monitoring settings")
			return
		}
		writeJSON(w, 200, value)
	case "baseline", "scan":
		if input.Action == "baseline" && !input.Confirm {
			writeError(w, 400, "Confirm the current files are trusted before creating a baseline")
			return
		}
		if input.Action == "scan" && value.Baseline == "" {
			writeError(w, 400, "Create a baseline first")
			return
		}
		if err := os.MkdirAll(a.integrityDir(), 0700); err != nil {
			writeError(w, 500, "Cannot prepare private integrity storage")
			return
		}
		integrityBusy = true
		go a.integrityRun(input.Action == "baseline")
		writeJSON(w, 202, map[string]bool{"running": true})
	default:
		writeError(w, 400, "Unknown action")
	}
}
func (a *App) startIntegrityMonitor() {
	if a.system.demo {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			integrityMu.Lock()
			value, err := a.integrityRead()
			if err == nil && value.Enabled && value.Baseline != "" && !integrityBusy && time.Since(value.CheckedAt) >= time.Duration(value.Interval)*time.Minute {
				integrityBusy = true
				go a.integrityRun(false)
			}
			integrityMu.Unlock()
		}
	}()
}
