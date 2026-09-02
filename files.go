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
	maxEditableFileSize = 4 << 20
	maxUploadSize       = 512 << 20
	maxArchiveMembers   = 100000
	maxExtractedSize    = int64(2 << 30)
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

func (s *System) ListFiles(site Site, relative string) (FileListing, error) {
	path, clean, err := resolveManagedPath(site.Root, relative, false)
	if err != nil {
		return FileListing{}, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return FileListing{}, fmt.Errorf("read directory: %w", err)
	}
	listing := FileListing{SiteID: site.ID, Domain: site.Domain, Path: clean, Entries: make([]FileEntry, 0, len(entries))}
	for _, entry := range entries {
		info, err := os.Lstat(filepath.Join(path, entry.Name()))
		if err != nil {
			continue
		}
		kind := "file"
		if info.IsDir() {
			kind = "directory"
		} else if info.Mode()&os.ModeSymlink != 0 {
			kind = "symlink"
		}
		entryPath := entry.Name()
		if clean != "" {
			entryPath = clean + "/" + entry.Name()
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

func (s *System) ReadManagedFile(site Site, relative string) (string, os.FileInfo, error) {
	path, _, err := resolveManagedPath(site.Root, relative, false)
	if err != nil {
		return "", nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", nil, fmt.Errorf("inspect file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", nil, errors.New("only regular files can be edited")
	}
	if info.Size() > maxEditableFileSize {
		return "", nil, fmt.Errorf("files larger than %d MiB cannot be edited in the browser", maxEditableFileSize>>20)
	}
	content, err := os.ReadFile(path)
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
	path, _, err := resolveManagedPath(site.Root, relative, false)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("only regular files can be edited")
	}
	if err := writeAtomic(path, []byte(content), info.Mode().Perm()); err != nil {
		return fmt.Errorf("save file: %w", err)
	}
	if !s.demo {
		_ = chownWebTree(path, s.webUser())
	}
	return nil
}

func (s *System) SaveUpload(site Site, directory, name string, source io.Reader) (FileEntry, error) {
	if err := validateFileName(name); err != nil {
		return FileEntry{}, err
	}
	dir, cleanDir, err := resolveManagedPath(site.Root, directory, false)
	if err != nil {
		return FileEntry{}, err
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return FileEntry{}, errors.New("upload destination is not a directory")
	}
	target, _, err := resolveManagedPath(site.Root, filepath.ToSlash(filepath.Join(cleanDir, name)), true)
	if err != nil {
		return FileEntry{}, err
	}
	if existing, err := os.Lstat(target); err == nil && existing.Mode()&os.ModeSymlink != 0 {
		return FileEntry{}, errors.New("refusing to overwrite a symbolic link")
	}
	temporary, err := os.CreateTemp(dir, ".litecp-upload-")
	if err != nil {
		return FileEntry{}, fmt.Errorf("create upload: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	written, copyErr := io.Copy(temporary, io.LimitReader(source, maxUploadSize+1))
	closeErr := temporary.Close()
	if copyErr != nil {
		return FileEntry{}, fmt.Errorf("receive upload: %w", copyErr)
	}
	if closeErr != nil {
		return FileEntry{}, fmt.Errorf("finish upload: %w", closeErr)
	}
	if written > maxUploadSize {
		return FileEntry{}, fmt.Errorf("uploads are limited to %d MiB per file", maxUploadSize>>20)
	}
	if err := os.Chmod(temporaryPath, 0640); err != nil {
		return FileEntry{}, err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return FileEntry{}, fmt.Errorf("store upload: %w", err)
	}
	if !s.demo {
		_ = chownWebTree(target, s.webUser())
	}
	stored, err := os.Stat(target)
	if err != nil {
		return FileEntry{}, err
	}
	relative := filepath.ToSlash(filepath.Join(cleanDir, name))
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
	target, _, err := resolveManagedPath(site.Root, filepath.ToSlash(filepath.Join(cleanParent, name)), true)
	if err != nil {
		return err
	}
	if err := os.Mkdir(target, 0750); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	if !s.demo {
		_ = chownWebTree(target, s.webUser())
	}
	return nil
}

func (s *System) RenameManagedPath(site Site, relative, destination string) error {
	source, sourceClean, err := resolveManagedPath(site.Root, relative, false)
	if err != nil {
		return err
	}
	if sourceClean == "" {
		return errors.New("the website root cannot be renamed")
	}
	target, targetClean, err := resolveManagedPath(site.Root, destination, true)
	if err != nil {
		return err
	}
	if targetClean == "" {
		return errors.New("the website root cannot be replaced")
	}
	if _, err := os.Lstat(target); err == nil {
		return errors.New("the destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(source, target); err != nil {
		return fmt.Errorf("rename path: %w", err)
	}
	return nil
}

func (s *System) DeleteManagedPath(site Site, relative string) error {
	target, clean, err := resolveManagedPath(site.Root, relative, false)
	if err != nil {
		return err
	}
	if clean == "" {
		return errors.New("the website root cannot be deleted")
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("delete path: %w", err)
	}
	return nil
}

func (s *System) ChmodManagedPath(site Site, relative, modeText string) error {
	target, clean, err := resolveManagedPath(site.Root, relative, false)
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
	if err := os.Chmod(target, value); err != nil {
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
	target, cleanDestination, err := resolveManagedPath(site.Root, destination, true)
	if err != nil {
		return FileEntry{}, err
	}
	if _, err := os.Stat(target); err == nil {
		return FileEntry{}, errors.New("archive destination already exists")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
		return FileEntry{}, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".litecp-archive-*.zip")
	if err != nil {
		return FileEntry{}, fmt.Errorf("create archive: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	writer := zip.NewWriter(temporary)
	seen := map[string]bool{}
	members := 0
	for _, relative := range paths {
		if err := ctx.Err(); err != nil {
			_ = writer.Close()
			_ = temporary.Close()
			return FileEntry{}, err
		}
		source, _, err := resolveManagedPath(site.Root, relative, false)
		if err != nil {
			_ = writer.Close()
			_ = temporary.Close()
			return FileEntry{}, err
		}
		err = filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path == temporaryPath {
				return nil
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("symbolic links cannot be archived: %s", info.Name())
			}
			archiveName, err := filepath.Rel(site.Root, path)
			if err != nil {
				return err
			}
			archiveName = filepath.ToSlash(archiveName)
			if archiveName == "." || seen[archiveName] {
				return nil
			}
			seen[archiveName] = true
			members++
			if members > maxArchiveMembers {
				return errors.New("archive contains too many entries")
			}
			header, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			header.Name = archiveName
			if info.IsDir() {
				header.Name += "/"
				header.Method = zip.Store
				_, err = writer.CreateHeader(header)
				return err
			}
			header.Method = zip.Deflate
			destination, err := writer.CreateHeader(header)
			if err != nil {
				return err
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(destination, file)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		})
		if err != nil {
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
	if err := os.Chmod(temporaryPath, 0640); err != nil {
		return FileEntry{}, err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return FileEntry{}, fmt.Errorf("store archive: %w", err)
	}
	if !s.demo {
		_ = chownWebTree(target, s.webUser())
	}
	info, err := os.Stat(target)
	if err != nil {
		return FileEntry{}, err
	}
	return FileEntry{Name: filepath.Base(target), Path: filepath.ToSlash(cleanDestination), Type: "file", Size: info.Size(), Mode: "0640", Modified: info.ModTime().UTC()}, nil
}

func (s *System) ExtractManagedArchive(ctx context.Context, site Site, archiveRelative, destinationRelative string) error {
	archivePath, _, err := resolveManagedPath(site.Root, archiveRelative, false)
	if err != nil {
		return err
	}
	destination, _, err := resolveManagedPath(site.Root, destinationRelative, false)
	if err != nil {
		return err
	}
	info, err := os.Stat(destination)
	if err != nil || !info.IsDir() {
		return errors.New("extraction destination is not a directory")
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open ZIP archive: %w", err)
	}
	defer reader.Close()
	if len(reader.File) > maxArchiveMembers {
		return errors.New("archive contains too many entries")
	}
	var total int64
	for _, member := range reader.File {
		total += int64(member.UncompressedSize64)
		if total > maxExtractedSize {
			return errors.New("archive expands beyond the 2 GiB safety limit")
		}
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
		relativeTarget, err := filepath.Rel(site.Root, filepath.Join(destination, name))
		if err != nil {
			return err
		}
		target, _, err := resolveManagedPath(site.Root, filepath.ToSlash(relativeTarget), true)
		if err != nil {
			return fmt.Errorf("unsafe archive member %s: %w", member.Name, err)
		}
		if member.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0750); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
			return err
		}
		source, err := member.Open()
		if err != nil {
			return err
		}
		mode := member.Mode().Perm()
		if mode == 0 || mode&0002 != 0 || mode&04000 != 0 {
			mode = 0640
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			_ = source.Close()
			return err
		}
		written, copyErr := io.Copy(output, io.LimitReader(source, int64(member.UncompressedSize64)+1))
		outputErr := output.Close()
		sourceErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		if written > int64(member.UncompressedSize64) {
			return errors.New("archive member exceeded its declared size")
		}
		if outputErr != nil {
			return outputErr
		}
		if sourceErr != nil {
			return sourceErr
		}
	}
	if !s.demo {
		_ = chownWebTree(destination, s.webUser())
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
