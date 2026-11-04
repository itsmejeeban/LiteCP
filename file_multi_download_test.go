package main

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectedDownloadZIPAndTraversal(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	store.cfg.Sites = []Site{{ID: "zip-site", Domain: "zip.test", Root: root}}
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	app := &App{store: store, system: NewSystem(t.TempDir())}
	request := func(paths []string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/files/download-selected?siteId=zip-site", strings.NewReader(url.Values{"paths": paths}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		app.downloadSelectedFiles(w, r)
		return w
	}
	w := request([]string{"a.txt", "b.txt"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != 2 {
		t.Fatal("wrong ZIP member count")
	}
	for _, f := range archive.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(r)
		r.Close()
		if err != nil || string(content) != f.Name {
			t.Fatal("ZIP content mismatch")
		}
	}
	if request([]string{"a.txt", "../escape"}).Code != 400 {
		t.Fatal("traversal accepted")
	}
	if request([]string{"a.txt", "a.txt"}).Code != 400 {
		t.Fatal("duplicates accepted")
	}
	if request([]string{"a.txt", "missing.txt"}).Code != 404 {
		t.Fatal("missing file accepted")
	}
	get := func(values url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/files/download-selected?"+values.Encode(), nil)
		w := httptest.NewRecorder()
		app.downloadSelectedFiles(w, r)
		return w
	}
	w = get(url.Values{"siteId": {"zip-site"}, "paths": {"a.txt", "b.txt"}})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	archive, err = zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil || len(archive.File) != 2 {
		t.Fatal("GET download did not produce both ZIP members", err)
	}
	if get(url.Values{"siteId": {"zip-site"}, "paths": {"a.txt", "../escape"}}).Code != 400 {
		t.Fatal("GET traversal accepted")
	}
	if get(url.Values{"siteId": {"server-explorer"}, "paths": {"a", "b"}}).Code != 403 {
		t.Fatal("server explorer selection accepted")
	}
	if get(url.Values{"siteId": {"zip-site"}, "paths": {strings.Repeat("a", 8300), "b"}}).Code != 414 {
		t.Fatal("oversized selection accepted")
	}
}
