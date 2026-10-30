package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxBulkFileOperations = 500
	maxBulkCopySize       = int64(8 << 30)
)

type TrashEntry struct {
	ID           string    `json:"id"`
	SiteID       string    `json:"siteId"`
	Domain       string    `json:"domain"`
	OriginalPath string    `json:"originalPath"`
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	Size         int64     `json:"size"`
	DeletedAt    time.Time `json:"deletedAt"`
}

func (s *System) trashRoot() string {
	if s.demo {
		return filepath.Join(s.dataDir, "trash")
	}
	return "/var/www/litecp/.trash"
}

func (s *System) siteTrashRoot(site Site) string {
	if home := siteHome(site); home != "" {
		return filepath.Join(home, ".trash")
	}
	return filepath.Join(s.trashRoot(), safeSiteID(site.ID))
}

func (s *System) MoveManagedPathToTrash(site Site, relative string) (TrashEntry, error) {
	if protectedSitePath(site, relative) {
		return TrashEntry{}, errors.New("managed website folders cannot be moved to trash")
	}
	_, clean, err := resolveManagedPath(managedSiteRoot(site), relative, false)
	if err != nil {
		return TrashEntry{}, err
	}
	if clean == "" {
		return TrashEntry{}, errors.New("the website root cannot be deleted")
	}
	file, err := secureOpenManaged(managedSiteRoot(site), clean, os.O_RDONLY, 0)
	if err != nil {
		return TrashEntry{}, err
	}
	info, err := file.Stat()
	_ = file.Close()
	if err != nil {
		return TrashEntry{}, err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return TrashEntry{}, errors.New("only regular files and directories can be moved to trash")
	}
	id, err := randomToken(10)
	if err != nil {
		return TrashEntry{}, err
	}
	id = safeSiteID(id)
	siteTrash := s.siteTrashRoot(site)
	itemRoot := filepath.Join(siteTrash, id)
	if err := os.MkdirAll(itemRoot, 0700); err != nil {
		return TrashEntry{}, fmt.Errorf("create trash entry: %w", err)
	}
	entry := TrashEntry{
		ID: id, SiteID: site.ID, Domain: site.Domain, OriginalPath: filepath.ToSlash(clean),
		Name: filepath.Base(clean), Type: "file", Size: info.Size(), DeletedAt: time.Now().UTC(),
	}
	if info.IsDir() {
		entry.Type = "directory"
	}
	metadata, err := json.Marshal(entry)
	if err != nil {
		_ = os.RemoveAll(itemRoot)
		return TrashEntry{}, err
	}
	if err := writeAtomic(filepath.Join(itemRoot, "metadata.json"), metadata, 0600); err != nil {
		_ = os.RemoveAll(itemRoot)
		return TrashEntry{}, err
	}
	if err := secureMoveBetweenRoots(managedSiteRoot(site), clean, itemRoot, "payload"); err != nil {
		_ = os.RemoveAll(itemRoot)
		return TrashEntry{}, fmt.Errorf("move path to trash: %w", err)
	}
	if entry.Type == "directory" {
		entry.Size = directorySize(filepath.Join(itemRoot, "payload"))
		metadata, _ = json.Marshal(entry)
		_ = writeAtomic(filepath.Join(itemRoot, "metadata.json"), metadata, 0600)
	}
	return entry, nil
}

func directorySize(root string) int64 {
	var total int64
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func (s *System) readTrashEntry(site Site, id string) (TrashEntry, string, error) {
	if id == "" || safeSiteID(id) != id {
		return TrashEntry{}, "", errors.New("invalid trash entry")
	}
	itemRoot := filepath.Join(s.siteTrashRoot(site), id)
	if !pathWithinRoot(s.siteTrashRoot(site), itemRoot) {
		return TrashEntry{}, "", errors.New("invalid trash entry")
	}
	content, err := os.ReadFile(filepath.Join(itemRoot, "metadata.json"))
	if err != nil {
		return TrashEntry{}, "", errors.New("trash entry not found")
	}
	var entry TrashEntry
	if json.Unmarshal(content, &entry) != nil || entry.ID != id || entry.SiteID != site.ID {
		return TrashEntry{}, "", errors.New("trash entry metadata is invalid")
	}
	info, err := os.Lstat(filepath.Join(itemRoot, "payload"))
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return TrashEntry{}, "", errors.New("trash entry payload is missing or unsafe")
	}
	return entry, itemRoot, nil
}

