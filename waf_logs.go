package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
)

type wafLogEntry struct {
	Time    string `json:"time"`
	Message string `json:"message"`
	Blocked bool   `json:"blocked"`
}

var wafBoundary = regexp.MustCompile(`(?m)^--([A-Za-z0-9]+)-A--\r?$`)
var wafMessage = regexp.MustCompile(`\[msg "((?:\\.|[^"\\])*)"\]`)
var wafRuleID = regexp.MustCompile(`\[id "([0-9]+)"\]`)
var wafTime = regexp.MustCompile(`\[([^\]]+)\]`)

// Only expose rule summaries, never audit request bodies, cookies or headers.
func parseWAFLogs(data string, blocked bool) []wafLogEntry {
	result := []wafLogEntry{}
	starts := wafBoundary.FindAllStringSubmatchIndex(data, -1)
	for i := len(starts) - 1; i >= 0 && len(result) < 200; i-- {
		start := starts[i]
		end := len(data)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		record := data[start[0]:end]
		id := data[start[2]:start[3]]
		if !strings.Contains(record, "--"+id+"-Z--") {
			continue
		}
		h := strings.Index(record, "--"+id+"-H--")
		if h < 0 {
			continue
		}
		summary := record[h:]
		if endH := strings.Index(summary, "\n--"+id+"-"); endH >= 0 {
			summary = summary[:endH]
		}
		denied := strings.Contains(summary, "Access denied with code ") || strings.Contains(summary, "Action: Intercepted")
		if denied != blocked {
			continue
		}
		messages := []string{}
		for _, line := range strings.Split(summary, "\n") {
			if !strings.HasPrefix(line, "Message:") {
				continue
			}
			msg := wafMessage.FindStringSubmatch(line)
			if len(msg) < 2 {
				continue
			}
			text := msg[1]
			if rule := wafRuleID.FindStringSubmatch(line); len(rule) == 2 {
				text = "Rule " + rule[1] + ": " + text
			}
			if len(text) > 600 {
				text = text[:600]
			}
			messages = append(messages, text)
		}
		if len(messages) == 0 {
			continue
		}
		stamp := ""
		if t := wafTime.FindStringSubmatch(record[:h]); len(t) == 2 {
			stamp = t[1]
		}
		result = append(result, wafLogEntry{Time: stamp, Message: strings.Join(messages, "\n"), Blocked: denied})
	}
	return result
}

func (a *App) wafLogs(w http.ResponseWriter, r *http.Request) {
	site := a.store.SiteByID(r.URL.Query().Get("id"))
	if site == nil {
		writeError(w, 404, "Website not found.")
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind != "detected" && kind != "blocked" {
		writeError(w, 400, "Invalid log type.")
		return
	}
	path := fmt.Sprintf("/var/log/nginx/litecp-waf-%s.log", safeSiteID(site.ID))
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		writeJSON(w, 200, map[string]any{"entries": []wafLogEntry{}, "note": "No WAF events recorded yet."})
		return
	}
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, 503, "WAF log is unavailable.")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, 503, "Cannot read WAF log.")
		return
	}
	defer f.Close()
	start := info.Size() - 2*1024*1024
	if start < 0 {
		start = 0
	}
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		writeError(w, 503, "Cannot read WAF log.")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, 2*1024*1024))
	if err != nil {
		writeError(w, 503, "Cannot read WAF log.")
		return
	}
	writeJSON(w, 200, map[string]any{"entries": parseWAFLogs(string(data), kind == "blocked"), "note": "Latest 200 matching events from the last 2 MiB of the current log. Request bodies and credentials are omitted."})
}
