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

func TestTimerServiceLabel(t *testing.T) {
	for _, item := range []struct{ state, sub, want string }{{"running", "waiting", "Scheduled / waiting"}, {"running", "elapsed", "Active timer"}, {"failed", "failed", "failed"}, {"stopped", "dead", "stopped"}} {
		if got := timerServiceLabel(item.state, item.sub); got != item.want {
			t.Fatalf("%+v: %s", item, got)
		}
	}
}
func TestFTPSServiceDetection(t *testing.T) {
	for _, unit := range []string{"litecp-ftps", "vsftpd", "pure-ftpd", "proftpd", ""} {
		if got := ftpsServiceUnit(func(candidate string) bool { return candidate == unit }); got != unit {
			t.Fatalf("wanted %s, got %s", unit, got)
		}
	}
	if got := ftpsServiceUnit(func(string) bool { return true }); got != "litecp-ftps" {
		t.Fatal("LiteCP-managed service must take precedence", got)
	}
}
