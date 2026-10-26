//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestManagedFIFOIsRejectedWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "pipe")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		file, err := secureOpenManaged(root, "pipe", os.O_RDONLY, 0)
		if file != nil {
			_ = file.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("managed files must reject FIFOs")
		}
	case <-time.After(time.Second):
		// Release a regressed blocking reader so the test does not leak it.
		fd, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK, 0)
		if err == nil {
			_ = unix.Close(fd)
		}
		t.Fatal("opening a website FIFO blocked the request")
	}
	listing, err := (&System{}).ListFiles(Site{Root: root}, "")
	if err != nil || len(listing.Entries) != 0 {
		t.Fatalf("special files must be omitted from directory listings: %v", err)
	}
}
