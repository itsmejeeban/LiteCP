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

func TestWebsiteScanSELinuxLogTransport(t *testing.T) {
	command, err := websiteScanCommand(context.Background(), Site{RuntimeUID: 1800123456, RuntimeGID: 994}, "/home/litecp/qa/public_html", "/var/lib/litecp/website-scans/scan-test.log")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(command.Args, " ")
	if strings.Contains(args, "--pipe") {
		t.Fatal("broker pipes crossed SELinux domains")
	}
	for _, expected := range []string{"--wait", "StandardOutput=file:/var/lib/litecp/website-scans/scan-test.log", "StandardError=inherit", "--reuid=1800123456", "PrivateNetwork=yes", "ProtectSystem=strict"} {
		if !strings.Contains(args, expected) {
			t.Fatal("missing", expected)
		}
	}
}

func TestWebsiteScanNeverAcceptsLauncherFailureAsResult(t *testing.T) {
	for _, text := range []string{"Failed to start transient service unit: Connection reset by peer", "", "LibClamAV Error: temporary directory unavailable\nSCAN SUMMARY\nInfected files: 0", "/file: read failure ERROR\nSCAN SUMMARY"} {
		if websiteScanOutputComplete(text, false) || websiteScanOutputComplete(text, true) {
			t.Fatal("incomplete scan accepted", text)
		}
	}
	if websiteScanOutputComplete("SCAN SUMMARY\nInfected files: 0", true) {
		t.Fatal("exit one without detections accepted")
	}
	if !websiteScanOutputComplete("/test: Eicar-Test-Signature FOUND\nSCAN SUMMARY\nInfected files: 1", true) {
		t.Fatal("valid detection rejected")
	}
	if !websiteScanOutputComplete("SCAN SUMMARY\nInfected files: 0", false) {
		t.Fatal("completed clean scan rejected")
	}
}
