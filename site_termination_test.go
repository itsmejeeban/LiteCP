package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPermanentWebsiteRemovalRejectsUnmanagedData(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	if err := app.terminateWebsite(context.Background(), Site{Root: root}); err == nil {
		t.Fatal("unmanaged root accepted")
	}
	content, err := os.ReadFile(file)
	if err != nil || string(content) != "keep" {
		t.Fatal("existing files changed", err)
	}
}
