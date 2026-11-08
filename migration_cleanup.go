package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type migrationCleanupResult struct {
	Cleaned      int      `json:"cleaned"`
	Stages       int      `json:"stages"`
	Skipped      int      `json:"skipped"`
	RemovedPaths int      `json:"removedPaths"`
	Issues       []string `json:"issues"`
}

// Called only after explicit cleanup confirmation, under withAuth's mutation
// lock. Keep job history, credentials, restored websites and recovery backups.
func (a *App) cleanMigrationTemporaryFiles() (migrationCleanupResult, error) {
	result := migrationCleanupResult{Issues: []string{}}
	liveParent := filepath.Join(a.system.dataDir, "live-migrations")
	stageParent := filepath.Join(a.system.dataDir, "migrations")
	readParent := func(parent string) ([]os.DirEntry, error) {
		info, err := os.Lstat(parent)
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("migration storage is not a real directory: %s", parent)
		}
		return os.ReadDir(parent)
	}
	entries, err := readParent(liveParent)
	if err != nil {
		return result, err
	}
	stages, err := readParent(stageParent)
	if err != nil {
		return result, err
	}
	// Remove only an immediate child of the checked private storage parent.
	remove := func(parent, name string) bool {
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
			return false
		}
		target := filepath.Join(parent, name)
		info, err := os.Lstat(target)
		if errors.Is(err, os.ErrNotExist) {
			return true
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			result.Issues = append(result.Issues, target+": cannot safely inspect temporary files")
			return false
		}
		if err = os.RemoveAll(target); err != nil {
			result.Issues = append(result.Issues, target+": "+err.Error())
			return false
		}
		result.RemovedPaths++
		return true
	}
	eligible := map[string]bool{}
	known := map[string]bool{}
	referenced := map[string]bool{}
	// Restore stages can have an ID different from their live history job.
	for _, entry := range entries {
		if job, err := a.loadLiveJob(entry.Name()); err == nil && job.Preview != nil {
			referenced[job.Preview.ID] = true
		}
	}
	for _, entry := range entries {
		id := entry.Name()
		known[id] = true
		if !entry.IsDir() {
			result.Skipped++
			continue
		}
		job, err := a.loadLiveJob(id)
		liveJobMu.Lock()
		running := liveRunning[id]
		liveJobMu.Unlock()
		if err != nil || !migrationCleanupEligible(job.Status) || running || liveCaptureActive(id) {
			result.Skipped++
			if err != nil {
				result.Issues = append(result.Issues, filepath.Join(liveParent, id)+": job history is unreadable; files preserved")
			}
			continue
		}
		eligible[id] = true
		root := filepath.Join(liveParent, id)
		ok := remove(root, "native-backup")
		if !remove(root, "portable-backup") {
			ok = false
		}
		if !remove(stageParent, id) {
			ok = false
		}
		if job.Preview != nil && job.Preview.ID != id && safeSiteID(job.Preview.ID) == job.Preview.ID && job.Preview.ID != "" {
			// Preserve a stage referenced by any other nonterminal job.
			shared := false
			for _, other := range entries {
				if other.Name() == id {
					continue
				}
				if owner, e := a.loadLiveJob(other.Name()); e == nil && owner.Preview != nil && owner.Preview.ID == job.Preview.ID {
					shared = true
					break
				}
			}
			if !shared && !remove(stageParent, job.Preview.ID) {
				ok = false
			}
		}
		if !ok {
			continue
		}
		job.NativeReady, job.NativeBytes, job.Preview, job.Progress = false, 0, nil, nil
		job.ArtifactsCleaned = true
		job.Message = "Migration temporary archives and staging files cleaned. Website data and job history were preserved."
		if err := a.saveLiveJob(&job); err != nil {
			result.Issues = append(result.Issues, root+": temporary files removed but history update failed")
			continue
		}
		result.Cleaned++
	}
	for _, entry := range stages {
		name := entry.Name()
		if known[name] || referenced[name] {
			continue
		} // Already cleaned, or explicitly protected.
		if !entry.IsDir() {
			result.Skipped++
			continue
		}
		// Failed restaging retains fresh and previous trees with these generated
		// names. Never remove one while its owning job can still use it.
		stoppedRestage := false
		for id := range eligible {
			if strings.HasPrefix(name, ".restage-"+id+"-") {
				stoppedRestage = true
				break
			}
		}
		// Independently uploaded archives have no live job. A valid manifest
		// identifies them as migration staging, rather than unrelated data.
		_, _, manifestErr := a.loadMigration(name)
		if !stoppedRestage && manifestErr != nil {
			result.Skipped++
			result.Issues = append(result.Issues, filepath.Join(stageParent, name)+": unrecognized staging directory; files preserved")
			continue
		}
		if remove(stageParent, name) {
			result.Stages++
		}
	}
	return result, nil
}

