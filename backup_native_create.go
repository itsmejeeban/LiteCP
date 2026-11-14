package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (s *Store) websiteBackupDatabases(siteID string) []ManagedDatabase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var databases []ManagedDatabase
	for _, database := range s.cfg.Databases {
		if database.SiteID == siteID {
			databases = append(databases, database)
		}
	}
	return databases
}

func backupDatabaseNames(site Site, databases []ManagedDatabase) ([]string, error) {
	names := make(map[string]bool)
	if site.Database != "" {
		names[site.Database] = true
	}
	for _, db := range databases {
		if db.SiteID == site.ID {
			names[db.Name] = true
		}
	}
	result := make([]string, 0, len(names))
	for name := range names {
		if len(name) > 64 || !databaseObjectPattern.MatchString(name) {
			return nil, fmt.Errorf("invalid registered database name")
		}
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

func excludedNativeHomeBackupPath(relative string) bool {
	switch relative {
	case "public_html", "backups", "logs", "tmp", ".trash":
		return true
	}
	if strings.Contains(relative, "/") {
		return false
	}
	for _, prefix := range []string{".auto-migration-", ".auto-private-migration-", ".restore-home-previous-", ".litecp-migration-", ".litecp-private-restore-"} {
		if strings.HasPrefix(relative, prefix) {
			return true
		}
	}
	return false
}

// Write only the regular files and directories the native restore can read.
// Open files through the same confined path helper used by Website Backups.
func writeNativeBackupTree(ctx context.Context, root, destination string, exclude func(string) bool) (int64, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("backup source must be a real directory")
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, err
	}
	gzipWriter := gzip.NewWriter(output)
	tarWriter := tar.NewWriter(gzipWriter)
	var total int64
	var count int
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == "." {
			return err
		}
		name := filepath.ToSlash(relative)
		if exclude != nil && exclude(name) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if clean, err := safeArchiveName(name); err != nil || clean != name {
			return fmt.Errorf("unsupported backup path %q", name)
		}
		count++
		if count > maxArchiveMembers {
			return fmt.Errorf("backup tree exceeds %d entries", maxArchiveMembers)
		}
		if entry.IsDir() {
			return tarWriter.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: 0750})
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported link or special file %q", name)
		}
		input, err := secureOpenManaged(root, name, os.O_RDONLY, 0)
		if err != nil {
			return err
		}
		defer input.Close()
		info, err := input.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > folderMigrationExtractLimit-total {
			return fmt.Errorf("unsupported backup entry %q", name)
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = name
		if err = tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if _, err = io.CopyN(tarWriter, migrationContextReader{ctx, input}, header.Size); err != nil {
			return fmt.Errorf("copy %s: %w", name, err)
		}
		after, err := input.Stat()
		if err != nil {
			return err
		}
		if after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			return fmt.Errorf("file changed during backup: %s", name)
		}
		total += header.Size
		return nil
	})
	// Close each layer even after an error and report the first failure.
	tarErr := tarWriter.Close()
	gzipErr := gzipWriter.Close()
	closeErr := output.Close()
	for _, err := range []error{walkErr, tarErr, gzipErr, closeErr} {
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

type nativeDatabaseDumper func(context.Context, string, string) error

func dumpNativeBackupDatabase(ctx context.Context, name, destination string) error {
	dump := firstAvailableCommand("mysqldump", "mariadb-dump")
	_, err := runCommand(ctx, 30*time.Minute, dump, "--protocol=socket", "--user=root", "--single-transaction", "--routines", "--triggers", "--events", "--result-file="+destination, name)
	return err
}

func createNativeBackupParts(ctx context.Context, site Site, databases []ManagedDatabase, staging, home string, dump nativeDatabaseDumper) (files, sql int64, scope string, err error) {
	names, err := backupDatabaseNames(site, databases)
	if err != nil {
		return 0, 0, "", err
	}
	files, err = writeNativeBackupTree(ctx, site.Root, filepath.Join(staging, "website.tar.gz"), nil)
	if err != nil {
		return 0, 0, "", fmt.Errorf("archive public files: %w", err)
	}
	scope = "document-root"
	if home != "" {
		extras, e := writeNativeBackupTree(ctx, home, filepath.Join(staging, "home.tar.gz"), excludedNativeHomeBackupPath)
		if e != nil {
			return 0, 0, "", fmt.Errorf("archive private website files: %w", e)
		}
		if extras > folderMigrationExtractLimit-files {
			return 0, 0, "", fmt.Errorf("website backup exceeds supported size accounting")
		}
		files += extras
		scope = "website-home"
	}
	if len(names) == 0 {
		return files, 0, scope, nil
	}
	dir := filepath.Join(staging, "databases")
	if err := os.Mkdir(dir, 0700); err != nil {
		return 0, 0, "", err
	}
	for _, name := range names {
		path := filepath.Join(dir, name+".sql")
		if err = dump(ctx, name, path); err != nil {
			return 0, 0, "", fmt.Errorf("dump database %s: %w", name, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return 0, 0, "", err
		}
		if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > folderMigrationExtractLimit-files-sql {
			return 0, 0, "", fmt.Errorf("invalid dump size for database %s", name)
		}
		sql += info.Size()
	}
	return files, sql, scope, nil
}

// Publish only after both file trees and every database dump finish. The final
// link is exclusive: a failed run cannot replace or expose a previous backup.
func publishNativeWebsiteBackup(ctx context.Context, site Site, databases []ManagedDatabase, backup Backup, home string, dump nativeDatabaseDumper) (Backup, error) {
	backupDir := filepath.Dir(backup.Path)
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return Backup{}, fmt.Errorf("create backup directory: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(backupDir); err != nil || filepath.Clean(resolved) != filepath.Clean(backupDir) {
		return Backup{}, fmt.Errorf("backup directory cannot be safely verified")
	}
	staging, err := os.MkdirTemp(backupDir, ".litecp-staging-")
	if err != nil {
		return Backup{}, fmt.Errorf("create backup staging directory: %w", err)
	}
	defer os.RemoveAll(staging) // Only this run's newly created private staging tree.
	contents := filepath.Join(staging, "contents")
	if err := os.Mkdir(contents, 0700); err != nil {
		return Backup{}, err
	}
	files, sql, scope, err := createNativeBackupParts(ctx, site, databases, contents, home, dump)
	if err != nil {
		return Backup{}, err
	}
	archive := filepath.Join(staging, "archive.tar.gz")
	if _, err := writeNativeBackupTree(ctx, contents, archive, nil); err != nil {
		return Backup{}, fmt.Errorf("finalize backup: %w", err)
	}
	info, err := os.Stat(archive)
	if err != nil {
		return Backup{}, fmt.Errorf("inspect backup: %w", err)
	}
	if err := os.Link(archive, backup.Path); err != nil {
		return Backup{}, fmt.Errorf("publish completed backup: %w", err)
	}
	backup.Size = info.Size()
	backup.FileBytes, backup.DatabaseDumpBytes, backup.Scope = &files, &sql, scope
	backup.Status = "complete"
	return backup, nil
}

// Restore account scaffolding permissions while it is still inaccessible in
// the private stage; web processes must never own its SSL/config directories.
func protectNativeBackupHomeStage(root string, site Site) error {
	for _, name := range siteHomeScaffoldDirectories {
		if strings.Contains(name, "/") {
			continue
		}
		target := filepath.Join(root, name)
		info, err := os.Lstat(target)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("invalid account directory in native backup: %s", name)
		}
		uid, gid := 0, 0
		if name == "tmp" || name == "public_ftp" {
			uid, gid = site.RuntimeUID, site.RuntimeGID
		}
		if err := os.Chown(target, uid, gid); err != nil {
			return err
		}
		if err := os.Chmod(target, 0700); err != nil {
			return err
		}
	}
	return nil
}
