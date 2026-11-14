package main

import (
	"encoding/json"
	"net/http"
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
	a.alertFailedOperation(r, out.status)
	actor := a.requestActor(r)
	item := operationAudit{At: started.UTC(), Actor: actor, Method: r.Method, Route: r.URL.Path, Status: out.status, DurationMS: time.Since(started).Milliseconds()}
	operationAuditMu.Lock()
	defer operationAuditMu.Unlock()
	_ = appendHistoryRecord(a.system.dataDir, "operation-audit", item.At, item)
}
func (a *App) readOperationAudit(search string) []operationAudit {
	items := []operationAudit{}
	search = strings.ToLower(search)
	_ = scanHistoryRecords(a.system.dataDir, "operation-audit", "operation-audit.jsonl", time.Time{}, 2<<20, func(line []byte) {
		var v operationAudit
		if json.Unmarshal(line, &v) != nil {
			return
		}
		if search != "" && !strings.Contains(strings.ToLower(v.Actor+" "+v.Method+" "+v.Route), search) {
			return
		}
		items = append(items, v)
		if len(items) > 500 {
			items = items[1:]
		}
	})
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items
}
func (a *App) operationsTrends() []usageSample {
	bins := usageTotals{}
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	_ = scanHistoryRecords(a.system.dataDir, "server-usage", "usage-history.jsonl", cutoff, 8<<20, func(line []byte) {
		var p usageSample
		if json.Unmarshal(line, &p) == nil {
			bins.add(p, "hourly", cutoff)
		}
	})
	return bins.points()
}
