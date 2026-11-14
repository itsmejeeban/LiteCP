package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResourceCPUUsesWholePublishedInterval(t *testing.T) {
	start := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	previous := resourceCPUReading{}.update(resourceSample{At: start, UsageUsec: 100}, start)
	if previous.percent != nil {
		t.Fatal("first reading needs a baseline")
	}
	// All 600 ms of CPU time can occur before the final one-second check.
	next := resourceSample{At: start.Add(3 * time.Second), UsageUsec: 600100}
	reading := previous.update(next, next.At)
	if reading.percent == nil || *reading.percent != 20 {
		t.Fatalf("expected 20 percent over the published interval, got %+v", reading)
	}
	if repeated := reading.update(next, next.At.Add(time.Second)); repeated.percent != reading.percent {
		t.Fatal("reading the same snapshot again lost the measurement")
	}
	idle := resourceSample{At: next.At.Add(3 * time.Second), UsageUsec: next.UsageUsec}
	reading = reading.update(idle, idle.At)
	if reading.percent == nil || *reading.percent != 0 {
		t.Fatal("an idle interval must report a measured zero")
	}
	reset := resourceSample{At: idle.At.Add(3 * time.Second), UsageUsec: 50}
	reading = reading.update(reset, reset.At)
	if reading.percent != nil {
		t.Fatal("counter resets require a new baseline")
	}
	if got := reading.update(reset, reset.At.Add(10*time.Second)); !got.sample.At.IsZero() {
		t.Fatal("stale snapshots must not supply a baseline")
	}
	late := resourceSample{At: reset.At.Add(20 * time.Second), UsageUsec: 300}
	if got := reading.update(late, late.At); got.percent != nil {
		t.Fatal("a gap in sampling must not appear as current usage")
	}
}

func TestResourceListOnlyConfiguredWebsites(t *testing.T) {
	p := validResourcePolicy()
	p.Enabled = false
	app := &App{system: &System{demo: true}, store: &Store{cfg: Config{Sites: []Site{{ID: "saved", Domain: "saved.test", ResourceLimits: &p}, {ID: "other", Domain: "other.test"}}}}}
	result := httptest.NewRecorder()
	app.websiteResourceLimits(result, httptest.NewRequest("GET", "/api/sites/resource-limits", nil))
	var response struct {
		Items []websiteResourceRow `json:"items"`
	}
	if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &response) != nil || len(response.Items) != 1 {
		t.Fatalf("unexpected list: %d %s", result.Code, result.Body.String())
	}
	row := response.Items[0]
	if row.ID != "saved" || row.Enforced || row.State != "Disabled" || row.Usage.Available {
		t.Fatalf("misleading resource state: %+v", row)
	}
}

func TestResourceObservedProcessParsing(t *testing.T) {
	p := parseWebsiteProcesses("1001 42 10 2 1024\n0 1 10 1 100\n1001 43 NaN 1 100\n1001 44 10 1 -1\n")
	if len(p) != 1 || p[42].RSS != 1048576 {
		t.Fatalf("invalid counters: %+v", p)
	}
}

func TestResourceObservedUsageIntervals(t *testing.T) {
	before := parseWebsiteProcesses("1001 42 10 2 1024\n1002 99 10 2 1024")
	now := parseWebsiteProcesses("1001 42 13 4 2048\n1002 99 13 4 1024")
	u := aggregateWebsiteProcesses(1001, before, now, 3)
	if !u.Available || u.Processes != 1 || u.Memory != 2097152 || u.CPU == nil || *u.CPU < 66 || *u.CPU > 67 {
		t.Fatalf("wrong interval: %+v", u)
	}
	if aggregateWebsiteProcesses(1001, nil, now, 0).CPU != nil {
		t.Fatal("first sample must not invent CPU usage")
	}
	if aggregateWebsiteProcesses(0, before, now, 3).Available {
		t.Fatal("root must not be attributed to a website")
	}
	reused := parseWebsiteProcesses("1001 42 1 5 2048")
	if v := aggregateWebsiteProcesses(1001, before, reused, 3); v.CPU == nil || *v.CPU != 0 {
		t.Fatal("reused PID contributed CPU")
	}
}
