//go:build !linux

package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

func secureOpenManaged(root, relative string, flags int, mode os.FileMode) (*os.File, error) {
	path, _, err := resolveManagedPath(root, relative, flags&os.O_CREATE != 0)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(path, flags, mode)
}

func secureWriteManaged(root, relative string, source io.Reader, maximum int64, mode os.FileMode, uid, gid int) (int64, error) {
	return secureWriteManagedMode(root, relative, source, maximum, mode, false)
}

func secureWriteManagedNoReplace(root, relative string, source io.Reader, maximum int64, mode os.FileMode, uid, gid int) (int64, error) {
	return secureWriteManagedMode(root, relative, source, maximum, mode, true)
}

func secureWriteManagedMode(root, relative string, source io.Reader, maximum int64, mode os.FileMode, noReplace bool) (int64, error) {
	path, _, err := resolveManagedPath(root, relative, true)
	if err != nil {
		return 0, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".litecp-write-")
	if err != nil {
		return 0, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	written, err := io.Copy(temporary, managedWriteReader(source, maximum))
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return written, err
	}
	if maximum >= 0 && written > maximum {
		return written, errors.New("managed file exceeds its size limit")
	}
	if err := os.Chmod(temporaryPath, mode); err != nil {
		return written, err
	}
	if noReplace {
		if _, err := os.Lstat(path); err == nil {
			return written, errors.New("the destination already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return written, err
		}
	}
	return written, os.Rename(temporaryPath, path)
}

func secureEnsureManagedDirectory(root, relative string, mode os.FileMode, uid, gid int) error {
	path, _, err := resolveManagedPath(root, relative, true)
	if err != nil {
		return err
	}
	return os.MkdirAll(path, mode)
}

func secureCreateManagedDirectory(root, parent, name string, mode os.FileMode, uid, gid int) error {
	path, _, err := resolveManagedPath(root, filepath.ToSlash(filepath.Join(parent, name)), true)
	if err != nil {
		return err
	}
	return os.Mkdir(path, mode)
}

func secureRenameManaged(root, source, destination string) error {
	sourcePath, _, err := resolveManagedPath(root, source, false)
	if err != nil {
		return err
	}
	destinationPath, _, err := resolveManagedPath(root, destination, true)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destinationPath); err == nil {
		return errors.New("the destination already exists")
	}
	return os.Rename(sourcePath, destinationPath)
}

func secureMoveBetweenRoots(sourceRoot, source, destinationRoot, destination string) error {
	sourcePath, _, err := resolveManagedPath(sourceRoot, source, false)
	if err != nil {
		return err
	}
	destinationPath, _, err := resolveManagedPath(destinationRoot, destination, true)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destinationPath); err == nil {
		return errors.New("the destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(sourcePath, destinationPath)
}

func secureRemoveManaged(root, relative string) error {
	path, _, err := resolveManagedPath(root, relative, false)
	if err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func secureRemoveManagedDirectoryIfEmpty(root, relative string) error {
	path, _, err := resolveManagedPath(root, relative, false)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

func secureChmodManaged(root, relative string, mode os.FileMode) error {
	path, _, err := resolveManagedPath(root, relative, false)
	if err != nil {
		return err
	}
	return os.Chmod(path, mode)
}
