//go:build linux

package main

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func resourceDirectoryTestMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}

func TestResourceRuntimeDirectoriesUnderPrivateUmask(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned runtime fixture requires Linux root")
	}
	if root := os.Getenv("LITECP_TEST_RESOURCE_DIRECTORY_UMASK"); root != "" {
		// Process-wide umask changes stay in this child, away from other tests.
		syscall.Umask(0077)
		base := filepath.Join(root, "runtime")
		dir := filepath.Join(base, "site")
		if err := resourceEnsureRuntimeDirectories(base, dir); err != nil {
			t.Fatal(err)
		}
		resourceDirectoryTestMode(t, base, 0755)
		resourceDirectoryTestMode(t, dir, 0755)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestResourceRuntimeDirectoriesUnderPrivateUmask$")
	command.Env = append(os.Environ(), "LITECP_TEST_RESOURCE_DIRECTORY_UMASK="+t.TempDir())
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("private-umask runtime preparation: %v\n%s", err, output)
	}
}

func TestResourceRuntimeDirectoriesRepairExisting(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned runtime fixture requires Linux root")
	}
	base := filepath.Join(t.TempDir(), "runtime")
	dir := filepath.Join(base, "site")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	plan := filepath.Join(dir, "plan.json")
	if err := writeAtomic(plan, []byte("private plan"), 0600); err != nil {
		t.Fatal(err)
	}
	recovery := filepath.Join(dir, "recovery")
	if err := os.Mkdir(recovery, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := resourceEnsureRuntimeDirectories(base, dir); err != nil {
			t.Fatal(err)
		}
		resourceDirectoryTestMode(t, base, 0755)
		resourceDirectoryTestMode(t, dir, 0755)
		resourceDirectoryTestMode(t, plan, 0600)
		resourceDirectoryTestMode(t, recovery, 0700)
	}
	if content, err := os.ReadFile(plan); err != nil || string(content) != "private plan" {
		t.Fatalf("private plan changed: %q, %v", content, err)
	}
}

func TestResourceRuntimeDirectoriesRejectUnsafe(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root-owned runtime fixture requires Linux root")
	}
	for _, name := range []string{"base symlink", "site symlink", "base writable", "site writable", "base file", "site file", "site owned by website", "outside base"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			base := filepath.Join(root, "runtime")
			dir := filepath.Join(base, "site")
			target := filepath.Join(root, "target")
			if err := os.Mkdir(target, 0700); err != nil {
				t.Fatal(err)
			}
			var err error
			switch name {
			case "base symlink":
				err = os.Symlink(target, base)
			case "base file":
				err = os.WriteFile(base, nil, 0600)
			default:
				err = os.Mkdir(base, 0700)
				if err == nil {
					switch name {
					case "site symlink":
						err = os.Symlink(target, dir)
					case "site file":
						err = os.WriteFile(dir, nil, 0600)
					case "base writable":
						err = os.Chmod(base, 0777)
					case "outside base":
						dir = filepath.Join(root, "outside")
					default:
						err = os.Mkdir(dir, 0700)
						if err == nil && name == "site writable" {
							err = os.Chmod(dir, 0777)
						} else if err == nil && name == "site owned by website" {
							err = os.Chown(dir, 65534, 65534)
						}
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := resourceEnsureRuntimeDirectories(base, dir); err == nil {
				t.Fatal("unsafe resource runtime directory was accepted")
			}
			resourceDirectoryTestMode(t, target, 0700)
		})
	}
}

func TestResourceRuntimeSocketTraversal(t *testing.T) {
	if socket := os.Getenv("LITECP_TEST_RESOURCE_TRAVERSAL_SOCKET"); socket != "" {
		if os.Geteuid() == 0 {
			t.Fatal("socket client must run without root")
		}
		client, err := net.DialTimeout("unix", socket, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		client.Close()
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("unprivileged worker traversal fixture requires Linux root")
	}
	root := t.TempDir()
	if err := os.Chmod(filepath.Dir(root), 0711); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(root, "r")
	dir := filepath.Join(base, "s")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := resourceEnsureRuntimeDirectories(base, dir); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "p")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chown(socket, 0, 65534); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(socket, 0660); err != nil {
		t.Fatal(err)
	}
	// An inherited executable descriptor avoids Go's private build-directory
	// permissions becoming part of this worker-directory traversal test.
	executable, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer executable.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/proc/self/fd/3", "-test.run=^TestResourceRuntimeSocketTraversal$")
	command.ExtraFiles = []*os.File{executable}
	command.Env = append(os.Environ(), "LITECP_TEST_RESOURCE_TRAVERSAL_SOCKET="+socket)
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534, Groups: []uint32{}}}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("unprivileged worker could not reach its socket: %v\n%s", err, output)
	}
}
