package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmptyProvisioningScaffold(t *testing.T) {
	for _, tc := range []struct {
		name, extra string
		file, want  bool
	}{
		{name: "failed provision", want: true},
		{name: "backup retained", extra: "backups/manual/backup.zip", file: true},
		{name: "private file retained", extra: "tmp/important.txt", file: true},
		{name: "public root retained", extra: "public_html"},
		{name: "unknown directory", extra: "application"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			for _, name := range siteHomeScaffoldDirectories {
				if err := os.MkdirAll(filepath.Join(home, name), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if tc.extra != "" {
				path := filepath.Join(home, tc.extra)
				var err error
				if tc.file {
					err = os.WriteFile(path, []byte("preserve"), 0600)
				} else {
					err = os.Mkdir(path, 0700)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			got, err := emptyProvisioningScaffold(home)
			if err != nil || got != tc.want {
				t.Fatalf("empty=%v, err=%v", got, err)
			}
		})
	}
	t.Run("incomplete scaffold", func(t *testing.T) {
		got, err := emptyProvisioningScaffold(t.TempDir())
		if err != nil || got {
			t.Fatalf("empty=%v, err=%v", got, err)
		}
	})
	t.Run("symbolic link", func(t *testing.T) {
		home := t.TempDir()
		for _, name := range siteHomeScaffoldDirectories {
			if err := os.MkdirAll(filepath.Join(home, name), 0700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(t.TempDir(), filepath.Join(home, "linked")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		got, err := emptyProvisioningScaffold(home)
		if err != nil || got {
			t.Fatalf("empty=%v, err=%v", got, err)
		}
	})
}
