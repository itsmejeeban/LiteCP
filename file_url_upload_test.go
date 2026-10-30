package main

import (
	"net"
	"testing"
)

func TestURLUploadAddressSafety(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.100.100.200", "::1", "::ffff:127.0.0.1", "fc00::1", "192.0.2.1", "2001:db8::1", "64:ff9b::a00:1"} {
		if publicDownloadIP(net.ParseIP(value)) {
			t.Fatalf("unsafe address accepted: %s", value)
		}
	}
	for _, value := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if !publicDownloadIP(net.ParseIP(value)) {
			t.Fatalf("public address rejected: %s", value)
		}
	}
	for _, raw := range []string{"file:///etc/passwd", "ftp://example.com/file", "https://user:password@example.com/file", "http://example.com:9090/file", "https://example.com/file#fragment"} {
		if _, err := validateUploadURL(raw); err == nil {
			t.Fatalf("unsafe URL accepted: %s", raw)
		}
	}
	if _, err := validateUploadURL("https://example.com/archive.zip?download=1"); err != nil {
		t.Fatal(err)
	}
}
