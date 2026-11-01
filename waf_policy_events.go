package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type wafPolicyEvent struct {
	Time    string `json:"time"`
	Website string `json:"website"`
	Message string `json:"message"`
	Blocked bool   `json:"blocked"`
}

func readWAFTail(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("log is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	start := info.Size() - 2*1024*1024
	if start < 0 {
		start = 0
	}
	if _, err = f.Seek(start, io.SeekStart); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(f, 2*1024*1024))
	return string(data), err
}

var wafRateClient = regexp.MustCompile(`client: ([^, ]+)`)
var wafRateZone = regexp.MustCompile(`zone "(lcw_[A-Za-z0-9_]+)"`)

func (a *App) wafPolicyEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	scope := r.URL.Query().Get("scope")
	kind := r.URL.Query().Get("kind")
	if kind != "detected" && kind != "blocked" && kind != "rate" {
		writeError(w, 400, "Choose detected, blocked or rate events.")
		return
	}
	sites := a.store.Sites()
	if scope != "server" {
		site := a.store.SiteByID(scope)
		if site == nil {
			writeError(w, 404, "Website not found.")
			return
		}
		sites = []Site{*site}
	}
	entries := []wafPolicyEvent{}
	unavailable := 0
	if kind == "rate" {
		text, err := readWAFTail("/var/log/nginx/error.log")
		if err != nil {
			unavailable++
		}
		lines := strings.Split(text, "\n")
		for i := len(lines) - 1; i >= 0 && len(entries) < 200; i-- {
			line := lines[i]
			zone := wafRateZone.FindStringSubmatch(line)
			if len(zone) < 2 || !strings.Contains(line, "limiting requests") {
				continue
			}
			prefix := wafRatePrefix("server")
			if scope != "server" {
				prefix = wafRatePrefix(safeSiteID(scope))
			}
			if !strings.HasPrefix(zone[1], prefix) {
				continue
			}
			client := "unknown"
			if m := wafRateClient.FindStringSubmatch(line); len(m) > 1 {
				client = m[1]
			}
			stamp := ""
			if len(line) >= 19 {
				stamp = line[:19]
			}
			entries = append(entries, wafPolicyEvent{Time: stamp, Message: zone[1] + " | client " + client, Blocked: !strings.Contains(line, "dry run")})
		}
	} else {
		paths := []struct{ path, website string }{}
		if scope == "server" {
			paths = append(paths, struct{ path, website string }{"/var/log/nginx/litecp-waf-server.log", "Server"})
		}
		for i, site := range sites {
			if i >= 20 {
				break
			}
			paths = append(paths, struct{ path, website string }{fmt.Sprintf("/var/log/nginx/litecp-waf-%s.log", safeSiteID(site.ID)), site.Domain})
		}
		for _, source := range paths {
			text, err := readWAFTail(source.path)
			if err != nil {
				if !os.IsNotExist(err) {
					unavailable++
				}
				continue
			}
			for _, entry := range parseWAFLogs(text, kind == "blocked") {
				if scope == "server" && !strings.Contains(entry.Message, "Server rule ") && !strings.Contains(entry.Message, "OWASP") {
					continue
				}
				entries = append(entries, wafPolicyEvent{entry.Time, source.website, entry.Message, entry.Blocked})
				if len(entries) >= 1000 {
					break
				}
			}
			if len(entries) >= 1000 {
				break
			}
		}
	}
	search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
	filtered := []wafPolicyEvent{}
	for _, e := range entries {
		if search == "" || strings.Contains(strings.ToLower(e.Website+" "+e.Message+" "+e.Time), search) {
			filtered = append(filtered, e)
		}
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pages := (len(filtered) + 19) / 20
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}
	start := (page - 1) * 20
	end := start + 20
	if end > len(filtered) {
		end = len(filtered)
	}
	writeJSON(w, 200, map[string]any{"entries": filtered[start:end], "page": page, "pages": pages, "total": len(filtered), "unavailableLogs": unavailable, "note": "Bounded recent log sample (at most 20 websites), not lifetime totals. Bodies, cookies and authorization headers are not returned. Rate events come from the default Nginx error log; redirected logs require administrator configuration. Server custom events are separated by rule prefix; managed CRS events are in website logs."})
}
