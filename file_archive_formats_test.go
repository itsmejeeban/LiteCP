package main

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedArchiveFormatsRoundTrip(t *testing.T) {
	for _, format := range []string{"zip", "tar", "tar.gz", "tar.bz2"} {
		t.Run(format, func(t *testing.T) {
			if format == "tar.bz2" {
				if _, err := exec.LookPath("bzip2"); err != nil {
					t.Skip("bzip2 required")
				}
			}
			system, site := featureSite(t)
			if err := os.MkdirAll(filepath.Join(site.Root, "source/nested"), 0750); err != nil {
				t.Fatal(err)
			}
			contents := []byte("Unicode café 日本語\n")
			if err := os.WriteFile(filepath.Join(site.Root, "source/nested/.file.txt"), contents, 0640); err != nil {
				t.Fatal(err)
			}
			entry, err := system.ArchiveManagedPaths(context.Background(), site, []string{"source"}, "backup."+format)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(site.Root, "destination"), 0750); err != nil {
				t.Fatal(err)
			}
			if err := system.ExtractManagedArchive(context.Background(), site, entry.Path, "destination"); err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(filepath.Join(site.Root, "destination/source/nested/.file.txt"))
			if err != nil || !bytes.Equal(actual, contents) {
				t.Fatalf("round trip: %q %v", actual, err)
			}
		})
	}
}
func TestTARRejectsUnsafeMembersBeforeChangingDestination(t *testing.T) {
	for _, header := range []*tar.Header{{Name: "../escape", Mode: 0640, Size: 1, Typeflag: tar.TypeReg}, {Name: "/etc/escape", Mode: 0640, Size: 1, Typeflag: tar.TypeReg}, {Name: "link", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink}, {Name: "hardlink", Linkname: "/etc/passwd", Typeflag: tar.TypeLink}, {Name: "fifo", Typeflag: tar.TypeFifo}, {Name: "large", Size: maxExtractedSize + 1, Typeflag: tar.TypeReg}} {
		t.Run(strings.ReplaceAll(header.Name, "/", "_"), func(t *testing.T) {
			system, site := featureSite(t)
			var data bytes.Buffer
			writer := tar.NewWriter(&data)
			if err := writer.WriteHeader(header); err != nil {
				t.Fatal(err)
			}
			if header.Size == 1 {
				_, _ = writer.Write([]byte("x"))
			}
			_ = writer.Close()
			if err := os.WriteFile(filepath.Join(site.Root, "unsafe.tar"), data.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(site.Root, "destination"), 0750); err != nil {
				t.Fatal(err)
			}
			if err := system.ExtractManagedArchive(context.Background(), site, "unsafe.tar", "destination"); err == nil {
				t.Fatal("unsafe TAR accepted")
			}
			entries, _ := os.ReadDir(filepath.Join(site.Root, "destination"))
			if len(entries) != 0 {
				t.Fatal("destination changed before validation")
			}
		})
	}
}
