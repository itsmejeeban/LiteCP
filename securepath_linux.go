//go:build linux

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const managedResolveFlags = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_SYMLINKS

func cleanSecureRelative(relative string) (string, error) {
	if strings.ContainsRune(relative, 0) || filepath.IsAbs(relative) || strings.HasPrefix(relative, "/") || strings.HasPrefix(relative, `\`) {
		return "", errors.New("invalid website path")
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." {
		return ".", nil
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("path escapes the website root")
	}
	return clean, nil
}

func openSecureRoot(root string) (int, error) {
	return unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
}

func secureOpenManaged(root, relative string, flags int, mode os.FileMode) (*os.File, error) {
	clean, err := cleanSecureRelative(relative)
	if err != nil {
		return nil, err
	}
	rootFD, err := openSecureRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open website root: %w", err)
	}
	defer unix.Close(rootFD)
	how := &unix.OpenHow{Flags: uint64(flags | unix.O_CLOEXEC | unix.O_NOFOLLOW), Mode: uint64(mode.Perm()), Resolve: managedResolveFlags}
	fd, err := unix.Openat2(rootFD, clean, how)
	if err != nil {
		return nil, fmt.Errorf("open managed path: %w", err)
	}
	return os.NewFile(uintptr(fd), filepath.Base(clean)), nil
}

func secureParent(root, relative string) (int, string, error) {
	clean, err := cleanSecureRelative(relative)
	if err != nil {
		return -1, "", err
	}
	if clean == "." {
		return -1, "", errors.New("website root cannot be replaced")
	}
	rootFD, err := openSecureRoot(root)
	if err != nil {
		return -1, "", err
	}
	parent := filepath.Dir(clean)
	how := &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: managedResolveFlags}
	parentFD, err := unix.Openat2(rootFD, parent, how)
	_ = unix.Close(rootFD)
	if err != nil {
		return -1, "", fmt.Errorf("open managed parent: %w", err)
	}
	return parentFD, filepath.Base(clean), nil
}

func secureWriteManaged(root, relative string, source io.Reader, maximum int64, mode os.FileMode, uid, gid int) (int64, error) {
	return secureWriteManagedMode(root, relative, source, maximum, mode, uid, gid, false)
}

func secureWriteManagedNoReplace(root, relative string, source io.Reader, maximum int64, mode os.FileMode, uid, gid int) (int64, error) {
	return secureWriteManagedMode(root, relative, source, maximum, mode, uid, gid, true)
}

func secureWriteManagedMode(root, relative string, source io.Reader, maximum int64, mode os.FileMode, uid, gid int, noReplace bool) (int64, error) {
	parentFD, name, err := secureParent(root, relative)
	if err != nil {
		return 0, err
	}
	defer unix.Close(parentFD)
	token, err := randomToken(8)
	if err != nil {
		return 0, err
	}
	temporaryName := ".litecp-write-" + safeSiteID(token)
	fd, err := unix.Openat(parentFD, temporaryName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode.Perm()))
	if err != nil {
		return 0, err
	}
	temporary := os.NewFile(uintptr(fd), temporaryName)
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = unix.Unlinkat(parentFD, temporaryName, 0)
		}
	}()
	written, copyErr := io.Copy(temporary, io.LimitReader(source, maximum+1))
	if copyErr != nil {
		return written, copyErr
	}
	if written > maximum {
		return written, errors.New("managed file exceeds its size limit")
	}
	if err := unix.Fchown(fd, uid, gid); err != nil {
		return written, err
	}
	if err := unix.Fchmod(fd, uint32(mode.Perm())); err != nil {
		return written, err
	}
	if err := temporary.Sync(); err != nil {
		return written, err
	}
	if err := temporary.Close(); err != nil {
		return written, err
	}
	if noReplace {
		err = unix.Renameat2(parentFD, temporaryName, parentFD, name, unix.RENAME_NOREPLACE)
	} else {
		err = unix.Renameat(parentFD, temporaryName, parentFD, name)
	}
	if err != nil {
		return written, err
	}
	keep = true
	_ = unix.Fsync(parentFD)
	return written, nil
}

func secureEnsureManagedDirectory(root, relative string, mode os.FileMode, uid, gid int) error {
	clean, err := cleanSecureRelative(relative)
	if err != nil {
		return err
	}
	rootFD, err := openSecureRoot(root)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	currentFD := rootFD
	ownedFD := -1
	if clean == "." {
		return nil
	}
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		created := false
		if err := unix.Mkdirat(currentFD, part, uint32(mode.Perm())); err == nil {
			created = true
		} else if !errors.Is(err, unix.EEXIST) {
			if ownedFD >= 0 {
				_ = unix.Close(ownedFD)
			}
			return err
		}
		nextFD, err := unix.Openat(currentFD, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			if ownedFD >= 0 {
				_ = unix.Close(ownedFD)
			}
			return err
		}
		if created {
			if err := unix.Fchown(nextFD, uid, gid); err != nil {
				_ = unix.Close(nextFD)
				if ownedFD >= 0 {
					_ = unix.Close(ownedFD)
				}
				return err
			}
		}
		if ownedFD >= 0 {
			_ = unix.Close(ownedFD)
		}
		ownedFD = nextFD
		currentFD = nextFD
	}
	if ownedFD >= 0 {
		_ = unix.Close(ownedFD)
	}
	return nil
}

func secureCreateManagedDirectory(root, parent, name string, mode os.FileMode, uid, gid int) error {
	relative := filepath.ToSlash(filepath.Join(parent, name))
	parentFD, base, err := secureParent(root, relative)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	if err := unix.Mkdirat(parentFD, base, uint32(mode.Perm())); err != nil {
		return err
	}
	fd, err := unix.Openat(parentFD, base, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return unix.Fchown(fd, uid, gid)
}

func secureRenameManaged(root, source, destination string) error {
	sourceFD, sourceName, err := secureParent(root, source)
	if err != nil {
		return err
	}
	defer unix.Close(sourceFD)
	destinationFD, destinationName, err := secureParent(root, destination)
	if err != nil {
		return err
	}
	defer unix.Close(destinationFD)
	if err := unix.Renameat2(sourceFD, sourceName, destinationFD, destinationName, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return errors.New("the destination already exists")
		}
		return err
	}
	return nil
}

func secureMoveBetweenRoots(sourceRoot, source, destinationRoot, destination string) error {
	sourceFD, sourceName, err := secureParent(sourceRoot, source)
	if err != nil {
		return err
	}
	defer unix.Close(sourceFD)
	destinationFD, destinationName, err := secureParent(destinationRoot, destination)
	if err != nil {
		return err
	}
	defer unix.Close(destinationFD)
	if err := unix.Renameat2(sourceFD, sourceName, destinationFD, destinationName, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return errors.New("the destination already exists")
		}
		if errors.Is(err, unix.EXDEV) {
			return errors.New("website and trash storage must be on the same filesystem")
		}
		return err
	}
	_ = unix.Fsync(sourceFD)
	_ = unix.Fsync(destinationFD)
	return nil
}

func secureRemoveManaged(root, relative string) error {
	parentFD, name, err := secureParent(root, relative)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	return secureRemoveAt(parentFD, name)
}

func secureRemoveAt(parentFD int, name string) error {
	var stat unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return unix.Unlinkat(parentFD, name, 0)
	}
	directoryFD, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(directoryFD), name)
	entries, err := directory.ReadDir(-1)
	if err != nil {
		_ = directory.Close()
		return err
	}
	for _, entry := range entries {
		if err := secureRemoveAt(directoryFD, entry.Name()); err != nil {
			_ = directory.Close()
			return err
		}
	}
	if err := directory.Close(); err != nil {
		return err
	}
	return unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR)
}

func secureChmodManaged(root, relative string, mode os.FileMode) error {
	file, err := secureOpenManaged(root, relative, unix.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Chmod(mode)
}
