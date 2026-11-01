package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func trafficFixture(t *testing.T, id string, change func(*trafficRaw)) []byte {
	t.Helper()
	raw := trafficRaw{ID: id, Time: "2026-10-31T23:30:00+05:30", Path: "/products?password=secret", Method: "GET", Status: "200", Bytes: "1234", RequestBytes: "300", Duration: "0.025", Referrer: "https://example.org/path?token=secret", Agent: "Mozilla/5.0 (Windows NT 10.0) Chrome/130.0 Safari/537.36", Address: "192.0.2.127", ContentType: "text/html; charset=utf-8", Protocol: "HTTP/2.0"}
	if change != nil {
		change(&raw)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func TestWebsiteTrafficParsingAndPrivacy(t *testing.T) {
	e, err := parseTrafficEvent(trafficFixture(t, "one", nil))
	if err != nil {
		t.Fatal(err)
	}
	if e.Path != "/products" || e.Referrer != "example.org" || e.Address != "192.0.2.0/24" || e.Browser != "Chrome" || e.Device != "Desktop" || e.OS != "Windows" || !e.Pageview {
		t.Fatalf("unexpected event: %+v", e)
	}
	if e.Time.Format(time.RFC3339) != "2026-10-31T18:00:00Z" {
		t.Fatal("UTC conversion", e.Time)
	}
	encoded, _ := json.Marshal(e)
	if bytes.Contains(encoded, []byte("secret")) || bytes.Contains(encoded, []byte("192.0.2.127")) || bytes.Contains(encoded, []byte("Mozilla")) {
		t.Fatal("sensitive source values persisted", string(encoded))
	}
	cases := []struct {
		name               string
		change             func(*trafficRaw)
		pageview, download bool
	}{
		{"asset", func(r *trafficRaw) { r.Path = "/app.js" }, false, false},
		{"api", func(r *trafficRaw) { r.ContentType = "application/json" }, false, false},
		{"bot", func(r *trafficRaw) { r.Agent = "Googlebot/2.1" }, false, false},
		{"error", func(r *trafficRaw) { r.Status = "404" }, false, false},
		{"redirect", func(r *trafficRaw) { r.Status = "301" }, false, false},
		{"post", func(r *trafficRaw) { r.Method = "POST" }, false, false},
		{"health", func(r *trafficRaw) { r.Path = "/healthz" }, false, false},
		{"download", func(r *trafficRaw) { r.Path = "/file.zip"; r.ContentType = "application/zip" }, false, true},
		{"xhtml", func(r *trafficRaw) { r.ContentType = "application/xhtml+xml" }, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := parseTrafficEvent(trafficFixture(t, tc.name, tc.change))
			if err != nil || e.Pageview != tc.pageview || e.Download != tc.download {
				t.Fatalf("%+v %v", e, err)
			}
		})
	}
	for _, invalid := range []string{"NaN", "+Inf", "-1", "90000"} {
		if _, err := parseTrafficEvent(trafficFixture(t, "bad", func(r *trafficRaw) { r.Duration = invalid })); err == nil {
			t.Fatal("accepted invalid duration", invalid)
		}
	}
	if maskTrafficAddress("2001:db8:abcd:12::1") != "2001:db8:abcd::/48" {
		t.Fatal("IPv6 masking")
	}
	device, browser, system, _ := trafficClient("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0) Version/17.0 Mobile/15E148 Safari/604.1")
	if device != "Mobile" || browser != "Safari" || system != "iOS" {
		t.Fatal(device, browser, system)
	}
	e, _ = parseTrafficEvent(trafficFixture(t, "none", func(r *trafficRaw) { r.Referrer = "" }))
	if e.Referrer != "Direct / Unknown" {
		t.Fatal(e.Referrer)
	}
}

func TestWebsiteTrafficCheckpointRotationAndRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traffic-analytics.log")
	one := trafficFixture(t, "one", nil)
	two := trafficFixture(t, "two", nil)
	if err := os.WriteFile(path, append(append([]byte{}, one...), two[:len(two)/2]...), 0600); err != nil {
		t.Fatal(err)
	}
	s := newTrafficState()
	budget := int64(trafficReadBudget)
	if _, err := collectTrafficFile(path, s, &budget, nil); err != nil {
		t.Fatal(err)
	}
	if len(s.Live) != 1 {
		t.Fatal("partial request counted")
	}
	data, _ := json.Marshal(s)
	statePath := filepath.Join(dir, "state.json")
	_ = os.WriteFile(statePath, data, 0600)
	restarted, err := readTrafficState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write(two[len(two)/2:])
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	budget = trafficReadBudget
	if _, err = collectTrafficFile(path, restarted, &budget, nil); err != nil {
		t.Fatal(err)
	}
	if len(restarted.Live) != 2 {
		t.Fatal("restart lost or duplicated requests", len(restarted.Live))
	}
	budget = trafficReadBudget
	changed, err := collectTrafficFile(path, restarted, &budget, nil)
	if err != nil || changed {
		t.Fatal("unchanged log recounted", err)
	}
	// Simulate delayed compression by writing a new compressed representation;
	// preserve the original fixture rather than deleting it.
	compressed := filepath.Join(dir, "traffic-analytics.log.1.gz")
	out, err := os.Create(compressed)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(out)
	_, _ = gz.Write(append(append([]byte{}, one...), two...))
	_ = gz.Close()
	_ = out.Close()
	budget = trafficReadBudget
	if _, err = collectTrafficFile(compressed, restarted, &budget, nil); err != nil {
		t.Fatal(err)
	}
	if len(restarted.Live) != 2 {
		t.Fatal("gzip rotation duplicated traffic")
	}
	budget = trafficReadBudget
	changed, err = collectTrafficFile(compressed, restarted, &budget, nil)
	if err != nil || changed {
		t.Fatal("completed compressed file revisited", err)
	}
	// Copy-truncate/reuse changes the first request ID and therefore identity.
	_ = os.WriteFile(path, trafficFixture(t, "three", nil), 0600)
	budget = trafficReadBudget
	if _, err = collectTrafficFile(path, restarted, &budget, nil); err != nil {
		t.Fatal(err)
	}
	if len(restarted.Live) != 3 {
		t.Fatal("new rotated identity not counted")
	}
}

