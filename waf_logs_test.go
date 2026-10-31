package main

import (
	"strings"
	"testing"
)

func TestWAFLogClassification(t *testing.T) {
	record := func(id, action string) string {
		return "--" + id + "-A--\n[01/Nov/2026:01:00:00 +0530] id\n--" + id + "-B--\nCookie: secret\n--" + id + "-H--\nMessage: Warning. [id \"942100\"] [msg \"SQL Injection detected\"]\n" + action + "\n--" + id + "-Z--\n"
	}
	data := record("abc", "Engine-Mode: DETECTION_ONLY") + record("def", "Action: Intercepted (phase 2)")
	for _, blocked := range []bool{false, true} {
		rows := parseWAFLogs(data, blocked)
		if len(rows) != 1 || rows[0].Blocked != blocked || !strings.Contains(rows[0].Message, "942100") {
			t.Fatal(rows)
		}
		if strings.Contains(rows[0].Message, "secret") {
			t.Fatal("sensitive headers leaked")
		}
	}
	if len(parseWAFLogs("--abc-A--\n--abc-H--\nMessage: Warning [msg \"incomplete\"]", false)) != 0 {
		t.Fatal("partial record exposed")
	}
}
