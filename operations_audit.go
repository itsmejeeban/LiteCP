package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type operationAudit struct {
	At         time.Time `json:"at"`
	Actor      string    `json:"actor"`
	Method     string    `json:"method"`
	Route      string    `json:"route"`
	Status     int       `json:"status"`
	DurationMS int64     `json:"durationMs"`
}

var operationAuditMu sync.Mutex

type operationResponse struct {
	http.ResponseWriter
	status int
}

func (w *operationResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *operationResponse) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func (w *operationResponse) Flush() {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *operationResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (a *App) auditOperation(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if a.store == nil || a.system == nil || a.system.dataDir == "" || r.Method == "GET" || r.Method == "HEAD" || strings.HasPrefix(r.URL.Path, "/api/terminal") {
		next(w, r)
		return
	}
	started := time.Now()
	out := &operationResponse{ResponseWriter: w}
	next(out, r)
	if out.status == 0 {
		out.status = 200
	}
	actor := a.requestActor(r)
	item := operationAudit{At: started.UTC(), Actor: actor, Method: r.Method, Route: r.URL.Path, Status: out.status, DurationMS: time.Since(started).Milliseconds()}
	data, e := json.Marshal(item)
	if e != nil {
		return
	}
	operationAuditMu.Lock()
	defer operationAuditMu.Unlock()
	f, e := os.OpenFile(filepath.Join(a.system.dataDir, "operation-audit.jsonl"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if e != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}
func (a *App) readOperationAudit(search string) []operationAudit {
	items := []operationAudit{}
	f, e := os.Open(filepath.Join(a.system.dataDir, "operation-audit.jsonl"))
	if e != nil {
		return items
	}
	defer f.Close()
	// Tail a bounded byte window rather than reading an ever-growing audit log.
	if info, e := f.Stat(); e == nil && info.Size() > 2<<20 {
		_, _ = f.Seek(-(2 << 20), 2)
	}
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 64<<10)
	search = strings.ToLower(search)
	for scan.Scan() {
		var v operationAudit
		if json.Unmarshal(scan.Bytes(), &v) != nil {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(v.Actor+" "+v.Method+" "+v.Route), search) {
			continue
		}
		items = append(items, v)
		if len(items) > 500 {
			items = items[1:]
		}
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items
}
func (a *App) operationsTrends() []usageSample {
	items := []usageSample{}
	f, e := os.Open(filepath.Join(a.system.dataDir, "usage-history.jsonl"))
	if e != nil {
		return items
	}
	defer f.Close()
	if info, e := f.Stat(); e == nil && info.Size() > 8<<20 {
		_, _ = f.Seek(-(8 << 20), 2)
	}
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		var p usageSample
		if json.Unmarshal(scan.Bytes(), &p) == nil && p.Time.After(time.Now().Add(-7*24*time.Hour)) {
			items = append(items, p)
		}
	}
	return aggregateUsage(items, "hourly", time.Now().Add(-7*24*time.Hour))
}
