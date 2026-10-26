package main

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestCurrentResolverParsing(t *testing.T) {
	content := "# NetworkManager\nnameserver 185.12.64.1\nnameserver 2a01:4ff:ff00::add:2 # IPv6\nsearch example.test\nnameserver invalid\nnameserver 185.12.64.1\n"
	want := []string{"185.12.64.1", "2a01:4ff:ff00::add:2"}
	if got := parseSystemResolvers(content); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v; want %v", got, want)
	}
	if values := parseSystemResolvers(""); values == nil || len(values) != 0 {
		t.Fatal("empty resolver configuration must produce an empty array")
	}
}

func TestGlobalTimezoneCatalog(t *testing.T) {
	content, err := webAssets.ReadFile("web/timezones.json")
	if err != nil {
		t.Fatal(err)
	}
	var zones []string
	if err := json.Unmarshal(content, &zones); err != nil {
		t.Fatal(err)
	}
	if len(zones) < 400 {
		t.Fatal("global time-zone catalog is incomplete")
	}
	seen := map[string]bool{}
	for _, zone := range zones {
		if seen[zone] {
			t.Fatalf("duplicate timezone %s", zone)
		}
		seen[zone] = true
		if _, err := time.LoadLocation(zone); err != nil {
			t.Errorf("unsupported server timezone %s: %v", zone, err)
		}
	}
	for _, zone := range []string{"UTC", "Asia/Kolkata", "Pacific/Auckland", "Africa/Lagos", "America/New_York", "Europe/London"} {
		if !seen[zone] {
			t.Errorf("missing timezone %s", zone)
		}
	}
}
