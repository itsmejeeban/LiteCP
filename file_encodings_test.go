package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTextEncodingRoundTripsAndFailedSavePreservesFile(t *testing.T) {
	if _, err := exec.LookPath("iconv"); err != nil {
		t.Skip("iconv required")
	}
	system, site := featureSite(t)
	for _, encoding := range []string{"UTF-8", "UTF-16LE", "UTF-16BE", "WINDOWS-1252", "ISO-8859-1"} {
		t.Run(encoding, func(t *testing.T) {
			original, err := convertEditorText([]byte("café £"), "UTF-8", encoding)
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(site.Root, "sample.txt")
			if err = os.WriteFile(target, original, 0640); err != nil {
				t.Fatal(err)
			}
			content, _, err := system.ReadManagedText(site, "sample.txt", encoding)
			if err != nil || content != "café £" {
				t.Fatalf("decode %q %v", content, err)
			}
			if err = system.WriteManagedText(site, "sample.txt", "updated café £", encoding); err != nil {
				t.Fatal(err)
			}
			content, _, err = system.ReadManagedText(site, "sample.txt", encoding)
			if err != nil || content != "updated café £" {
				t.Fatalf("save %q %v", content, err)
			}
		})
	}
	target := filepath.Join(site.Root, "safe.txt")
	_ = os.WriteFile(target, []byte("original"), 0640)
	if err := system.WriteManagedText(site, "safe.txt", "日本語", "ASCII"); err == nil {
		t.Fatal("lossy conversion accepted")
	}
	actual, _ := os.ReadFile(target)
	if !bytes.Equal(actual, []byte("original")) {
		t.Fatal("failed conversion replaced original")
	}
	if _, _, err := system.ReadManagedText(site, "safe.txt", "UTF-8//IGNORE"); err == nil {
		t.Fatal("unvalidated iconv encoding accepted")
	}
}
func TestEditorAcceptsTextAboveOldFourMiBLimit(t *testing.T) {
	system, site := featureSite(t)
	content := strings.Repeat("a", 5<<20)
	if err := os.WriteFile(filepath.Join(site.Root, "large.txt"), []byte(content), 0640); err != nil {
		t.Fatal(err)
	}
	result, _, err := system.ReadManagedText(site, "large.txt", "UTF-8")
	if err != nil || result != content {
		t.Fatal("large text was not readable", err)
	}
}
