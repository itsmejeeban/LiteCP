package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const retainedWebsiteBackupRoot = "/var/backups/litecp/retained-websites"

var retainedWebsiteIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func retainedWebsiteDirectory(id string) string {
	return filepath.Join(retainedWebsiteBackupRoot, id)
}
func (a *App) retainWebsiteBackups(site Site) error {
	if !retainedWebsiteIDPattern.MatchString(site.ID) {
		return fmt.Errorf("invalid website ID; removal stopped")
	}
	directory := retainedWebsiteDirectory(site.ID)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(site)
	if err != nil {
		return err
	}
	if err = writeAtomic(filepath.Join(directory, "site.json"), data, 0600); err != nil {
		return err
	}
	old := filepath.Join(siteHome(site), "backups")
	target := filepath.Join(directory, "backups")
	if _, err = os.Lstat(old); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err = moveWebsiteBackupsToSafety(old, target); err != nil {
		return err
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	original := append([]Backup{}, a.store.cfg.Backups...)
	for i := range a.store.cfg.Backups {
		b := &a.store.cfg.Backups[i]
		if b.SiteID == site.ID && strings.HasPrefix(b.Path, old+string(filepath.Separator)) {
			b.Path = target + strings.TrimPrefix(b.Path, old)
		}
	}
	if err = a.store.saveLocked(); err != nil {
		a.store.cfg.Backups = original
		var rollback error
		if _, statErr := os.Lstat(old); os.IsNotExist(statErr) {
			rollback = os.Rename(target, old)
		}
		return fmt.Errorf("cannot save retained backup paths: %v (rollback: %v)", err, rollback)
	}
	return nil
}
func moveWebsiteBackupsToSafety(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("unsafe backup folder; website removal stopped")
	}
	if _, err = os.Lstat(target); !os.IsNotExist(err) {
		return fmt.Errorf("retained backup folder already exists; website removal stopped")
	}
	if err = os.Rename(source, target); err != nil {
		if !errors.Is(err, syscall.EXDEV) {
			return fmt.Errorf("cannot preserve local backups; website removal stopped: %w", err)
		}
		// Across mounts rename is impossible. Copy and verify every byte first;
		// leave originals intact until the confirmed website deletion removes home.
		if err = copyRetainedBackupTree(source, target); err != nil {
			return fmt.Errorf("cannot verify retained backups; website removal stopped (source and partial copy preserved): %w", err)
		}
	}
	return nil
}
func retainedWebsite(id string) (Site, error) {
	var site Site
	if !retainedWebsiteIDPattern.MatchString(id) {
		return site, fmt.Errorf("invalid backup website ID")
	}
	data, err := os.ReadFile(filepath.Join(retainedWebsiteDirectory(id), "site.json"))
	if err != nil {
		return site, err
	}
	err = json.Unmarshal(data, &site)
	if err != nil || site.ID != id || !domainPattern.MatchString(site.Domain) {
		return Site{}, fmt.Errorf("invalid retained website metadata")
	}
	return site, nil
}

// Never follow symlinks or overwrite an existing destination. Failed copies
// remain available for review, and must never authorize website deletion.
func copyRetainedBackupTree(source, target string) error {
	return filepath.Walk(source, func(name string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, relative)
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("backup contains a symbolic link: %s", relative)
		}
		if info.IsDir() {
			return os.Mkdir(dest, 0700)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("backup contains a special file: %s", relative)
		}
		input, err := os.Open(name)
		if err != nil {
			return err
		}
		defer input.Close()
		opened, err := input.Stat()
		if err != nil {
			return err
		}
		if !os.SameFile(info, opened) {
			return fmt.Errorf("backup changed during preservation")
		}
		output, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		digest := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(output, digest), input)
		if copyErr == nil {
			copyErr = output.Sync()
		}
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		after, err := input.Stat()
		if err != nil {
			return err
		}
		if n != info.Size() || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			return fmt.Errorf("backup changed during preservation")
		}
		verify, err := os.Open(dest)
		if err != nil {
			return err
		}
		check := sha256.New()
		_, err = io.Copy(check, verify)
		closeErr = verify.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if string(check.Sum(nil)) != string(digest.Sum(nil)) {
			return fmt.Errorf("retained backup verification failed")
		}
		return nil
	})
}
