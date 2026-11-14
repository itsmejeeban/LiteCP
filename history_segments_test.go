package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryDailySegmentsPreserveLegacyAndBoundQueries(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	legacy := filepath.Join(dir, "old.jsonl")
	original := []byte("{\"value\":\"legacy\"}\n")
	if err := os.WriteFile(legacy, original, 0600); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		at    time.Time
		value string
	}{{now.AddDate(0, 0, -4), "archived"}, {now, "current"}} {
		if err := appendHistoryRecord(dir, "test", row.at, map[string]string{"value": row.value}); err != nil {
			t.Fatal(err)
		}
	}
	found := map[string]bool{}
	err := scanHistoryRecords(dir, "test", "old.jsonl", now.AddDate(0, 0, -1), 4096, func(line []byte) {
		var v map[string]string
		if json.Unmarshal(line, &v) == nil {
			found[v["value"]] = true
		}
	})
	if err != nil || !found["legacy"] || !found["current"] || found["archived"] {
		t.Fatal("date selection or legacy compatibility failed", found, err)
	}
	after, _ := os.ReadFile(legacy)
	if string(after) != string(original) {
		t.Fatal("legacy history was changed")
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "history", "test", "*.jsonl"))
	if len(matches) != 2 {
		t.Fatal("archived segments were removed")
	}
}

func TestHistoryDailySegmentsShareTailBudgetWithLegacy(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "legacy.jsonl")
	if err := os.WriteFile(legacy, []byte("old row\nrecent row\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := appendHistoryLines(dir, "test", time.Now(), []byte("new row\n")); err != nil {
		t.Fatal(err)
	}
	var found string
	err := scanHistoryRecords(dir, "test", "legacy.jsonl", time.Now().Add(-time.Hour), int64(len("recent row\nnew row\n")), func(line []byte) {
		found += string(line) + "\n"
	})
	if err != nil || found != "recent row\nnew row\n" {
		t.Fatalf("tail budget missed boundary or included excess records: %q, %v", found, err)
	}
}