func (s *System) ListTrash(site Site) ([]TrashEntry, error) {
	root := s.siteTrashRoot(site)
	items, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []TrashEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]TrashEntry, 0, len(items))
	for _, item := range items {
		if !item.IsDir() {
			continue
		}
		entry, _, err := s.readTrashEntry(site, item.Name())
		if err == nil {
			result = append(result, entry)
		}
	}
	// Assign recognizable versions oldest first, including legacy payload entries.
	sort.Slice(result, func(i, j int) bool {
		if result[i].DeletedAt.Equal(result[j].DeletedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].DeletedAt.Before(result[j].DeletedAt)
	})
	counts := make(map[string]int)
	for i := range result {
		name := filepath.Base(result[i].OriginalPath)
		counts[name]++
		result[i].Name = name
		if counts[name] > 1 {
			ext := filepath.Ext(name)
			if result[i].Type == "directory" {
				ext = ""
			}
			result[i].Name = fmt.Sprintf("%s (v%d)%s", strings.TrimSuffix(name, ext), counts[name], ext)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].DeletedAt.After(result[j].DeletedAt) })
	return result, nil
}

func (s *System) RestoreTrashEntry(site Site, id, destination string) (string, error) {
	entry, itemRoot, err := s.readTrashEntry(site, id)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(destination) == "" {
		destination = entry.OriginalPath
	}
	_, cleanDestination, err := resolveManagedPath(managedSiteRoot(site), destination, true)
	if err != nil || cleanDestination == "" {
		return "", errors.New("restore destination is invalid")
	}
	if exists, inspectErr := s.ManagedPathExists(site, cleanDestination); inspectErr != nil {
		return "", inspectErr
	} else if exists {
		return "", errors.New("restore destination already exists")
	}
	if err := secureMoveBetweenRoots(itemRoot, "payload", managedSiteRoot(site), cleanDestination); err != nil {
		return "", fmt.Errorf("restore trash entry: %w", err)
	}
	_ = os.Remove(filepath.Join(itemRoot, "metadata.json"))
	_ = os.Remove(itemRoot)
	return filepath.ToSlash(cleanDestination), nil
}

func (s *System) DeleteTrashEntry(site Site, id string) error {
	_, itemRoot, err := s.readTrashEntry(site, id)
	if err != nil {
		return err
	}
	return os.RemoveAll(itemRoot)
}

func (s *System) EmptyTrash(site Site) error {
	root := s.siteTrashRoot(site)
	if siteHome(site) == "" && !pathWithinRoot(s.trashRoot(), root) {
		return errors.New("invalid trash root")
	}
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	return os.MkdirAll(root, 0700)
}

func (s *System) CreateManagedFile(site Site, directory, name string) (FileEntry, error) {
	return s.SaveNewUpload(site, directory, name, strings.NewReader(""))
}