func TestWebsiteTrafficBudgetMalformedAndDimensionBounds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log")
	var log bytes.Buffer
	log.WriteString("invalid json\n")
	for i := 0; i < 120; i++ {
		log.Write(trafficFixture(t, fmt.Sprint(i), func(r *trafficRaw) { r.Path = fmt.Sprintf("/page-%d", i) }))
	}
	_ = os.WriteFile(path, log.Bytes(), 0600)
	s := newTrafficState()
	budget := int64(2048)
	if _, err := collectTrafficFile(path, s, &budget, nil); err != nil {
		t.Fatal(err)
	}
	if s.Malformed != 1 || len(s.Live) >= 120 || budget < 0 {
		t.Fatal("unbounded or malformed handling failed")
	}
	budget = trafficReadBudget
	if _, err := collectTrafficFile(path, s, &budget, nil); err != nil {
		t.Fatal(err)
	}
	day := s.Days["2026-10-31"]
	if len(day.Dimensions["pages"]) > trafficDimensions+1 || day.Dimensions["pages"]["Other (bounded detail)"] == nil {
		t.Fatal("unbounded dimensions")
	}
	if day.Hours["18"].Requests != 120 {
		t.Fatal("detail cap lost totals")
	}
	if len(s.Live) != 120 {
		t.Fatal("request count", len(s.Live))
	}
}

