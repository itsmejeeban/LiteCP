package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type releaseTestTransport func(*http.Request) (*http.Response, error)

func (f releaseTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestInaccessibleReleaseIsNotSuccessfulCheck(t *testing.T) {
	previous := http.DefaultTransport
	defer func() {
		http.DefaultTransport = previous
		releaseCache.until = time.Time{}
	}()
	http.DefaultTransport = releaseTestTransport(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	releaseCache.until = time.Time{}
	result := checkRelease("5.0.2")
	if result.Checked || result.Available || !strings.Contains(result.Message, "private") {
		t.Fatalf("Inaccessible releases must report an unsuccessful check: %+v", result)
	}
}

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
