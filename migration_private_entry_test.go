package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateMigrationAcceptsStandaloneApplicationFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.php"), []byte("<?php return [];"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "source"), 0700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err = validateMigrationPrivateEntry(entry); err != nil {
			t.Fatal(err)
		}
	}
}

type unsafePrivateEntry struct{ mode os.FileMode }

func (e unsafePrivateEntry) Name() string               { return "unsafe" }
func (e unsafePrivateEntry) IsDir() bool                { return false }
func (e unsafePrivateEntry) Type() os.FileMode          { return e.mode }
func (e unsafePrivateEntry) Info() (os.FileInfo, error) { return nil, nil }
func TestPrivateMigrationRejectsLinksAndSpecialFiles(t *testing.T) {
	for _, mode := range []os.FileMode{os.ModeSymlink, os.ModeNamedPipe, os.ModeSocket, os.ModeDevice} {
		if validateMigrationPrivateEntry(unsafePrivateEntry{mode}) == nil {
			t.Fatalf("accepted unsafe mode %v", mode)
		}
	}
}
