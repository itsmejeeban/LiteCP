package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAPICatalogCoversPanelRoutes(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	excluded := map[string]bool{"/api/login": true, "/api/logout": true, "/api/v1": true, "/api/v1/request": true, "/api/phpmyadmin/": true}
	paths := map[string]bool{}
	names := map[string]bool{}
	for _, p := range apiActions {
		if names[p.Name] {
			t.Fatal("duplicate permission", p.Name)
		}
		names[p.Name] = true
	}
	for name, route := range apiRouteActions {
		if excluded[route.Path] || strings.HasPrefix(route.Path, "/internal/") {
			t.Fatal("session or recursive route exposed", name)
		}
		if route.Method != "GET" && route.Method != "POST" && route.Method != "PUT" && route.Method != "DELETE" {
			t.Fatal("unexpected method", route)
		}
		path := route.Path
		if route.ObjectID {
			path += "fixture"
		}
		request := httptest.NewRequest(route.Method, "https://panel.example"+path, nil)
		_, pattern := (&App{}).routeMux().Handler(request)
		if pattern != route.Path {
			t.Fatal("catalog route mismatch", name, pattern)
		}
		if !brokerAllowedOperations[route.Path] && route.Unavailable == "" {
			t.Fatal("catalog route unavailable through broker", route.Path)
		}
		paths[route.Path] = true
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) < 1 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "HandleFunc" {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			return true
		}
		path, _ := strconv.Unquote(lit.Value)
		if strings.HasPrefix(path, "/api/") && !excluded[path] && !paths[path] {
			t.Errorf("panel route lacks explicit API permission: %s", path)
		}
		return true
	})
}

func TestAPIStreamingUploadDownloadAndAdministration(t *testing.T) {
	a := apiTestApp(t)
	id, secret := apiTestKey(t, a)
	a.store.cfg.APIRules[0].Permissions = []string{"panel.files.upload.post", "panel.files.download.get", "panel.access.post"}
	root := t.TempDir()
	a.store.cfg.Sites = []Site{{ID: "fixture", Domain: "api.example.com", Root: root}}
	payload := bytes.Repeat([]byte{0, 1, 2, 255, 34, 13, 10}, 20000)
	var body bytes.Buffer
	multipartBody := multipart.NewWriter(&body)
	_ = multipartBody.WriteField("siteId", "fixture")
	_ = multipartBody.WriteField("noReplace", "1")
	part, err := multipartBody.CreateFormFile("files", "api-audit.bin")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(payload)
	_ = multipartBody.Close()
	request := httptest.NewRequest("POST", "https://panel.example/api/v1/request", &body)
	request.Header.Set("Content-Type", multipartBody.FormDataContentType())
	request.Header.Set("X-LiteCP-Identifier", id)
	request.Header.Set("X-LiteCP-Secret", secret)
	request.Header.Set("X-LiteCP-Action", "panel.files.upload.post")
	request.RemoteAddr = "203.0.113.10:1234"
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, request)
	if w.Code != 201 && w.Code != 200 {
		t.Fatal("multipart upload failed", w.Code, w.Body.String())
	}
	saved, err := os.ReadFile(filepath.Join(root, "api-audit.bin"))
	if err != nil || !bytes.Equal(saved, payload) {
		t.Fatal("streamed bytes were changed", err)
	}
	request = httptest.NewRequest("POST", "https://panel.example/api/v1/request?siteId=fixture&path=api-audit.bin", nil)
	request.Header.Set("X-LiteCP-Identifier", id)
	request.Header.Set("X-LiteCP-Secret", secret)
	request.Header.Set("X-LiteCP-Action", "panel.files.download.get")
	request.Header.Set("Range", "bytes=0-9")
	w = httptest.NewRecorder()
	a.routes().ServeHTTP(w, request)
	if w.Code != 206 || !bytes.Equal(w.Body.Bytes(), payload[:10]) || w.Header().Get("Content-Disposition") == "" {
		t.Fatal("range download metadata or bytes lost", w.Code)
	}
	w = apiCall(a, id, secret, "panel.access.post", `{"operation":"save-rule","rule":{"name":"Provisioning audit","permissions":["panel.version.get"]}}`)
	if w.Code != 200 {
		t.Fatal("granted API administration failed", w.Code, w.Body.String())
	}
	if len(a.store.cfg.APIRules) != 2 {
		t.Fatal("API rule was not persisted")
	}
}

func TestAPICatalogDenyByDefaultForEveryAction(t *testing.T) {
	a := apiTestApp(t)
	id, secret := apiTestKey(t, a)
	// Each ungranted action must be rejected before reaching a real handler.
	for name := range apiRouteActions {
		a.apiBuckets = nil
		w := apiCall(a, id, secret, name, "")
		if w.Code != 403 {
			t.Fatal(name, w.Code, w.Body.String())
		}
	}
}

func TestAPIExpandedDispatchAndExactGrant(t *testing.T) {
	a := apiTestApp(t)
	id, secret := apiTestKey(t, a)
	a.store.cfg.APIRules[0].Permissions = []string{"panel.version.get", "panel.access.get", "panel.files.encodings.get"}
	w := apiCall(a, id, secret, "panel.version.get", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "version") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = apiCall(a, id, secret, "panel.access.get", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "secretHash") {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := apiRoutePath(apiRouteAction{Path: "/api/sites/", ObjectID: true}, "../access"); err == nil {
		t.Fatal("suffix traversal accepted")
	}
	r := httptest.NewRequest("GET", "https://panel.example/api/state", nil)
	r.Header.Set("X-LiteCP-Authorized", "true")
	if validExternalAPIGrant(r) {
		t.Fatal("header forged internal grant")
	}
	r = httptest.NewRequest("POST", "https://panel.example/api/v1/request", nil)
	r.Header.Set("X-LiteCP-Identifier", id)
	r.Header.Set("X-LiteCP-Secret", secret)
	r.Header.Set("X-LiteCP-Action", "panel.files.encodings.get")
	r.Header.Set("Cookie", "litecp_session=invalid")
	r.RemoteAddr = "203.0.113.10:1234"
	w = httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "encodings") {
		t.Fatal("raw-mode dispatch failed", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("POST", "https://panel.example/api/v1/request?secret=forbidden", nil)
	r.Header.Set("X-LiteCP-Identifier", id)
	r.Header.Set("X-LiteCP-Secret", secret)
	r.Header.Set("X-LiteCP-Action", "panel.version.get")
	w = httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatal("query secret accepted", w.Code)
	}
}
