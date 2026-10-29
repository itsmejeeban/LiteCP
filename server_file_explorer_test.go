package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServerExplorerScope(t *testing.T) {
	app := &App{system:&System{}}
	site, err := app.requestSite(httptest.NewRequest(http.MethodGet,"/api/files?siteId=server-explorer&path=etc",nil))
	if err != nil || site == nil || site.Root != "/" { t.Fatalf("server root unavailable: %v",err) }
	for _, path := range []string{"proc", "proc/1", "sys/kernel", "dev/zero", "run/secrets"} {
		if _, err := app.requestSite(httptest.NewRequest(http.MethodGet,"/api/files?siteId=server-explorer&path="+path,nil)); err == nil { t.Fatalf("virtual path allowed: %s",path) }
	}
	if _, err := app.requestSite(httptest.NewRequest(http.MethodDelete,"/api/files?siteId=server-explorer",nil)); err == nil { t.Fatal("server explorer accepted mutation") }
}
