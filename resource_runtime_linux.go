//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type resourceFSXAttr struct {
	Flags, ExtSize, Nextents, Project, CowExtSize uint32
	Pad                                           [8]byte
}
type resourceDqblk struct {
	Hard, Soft, Space, IHard, ISoft, Inodes, BTime, ITime uint64
	Valid                                                 uint32
	Pad                                                   uint32
}

// resourceEnsureRuntimeDirectories repairs only the two root-owned traversal
// directories. The host executor uses umask 0077; Mkdir's requested mode alone
// cannot make the private worker sockets reachable by Nginx's unprivileged user.
// Contents such as plan.json and recovery directories keep their private modes.
func resourceEnsureRuntimeDirectories(base, dir string) error {
	base, dir = filepath.Clean(base), filepath.Clean(dir)
	if !filepath.IsAbs(base) || !filepath.IsAbs(dir) || filepath.Dir(dir) != base || dir == base {
		return fmt.Errorf("resource runtime directory must be a direct child of its absolute base")
	}
	for _, path := range []string{base, dir} {
		if err := os.Mkdir(path, 0755); err != nil && !os.IsExist(err) {
			return err
		}
		// Do not follow a symlink when validating or changing permissions.
		f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return fmt.Errorf("open root-owned resource runtime directory %s: %w", path, err)
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !info.IsDir() || info.Mode().Perm()&0022 != 0 || !ok || st.Uid != 0 {
			f.Close()
			return fmt.Errorf("resource runtime parent must be a root-owned real directory without group/world write access: %s", path)
		}
		err = f.Chmod(0755)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func resourcePrepareStorage(site Site) error {
	base := filepath.Join(siteStorageBase, ".litecp-resource-runtime")
	dir := resourceProtectedStorage(site)
	info, err := os.Lstat(siteStorageBase)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 || !ok || st.Uid != 0 {
		return fmt.Errorf("protected quota runtime parent must be a root-owned real directory: %s", siteStorageBase)
	}
	if err := resourceEnsureRuntimeDirectories(base, dir); err != nil {
		return err
	}
	tmp := filepath.Join(dir, "tmp")
	if err := os.Mkdir(tmp, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err = os.Lstat(tmp)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("private temporary path is not a real directory")
	}
	if err = os.Chown(tmp, site.RuntimeUID, site.RuntimeGID); err != nil {
		return err
	}
	if err = os.Chmod(tmp, 0700); err != nil {
		return err
	}
	return nil
}

func resourceRuntimeLock(id string, watch bool) (func(), error) {
	dir := resourceDirectory(id)
	if err := resourceEnsureRuntimeDirectories(resourceRuntimeBase, dir); err != nil {
		return nil, err
	}
	name := "transition.lock"
	if watch {
		name = "monitor.lock"
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another website resource transition is in progress")
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
func resourceCgroup(id string) (string, error) {
	out, err := runCommandNoContext("systemctl", "show", resourceSliceName(id), "--property=ControlGroup", "--value")
	if err != nil {
		return "", err
	}
	rel := strings.TrimSpace(out)
	if !strings.HasPrefix(rel, "/") || strings.Contains(rel, "..") || !strings.HasSuffix(rel, "/"+resourceSliceName(id)) {
		return "", fmt.Errorf("invalid website control group")
	}
	return filepath.Join("/sys/fs/cgroup", rel), nil
}
func runCommandNoContext(name string, args ...string) (string, error) {
	return runCommand(context.Background(), 15*time.Second, name, args...)
}
func resourceSetOOMGroup(id string) error {
	dir, err := resourceCgroup(id)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "memory.oom.group"), []byte("1\n"), 0644)
}
func resourceKillCgroup(id string) error {
	dir, err := resourceCgroup(id)
	if err != nil {
		state, e := runCommandNoContext("systemctl", "show", resourceSliceName(id), "--property=ActiveState", "--value")
		if e == nil && (strings.TrimSpace(state) == "inactive" || strings.TrimSpace(state) == "failed") {
			return nil
		}
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "cgroup.kill"), []byte("1\n"), 0644); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
func resourceVerifyKernel(id string, p WebsiteResourceLimits) error {
	dir, err := resourceCgroup(id)
	if err != nil {
		return err
	}
	if _, err = os.Stat(filepath.Join(dir, "cgroup.kill")); err != nil {
		return fmt.Errorf("kernel cgroup-wide termination is unavailable: %w", err)
	}
	// systemd can rewrite the kernel-only OOM flag while applying other
	// websites' units. Reapply it and verify the same cgroup directory.
	for attempt := 0; ; attempt++ {
		if err := os.WriteFile(filepath.Join(dir, "memory.oom.group"), []byte("1\n"), 0644); err != nil {
			return err
		}
		err := verifyResourceSlice(p, func(name string) ([]byte, error) {
			return os.ReadFile(filepath.Join(dir, name))
		})
		if err != errResourceOOMGroup || attempt == 3 {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}
func resourceLiveSample(id string, disk int64) (resourceSample, int64, int, error) {
	dir, err := resourceCgroup(id)
	if err != nil {
		return resourceSample{}, 0, 0, err
	}
	sample, err := readResourceCounters(dir, time.Now())
	if err != nil {
		return sample, 0, 0, err
	}
	sample.DiskBytes = disk
	read := func(name string) (int64, error) {
		b, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			return 0, e
		}
		return strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	}
	memory, err := read("memory.current")
	if err != nil {
		return sample, 0, 0, err
	}
	tasks, err := read("pids.current")
	return sample, memory, int(tasks), err
}
func resourceBlockDevice(site Site) string {
	out, err := runCommandNoContext("findmnt", "-n", "-o", "SOURCE", "-T", siteHome(site))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
func resourceQuotaUsage(site Site) (int64, error) {
	q, err := resourceQuotaInfo(site)
	if err != nil {
		return 0, err
	}
	return int64(q.Space), nil
}
func resourceQuotaInfo(site Site) (resourceDqblk, error) {
	var quota resourceDqblk
	source := resourceBlockDevice(site)
	if !strings.HasPrefix(source, "/dev/") {
		return quota, fmt.Errorf("cannot resolve website quota device")
	}
	p, err := syscall.BytePtrFromString(source)
	if err != nil {
		return quota, err
	}
	_, _, errno := syscall.Syscall6(syscall.SYS_QUOTACTL, uintptr((0x800007<<8)|2), uintptr(unsafe.Pointer(p)), uintptr(site.RuntimeUID), uintptr(unsafe.Pointer(&quota)), 0, 0)
	if errno != 0 {
		return quota, fmt.Errorf("read project quota: %w", errno)
	}
	return quota, nil
}
func resourceQuotaSet(site Site, mib int64) error {
	info, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	kind, mount, ready := resourceMount(siteHome(site), string(info))
	if !ready || kind != "ext4" {
		return fmt.Errorf("this executor requires active ext4 project quotas")
	}
	return resourceRun("setquota", "-P", strconv.Itoa(site.RuntimeUID), "0", strconv.FormatInt(mib*1024, 10), "0", "0", mount)
}
func resourceQuotaPrepare(site Site, mib int64) error {
	// Walk with directory file descriptors. Website-controlled symlink swaps
	// cannot redirect project assignment into another website or the OS.
	home := siteHome(site)
	fd, err := syscall.Open(home, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	root := os.NewFile(uintptr(fd), home)
	defer root.Close()
	var stat syscall.Stat_t
	if err = syscall.Fstat(fd, &stat); err != nil {
		return err
	}
	device := stat.Dev
	initial, err := resourceQuotaInfo(site)
	if err != nil {
		return err
	}
	var rootAttr resourceFSXAttr
	_, _, rootErr := syscall.Syscall(syscall.SYS_IOCTL, root.Fd(), 0x801c581f, uintptr(unsafe.Pointer(&rootAttr)))
	if rootErr != 0 {
		return rootErr
	}
	if initial.Hard != 0 || initial.Soft != 0 || initial.IHard != 0 || initial.ISoft != 0 {
		return fmt.Errorf("an existing project quota is present; review it before enabling LiteCP enforcement")
	}
	if rootAttr.Project == 0 && initial.Space != 0 {
		return fmt.Errorf("quota project identity is already used outside this website")
	}
	var assign func(*os.File) error
	assign = func(f *os.File) error {
		var st syscall.Stat_t
		if err := syscall.Fstat(int(f.Fd()), &st); err != nil {
			return err
		}
		if st.Dev != device {
			return fmt.Errorf("website contains another mount; quota assignment refused")
		}
		dir := st.Mode&syscall.S_IFMT == syscall.S_IFDIR
		if !dir && st.Mode&syscall.S_IFMT != syscall.S_IFREG {
			return fmt.Errorf("unsupported special file in website quota tree")
		}
		if !dir && st.Nlink > 1 {
			return fmt.Errorf("hard-linked website files require review before assigning a project quota")
		}
		var attr resourceFSXAttr
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), 0x801c581f, uintptr(unsafe.Pointer(&attr)))
		if errno != 0 {
			return errno
		}
		if attr.Project != 0 && attr.Project != uint32(site.RuntimeUID) {
			return fmt.Errorf("website already belongs to a different quota project")
		}
		attr.Project = uint32(site.RuntimeUID)
		if dir {
			attr.Flags |= 0x200
		}
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), 0x401c5820, uintptr(unsafe.Pointer(&attr)))
		if errno != 0 {
			return errno
		}
		if !dir {
			return nil
		}
		names, err := f.Readdirnames(-1)
		if err != nil {
			return err
		}
		for _, name := range names {
			child, e := syscall.Openat(int(f.Fd()), name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
			if e == syscall.ELOOP {
				continue
			}
			if e != nil {
				return e
			}
			c := os.NewFile(uintptr(child), name)
			e = assign(c)
			c.Close()
			if e != nil {
				return e
			}
		}
		return nil
	}
	if err = assign(root); err != nil {
		return err
	}
	extraFD, err := syscall.Open(resourceProtectedStorage(site), syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	extra := os.NewFile(uintptr(extraFD), "private-runtime")
	err = assign(extra)
	extra.Close()
	if err != nil {
		return err
	}
	usage, err := resourceQuotaUsage(site)
	if err != nil {
		return err
	}
	if usage >= mib*1048576 {
		return fmt.Errorf("website already uses the requested disk allowance; increase it before enabling")
	}
	if err = resourceQuotaSet(site, mib); err != nil {
		return err
	}
	verified := false
	defer func() {
		if !verified {
			_ = resourceQuotaSet(site, 0)
		}
	}()
	source := resourceBlockDevice(site)
	p, err := syscall.BytePtrFromString(source)
	if err != nil {
		return err
	}
	var q resourceDqblk
	_, _, errno := syscall.Syscall6(syscall.SYS_QUOTACTL, uintptr((0x800007<<8)|2), uintptr(unsafe.Pointer(p)), uintptr(site.RuntimeUID), uintptr(unsafe.Pointer(&q)), 0, 0)
	if errno != 0 {
		return errno
	}
	if q.Hard != uint64(mib*1024) {
		return fmt.Errorf("kernel project quota did not match requested allowance")
	}
	verified = true
	return nil
}

func resourceAdoptUID(site Site) error {
	dir, err := resourceCgroup(site.ID)
	if err != nil {
		return err
	}
	leaf := filepath.Join(dir, "legacy")
	if err = os.MkdirAll(leaf, 0755); err != nil {
		return err
	}
	for pass := 0; pass < 5; pass++ {
		entries, err := os.ReadDir("/proc")
		if err != nil {
			return err
		}
		moved := 0
		for _, entry := range entries {
			if _, err := strconv.Atoi(entry.Name()); err != nil {
				continue
			}
			status, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "status"))
			if err != nil {
				continue
			}
			wanted := false
			for _, line := range strings.Split(string(status), "\n") {
				if strings.HasPrefix(line, "Uid:") {
					fields := strings.Fields(line)
					wanted = len(fields) > 1 && fields[1] == strconv.Itoa(site.RuntimeUID)
					break
				}
			}
			if !wanted {
				continue
			}
			group, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cgroup"))
			if err != nil {
				continue
			}
			if strings.Contains(string(group), "/"+resourceSliceName(site.ID)) {
				continue
			}
			if err = os.WriteFile(filepath.Join(leaf, "cgroup.procs"), []byte(entry.Name()), 0644); err != nil {
				if _, e := os.Stat(filepath.Join("/proc", entry.Name())); os.IsNotExist(e) {
					continue
				}
				return err
			}
			moved++
		}
		if moved == 0 {
			return nil
		}
	}
	return fmt.Errorf("website processes continued to escape the resource boundary")
}

func resourceQuotaVerifiedUsage(site Site, mib int64) (int64, error) {
	q, err := resourceQuotaInfo(site)
	if err != nil {
		return 0, err
	}
	if q.Hard != uint64(mib*1024) {
		return 0, fmt.Errorf("project quota enforcement changed unexpectedly")
	}
	return int64(q.Space), nil
}
