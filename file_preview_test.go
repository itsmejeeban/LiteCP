package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilePreviewSourceStaysInlineText(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	site := Site{ID: "preview-test", Domain: "preview.example.test", Root: root}
	store.cfg.Sites = []Site{site}
	app := &App{store: store, system: NewSystem(t.TempDir())}
	for _, name := range []string{"index.php", "app.js", "page.html", "image.svg"} {
		body := "<script>alert('preview must not execute')</script>"
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		request := httptest.NewRequest("GET", "/api/files/download?preview=1&siteId="+site.ID+"&path="+name, nil)
		app.downloadFile(response, request)
		if response.Code != 200 || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain") || !strings.HasPrefix(response.Header().Get("Content-Disposition"), "inline") || response.Header().Get("Content-Security-Policy") != "sandbox" || response.Body.String() != body {
			t.Fatalf("unsafe or unusable preview of %s: %d %v", name, response.Code, response.Header())
		}
	}
}
