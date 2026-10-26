package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Create the competing upload at a deterministic point after the initial
// destination check, avoiding timing-dependent filesystem races in the test.
type archiveDestinationContext struct {
	context.Context
	onTraversal func()
	once sync.Once
}

func (c *archiveDestinationContext) Err() error {
	c.once.Do(c.onTraversal)
	return c.Context.Err()
}

func TestFileArchivePreservesDestinationCreatedDuringCompression(t *testing.T) {
	system := &System{dataDir: t.TempDir(), demo: true}
	site := Site{ID: "archive_race", Root: t.TempDir()}
	if err := os.WriteFile(filepath.Join(site.Root, "source.txt"), []byte("archive source"), 0640); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(site.Root, "backup.zip")
	ctx := &archiveDestinationContext{Context: context.Background(), onTraversal: func() {
		if err := os.WriteFile(destination, []byte("new concurrent upload"), 0640); err != nil {
			t.Fatal(err)
		}
	}}
	if _, err := system.ArchiveManagedPaths(ctx, site, []string{"source.txt"}, "backup.zip"); err == nil {
		t.Fatal("archive replaced a destination created during compression")
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "new concurrent upload" {
		t.Fatalf("concurrent upload changed: %q, %v", content, err)
	}
}
