package main

import (
	"os"
	"strings"
	"testing"
)

// Optional read-only probe of a deployed website under the service sandbox.
func TestFileManagerRuntimeListing(t *testing.T) {
	root := os.Getenv("LITECP_VERIFY_ROOT")
	if root == "" {
		t.Skip("set LITECP_VERIFY_ROOT for the service runtime check")
	}
	s := NewSystem(t.TempDir())
	listing, err := s.ListFiles(Site{ID: "runtime-check", Root: root}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Listed %d entries", len(listing.Entries))
}

func TestFileManagerCRUD(t *testing.T) {
	s := &System{dataDir: t.TempDir(), demo: true}
	site := Site{ID: "file-test", Root: t.TempDir()}
	if err := s.CreateManagedDirectory(site, "", "docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateManagedFile(site, "docs", "test.txt"); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteManagedFile(site, "docs/test.txt", "edited"); err != nil {
		t.Fatal(err)
	}
	content, _, err := s.ReadManagedFile(site, "docs/test.txt")
	if err != nil || content != "edited" {
		t.Fatalf("read edit: %q, %v", content, err)
	}
	if _, err := s.SaveNewUpload(site, "docs", "upload.txt", strings.NewReader("uploaded")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveNewUpload(site, "docs", "upload.txt", strings.NewReader("overwrite")); err == nil {
		t.Fatal("upload overwrote existing file")
	}
	if err := s.RenameManagedPath(site, "docs/test.txt", "docs/renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if err := s.ChmodManagedPath(site, "docs/renamed.txt", "0640"); err != nil {
		t.Fatal(err)
	}
	listing, err := s.ListFiles(site, "docs")
	if err != nil || len(listing.Entries) != 2 {
		t.Fatalf("list: %+v, %v", listing, err)
	}
	if err := s.DeleteManagedPath(site, "docs/upload.txt"); err != nil {
		t.Fatal(err)
	}
}