func TestWebsiteTrafficDateAggregationAndPagination(t *testing.T) {
	s := newTrafficState()
	for i := 0; i < 30; i++ {
		e, _ := parseTrafficEvent(trafficFixture(t, fmt.Sprint(i), func(r *trafficRaw) { r.Path = fmt.Sprintf("/page-%d", i) }))
		if err := s.record(e); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	q := url.Values{"dimension": {"pages"}, "page": {"2"}, "size": {"10"}, "group": {"weekly"}}
	report := buildTrafficReport(s, start, end, q)
	if report["summary"].(trafficMetric).Requests != 30 || report["page"] != 2 || len(report["rows"].([]trafficRow)) != 10 {
		t.Fatal("aggregation or pagination", report)
	}
	if report["points"].([]trafficPoint)[0].Time != "2026-10-26" {
		t.Fatal("Monday week boundary")
	}
	q.Set("group", "monthly")
	if buildTrafficReport(s, start, end, q)["points"].([]trafficPoint)[0].Time != "2026-10" {
		t.Fatal("month grouping")
	}
	q.Set("group", "hourly")
	if buildTrafficReport(s, start, end, q)["points"].([]trafficPoint)[0].Time != "2026-10-31T18:00:00Z" {
		t.Fatal("hour grouping")
	}
	if buildTrafficReport(s, end, end.AddDate(0, 0, 1), q)["summary"].(trafficMetric).Requests != 0 {
		t.Fatal("date isolation failed")
	}
	if _, _, err := trafficRange(url.Values{"range": {"custom"}, "from": {"2020-01-01"}, "to": {"2026-11-01"}}, end); err == nil {
		t.Fatal("unbounded dates accepted")
	}
	q.Set("page", "2147483647")
	if buildTrafficReport(s, start, end, q)["page"] != 3 {
		t.Fatal("page bound")
	}
}

func TestWebsiteTrafficNginxConfigurations(t *testing.T) {
	site := Site{ID: "traffic-test", Domain: "traffic.example", Root: defaultSiteRoot("traffic.example")}
	system := &System{demo: true}
	normal := system.nginxConfig(site)
	if strings.Count(normal, "traffic-analytics.log") != 2 || strings.Contains(normal, "access_log off;") || !strings.Contains(filepath.ToSlash(normal), "/logs/access.log;") {
		t.Fatal("normal or asset analytics missing", normal)
	}
	if injectTrafficLogging(normal, site) != normal {
		t.Fatal("logging reconciliation is not idempotent")
	}
	site.Application = &ApplicationHosting{Enabled: true, Port: 8123}
	proxy := system.nginxConfig(site)
	if !strings.Contains(proxy, "proxy_pass") || strings.Count(proxy, "traffic-analytics.log") != 1 {
		t.Fatal("application proxy not logged", proxy)
	}
	existing := "server {\n listen 443 ssl;\n ssl_certificate /keep/fullchain.pem;\n access_log /keep/access.log;\n}\nserver {\n return 301 https://$host$request_uri;\n}\n"
	updated := injectTrafficLogging(existing, site)
	if strings.Count(updated, "traffic-analytics.log") != 2 || !strings.Contains(updated, "ssl_certificate /keep/fullchain.pem;") || !strings.Contains(updated, "access_log /keep/access.log;") || !strings.Contains(updated, "return 301") {
		t.Fatal("existing configuration changed", updated)
	}
	if strings.Contains(trafficNginxFormat, "$http_cookie") || strings.Contains(trafficNginxFormat, "$http_authorization") || strings.Contains(trafficNginxFormat, "$http_x_forwarded_for") {
		t.Fatal("unsafe logging source")
	}
}

func TestWebsiteTrafficAPIIsolationAndLocalGeo(t *testing.T) {
	dataDir := t.TempDir()
	store, err := OpenStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	store.cfg.Sites = []Site{{ID: "one", Domain: "one.example", Root: defaultSiteRoot("one.example")}, {ID: "two", Domain: "two.example", Root: defaultSiteRoot("two.example")}}
	s := newTrafficState()
	e, _ := parseTrafficEvent(trafficFixture(t, "one", nil))
	_ = s.record(e)
	s.Updated = time.Now().UTC()
	path := trafficStatePath(dataDir, "one")
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	encoded, _ := json.Marshal(s)
	_ = os.WriteFile(path, encoded, 0600)
	a := &App{store: store, system: &System{dataDir: dataDir, demo: true}, traffic: &trafficService{problems: map[string]string{}}}
	for _, tc := range []struct {
		id   string
		want int64
	}{{"one", 1}, {"two", 0}} {
		recorder := httptest.NewRecorder()
		a.websiteTraffic(recorder, httptest.NewRequest("GET", "/api/sites/traffic?siteId="+tc.id+"&range=custom&from=2026-10-31&to=2026-10-31", nil))
		if recorder.Code != 200 {
			t.Fatal(recorder.Body.String())
		}
		var report struct {
			Summary trafficMetric `json:"summary"`
		}
		if err = json.Unmarshal(recorder.Body.Bytes(), &report); err != nil || report.Summary.Requests != tc.want {
			t.Fatal("website isolation", recorder.Body.String(), err)
		}
	}
	recorder := httptest.NewRecorder()
	a.websiteTraffic(recorder, httptest.NewRequest("GET", "/api/sites/traffic?siteId=../one", nil))
	if recorder.Code != 404 {
		t.Fatal("unknown site accepted")
	}
	recorder = httptest.NewRecorder()
	a.withAuth(a.websiteTraffic)(recorder, httptest.NewRequest("GET", "/api/sites/traffic?siteId=one", nil))
	if recorder.Code != 401 {
		t.Fatal("unauthenticated analytics accessible")
	}
	if trafficStatePath(dataDir, "a-b") == trafficStatePath(dataDir, "a_b") {
		t.Fatal("site identity collision")
	}
	_ = os.WriteFile(filepath.Join(dataDir, "website-traffic-geo.json"), []byte(`{"192.0.0.0/8":"Broad","192.0.2.0/24":"Specific","2001:db8::/32":"IPv6"}`), 0600)
	geo := loadTrafficGeo(dataDir)
	if geo.country("192.0.2.5") != "Specific" || geo.country("2001:db8::1") != "IPv6" || geo.country("127.0.0.1") != "Unavailable" {
		t.Fatal("local country lookup failed")
	}
	q := httptest.NewRecorder()
	a.websiteTraffic(q, httptest.NewRequest("GET", "/api/sites/traffic?siteId=one&view=live&range=custom&from=2026-10-31&to=2026-10-31&filter=not-matching", nil))
	if !strings.Contains(q.Body.String(), `"entries":[]`) {
		t.Fatal("live filtering failed", q.Body.String())
	}
}

func TestWebsiteTrafficSustainedCollectionAndOldReplay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traffic.log")
	var data bytes.Buffer
	for i := 0; i < 5000; i++ {
		data.Write(trafficFixture(t, fmt.Sprint(i), func(r *trafficRaw) { r.Path = fmt.Sprintf("/item-%d", i) }))
	}
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	s := newTrafficState()
	latest, _ := parseTrafficEvent(trafficFixture(t, "latest", func(r *trafficRaw) { r.Path = "/latest"; r.Time = "2026-10-31T23:00:00Z" }))
	if err := s.record(latest); err != nil {
		t.Fatal(err)
	}
	iterations := 0
	for {
		iterations++
		budget := int64(trafficReadBudget)
		changed, err := collectTrafficFile(path, s, &budget, nil)
		if err != nil {
			t.Fatal(err)
		}
		if budget < 0 {
			t.Fatal("read budget exceeded")
		}
		if !changed {
			break
		}
		if iterations > 40 {
			t.Fatal("collector failed to catch up")
		}
	}
	if len(s.Live) != 200 || s.Live[len(s.Live)-1].Path != "/latest" {
		t.Fatal("late replay displaced newer live events")
	}
	if s.Days["2026-10-31"].Hours["18"].Requests != 5000 {
		t.Fatal("sustained collection lost or duplicated requests")
	}
	encoded, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 512<<10 {
		t.Fatal("unbounded high-cardinality history", len(encoded))
	}
	t.Logf("5000 requests processed in %d bounded passes; snapshot %d bytes; live entries %d", iterations, len(encoded), len(s.Live))
}