// Successful restores no longer need transfer archives or extraction staging.
// Review/failed jobs retain these files for retry; recovery backups are untouched.
func (a *App) cleanSuccessfulMigration(job *LiveMigrationJob) error {
	if job.Status != "restored" || job.ArtifactsCleaned {
		return nil
	}
	root, err := a.liveJobRoot(job.ID)
	if err != nil {
		return err
	}
	stageParent := filepath.Join(a.system.dataDir, "migrations")
	type cleanupTarget struct {
		path string
		file bool
	}
	targets := []cleanupTarget{
		{path: filepath.Join(root, "native-backup"), file: true},
		{path: filepath.Join(root, "portable-backup"), file: true},
		{path: filepath.Join(stageParent, job.ID)},
	}
	if job.Preview != nil && job.Preview.ID != job.ID {
		id := job.Preview.ID
		if id == "" || safeSiteID(id) != id {
			return errors.New("invalid migration staging ID; temporary files preserved")
		}
		entries, e := os.ReadDir(filepath.Dir(root))
		if e != nil {
			return e
		}
		for _, entry := range entries {
			if entry.Name() == job.ID {
				continue
			}
			other, e := a.loadLiveJob(entry.Name())
			if e != nil {
				return errors.New("cannot verify staging ownership; temporary files preserved")
			}
			if other.Preview != nil && other.Preview.ID == id {
				return errors.New("shared migration stage preserved")
			}
		}
		targets = append(targets, cleanupTarget{path: filepath.Join(stageParent, id)})
	}
	// Check every component before removal, including parents, to reject symlink escapes.
	for _, target := range targets {
		rel, e := filepath.Rel(a.system.dataDir, target.path)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return errors.New("unsafe migration cleanup path")
		}
		current := a.system.dataDir
		parts := append([]string{"."}, strings.Split(rel, string(os.PathSeparator))...)
		for i, part := range parts {
			current = filepath.Join(current, part)
			info, e := os.Lstat(current)
			if errors.Is(e, os.ErrNotExist) {
				break
			}
			if e != nil {
				return e
			}
			if target.file && i == len(parts)-1 {
				if !info.Mode().IsRegular() {
					return errors.New("migration backup is not a regular file")
				}
			} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("migration cleanup path is not a real directory")
			}
		}
	}
	for _, target := range targets {
		remove := os.RemoveAll
		if target.file {
			remove = os.Remove
		}
		if err := remove(target.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	job.NativeReady, job.NativeBytes, job.Preview, job.Progress = false, 0, nil, nil
	job.ArtifactsCleaned = true
	return nil
}

func (a *App) finishSuccessfulMigrationCleanup(job *LiveMigrationJob) {
	if err := a.cleanSuccessfulMigration(job); err != nil {
		job.Warnings = append(job.Warnings, "Automatic temporary-file cleanup needs attention: "+err.Error())
	}
	_ = a.saveLiveJob(job)
}
