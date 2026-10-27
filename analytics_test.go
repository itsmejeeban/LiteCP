package main

import (
	"testing"
	"time"
)

func TestUsageAggregation(t *testing.T) {
	at := time.Date(2026, 10, 28, 13, 1, 0, 0, time.UTC)
	samples := []usageSample{{Time: at, CPU: 10, Memory: 40, Disk: 60}, {Time: at.Add(time.Minute), CPU: 30, Memory: 60, Disk: 80}, {Time: at.Add(-48 * time.Hour), CPU: 99}}
	values := aggregateUsage(samples, "hourly", at.Add(-time.Hour))
	if len(values) != 1 || values[0].CPU != 20 || values[0].Memory != 50 || values[0].Disk != 70 {
		t.Fatalf("unexpected averages: %+v", values)
	}
	if usageBucket(at, "weekly").Weekday() != time.Monday {
		t.Fatal("weekly buckets must start Monday")
	}
	if usageBucket(at, "monthly").Day() != 1 {
		t.Fatal("monthly buckets must start on day one")
	}
}
