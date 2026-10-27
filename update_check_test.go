package main

import "testing"

func TestNewerRelease(t *testing.T) {
	for _, c := range []struct {
		next, current string
		want          bool
	}{{"v0.5.2", "0.5.1", true}, {"v0.10.0", "0.9.9", true}, {"v1.0.0", "0.5.1", true}, {"v0.5.1", "0.5.1", false}, {"v0.5.0", "0.5.1", false}, {"v0.5.2-rc1", "0.5.1", false}, {"garbage", "0.5.1", false}, {"v0.5.2", "0.5.1-dev", true}} {
		if got := newerRelease(c.next, c.current); got != c.want {
			t.Errorf("%s vs %s: %v", c.next, c.current, got)
		}
	}
}
