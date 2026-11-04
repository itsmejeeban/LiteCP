package main

import (
	"context"
	"strings"
	"testing"
)

func TestFileTransferMissingDestinationExplainsFolderRequirement(t *testing.T) {
	system, site := featureSite(t)
	for _, operation := range []string{"copy", "move"} {
		t.Run(operation, func(t *testing.T) {
			var err error
			if operation == "copy" {
				err = system.CopyManagedPaths(context.Background(), site, []string{"source.txt"}, "missing-folder")
			} else {
				err = system.MoveManagedPaths(site, []string{"source.txt"}, "missing-folder")
			}
			if err == nil || !strings.Contains(err.Error(), "existing destination folder") || strings.Contains(err.Error(), site.Root) {
				t.Fatalf("expected actionable folder error without server path, got %v", err)
			}
		})
	}
}
