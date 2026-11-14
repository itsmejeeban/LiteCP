package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTrafficCheckpointBatchesAndReplaysAfterRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state", "site.json")
	log := filepath.Join(dir, "traffic.log")
	line := trafficFixture(t, "batch-one", nil)
	if err := os.WriteFile(log, line, 0600); err != nil {
		t.Fatal(err)
	}
	service := &trafficService{}
	checkpoint, err := service.trafficCheckpoint(path)
	if err != nil {
		t.Fatal(err)
	}
	budget := int64(trafficReadBudget)
	changed, err := collectTrafficFile(log, checkpoint.state, &budget, nil)
	if err != nil || !changed {
		t.Fatal("collection failed", err)
	}
	checkpoint.dirty = true
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("unscheduled checkpoint was persisted")
	}
	reused, err := service.trafficCheckpoint(path)
	if err != nil || reused != checkpoint {
		t.Fatal("decoded state was not reused", err)
	}
	// A new collector starts at the persisted offset (zero here) and replays
	// the unflushed request, counting it once.
	restarted := &trafficService{}
	replay, err := restarted.trafficCheckpoint(path)
	if err != nil {
		t.Fatal(err)
	}
	budget = trafficReadBudget
	_, err = collectTrafficFile(log, replay.state, &budget, nil)
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := json.Marshal(checkpoint.state)
	// Started timestamps differ across a fresh uncheckpointed collector.
	replay.state.Started = checkpoint.state.Started
	actual, _ := json.Marshal(replay.state)
	if string(expected) != string(actual) {
		t.Fatal("replay differed from original counters")
	}
	if err := service.flushTrafficCheckpoint(path, checkpoint); err != nil {
		t.Fatal(err)
	}
	durable, err := readTrafficState(path)
	if err != nil {
		t.Fatal(err)
	}
	budget = trafficReadBudget
	changed, err = collectTrafficFile(log, durable, &budget, nil)
	if err != nil || changed {
		t.Fatal("flushed request replayed twice", err)
	}
}

func TestTrafficCheckpointDoesNotOverwriteExternalChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "site.json")
	service := &trafficService{}
	c, err := service.trafficCheckpoint(path)
	if err != nil {
		t.Fatal(err)
	}
	c.dirty = true
	if err := os.WriteFile(path, []byte("external change"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := service.flushTrafficCheckpoint(path, c); err == nil {
		t.Fatal("external checkpoint overwritten")
	}
	if _, err := service.trafficCheckpoint(path); err == nil {
		t.Fatal("dirty state discarded after external change")
	}
}

func TestTrafficCheckpointEvictsWithDurableOffsets(t *testing.T) {
	dir := t.TempDir()
	service := &trafficService{}
	first := filepath.Join(dir, "first.json")
	c, err := service.trafficCheckpoint(first)
	if err != nil {
		t.Fatal(err)
	}
	c.used = time.Now().Add(-time.Hour)
	c.dirty = true
	for i := 0; i < trafficCacheEntries; i++ {
		_, err := service.trafficCheckpoint(filepath.Join(dir, time.Unix(int64(i), 0).Format("150405")+".json"))
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(service.states) > trafficCacheEntries {
		t.Fatal("cache entry bound exceeded")
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatal("dirty eviction was not persisted", err)
	}
}

func TestTrafficCheckpointBudgetIncludesGrowingEntries(t *testing.T) {
	dir := t.TempDir()
	service := &trafficService{}
	for i := 0; i < 10; i++ {
		c, err := service.trafficCheckpoint(filepath.Join(dir, time.Unix(int64(i), 0).Format("150405")+".json"))
		if err != nil {
			t.Fatal(err)
		}
		c.bytes = 1 << 20
		c.dirty = true
	}
	if err := service.trimTrafficCheckpoints(); err != nil {
		t.Fatal(err)
	}
	var total int64
	for _, c := range service.states {
		total += c.bytes
	}
	if total > trafficCacheBytes {
		t.Fatal("growing cache exceeded its byte budget", total)
	}
}

func TestTrafficCheckpointEvictionSkipsModifiedSite(t *testing.T) {
	dir := t.TempDir()
	service := &trafficService{}
	modified := filepath.Join(dir, "oldest.json")
	c, err := service.trafficCheckpoint(modified)
	if err != nil {
		t.Fatal(err)
	}
	c.used = time.Now().Add(-time.Hour)
	c.dirty = true
	if err := os.WriteFile(modified, []byte("external checkpoint"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < trafficCacheEntries; i++ {
		if _, err := service.trafficCheckpoint(filepath.Join(dir, time.Unix(int64(i), 0).Format("150405")+".json")); err != nil {
			t.Fatal("external checkpoint blocked unrelated sites", err)
		}
	}
	if len(service.states) != trafficCacheEntries || service.states[modified] != c || !c.dirty {
		t.Fatal("external checkpoint or pending state was discarded")
	}
}
