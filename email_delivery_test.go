package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmailDeliveryRequiresAuthentication(t *testing.T) {
	a := &App{}
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, httptest.NewRequest("GET", "/api/mail/delivery", nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}
func TestParseEmailDeliveryStatuses(t *testing.T) {
	logs := []string{
		`{"SYSLOG_IDENTIFIER":"postfix/qmgr","MESSAGE":"ABC123: from=<sender@example.test>, size=30, nrcpt=1 (queue active)","__REALTIME_TIMESTAMP":"1000000"}`,
		`{"SYSLOG_IDENTIFIER":"postfix/smtp","MESSAGE":"ABC123: to=<to@example.test>, relay=none, status=deferred (Connection timed out)","__REALTIME_TIMESTAMP":"2000000"}`,
		`{"SYSLOG_IDENTIFIER":"postfix/smtp","MESSAGE":"ABC123: to=<to@example.test>, relay=mx, status=sent (250 accepted)","__REALTIME_TIMESTAMP":"3000000"}`,
		`{"SYSLOG_IDENTIFIER":"postfix/lmtp","MESSAGE":"XYZ789: to=<local@example.test>, relay=local, status=sent (250 Saved)","__REALTIME_TIMESTAMP":"4000000"}`,
		`{"SYSLOG_IDENTIFIER":"postfix/smtpd","MESSAGE":"NOQUEUE: reject: RCPT from host: 550 rejected; from=<bad@example.test> to=<unknown@example.test>","__REALTIME_TIMESTAMP":"5000000"}`,
		`{"SYSLOG_IDENTIFIER":"dovecot","MESSAGE":"Login user=qa","__REALTIME_TIMESTAMP":"6000000"}`,
		`not json`,
	}
	items := parseEmailDeliveries(strings.Join(logs, "\n"))
	if len(items) != 4 {
		t.Fatalf("got %+v", items)
	}
	for i, status := range []string{"rejected", "delivered", "sent", "deferred"} {
		if items[i].Status != status {
			t.Fatalf("%d: %+v", i, items[i])
		}
	}
	if items[2].Sender != "sender@example.test" || items[0].Recipient != "unknown@example.test" {
		t.Fatal("address metadata missing")
	}
}
func TestEmailDeliveryQueuedAndRemoved(t *testing.T) {
	log := `{"SYSLOG_IDENTIFIER":"postfix/qmgr","MESSAGE":"ABC123: from=<>, size=30, nrcpt=1 (queue active)","__REALTIME_TIMESTAMP":"1000000"}`
	if items := parseEmailDeliveries(log); len(items) != 1 || items[0].Status != "queued" {
		t.Fatal(items)
	}
	log += "\n" + `{"SYSLOG_IDENTIFIER":"postfix/qmgr","MESSAGE":"ABC123: removed","__REALTIME_TIMESTAMP":"2000000"}`
	if items := parseEmailDeliveries(log); len(items) != 0 {
		t.Fatal(items)
	}
}
