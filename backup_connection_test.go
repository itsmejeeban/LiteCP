package main

import (
	"context"
	"strings"
	"testing"
)

func TestBackupConnectionFTPConfigIsReadOnly(t *testing.T) {
	for _, protocol := range []string{"ftp", "ftps"} {
		destination := BackupDestination{Protocol: protocol, Host: "backup.example.test", Port: 990, Username: "backup", RemotePath: "/backup folder"}
		config, err := backupConnectionCurlConfig(destination, `secret"with\escapes`)
		if err != nil {
			t.Fatal(err)
		}
		for _, required := range []string{"list-only\n", "max-time = 35", "/backup%20folder/", `user = "backup:secret\"with\\escapes"`, `output = "/dev/null"`} {
			if !strings.Contains(config, required) {
				t.Fatalf("missing %q in %s", required, config)
			}
		}
		for _, forbidden := range []string{"upload-file", "ftp-create-dirs", "quote =", "insecure"} {
			if strings.Contains(config, forbidden) {
				t.Fatalf("unsafe test option %s", forbidden)
			}
		}
		if strings.Contains(config, "ssl-reqd") != (protocol == "ftps") {
			t.Fatal("wrong TLS requirement")
		}
	}
}

func TestBackupConnectionRejectsConfigInjection(t *testing.T) {
	destination := BackupDestination{Protocol: "ftp", Host: "example.test", Port: 21, Username: "backup", RemotePath: "/backups"}
	for _, password := range []string{"secret\nquote = DELE file", "secret\rline", "secret\x00"} {
		if _, err := backupConnectionCurlConfig(destination, password); err == nil {
			t.Fatal("accepted password control characters")
		}
	}
	destination.RemotePath = "/backups\nquote = DELE file"
	if _, err := backupConnectionCurlConfig(destination, "secret"); err == nil {
		t.Fatal("accepted path config injection")
	}
}

func TestBackupConnectionDemoDoesNotReportSuccess(t *testing.T) {
	destination := BackupDestination{Name: "fixture", Protocol: "ftp", Host: "example.test", Port: 21, Username: "backup", RemotePath: "/backups", PasswordCipher: "fixture"}
	err := (&System{demo: true}).testBackupConnection(context.Background(), destination)
	if err == nil || !strings.Contains(err.Error(), "installed server") {
		t.Fatalf("demo falsely reported a real connection: %v", err)
	}
}