func TestWebsiteTrafficCompressedCatchupAndCorruptHistory(t *testing.T) {
	dir := t.TempDir()
	one, two := trafficFixture(t, "one", nil), trafficFixture(t, "two", nil)
	path := filepath.Join(dir, "log")
	_ = os.WriteFile(path, one, 0600)
	s := newTrafficState()
	budget := int64(trafficReadBudget)
	if _, err := collectTrafficFile(path, s, &budget, nil); err != nil {
		t.Fatal(err)
	}
	rotated := filepath.Join(dir, "log.1.gz")
	f, err := os.Create(rotated)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	_, _ = gz.Write(append(append([]byte{}, one...), two...))
	_ = gz.Close()
	_ = f.Close()
	budget = trafficReadBudget
	if _, err = collectTrafficFile(rotated, s, &budget, nil); err != nil {
		t.Fatal(err)
	}
	if len(s.Live) != 2 {
		t.Fatal("compressed catch-up lost uncheckpointed tail")
	}
	for i, invalid := range []string{`null`, `{}`, `{"started":"2026-01-01T00:00:00Z","offsets":{},"days":{"2026-10-31":null}}`} {
		badPath := filepath.Join(dir, fmt.Sprintf("bad-%d.json", i))
		_ = os.WriteFile(badPath, []byte(invalid), 0600)
		if _, err = readTrafficState(badPath); err == nil {
			t.Fatal("corrupt history accepted", invalid)
		}
	}
}

func TestWebsiteTrafficNginxSyntaxWhenInstalled(t *testing.T) {
	nginx, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("Nginx binary is not installed; destination Linux validation remains required")
	}
	dir := filepath.ToSlash(t.TempDir())
	config := fmt.Sprintf("pid %q;\nerror_log %q;\nevents {}\nhttp {\n%s\nserver { listen 127.0.0.1:19097; access_log %q litecp_traffic; return 200 'ok'; }\n}\n", dir+"/nginx.pid", dir+"/error.log", trafficNginxFormat, dir+"/traffic.log")
	path := dir + "/nginx.conf"
	if err = os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(nginx, "-t", "-p", dir+"/", "-c", path).CombinedOutput(); err != nil {
		t.Fatalf("shared analytics log configuration invalid: %v\n%s", err, output)
	}
}
