package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxEditableFileSize   = 4 << 20
	maxUploadSize         = -1 // Negative means no application-imposed upload size cap.
	maxArchiveMembers     = 100000
	maxExtractedSize      = int64(2 << 30)
	maxCreatedArchiveSize = int64(8 << 30)
)

type FileEntry struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Type     string    `json:"type"`
	Size     int64     `json:"size"`
	Mode     string    `json:"mode"`
	Modified time.Time `json:"modified"`
}

type FileListing struct {
	SiteID  string      `json:"siteId"`
	Domain  string      `json:"domain"`
	Path    string      `json:"path"`
	Entries []FileEntry `json:"entries"`
}

type FileSearchResult struct {
	SiteID    string      `json:"siteId"`
	Domain    string      `json:"domain"`
	Root      string      `json:"root"`
	Entries   []FileEntry `json:"entries"`
	Truncated bool        `json:"truncated"`
}

const (
	maxFileSearchDirectories = 10000
	maxFileSearchResults     = 1000
)

type cappedArchiveWriter struct {
	destination io.Writer
	written     int64
	maximum     int64
}

func (w *cappedArchiveWriter) Write(value []byte) (int, error) {
	if int64(len(value)) > w.maximum-w.written {
		return 0, errors.New("archive exceeds the 8 GiB safety limit")
	}
	written, err := w.destination.Write(value)
	w.written += int64(written)
	return written, err
}

func (s *System) ListFiles(site Site, relative string) (FileListing, error) {
	_, clean, err := resolveManagedPath(site.Root, relative, false)
	if err != nil {
		return FileListing{}, err
	}
	directory, err := secureOpenManaged(site.Root, clean, os.O_RDONLY, 0)
	if err != nil {
		return FileListing{}, fmt.Errorf("read directory: %w", err)
	}
	defer directory.Close()
	directoryInfo, err := directory.Stat()
	if err != nil || !directoryInfo.IsDir() {
		return FileListing{}, errors.New("website path is not a directory")
	}
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return FileListing{}, fmt.Errorf("read directory: %w", err)
	}
	listing := FileListing{SiteID: site.ID, Domain: site.Domain, Path: clean, Entries: make([]FileEntry, 0, len(entries))}
	for _, entry := range entries {
		entryPath := entry.Name()
		if clean != "" {
			entryPath = clean + "/" + entry.Name()
		}
		file, err := secureOpenManaged(site.Root, entryPath, os.O_RDONLY, 0)
		if err != nil {
			// Symlinks and paths concurrently replaced with symlinks are intentionally
			// hidden because the file manager never follows them.
			continue
		}
		info, err := file.Stat()
		_ = file.Close()
		if err != nil {
			continue
		}
		kind := "file"
		if info.IsDir() {
			kind = "directory"
		}
		listing.Entries = append(listing.Entries, FileEntry{
			Name: entry.Name(), Path: filepath.ToSlash(entryPath), Type: kind,
			Size: info.Size(), Mode: fmt.Sprintf("%04o", info.Mode().Perm()), Modified: info.ModTime().UTC(),
		})
	}
	sort.Slice(listing.Entries, func(i, j int) bool {
		if listing.Entries[i].Type == listing.Entries[j].Type {
			return strings.ToLower(listing.Entries[i].Name) < strings.ToLower(listing.Entries[j].Name)
		}
		return listing.Entries[i].Type == "directory"
	})
	return listing, nil
}

// SearchManagedFiles performs a bounded recursive name search without ever
// leaving the selected website root. ListFiles deliberately re-opens every
// directory through secureOpenManaged, so symlinks and concurrent path swaps
// receive the same protection as normal File Manager navigation.
func (s *System) SearchManagedFiles(site Site, root, query string, includeHidden bool) (FileSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return FileSearchResult{}, errors.New("enter a file or folder name to search")
	}
	if len(query) > 200 {
		return FileSearchResult{}, errors.New("search text is too long")
	}
	_, cleanRoot, err := resolveManagedPath(site.Root, root, false)
	if err != nil {
		return FileSearchResult{}, err
	}
	result := FileSearchResult{SiteID: site.ID, Domain: site.Domain, Root: cleanRoot, Entries: []FileEntry{}}
	queue := []string{cleanRoot}
	needle := strings.ToLower(query)
	visited := 0
	for len(queue) > 0 {
		if visited >= maxFileSearchDirectories || len(result.Entries) >= maxFileSearchResults {
			result.Truncated = true
			break
		}
		current := queue[0]
		queue = queue[1:]
		visited++
		listing, listErr := s.ListFiles(site, current)
		if listErr != nil {
			if current == cleanRoot {
				return FileSearchResult{}, listErr
			}
			continue
		}
		for _, entry := range listing.Entries {
			if !includeHidden && strings.HasPrefix(entry.Name, ".") {
				continue
			}
			if strings.Contains(strings.ToLower(entry.Name), needle) {
				result.Entries = append(result.Entries, entry)
				if len(result.Entries) >= maxFileSearchResults {
					result.Truncated = true
					break
				}
			}
			if entry.Type == "directory" {
				queue = append(queue, entry.Path)
			}
		}
	}
	sort.Slice(result.Entries, func(i, j int) bool {
		return strings.ToLower(result.Entries[i].Path) < strings.ToLower(result.Entries[j].Path)
	})
	return result, nil
}

