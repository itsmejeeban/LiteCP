package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryTarRejectsUnsafeMembers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers []tar.Header
		fail    bool
	}{
		{"regular", []tar.Header{{Name: "index.php", Size: 2, Typeflag: tar.TypeReg}}, false},
		{"traversal", []tar.Header{{Name: "../escape", Size: 2, Typeflag: tar.TypeReg}}, true},
		{"link", []tar.Header{{Name: "link", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink}}, true},
		{"duplicate", []tar.Header{{Name: "a", Size: 2, Typeflag: tar.TypeReg}, {Name: "a", Size: 2, Typeflag: tar.TypeReg}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			archive := filepath.Join(dir, "backup.tar.gz")
			f, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(f)
			tw := tar.NewWriter(gz)
			for _, h := range tc.headers {
				if err := tw.WriteHeader(&h); err != nil {
					t.Fatal(err)
				}
				if h.Size > 0 {
					if _, err := tw.Write([]byte("ok")); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			gz.Close()
			f.Close()
			root := filepath.Join(dir, "files")
			os.Mkdir(root, 0700)
			n, _, err := unpackHistoryTar(context.Background(), archive, root)
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v", err)
			}
			if !tc.fail && n != 1 {
				t.Fatal(n)
			}
		})
	}
}
