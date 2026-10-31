package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestCachedImageAssets(t *testing.T) {
	assets := fstest.MapFS{"icon.svg": {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)}}
	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(404)
	})
	handler := cachedImageAssets(fallback, assets)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/icon.svg?v=1", nil))
	if response.Code != 200 || response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || response.Header().Get("ETag") == "" {
		t.Fatalf("image was not cached: %v", response.Result())
	}
	request := httptest.NewRequest("GET", "/icon.svg?v=1", nil)
	request.Header.Set("If-None-Match", response.Header().Get("ETag"))
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, request)
	if second.Code != 304 || second.Body.Len() != 0 {
		t.Fatal("unchanged image should return 304 without a body")
	}
	for _, url := range []string{"/", "/api/state", "/missing.svg"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("unexpected caching of %s", url)
		}
	}
}