func (s *System) ManagedPathExists(site Site, relative string) (bool, error) {
	file, err := secureOpenManaged(site.Root, relative, os.O_RDONLY, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, file.Close()
}

func (s *System) ReadManagedFile(site Site, relative string) (string, os.FileInfo, error) {
	file, err := secureOpenManaged(site.Root, relative, os.O_RDONLY, 0)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", nil, fmt.Errorf("inspect file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", nil, errors.New("only regular files can be edited")
	}
	if info.Size() > maxEditableFileSize {
		return "", nil, fmt.Errorf("files larger than %d MiB cannot be edited in the browser", maxEditableFileSize>>20)
	}
	content, err := io.ReadAll(io.LimitReader(file, maxEditableFileSize+1))
	if err != nil {
		return "", nil, fmt.Errorf("read file: %w", err)
	}
	if !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0 {
		return "", nil, errors.New("binary files cannot be edited in the browser")
	}
	return string(content), info, nil
}

func (s *System) WriteManagedFile(site Site, relative, content string) error {
	if len(content) > maxEditableFileSize {
		return fmt.Errorf("file content exceeds %d MiB", maxEditableFileSize>>20)
	}
	file, err := secureOpenManaged(site.Root, relative, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	_ = file.Close()
	if err != nil {
		return fmt.Errorf("inspect file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("only regular files can be edited")
	}
	ownerID, groupID, err := s.siteFileOwnership(site)
	if err != nil {
		return err
	}
	if _, err := secureWriteManaged(site.Root, relative, strings.NewReader(content), maxEditableFileSize, info.Mode().Perm(), ownerID, groupID); err != nil {
		return fmt.Errorf("save file: %w", err)
	}
	return nil
}

func (s *System) SaveUpload(site Site, directory, name string, source io.Reader) (FileEntry, error) {
	return s.saveUpload(site, directory, name, source, false)
}

func (s *System) SaveNewUpload(site Site, directory, name string, source io.Reader) (FileEntry, error) {
	return s.saveUpload(site, directory, name, source, true)
}

func (s *System) saveUpload(site Site, directory, name string, source io.Reader, noReplace bool) (FileEntry, error) {
	if err := validateFileName(name); err != nil {
		return FileEntry{}, err
	}
	_, cleanDir, err := resolveManagedPath(site.Root, directory, false)
	if err != nil {
		return FileEntry{}, err
	}
	dir, err := secureOpenManaged(site.Root, cleanDir, os.O_RDONLY, 0)
	if err != nil {
		return FileEntry{}, errors.New("upload destination is not a directory")
	}
	info, err := dir.Stat()
	_ = dir.Close()
	if err != nil || !info.IsDir() {
		return FileEntry{}, errors.New("upload destination is not a directory")
	}
	relative := filepath.ToSlash(filepath.Join(cleanDir, name))
	ownerID, groupID, err := s.siteFileOwnership(site)
	if err != nil {
		return FileEntry{}, err
	}
	write := secureWriteManaged
	if noReplace {
		write = secureWriteManagedNoReplace
	}
	_, err = write(site.Root, relative, source, maxUploadSize, 0640, ownerID, groupID)
	if err != nil {
		return FileEntry{}, fmt.Errorf("receive upload: %w", err)
	}
	storedFile, err := secureOpenManaged(site.Root, relative, os.O_RDONLY, 0)
	if err != nil {
		return FileEntry{}, err
	}
	stored, err := storedFile.Stat()
	_ = storedFile.Close()
	if err != nil {
		return FileEntry{}, err
	}
	return FileEntry{Name: name, Path: relative, Type: "file", Size: stored.Size(), Mode: "0640", Modified: stored.ModTime().UTC()}, nil
}

func (s *System) CreateManagedDirectory(site Site, parent, name string) error {
	if err := validateFileName(name); err != nil {
		return err
	}
	_, cleanParent, err := resolveManagedPath(site.Root, parent, false)
	if err != nil {
		return err
	}
	ownerID, groupID, err := s.siteFileOwnership(site)
	if err != nil {
		return err
	}
	if err := secureCreateManagedDirectory(site.Root, cleanParent, name, 0750, ownerID, groupID); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	return nil
}

func (s *System) RenameManagedPath(site Site, relative, destination string) error {
	_, sourceClean, err := resolveManagedPath(site.Root, relative, false)
	if err != nil {
		return err
	}
	if sourceClean == "" {
		return errors.New("the website root cannot be renamed")
	}
	_, targetClean, err := resolveManagedPath(site.Root, destination, true)
	if err != nil {
		return err
	}
	if targetClean == "" {
		return errors.New("the website root cannot be replaced")
	}
	if err := secureRenameManaged(site.Root, sourceClean, targetClean); err != nil {
		return fmt.Errorf("rename path: %w", err)
	}
	return nil
}

func (s *System) DeleteManagedPath(site Site, relative string) error {
	_, clean, err := resolveManagedPath(site.Root, relative, false)
	if err != nil {
		return err
	}
	if clean == "" {
		return errors.New("the website root cannot be deleted")
	}
	if err := secureRemoveManaged(site.Root, clean); err != nil {
		return fmt.Errorf("delete path: %w", err)
	}
	return nil
}

func (s *System) ChmodManagedPath(site Site, relative, modeText string) error {
	_, clean, err := resolveManagedPath(site.Root, relative, false)
	if err != nil {
		return err
	}
	if clean == "" {
		return errors.New("the website root permissions cannot be changed")
	}
	value, err := parseSafeFileMode(modeText)
	if err != nil {
		return err
	}
	if err := secureChmodManaged(site.Root, clean, value); err != nil {
		return fmt.Errorf("change permissions: %w", err)
	}
	return nil
}

func (s *System) ArchiveManagedPaths(ctx context.Context, site Site, paths []string, destination string) (FileEntry, error) {
	if len(paths) == 0 {
		return FileEntry{}, errors.New("select at least one file or directory")
	}
	if len(paths) > 500 {
		return FileEntry{}, errors.New("too many selected paths")
	}
	if !strings.HasSuffix(strings.ToLower(destination), ".zip") {
		destination += ".zip"
	}
	if err := validateFileName(filepath.Base(destination)); err != nil {
		return FileEntry{}, err
	}
	_, cleanDestination, err := resolveManagedPath(site.Root, destination, true)
	if err != nil {
		return FileEntry{}, err
	}
	exists, err := s.ManagedPathExists(site, cleanDestination)
	if err != nil {
		return FileEntry{}, fmt.Errorf("inspect archive destination: %w", err)
	}
	if exists {
		return FileEntry{}, errors.New("archive destination already exists")
	}
	ownerID, groupID, err := s.siteFileOwnership(site)
	if err != nil {
		return FileEntry{}, err
	}
	if err := secureEnsureManagedDirectory(site.Root, filepath.ToSlash(filepath.Dir(cleanDestination)), 0750, ownerID, groupID); err != nil {
		return FileEntry{}, err
	}
	temporary, err := os.CreateTemp(s.dataDir, ".litecp-archive-*.zip")
	if err != nil {
		return FileEntry{}, fmt.Errorf("create archive: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	limitedDestination := &cappedArchiveWriter{destination: temporary, maximum: maxCreatedArchiveSize}
	writer := zip.NewWriter(limitedDestination)
	seen := map[string]bool{}
	members := 0
	var addPath func(string) error
	addPath = func(relative string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, clean, err := resolveManagedPath(site.Root, relative, false)
		if err != nil {
			return err
		}
		archiveName := filepath.ToSlash(clean)
		if archiveName == "." {
			archiveName = ""
		}
		if archiveName != "" && seen[archiveName] {
			return nil
		}
		file, err := secureOpenManaged(site.Root, clean, os.O_RDONLY, 0)
		if err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			_ = file.Close()
			return fmt.Errorf("only regular files and directories can be archived: %s", archiveName)
		}
		if archiveName != "" {
			seen[archiveName] = true
			members++
			if members > maxArchiveMembers {
				_ = file.Close()
				return errors.New("archive contains too many entries")
			}
			header, err := zip.FileInfoHeader(info)
			if err != nil {
				_ = file.Close()
				return err
			}
			header.Name = archiveName
			if info.IsDir() {
				header.Name += "/"
				header.Method = zip.Store
				if _, err := writer.CreateHeader(header); err != nil {
					_ = file.Close()
					return err
				}
			} else {
				header.Method = zip.Deflate
				destination, err := writer.CreateHeader(header)
				if err != nil {
					_ = file.Close()
					return err
				}
				_, copyErr := io.Copy(destination, file)
				closeErr := file.Close()
				if copyErr != nil {
					return copyErr
				}
				return closeErr
			}
		}
		entries, err := file.ReadDir(-1)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			child := filepath.ToSlash(filepath.Join(clean, entry.Name()))
			if err := addPath(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, relative := range paths {
		if err := addPath(relative); err != nil {
			_ = writer.Close()
			_ = temporary.Close()
			return FileEntry{}, fmt.Errorf("archive files: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		_ = temporary.Close()
		return FileEntry{}, fmt.Errorf("finish archive: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return FileEntry{}, err
	}
	archiveSource, err := os.Open(temporaryPath)
	if err != nil {
		return FileEntry{}, err
	}
	written, storeErr := secureWriteManagedNoReplace(site.Root, cleanDestination, archiveSource, maxCreatedArchiveSize, 0640, ownerID, groupID)
	closeErr := archiveSource.Close()
	if storeErr != nil {
		return FileEntry{}, fmt.Errorf("store archive: %w", storeErr)
	}
	if closeErr != nil {
		return FileEntry{}, closeErr
	}
	storedArchive, err := secureOpenManaged(site.Root, cleanDestination, os.O_RDONLY, 0)
	if err != nil {
		return FileEntry{}, err
	}
	info, err := storedArchive.Stat()
	_ = storedArchive.Close()
	if err != nil {
		return FileEntry{}, err
	}
	if info.Size() != written {
		return FileEntry{}, errors.New("stored archive size verification failed")
	}
	return FileEntry{Name: filepath.Base(cleanDestination), Path: filepath.ToSlash(cleanDestination), Type: "file", Size: info.Size(), Mode: "0640", Modified: info.ModTime().UTC()}, nil
}

func (s *System) ExtractManagedArchive(ctx context.Context, site Site, archiveRelative, destinationRelative string) error {
	_, archiveClean, err := resolveManagedPath(site.Root, archiveRelative, false)
	if err != nil {
		return err
	}
	_, destinationClean, err := resolveManagedPath(site.Root, destinationRelative, false)
	if err != nil {
		return err
	}
	destinationFile, err := secureOpenManaged(site.Root, destinationClean, os.O_RDONLY, 0)
	if err != nil {
		return errors.New("extraction destination is not a directory")
	}
	info, err := destinationFile.Stat()
	_ = destinationFile.Close()
	if err != nil || !info.IsDir() {
		return errors.New("extraction destination is not a directory")
	}
	archiveFile, err := secureOpenManaged(site.Root, archiveClean, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("open ZIP archive: %w", err)
	}
	defer archiveFile.Close()
	archiveInfo, err := archiveFile.Stat()
	if err != nil || !archiveInfo.Mode().IsRegular() {
		return errors.New("ZIP archive must be a regular file")
	}
	reader, err := zip.NewReader(archiveFile, archiveInfo.Size())
	if err != nil {
		return fmt.Errorf("open ZIP archive: %w", err)
	}
	if len(reader.File) > maxArchiveMembers {
		return errors.New("archive contains too many entries")
	}
	ownerID, groupID, err := s.siteFileOwnership(site)
	if err != nil {
		return err
	}
	type extractionItem struct {
		member  *zip.File
		target  string
		mode    os.FileMode
		existed bool
	}
	type movedBackup struct{ target, backup string }
	items := make([]extractionItem, 0, len(reader.File))
	directoryExisted := map[string]bool{}
	targetKinds := map[string]string{}
	seenTargets := map[string]bool{}
	inspectManaged := func(relative string) (os.FileInfo, bool, error) {
		file, openErr := secureOpenManaged(site.Root, relative, os.O_RDONLY, 0)
		if openErr != nil {
			if errors.Is(openErr, os.ErrNotExist) {
				return nil, false, nil
			}
			return nil, false, openErr
		}
		defer file.Close()
		value, statErr := file.Stat()
		return value, true, statErr
	}
	var addDirectory func(string) error
	addDirectory = func(relative string) error {
		relative = filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
		if relative == "." || relative == "" {
			return nil
		}
		if kind := targetKinds[relative]; kind == "file" {
			return fmt.Errorf("archive path is both a file and a directory: %s", relative)
		}
		targetKinds[relative] = "directory"
		if _, planned := directoryExisted[relative]; planned {
			return nil
		}
		if err := addDirectory(filepath.ToSlash(filepath.Dir(relative))); err != nil {
			return err
		}
		current, exists, inspectErr := inspectManaged(relative)
		if inspectErr != nil {
			return inspectErr
		}
		if exists && !current.IsDir() {
			return fmt.Errorf("archive directory conflicts with an existing file: %s", relative)
		}
		directoryExisted[relative] = exists
		return nil
	}
	var total int64
	for _, member := range reader.File {
		if member.UncompressedSize64 > uint64(maxExtractedSize-total) {
			return errors.New("archive expands beyond the 2 GiB safety limit")
		}
		total += int64(member.UncompressedSize64)
		if err := ctx.Err(); err != nil {
			return err
		}
		if member.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive contains a symbolic link: %s", member.Name)
		}
		name := filepath.Clean(filepath.FromSlash(member.Name))
		if name == "." || filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("archive contains an unsafe path: %s", member.Name)
		}
		relativeTarget := filepath.ToSlash(filepath.Join(destinationClean, name))
		_, cleanTarget, err := resolveManagedPath(site.Root, relativeTarget, true)
		if err != nil {
			return fmt.Errorf("unsafe archive member %s: %w", member.Name, err)
		}
		if cleanTarget == archiveClean {
			return errors.New("archive cannot overwrite itself while it is being extracted")
		}
		if seenTargets[cleanTarget] {
			return fmt.Errorf("archive contains a duplicate path: %s", member.Name)
		}
		seenTargets[cleanTarget] = true
		if member.FileInfo().IsDir() {
			if err := addDirectory(cleanTarget); err != nil {
				return err
			}
			continue
		}
		if !member.Mode().IsRegular() {
			return fmt.Errorf("archive contains a non-regular file: %s", member.Name)
		}
		if kind := targetKinds[cleanTarget]; kind == "directory" {
			return fmt.Errorf("archive path is both a file and a directory: %s", member.Name)
		}
		targetKinds[cleanTarget] = "file"
		if err := addDirectory(filepath.ToSlash(filepath.Dir(cleanTarget))); err != nil {
			return err
		}
		current, exists, inspectErr := inspectManaged(cleanTarget)
		if inspectErr != nil {
			return inspectErr
		}
		if exists && !current.Mode().IsRegular() {
			return fmt.Errorf("archive file conflicts with a non-regular path: %s", member.Name)
		}
		mode := member.Mode().Perm()
		if mode == 0 || mode&0002 != 0 || mode&04000 != 0 {
			mode = 0640
		}
		items = append(items, extractionItem{member: member, target: cleanTarget, mode: mode, existed: exists})
	}

	directories := make([]string, 0, len(directoryExisted))
	for directory := range directoryExisted {
		directories = append(directories, directory)
	}
	sort.Slice(directories, func(i, j int) bool {
		left, right := strings.Count(filepath.ToSlash(directories[i]), "/"), strings.Count(filepath.ToSlash(directories[j]), "/")
		if left == right {
			return directories[i] < directories[j]
		}
		return left < right
	})
	token, err := randomToken(8)
	if err != nil {
		return err
	}
	backupRoot := ".litecp-extract-backup-" + safeSiteID(token)
	backupCreated := false
	backups := []movedBackup{}
	writtenTargets := []string{}
	rollback := func(cause error) error {
		var rollbackErr error
		for i := len(writtenTargets) - 1; i >= 0; i-- {
			if removeErr := secureRemoveManaged(site.Root, writtenTargets[i]); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) && rollbackErr == nil {
				rollbackErr = removeErr
			}
		}
		for i := len(backups) - 1; i >= 0; i-- {
			if restoreErr := secureRenameManaged(site.Root, backups[i].backup, backups[i].target); restoreErr != nil && rollbackErr == nil {
				rollbackErr = restoreErr
			}
		}
		for i := len(directories) - 1; i >= 0; i-- {
			if directoryExisted[directories[i]] {
				continue
			}
			// Remove only empty directories. A concurrent SFTP upload is preserved.
			_ = secureRemoveManagedDirectoryIfEmpty(site.Root, directories[i])
		}
		if backupCreated && rollbackErr == nil {
			if removeErr := secureRemoveManaged(site.Root, backupRoot); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				rollbackErr = removeErr
			}
		}
		if rollbackErr != nil {
			return fmt.Errorf("%v; extraction rollback failed: %v; recovery data was kept under %s", cause, rollbackErr, backupRoot)
		}
		return cause
	}
	for _, item := range items {
		if !item.existed {
			continue
		}
		if !backupCreated {
			if err := secureCreateManagedDirectory(site.Root, "", backupRoot, 0700, ownerID, groupID); err != nil {
				return err
			}
			backupCreated = true
		}
		backupTarget := filepath.ToSlash(filepath.Join(backupRoot, item.target))
		if err := secureEnsureManagedDirectory(site.Root, filepath.ToSlash(filepath.Dir(backupTarget)), 0700, ownerID, groupID); err != nil {
			return rollback(err)
		}
		if err := secureRenameManaged(site.Root, item.target, backupTarget); err != nil {
			return rollback(fmt.Errorf("preserve existing file %s: %w", item.target, err))
		}
		backups = append(backups, movedBackup{target: item.target, backup: backupTarget})
	}
	for _, directory := range directories {
		if directoryExisted[directory] {
			continue
		}
		if err := secureEnsureManagedDirectory(site.Root, directory, 0750, ownerID, groupID); err != nil {
			return rollback(err)
		}
	}
	for _, item := range items {
		source, openErr := item.member.Open()
		if openErr != nil {
			return rollback(openErr)
		}
		written, copyErr := secureWriteManagedNoReplace(site.Root, item.target, source, int64(item.member.UncompressedSize64), item.mode, ownerID, groupID)
		sourceErr := source.Close()
		if copyErr != nil {
			return rollback(copyErr)
		}
		writtenTargets = append(writtenTargets, item.target)
		if written > int64(item.member.UncompressedSize64) {
			return rollback(errors.New("archive member exceeded its declared size"))
		}
		if sourceErr != nil {
			return rollback(sourceErr)
		}
	}
	if backupCreated {
		if err := secureRemoveManaged(site.Root, backupRoot); err != nil {
			return fmt.Errorf("archive extracted, but old-file cleanup failed: %w", err)
		}
	}
	return nil
}

func resolveManagedPath(root, relative string, allowMissing bool) (string, string, error) {
	root = filepath.Clean(root)
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve website root: %w", err)
	}
	if strings.ContainsRune(relative, 0) || filepath.IsAbs(relative) || strings.HasPrefix(relative, "/") || strings.HasPrefix(relative, `\`) {
		return "", "", errors.New("invalid website path")
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." {
		clean = ""
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", "", errors.New("path escapes the website root")
	}
	target := filepath.Join(rootResolved, clean)
	if !pathWithinRoot(rootResolved, target) {
		return "", "", errors.New("path escapes the website root")
	}
	check := target
	for {
		resolved, resolveErr := filepath.EvalSymlinks(check)
		if resolveErr == nil {
			if !pathWithinRoot(rootResolved, resolved) {
				return "", "", errors.New("symbolic link escapes the website root")
			}
			break
		}
		if !allowMissing || !errors.Is(resolveErr, os.ErrNotExist) {
			return "", "", fmt.Errorf("resolve website path: %w", resolveErr)
		}
		parent := filepath.Dir(check)
		if parent == check || !pathWithinRoot(rootResolved, parent) {
			return "", "", errors.New("could not resolve website path")
		}
		check = parent
	}
	return target, filepath.ToSlash(clean), nil
}

func pathWithinRoot(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func validateFileName(name string) error {
	if name == "" || name == "." || name == ".." || len(name) > 255 || strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return errors.New("enter a valid file or directory name")
	}
	return nil
}

func parseSafeFileMode(value string) (os.FileMode, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "0")
	parsed, err := strconv.ParseUint(value, 8, 9)
	if err != nil {
		return 0, errors.New("permissions must be an octal mode such as 0644 or 0750")
	}
	mode := os.FileMode(parsed)
	if mode&0002 != 0 || mode&07000 != 0 {
		return 0, errors.New("world-writable and special permission bits are not allowed")
	}
	return mode, nil
}

func managedWriteReader(source io.Reader, maximum int64) io.Reader {
	if maximum < 0 {
		return source
	}
	return io.LimitReader(source, maximum+1)
}
