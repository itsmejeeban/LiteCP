package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type emailDelivery struct {
	Time      time.Time `json:"time"`
	QueueID   string    `json:"queueId"`
	Sender    string    `json:"sender"`
	Recipient string    `json:"recipient"`
	Status    string    `json:"status"`
	Detail    string    `json:"detail"`
}

var deliveryQueuePattern = regexp.MustCompile(`^([A-Za-z0-9]+):\s`)
var deliveryFromPattern = regexp.MustCompile(`\bfrom=<([^>]*)>`)
var deliveryToPattern = regexp.MustCompile(`\bto=<([^>]*)>`)
var deliveryStatusPattern = regexp.MustCompile(`\bstatus=(sent|deferred|bounced|expired)\b`)

func parseEmailDeliveries(lines string) []emailDelivery {
	senders := map[string]string{}
	pending := map[string]emailDelivery{}
	items := []emailDelivery{}
	for _, line := range strings.Split(lines, "\n") {
		var entry struct {
			Identifier string `json:"SYSLOG_IDENTIFIER"`
			Message    string `json:"MESSAGE"`
			Timestamp  string `json:"__REALTIME_TIMESTAMP"`
		}
		if json.Unmarshal([]byte(line), &entry) != nil || !strings.HasPrefix(entry.Identifier, "postfix/") {
			continue
		}
		micros, err := strconv.ParseInt(entry.Timestamp, 10, 64)
		if err != nil {
			continue
		}
		when := time.UnixMicro(micros).UTC()
		message := entry.Message
		queue := deliveryQueuePattern.FindStringSubmatch(message)
		if len(queue) == 0 {
			continue
		}
		id := queue[1]
		if from := deliveryFromPattern.FindStringSubmatch(message); len(from) > 0 && id != "NOQUEUE" {
			senders[id] = from[1]
			pending[id] = emailDelivery{Time: when, QueueID: id, Sender: from[1], Status: "queued", Detail: "Accepted by Postfix; no delivery result appears in this log window."}
		}
		to := deliveryToPattern.FindStringSubmatch(message)
		status := deliveryStatusPattern.FindStringSubmatch(message)
		if id == "NOQUEUE" && strings.Contains(message, "reject:") {
			item := emailDelivery{Time: when, QueueID: id, Status: "rejected", Detail: message}
			if from := deliveryFromPattern.FindStringSubmatch(message); len(from) > 0 {
				item.Sender = from[1]
			}
			if len(to) > 0 {
				item.Recipient = to[1]
			}
			items = append(items, item)
			continue
		}
		if len(status) > 0 && len(to) > 0 {
			state := status[1]
			if state == "sent" && (strings.HasSuffix(entry.Identifier, "/lmtp") || strings.HasSuffix(entry.Identifier, "/local") || strings.HasSuffix(entry.Identifier, "/virtual")) {
				state = "delivered"
			}
			items = append(items, emailDelivery{Time: when, QueueID: id, Sender: senders[id], Recipient: to[1], Status: state, Detail: message})
			delete(pending, id)
		}
		if strings.HasSuffix(message, ": removed") {
			delete(pending, id)
			delete(senders, id)
		}
	}
	for _, item := range pending {
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Time.After(items[j].Time) })
	if len(items) > 1000 {
		items = items[:1000]
	}
	return items
}

func (a *App) emailDeliveryLogs(w http.ResponseWriter, r *http.Request) {
	ranges := map[string]string{"day": "24 hours ago", "week": "7 days ago", "month": "30 days ago"}
	period := r.URL.Query().Get("range")
	if period == "" {
		period = "week"
	}
	since, ok := ranges[period]
	if !ok {
		writeError(w, 400, "Choose a valid log period.")
		return
	}
	if a.system.demo {
		writeJSON(w, 200, map[string]any{"items": []emailDelivery{}, "range": period, "note": "Delivery logs are available on the installed server."})
		return
	}
	output, err := runCommand(r.Context(), 15*time.Second, "journalctl", "--no-pager", "--since", since, "-n", "20000", "-o", "json", "SYSLOG_FACILITY=2")
	if err != nil {
		writeError(w, 502, "Cannot read mail delivery logs. Check the server journal service.")
		return
	}
	items := parseEmailDeliveries(output)
	writeJSON(w, 200, map[string]any{"items": items, "range": period, "note": "Latest 1,000 delivery events from up to 20,000 retained mail log lines. Sent means accepted by the next mail server; it does not confirm inbox placement."})
}
