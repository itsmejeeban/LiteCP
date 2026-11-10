package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type releaseTestTransport func(*http.Request) (*http.Response, error)

func TestManualReleaseCheckRefreshesCachedVersion(t *testing.T) {
	previous := http.DefaultTransport
	defer func() { http.DefaultTransport = previous; releaseCache.until = time.Time{} }()
	releaseCache.until = time.Time{}
	tag := "v5.0.37"
	calls := 0
	http.DefaultTransport = releaseTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 2 && r.Header.Get("Cache-Control") != "no-cache" {
			t.Error("manual check must request upstream revalidation")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"tag_name":"` + tag + `"}`))}, nil
	})
	if got := checkReleaseWithToken("5.0.36", "").Latest; got != tag {
		t.Fatalf("initial release: %s", got)
	}
	tag = "v5.0.39"
	if got := checkReleaseWithToken("5.0.36", "").Latest; got != "v5.0.37" || calls != 1 {
		t.Fatalf("automatic check did not reuse the cached release: %s, calls=%d", got, calls)
	}
	if got := lookupReleaseWithToken("5.0.36", "", true).Latest; got != tag || calls != 2 {
		t.Fatalf("manual check did not fetch the new release: %s, calls=%d", got, calls)
	}
}

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
	result := checkReleaseWithToken("5.0.2", "")
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

func TestPrivateReleaseAuthenticationAndCredentialRefresh(t *testing.T) {
	previous := http.DefaultTransport
	defer func() { http.DefaultTransport = previous; releaseCache.until = time.Time{} }()
	releaseCache.until = time.Time{}
	calls := 0
	http.DefaultTransport = releaseTestTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.Host != "api.github.com" {
			t.Fatal("Unexpected credential destination")
		}
		if calls == 1 {
			if request.Header.Get("Authorization") != "" {
				t.Fatal("Unexpected anonymous credential")
			}
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}
		if request.Header.Get("Authorization") != "Bearer test_read_only_token_123456789" {
			t.Fatal("Missing private-release authorization")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v5.0.6"}`))}, nil
	})
	if checkReleaseWithToken("5.0.5", "").Checked {
		t.Fatal("Anonymous access unexpectedly succeeded")
	}
	result := checkReleaseWithToken("5.0.5", "test_read_only_token_123456789")
	if !result.Checked || !result.Available || calls != 2 {
		t.Fatalf("Credential change must invalidate failed cache: %+v", result)
	}
}

func TestPrivateUpdateCredentialValidation(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Skip("Linux root ownership validation")
	}
	path := filepath.Join(t.TempDir(), "token")
	if token, err := readPrivateUpdateToken(path); err != nil || token != "" {
		t.Fatalf("Missing optional credential: %v", err)
	}
	if err := os.WriteFile(path, []byte("test_read_only_token_123456789\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if token, err := readPrivateUpdateToken(path); err != nil || token != "test_read_only_token_123456789" {
		t.Fatalf("Valid credential rejected: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateUpdateToken(path); err == nil {
		t.Fatal("World-readable credential accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test_token_123456789\nInjectedHeader"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateUpdateToken(path); err == nil {
		t.Fatal("Header injection accepted")
	}
	link := filepath.Join(filepath.Dir(path), "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateUpdateToken(link); err == nil {
		t.Fatal("Symlink credential accepted")
	}
}

func TestPrivateReleaseAuthFailureDoesNotExposeToken(t *testing.T) {
	previous := http.DefaultTransport
	defer func() { http.DefaultTransport = previous; releaseCache.until = time.Time{} }()
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		releaseCache.until = time.Time{}
		http.DefaultTransport = releaseTestTransport(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		})
		result := checkReleaseWithToken("5.0.5", "test_read_only_token_123456789")
		if result.Checked || !strings.Contains(result.Message, "GitHub rejected") || strings.Contains(result.Message, "test_read_only_token") {
			t.Fatalf("Unsafe authentication failure: %+v", result)
		}
	}
}
