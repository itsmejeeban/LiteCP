//go:build !windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationSQLPreservesHostClientConfiguration(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "mysql-invocations")
	// The installed server puts its socket in the host's normal client defaults.
	// Reject an invocation that suppresses those files, for both phases.
	script := `#!/bin/sh
case "$1" in --defaults-extra-file=*) defaults=${1#*=} ;; *) exit 31 ;; esac
test -f "$defaults" || exit 32
grep -q '^user=audit_target$' "$defaults" || exit 33
case " $* " in *" --user=audit_target "*) ;; *) exit 34 ;; esac
case " $* " in *" --protocol=socket "*) ;; *) exit 35 ;; esac
printf '%s\n' "$*" >> "$LITECP_AUDIT_MYSQL_LOG"
case " $* " in
  *" --execute "*) printf '0\n' ;;
  *)
    case " $* " in *" --binary-mode=1 "*) ;; *) exit 36 ;; esac
    case " $* " in *" --local-infile=0 "*) ;; *) exit 37 ;; esac
    cat > "$LITECP_AUDIT_MYSQL_LOG.sql"
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "mysql"), []byte(script), 0700); err != nil { t.Fatal(err) }
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LITECP_AUDIT_MYSQL_LOG", log)
	system := &System{dataDir: t.TempDir()}
	sql := filepath.Join(t.TempDir(), "fixture.sql")
	contents := "CREATE TABLE audit_fixture(id INT);\n"
	if err := os.WriteFile(sql, []byte(contents), 0600); err != nil { t.Fatal(err) }
	if err := system.importScopedSQL(context.Background(), sql, ManagedDatabase{Name: "audit_fixture", Username: "audit_target"}, "fixture-only-password-20261027"); err != nil { t.Fatal(err) }
	calls, err := os.ReadFile(log)
	if err != nil { t.Fatal(err) }
	if strings.Count(string(calls), "--defaults-extra-file=") != 2 { t.Fatalf("empty check/import options: %s", calls) }
	for _, object := range []string{"tables", "routines", "events"} {
		if !strings.Contains(string(calls), "information_schema."+object) { t.Fatalf("empty check ignores %s", object) }
	}
	imported, err := os.ReadFile(log+".sql")
	if err != nil || string(imported) != contents { t.Fatalf("SQL input changed: %q, %v", imported, err) }
}

func TestMigrationSQLRejectsExistingRoutinesAndEvents(t *testing.T) {
	for _, object := range []string{"routines", "events"} {
		t.Run(object, func(t *testing.T) {
			bin := t.TempDir()
			marker := filepath.Join(t.TempDir(), "unsafe-import-or-reset")
			script := `#!/bin/sh
case " $* " in *" --execute "*)
  case "$*" in *"information_schema.$LITECP_AUDIT_EXISTING_OBJECT"*) printf '1\n'; exit 0 ;; esac
  ;;
esac
printf 'unexpected import/reset' > "$LITECP_AUDIT_UNSAFE_MARKER"
exit 39
`
			if err := os.WriteFile(filepath.Join(bin, "mysql"), []byte(script), 0700); err != nil { t.Fatal(err) }
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("LITECP_AUDIT_EXISTING_OBJECT", object)
			t.Setenv("LITECP_AUDIT_UNSAFE_MARKER", marker)
			sql := filepath.Join(t.TempDir(), "fixture.sql")
			if err := os.WriteFile(sql, []byte("invalid fixture SQL"), 0600); err != nil { t.Fatal(err) }
			system := &System{dataDir: t.TempDir()}
			err := system.importScopedSQL(context.Background(), sql, ManagedDatabase{Name: "audit_fixture", Username: "audit_target"}, "fixture-only-password-20261027")
			if err == nil || !strings.Contains(err.Error(), "must be empty") { t.Fatalf("existing %s accepted: %v", object, err) }
			if _, err := os.Stat(marker); !os.IsNotExist(err) { t.Fatal("nonempty database reached SQL import or reset") }
		})
	}
}
