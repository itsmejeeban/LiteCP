//go:build linux

package main

import (
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestResourceMonitorReadyNotification(t *testing.T) {
	if err := resourceNotifyMonitorReady(""); err != nil {
		t.Fatal("legacy simple units must remain compatible: ", err)
	}
	socket := filepath.Join(t.TempDir(), "notify.sock")
	listener, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: socket, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	listener.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := resourceNotifyMonitorReady(socket); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 32)
	n, _, err := listener.ReadFromUnix(buffer)
	if err != nil || string(buffer[:n]) != "READY=1" {
		t.Fatalf("invalid systemd readiness notification %q: %v", buffer[:n], err)
	}
	if err := resourceNotifyMonitorReady(filepath.Join(t.TempDir(), "missing.sock")); err == nil {
		t.Fatal("failed systemd notification must fail startup")
	}
}
