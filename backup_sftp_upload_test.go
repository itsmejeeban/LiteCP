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

func TestSFTPFailureDistinguishesLoginAndFolder(t *testing.T) {
	for _, fixture := range []struct{ output, want string }{
		{"sftp> cd \"/litecp\"\nstat remote: No such file or directory", "SFTP login succeeded, but the configured remote folder does not exist"},
		{"sftp> cd \"/backups\"\nPermission denied", "SFTP login succeeded, but access"},
		{"root@backup.example.test: Permission denied (publickey,password).\nConnection closed", "remote SSH server rejected the saved login"},
	} {
		message := backupSFTPFailure([]byte(fixture.output), "secret-password").Error()
		if !strings.Contains(message, fixture.want) {
			t.Fatalf("wrong diagnosis for %q: %s", fixture.output, message)
		}
	}
}

func TestSFTPFailureKeepsSecretsOutOfDiagnostics(t *testing.T) {
	message := backupSFTPFailure([]byte("unrecognized remote error secret-password"), "secret-password").Error()
	if strings.Contains(message, "secret-password") || !strings.Contains(message, "[redacted]") {
		t.Fatal("secret included in error")
	}
	if len(backupSFTPFailure([]byte(strings.Repeat("X", 1000)), "secret-password").Error()) > 650 {
		t.Fatal("unbounded diagnostic")
	}
}
