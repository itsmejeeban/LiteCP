package main

import "testing"

func TestServiceStates(t *testing.T) {
	for input, want := range map[string]string{"active": "running", "inactive": "stopped", "failed": "failed", "activating": "activating", "deactivating": "deactivating", "not-found": "unknown", "": "unknown"} {
		if got := normalizeServiceState(input); got != want {
			t.Errorf("%q: %s != %s", input, got, want)
		}
	}
	for _, test := range []struct {
		states []string
		want   string
	}{
		{nil, "not installed"}, {[]string{"stopped", "stopped"}, "stopped"}, {[]string{"running", "running"}, "running"}, {[]string{"running", "stopped"}, "degraded"}, {[]string{"failed", "failed"}, "failed"},
	} {
		if got := aggregateServiceStates(test.states); got != test.want {
			t.Errorf("%v: %s != %s", test.states, got, test.want)
		}
	}
}
