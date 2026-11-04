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

func TestWAFLogV3AuditFormat(t *testing.T) {
	data := "---7QpDtpSR---A--\r\n[04/Nov/2026:21:54:07 +0530] id\r\n---7QpDtpSR---B--\r\nAuthorization: secret\r\n---7QpDtpSR---C--\r\npassword=secret\r\n---7QpDtpSR---H--\r\nModSecurity: Access denied with code 403 (phase 2). [id \"7000001\"] [msg \"Website rule qa: Test path\"]\r\n---7QpDtpSR---Z--\r\n"
	rows := parseWAFLogs(data, true)
	if len(rows) != 1 || !rows[0].Blocked || !strings.Contains(rows[0].Message, "7000001") || strings.Contains(rows[0].Message, "secret") {
		t.Fatal(rows)
	}
	if len(parseWAFLogs(data, false)) != 0 {
		t.Fatal("blocked event classified as detected")
	}
	if len(parseWAFLogs(strings.ReplaceAll(data, "---7QpDtpSR---Z--", ""), true)) != 0 {
		t.Fatal("partial v3 event exposed")
	}
}
