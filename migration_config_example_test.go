package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenericMigrationExampleConfiguration(t *testing.T) {
	for _, active := range []string{"config.php", "config.local.php"} {
		t.Run(active, func(t *testing.T) {
			public := t.TempDir()
			config := `<?php return ['database'=>'source','username'=>'user','password'=>'secret','host'=>'localhost'];`
			example := strings.Replace(config, "'source'", "'example_database'", 1)
			for name, data := range map[string]string{active: config, "config.example.php": example} {
				if err := os.WriteFile(filepath.Join(public, name), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			plans, _, err := genericMigrationDatabasePlan(public, filepath.Join(public, "absent"), []string{"mysql/source.sql"})
			if err != nil || len(plans) != 1 || plans[0].Path != active {
				t.Fatalf("plans=%v err=%v", plans, err)
			}
			data, err := os.ReadFile(filepath.Join(public, "config.example.php"))
			if err != nil || string(data) != example {
				t.Fatal("example was modified", err)
			}
		})
	}
	t.Run("unmatched active configuration remains blocked", func(t *testing.T) {
		public := t.TempDir()
		config := `<?php return ['database'=>'missing','username'=>'user','password'=>'secret','host'=>'localhost'];`
		if err := os.WriteFile(filepath.Join(public, "config.php"), []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := genericMigrationDatabasePlan(public, filepath.Join(public, "absent"), []string{"mysql/source.sql"}); err == nil {
			t.Fatal("missing active database accepted")
		}
	})
	t.Run("standalone example still requires review", func(t *testing.T) {
		public := t.TempDir()
		path := filepath.Join(public, "config.example.php")
		if err := os.WriteFile(path, []byte("<?php"), 0600); err != nil {
			t.Fatal(err)
		}
		if migrationConfigExampleHasActiveSibling(path) {
			t.Fatal("example without active sibling ignored")
		}
	})
}
