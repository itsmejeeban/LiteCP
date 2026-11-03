package main

import (
	"context"
	"strings"
	"testing"
)

func TestWebsiteScanRestrictedLauncher(t *testing.T) {
	command, err := websiteScanCommand(context.Background(), Site{RuntimeUID: 1800123456, RuntimeGID: 994}, "/home/litecp/example.com/public_html")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(command.Args, " ")
	for _, want := range []string{"systemd-run", "--wait --pipe", "ProtectSystem=strict", "ProtectHome=read-only", "PrivateNetwork=yes", "--reuid=1800123456", "--regid=994", "--bounding-set=-all", "--follow-dir-symlinks=0", "--follow-file-symlinks=0", "-- /home/litecp/example.com/public_html"} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing %s", want)
		}
	}
	for _, forbidden := range []string{"ReadWritePaths=", "--remove", "--move=", "--copy="} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("scan allows modification: %s", forbidden)
		}
	}
	if _, err := websiteScanCommand(context.Background(), Site{}, "/tmp"); err == nil {
		t.Fatal("root identity accepted")
	}
}
