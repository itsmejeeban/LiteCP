package main

import (
	"net/http/httptest"
	"testing"
)

func TestDeleteAdditionalDatabasePreservesPrimaryLink(t *testing.T) {
	for _, primary := range []bool{false, true} {
		store, err := OpenStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		name := "extra_db"
		if primary {
			name = "primary_db"
		}
		store.cfg.Sites = []Site{{ID: "site", Database: "primary_db", DBUser: "primary_user"}}
		store.cfg.Databases = []ManagedDatabase{{ID: "db", SiteID: "site", Name: name, Username: "extra_user"}}
		app := &App{store: store, system: &System{demo: true}}
		response := httptest.NewRecorder()
		app.deleteManagedDatabase(response, httptest.NewRequest("DELETE", "/api/databases/db", nil))
		if response.Code != 200 {
			t.Fatal(response.Code, response.Body.String())
		}
		site := store.SiteByID("site")
		if primary {
			if site.Database != "" || site.DBUser != "" {
				t.Fatal("deleted primary remains linked")
			}
		} else if site.Database != "primary_db" || site.DBUser != "primary_user" {
			t.Fatal("deleting additional database changed primary link")
		}
	}
}
