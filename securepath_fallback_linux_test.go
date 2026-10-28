//go:build linux

package main

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestManagedFallbackRefusesEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "file.txt"), []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "nested", "file.txt"), filepath.Join(root, "file-link")); err != nil {
		t.Fatal(err)
	}
	fd, err := openSecureRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	for _, path := range []string{".", "nested/file.txt"} {
		opened, err := openManagedAtFallback(fd, path, unix.O_RDONLY, 0)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		unix.Close(opened)
	}
	for _, path := range []string{"../outside", "/etc/passwd", "link/file.txt", "file-link"} {
		opened, err := openManagedAtFallback(fd, path, unix.O_RDONLY, 0)
		if err == nil {
			unix.Close(opened)
			t.Fatalf("accepted unsafe path %s", path)
		}
	}
}
