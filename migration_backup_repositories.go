package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Home archives intentionally exclude generated backups. Copy the existing
// repositories into the staged home before the transactional directory swap.
// Originals remain untouched for rollback; the shared copier verifies bytes.
func preserveRestoreBackupRepositories(home, stagedHome string) error {
	for _, base := range []string{filepath.Join(home, "backups"), filepath.Join(stagedHome, "backups")} {
		info, err := os.Lstat(base)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe backup directory")
		}
	}
	for _, name := range []string{"manual", "scheduled"} {
		source := filepath.Join(home, "backups", name)
		info, err := os.Lstat(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe backup repository")
		}
		target := filepath.Join(stagedHome, "backups", name)
		if _, err = os.Lstat(target); err == nil {
			return fmt.Errorf("archive contains a reserved backup repository")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		if err = copyRetainedBackupTree(source, target); err != nil {
			return err
		}
	}
	return nil
}
