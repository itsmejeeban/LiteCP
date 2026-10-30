package main

import (
	"strings"
	"testing"
)

func TestSFTPBatchQuoting(t *testing.T) {
	for _, value := range []string{"/backup/path with spaces", "/backup/quote\"name", "/backup/back\\slash"} {
		quoted := sftpBatchQuote(value)
		if !strings.HasPrefix(quoted, "\"") || !strings.HasSuffix(quoted, "\"") {
			t.Fatal("unquoted path", quoted)
		}
	}
	if sftpBatchQuote("a\"b") != "\"a\\\"b\"" {
		t.Fatal("quote not escaped")
	}
}
