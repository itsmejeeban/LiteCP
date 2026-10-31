package main

import "testing"

func TestNativeBackupDatabaseMapping(t *testing.T) {
	site := Site{ID: "site-a", Database: "app_db", DBUser: "app_user"}
	a := &App{store: &Store{}}
	plan, err := a.nativeBackupDatabasePlan(site, []string{"database.sql"})
	if err != nil || plan["database.sql"].Name != "app_db" || plan["database.sql"].Username != "app_user" {
		t.Fatalf("invalid plan: %v %v", plan, err)
	}
	for _, dumps := range [][]string{{"other.sql"}, {"app_db.sql", "app_db.sql"}} {
		if _, err = a.nativeBackupDatabasePlan(site, dumps); err == nil {
			t.Fatalf("unsafe mapping accepted: %v", dumps)
		}
	}
}
func TestNativeBackupRejectsAnotherWebsiteDatabase(t *testing.T) {
	a := &App{store: &Store{}}
	a.store.cfg.Databases = []ManagedDatabase{{SiteID: "other-site", Name: "other_db", Username: "other_user"}}
	if _, err := a.nativeBackupDatabasePlan(Site{ID: "site-a"}, []string{"other_db.sql"}); err == nil {
		t.Fatal("other website database accepted")
	}
}
