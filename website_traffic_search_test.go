package main

import "testing"

func TestTrafficSearchFields(t *testing.T) {
	e := trafficEvent{Path: "/checkout", Method: "POST", Referrer: "example.org", Address: "192.0.2.0/24", Protocol: "HTTP/2.0", Cache: "HIT", Browser: "Firefox"}
	for field, query := range map[string]string{"page": "CHECKOUT", "method": "post", "referral": "example.org", "address": "192.0.2.", "protocol": "HTTP/2", "cache": "hit", "client": "firefox", "all": "POST"} {
		if !matchesTrafficSearch(e, field, query) {
			t.Errorf("missing match for %s", field)
		}
	}
	if matchesTrafficSearch(e, "method", "checkout") {
		t.Fatal("field-specific search leaked another field")
	}
	e.Cache = "-"
	if !matchesTrafficSearch(e, "cache", "unavailable") {
		t.Fatal("missing cache display match")
	}
}
