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

func TestChangeSiteDomainRelocatesManagedHome(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.cfg.Sites = []Site{{ID: "managed", Domain: "old.example.test", Root: defaultSiteRoot("old.example.test")}}
	app := &App{store: store, system: &System{demo: true}}
	response := httptest.NewRecorder()
	app.changeSiteDomain(response, httptest.NewRequest(http.MethodPut, "/api/sites/domain", strings.NewReader(`{"id":"managed","domain":"new.example.test"}`)))
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if got := store.SiteByID("managed").Root; got != defaultSiteRoot("new.example.test") {
		t.Fatalf("root not updated: %s", got)
	}
	// An already renamed domain must also repair its previous saved home.
	store.cfg.Sites[0].Root = defaultSiteRoot("old.example.test")
	response = httptest.NewRecorder()
	app.changeSiteDomain(response, httptest.NewRequest(http.MethodPut, "/api/sites/domain", strings.NewReader(`{"id":"managed","domain":"new.example.test"}`)))
	if response.Code != http.StatusOK || store.SiteByID("managed").Root != defaultSiteRoot("new.example.test") {
		t.Fatal("existing mismatch not repaired")
	}
}