func (s *System) CopyManagedPaths(ctx context.Context, site Site, paths []string, destination string) error {
	if err := validateBulkSelection(paths); err != nil {
		return err
	}
	if len(paths) == 0 || len(paths) > maxBulkFileOperations {
		return errors.New("select between 1 and 500 files or directories")
	}
	_, destinationClean, err := resolveManagedPath(managedSiteRoot(site), destination, false)
	if err != nil {
		return err
	}
	destinationFile, err := secureOpenManaged(managedSiteRoot(site), destinationClean, os.O_RDONLY, 0)
	if err != nil {
		return errors.New("copy destination is not a directory")
	}
	destinationInfo, err := destinationFile.Stat()
	_ = destinationFile.Close()
	if err != nil || !destinationInfo.IsDir() {
		return errors.New("copy destination is not a directory")
	}
	uid, gid, err := s.siteFileOwnership(site)
	if err != nil {
		return err
	}
	var copied int64
	var members int
	var copyPath func(string, string) error
	copyPath = func(source, target string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		members++
		if members > maxArchiveMembers {
			return errors.New("copy contains too many entries")
		}
		input, err := secureOpenManaged(managedSiteRoot(site), source, os.O_RDONLY, 0)
		if err != nil {
			return err
		}
		info, err := input.Stat()
		if err != nil {
			_ = input.Close()
			return err
		}
		if info.IsDir() {
			_ = input.Close()
			if target == source || strings.HasPrefix(filepath.ToSlash(target)+"/", filepath.ToSlash(source)+"/") {
				return errors.New("a directory cannot be copied into itself")
			}
			if err := secureEnsureManagedDirectory(managedSiteRoot(site), target, 0750, uid, gid); err != nil {
				return err
			}
			dir, err := secureOpenManaged(managedSiteRoot(site), source, os.O_RDONLY, 0)
			if err != nil {
				return err
			}
			entries, err := dir.ReadDir(-1)
			_ = dir.Close()
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if err := copyPath(filepath.ToSlash(filepath.Join(source, entry.Name())), filepath.ToSlash(filepath.Join(target, entry.Name()))); err != nil {
					return err
				}
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			_ = input.Close()
			return errors.New("only regular files and directories can be copied")
		}
		if copied+info.Size() > maxBulkCopySize {
			_ = input.Close()
			return errors.New("copy exceeds the 8 GiB safety limit")
		}
		mode := info.Mode().Perm()
		if mode&0002 != 0 || mode&07000 != 0 {
			mode = 0640
		}
		written, err := secureWriteManagedNoReplace(managedSiteRoot(site), target, io.LimitReader(input, info.Size()), info.Size(), mode, uid, gid)
		closeErr := input.Close()
		copied += written
		if err != nil {
			return err
		}
		return closeErr
	}
	// Preflight every top-level destination before creating any copy.
	for _, source := range paths {
		_, cleanSource, err := resolveManagedPath(managedSiteRoot(site), source, false)
		if err != nil || cleanSource == "" {
			return errors.New("copy source is invalid")
		}
		target := filepath.ToSlash(filepath.Join(destinationClean, filepath.Base(cleanSource)))
		if target == cleanSource || strings.HasPrefix(target+"/", filepath.ToSlash(cleanSource)+"/") {
			return errors.New("a directory cannot be copied into itself")
		}
		if exists, err := s.ManagedPathExists(site, target); err != nil {
			return err
		} else if exists {
			return fmt.Errorf("destination already contains %s", filepath.Base(cleanSource))
		}
	}
	for _, source := range paths {
		_, cleanSource, err := resolveManagedPath(managedSiteRoot(site), source, false)
		if err != nil || cleanSource == "" {
			return errors.New("copy source is invalid")
		}
		target := filepath.ToSlash(filepath.Join(destinationClean, filepath.Base(cleanSource)))
		if exists, err := s.ManagedPathExists(site, target); err != nil {
			return err
		} else if exists {
			return fmt.Errorf("destination already contains %s", filepath.Base(cleanSource))
		}
		if err := copyPath(cleanSource, target); err != nil {
			return fmt.Errorf("copy stopped at %s; partial copies may remain in the destination (source files are unchanged): %w", cleanSource, err)
		}
	}
	return nil
}

func (s *System) MoveManagedPaths(site Site, paths []string, destination string) error {
	if err := validateBulkSelection(paths); err != nil {
		return err
	}
	if len(paths) == 0 || len(paths) > maxBulkFileOperations {
		return errors.New("select between 1 and 500 files or directories")
	}
	_, destinationClean, err := resolveManagedPath(managedSiteRoot(site), destination, false)
	if err != nil {
		return err
	}
	destinationFile, err := secureOpenManaged(managedSiteRoot(site), destinationClean, os.O_RDONLY, 0)
	if err != nil {
		return errors.New("move destination is not a directory")
	}
	info, err := destinationFile.Stat()
	_ = destinationFile.Close()
	if err != nil || !info.IsDir() {
		return errors.New("move destination is not a directory")
	}
	targets := make([]string, len(paths))
	cleans := make([]string, len(paths))
	for i, source := range paths {
		if protectedSitePath(site, source) {
			return errors.New("managed website folders cannot be moved or replaced")
		}
		_, clean, err := resolveManagedPath(managedSiteRoot(site), source, false)
		if err != nil || clean == "" {
			return errors.New("move source is invalid")
		}
		target := filepath.ToSlash(filepath.Join(destinationClean, filepath.Base(clean)))
		if target == clean || strings.HasPrefix(filepath.ToSlash(destinationClean)+"/", filepath.ToSlash(clean)+"/") {
			return errors.New("a directory cannot be moved into itself")
		}
		if exists, err := s.ManagedPathExists(site, target); err != nil {
			return err
		} else if exists {
			return fmt.Errorf("destination already contains %s", filepath.Base(clean))
		}
		cleans[i], targets[i] = clean, target
	}
	for i := range cleans {
		if err := secureRenameManaged(managedSiteRoot(site), cleans[i], targets[i]); err != nil {
			return fmt.Errorf("move stopped at %s after %d successful moves; refresh the directory before retrying: %w", cleans[i], i, err)
		}
	}
	return nil
}

