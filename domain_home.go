package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Relocate only LiteCP-managed homes. Legacy/custom document roots remain intact.
func (s *System) relocateDomainHome(ctx context.Context, current, updated Site) (func(), error) {
	noop := func() {}
	if current.Root == updated.Root {
		return noop, nil
	}
	oldHome, newHome := siteHome(current), siteHome(updated)
	if oldHome == "" || newHome == "" {
		return noop, errors.New("invalid managed website directory change")
	}
	// Existing containers and FTP databases embed absolute home paths. Refuse the
	// move rather than leave their access pointing at a missing directory.
	if current.Application != nil {
		return noop, errors.New("directory rename requires removing the application hosting configuration first; website files are unchanged")
	}
	accounts, err := s.ftpAccounts()
	if err != nil {
		return noop, err
	}
	for _, account := range accounts {
		if account.SiteID == current.ID {
			return noop, errors.New("directory rename requires removing this website's FTP accounts first; website files are unchanged")
		}
	}
	info, err := os.Lstat(oldHome)
	if err != nil {
		return noop, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return noop, errors.New("website home must be a real directory")
	}
	if _, err = os.Lstat(newHome); !os.IsNotExist(err) {
		return noop, errors.New("destination website directory already exists or cannot be inspected; nothing was overwritten")
	}
	jobs, err := s.websiteCronJobs(current.ID)
	if err != nil {
		return noop, err
	}
	updatedJobs := append([]WebsiteCronJob{}, jobs...)
	for i := range updatedJobs {
		updatedJobs[i].Command = strings.ReplaceAll(updatedJobs[i].Command, oldHome+string(filepath.Separator), newHome+string(filepath.Separator))
	}
	if err = os.Rename(oldHome, newHome); err != nil {
		return noop, fmt.Errorf("move website home: %w", err)
	}
	undo := func() {
		_ = os.Rename(newHome, oldHome)
		if current.RuntimeUID >= 1000 {
			_ = s.ensureSitePHPPool(context.Background(), current)
		}
		if current.Cron {
			_ = s.SetCron(current, true)
		}
		if len(jobs) > 0 {
			_ = s.saveWebsiteCron(current, jobs)
		}
	}
	if updated.RuntimeUID >= 1000 {
		if err = s.ensureSitePHPPool(ctx, updated); err != nil {
			undo()
			return noop, err
		}
	}
	if updated.Cron {
		if err = s.SetCron(updated, true); err != nil {
			undo()
			return noop, err
		}
	}
	if len(jobs) > 0 {
		if err = s.saveWebsiteCron(updated, updatedJobs); err != nil {
			undo()
			return noop, err
		}
	}
	return undo, nil
}
