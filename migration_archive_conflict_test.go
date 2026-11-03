package main

import (
	"archive/tar"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationArchivePreflightConflicts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		entries  []string
		conflict bool
	}{
		{"child-before-file", []string{"public_html/assets/icon.txt", "public_html/assets"}, true},
		{"file-before-child", []string{"public_html/assets", "public_html/assets/icon.txt"}, true},
		{"normalized-windows-name", []string{`public_html/assets\icon.txt`, "public_html/assets/icon.txt"}, true},
		{"normal-files", []string{"public_html/assets/a.txt", "public_html/assets/b.txt"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "source.tar")
			f, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			w := tar.NewWriter(f)
			for _, name := range tc.entries {
				if err = w.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0600}); err != nil {
					t.Fatal(err)
				}
			}
			if err = w.Close(); err != nil {
				t.Fatal(err)
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
			_, err = migrationArchiveBytes(context.Background(), archive)
			if tc.conflict && (err == nil || !strings.Contains(err.Error(), "conflict")) {
				t.Fatalf("expected conflict before extraction, got %v", err)
			}
			if !tc.conflict && err != nil {
				t.Fatal(err)
			}
		})
	}
}