func validateBulkSelection(paths []string) error {
	if len(paths) < 1 || len(paths) > maxBulkFileOperations {
		return errors.New("select between 1 and 500 items")
	}
	seen := map[string]bool{}
	for i, value := range paths {
		clean := filepath.ToSlash(filepath.Clean(value))
		base := filepath.Base(clean)
		if seen[base] {
			return errors.New("selected items have duplicate destination names")
		}
		seen[base] = true
		for j := 0; j < i; j++ {
			other := filepath.ToSlash(filepath.Clean(paths[j]))
			if strings.HasPrefix(clean+"/", other+"/") || strings.HasPrefix(other+"/", clean+"/") {
				return errors.New("do not select both a directory and its contents")
			}
		}
	}
	return nil
}

func (a *App) trash(w http.ResponseWriter, r *http.Request) {
	siteID := r.URL.Query().Get("siteId")
	if r.Method != http.MethodGet {
		var input struct{ SiteID, ID, Destination string }
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		siteID = input.SiteID
		site := a.store.SiteByID(siteID)
		if site == nil {
			writeError(w, http.StatusNotFound, "Website not found.")
			return
		}
		switch r.Method {
		case http.MethodPost:
			path, err := a.system.RestoreTrashEntry(*site, input.ID, input.Destination)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			a.store.AddActivity("File restored", site.Domain+": "+path, "success")
			writeJSON(w, http.StatusOK, map[string]string{"path": path})
		case http.MethodDelete:
			if input.ID == "" {
				if err := a.system.EmptyTrash(*site); err != nil {
					writeError(w, http.StatusBadRequest, err.Error())
					return
				}
				a.store.AddActivity("Trash emptied", site.Domain, "warning")
			} else if err := a.system.DeleteTrashEntry(*site, input.ID); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		default:
			w.Header().Set("Allow", "GET, POST, DELETE")
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
		}
		return
	}
	site := a.store.SiteByID(siteID)
	if site == nil {
		writeError(w, http.StatusNotFound, "Website not found.")
		return
	}
	entries, err := a.system.ListTrash(*site)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (a *App) createFile(w http.ResponseWriter, r *http.Request) {
	var input struct{ SiteID, Path, Name string }
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	site := a.store.SiteByID(input.SiteID)
	if site == nil {
		writeError(w, http.StatusNotFound, "Website not found.")
		return
	}
	entry, err := a.system.CreateManagedFile(*site, input.Path, input.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.store.AddActivity("File created", site.Domain+": "+entry.Path, "success")
	writeJSON(w, http.StatusCreated, entry)
}

func (a *App) transferFiles(w http.ResponseWriter, r *http.Request) {
	var input struct {
		SiteID, Operation, Destination string
		Paths                          []string
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	site := a.store.SiteByID(input.SiteID)
	if site == nil {
		writeError(w, http.StatusNotFound, "Website not found.")
		return
	}
	var err error
	switch input.Operation {
	case "copy":
		err = a.system.CopyManagedPaths(r.Context(), *site, input.Paths, input.Destination)
	case "move":
		err = a.system.MoveManagedPaths(*site, input.Paths, input.Destination)
	default:
		writeError(w, http.StatusBadRequest, "Operation must be copy or move.")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.store.AddActivity("Files "+input.Operation+"d", fmt.Sprintf("%s: %d item(s)", site.Domain, len(input.Paths)), "success")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
