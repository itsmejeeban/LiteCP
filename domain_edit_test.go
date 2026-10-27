package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChangeSiteDomainPreservesWebsiteData(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.cfg.Sites = []Site{{
		ID: "site-one", Domain: "old.example.test", Root: "/var/www/litecp/old.example.test/public",
		SSL: true, Database: "cp_example", DBUser: "cp_example",
		Application: &ApplicationHosting{Language: "node", Certificate: "/old/fullchain.pem", CertificateKey: "/old/key.pem"},
	}}
	store.cfg.Databases = []ManagedDatabase{{ID: "db-one", SiteID: "site-one", Domain: "old.example.test", Name: "cp_example", Username: "cp_example"}}
	app := &App{store: store, system: &System{demo: true}}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/sites/domain", strings.NewReader(`{"id":"site-one","domain":"New.Example.Test"}`))
	app.changeSiteDomain(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("domain update returned %d: %s", response.Code, response.Body.String())
	}
	site := store.SiteByID("site-one")
	if site.Domain != "new.example.test" || site.Root != "/var/www/litecp/old.example.test/public" || site.Database != "cp_example" || site.SSL {
		t.Fatalf("website data changed unexpectedly: %#v", site)
	}
	if site.Application.Certificate != "" || site.Application.CertificateKey != "" {
		t.Fatal("old-domain application certificate was retained")
	}
	database := store.DatabaseBySiteID("site-one")
	if database == nil || database.Domain != "new.example.test" || database.Name != "cp_example" {
		t.Fatalf("database association was not updated safely: %#v", database)
	}
}

func TestChangeSiteDomainRejectsDuplicate(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.cfg.Sites = []Site{{ID: "site-one", Domain: "one.example.test"}, {ID: "site-two", Domain: "two.example.test"}}
	app := &App{store: store, system: &System{demo: true}}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/sites/domain", strings.NewReader(`{"id":"site-one","domain":"two.example.test"}`))
	app.changeSiteDomain(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("duplicate domain returned %d, want %d", response.Code, http.StatusConflict)
	}
	if store.SiteByID("site-one").Domain != "one.example.test" {
		t.Fatal("duplicate domain request changed the website")
	}
}
