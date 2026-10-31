package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Only the exact recorded object at its saved destination may be retrieved or removed.
func (s *System) historyRemoteOperation(ctx context.Context, d *BackupDestination, b Backup, operation, local string) error {
	if d == nil {
		return errors.New("Remote destination no longer exists")
	}
	if err := validateBackupDestination(*d, true); err != nil {
		return err
	}
	expected := (&url.URL{Scheme: d.Protocol, Host: net.JoinHostPort(d.Host, strconv.Itoa(d.Port)), Path: path.Join(d.RemotePath, filepath.Base(b.Path))}).String()
	if b.RemotePath != expected {
		return errors.New("Recorded remote location does not match the saved destination; restore its original configuration first")
	}
	password, err := s.DecryptSecret(d.PasswordCipher)
	if err != nil {
		return errors.New("Cannot read remote credentials; edit and save destination")
	}
	remote := path.Join(d.RemotePath, filepath.Base(b.Path))
	if strings.ContainsAny(remote+password+d.Username, "\r\n\x00") {
		return errors.New("Invalid remote credentials or path")
	}
	if d.Protocol == "sftp" {
		batch := "get " + sftpBatchQuote(remote) + " " + sftpBatchQuote(local) + "\n"
		if operation == "stat" {
			batch = "ls -l " + sftpBatchQuote(remote) + "\n"
		}
		if operation == "remove" {
			batch = "rm " + sftpBatchQuote(remote) + "\n"
		}
		return s.runBackupSFTP(ctx, *d, password, batch)
	}
	lines := []string{"fail", "silent", "show-error", "connect-timeout = 20", "max-time = 1800", `user = "` + curlConfigEscape(d.Username+":"+password) + `"`, `url = "` + curlConfigEscape(expected) + `"`}
	if d.Protocol == "ftps" {
		lines = append(lines, "ssl-reqd")
	}
	if operation == "remove" {
		// quote executes DELE for this one object; list-only prevents fetching it afterwards.
		directory := (&url.URL{Scheme: d.Protocol, Host: net.JoinHostPort(d.Host, strconv.Itoa(d.Port)), Path: path.Clean(d.RemotePath) + "/"}).String()
		lines[6] = `url = "` + curlConfigEscape(directory) + `"`
		lines = append(lines, `quote = "DELE `+curlConfigEscape(remote)+`"`, "list-only", `output = "/dev/null"`)
	} else if operation == "stat" {
		lines = append(lines, "head", `output = "/dev/null"`)
	} else {
		lines = append(lines, "max-filesize = "+strconv.FormatInt(b.Size, 10), `output = "`+curlConfigEscape(local)+`"`)
	}
	_, err = runCommandInput(ctx, 30*time.Minute, strings.Join(lines, "\n")+"\n", "curl", "--disable", "--config", "-")
	if err != nil {
		return errors.New("Remote backup operation failed: " + strings.ReplaceAll(err.Error(), password, "[redacted]"))
	}
	return nil
}
func (a *App) historyLocalCopy(ctx context.Context, b Backup) (string, error) {
	if b.Size <= 0 {
		return "", errors.New("Recorded backup size is invalid")
	}
	free, err := migrationFreeBytes(ctx, a.system.dataDir)
	if err != nil || free < b.Size+(512<<20) {
		return "", errors.New("Insufficient space to retrieve remote backup")
	}
	file, err := os.CreateTemp(a.system.dataDir, "history-download-*.tar.gz")
	if err != nil {
		return "", err
	}
	name := file.Name()
	file.Close()
	if err = a.system.historyRemoteOperation(ctx, a.store.BackupDestinationByID(b.DestinationID), b, "download", name); err != nil {
		os.Remove(name)
		return "", err
	}
	info, err := os.Stat(name)
	if err != nil || info.Size() != b.Size {
		os.Remove(name)
		return "", errors.New("Remote archive size does not match the recorded backup")
	}
	return name, nil
}

// Extract inert TAR members with the same confinement and size limits used for migration.
func unpackHistoryTar(ctx context.Context, archive, root string) (int, int64, error) {
	f, e := os.Open(archive)
	if e != nil {
		return 0, 0, e
	}
	defer f.Close()
	gz, e := gzip.NewReader(f)
	if e != nil {
		return 0, 0, e
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	count := 0
	var total int64
	seen := map[string]bool{}
	for {
		if e = ctx.Err(); e != nil {
			return count, total, e
		}
		h, e := tr.Next()
		if e == io.EOF {
			return count, total, nil
		}
		if e != nil {
			return count, total, e
		}
		name, e := safeArchiveName(h.Name)
		if e != nil {
			return count, total, e
		}
		if name == "." {
			continue
		}
		if seen[name] || len(seen) >= maxArchiveMembers {
			return count, total, errors.New("Duplicate or excessive archive members")
		}
		seen[name] = true
		if h.Typeflag == tar.TypeDir {
			e = secureEnsureManagedDirectory(root, name, 0700, os.Getuid(), os.Getgid())
		} else if h.Typeflag == tar.TypeReg || h.Typeflag == tar.TypeRegA {
			if h.Size < 0 || h.Size > migrationExtractLimit-total {
				return count, total, errors.New("Backup exceeds extraction limit")
			}
			e = secureEnsureManagedDirectory(root, path.Dir(name), 0700, os.Getuid(), os.Getgid())
			if e == nil {
				var n int64
				n, e = secureWriteManagedNoReplace(root, name, tr, h.Size, 0600, os.Getuid(), os.Getgid())
				total += n
				count++
				if n != h.Size && e == nil {
					e = io.ErrUnexpectedEOF
				}
			}
		} else {
			e = errors.New("Backup contains links or special files")
		}
		if e != nil {
			return count, total, e
		}
	}
}
func (a *App) prepareHistoryBackup(ctx context.Context, b Backup) (MigrationPreview, error) {
	var p MigrationPreview
	if a.store.SiteByID(b.SiteID) == nil {
		return p, errors.New("Create or select the source website before restoring its backup")
	}
	parent := filepath.Join(a.system.dataDir, "migrations")
	if e := os.MkdirAll(parent, 0700); e != nil {
		return p, e
	}
	entries, _ := os.ReadDir(parent)
	for _, entry := range entries {
		preview, root, err := a.loadMigration(entry.Name())
		if err != nil {
			continue
		}
		origin, _ := os.ReadFile(filepath.Join(root, "backup-origin"))
		owner, _ := os.ReadFile(filepath.Join(root, "backup-owner"))
		if string(origin) == "history:"+b.ID && string(owner) == b.SiteID {
			return preview, nil
		}
	}
	count, e := a.retainedMigrationStages()
	if e != nil {
		return p, e
	}
	if count >= 3 && ctx.Value(directBackupRestoreContext{}) != true {
		return p, errors.New("Review retained migration stages before inspecting another backup")
	}
	if !validScheduledBackupPath(b.Path) {
		return p, errors.New("Backup is outside managed storage")
	}
	archive := b.Path
	resolved, e := filepath.EvalSymlinks(archive)
	if errors.Is(e, os.ErrNotExist) && b.RemotePath != "" {
		archive, e = a.historyLocalCopy(ctx, b)
		if e != nil {
			return p, e
		}
		defer os.Remove(archive)
	} else if e != nil || !validScheduledBackupPath(archive) || filepath.Clean(resolved) != filepath.Clean(archive) {
		return p, errors.New("Backup path cannot be verified")
	}
	info, e := os.Stat(archive)
	if e != nil {
		return p, e
	}
	free, e := migrationFreeBytes(ctx, parent)
	if e != nil || free < info.Size()*3+(512<<20) {
		return p, errors.New("Insufficient free space to inspect backup")
	}
	id, e := randomToken(18)
	if e != nil {
		return p, e
	}
	id = safeSiteID(id)
	root := filepath.Join(parent, id)
	outer := filepath.Join(root, "outer")
	files := filepath.Join(root, "files", "public_html")
	if e = os.MkdirAll(outer, 0700); e != nil {
		return p, e
	}
	if e = os.MkdirAll(files, 0700); e != nil {
		return p, e
	}
	if _, _, e = unpackHistoryTar(ctx, archive, outer); e != nil {
		return p, e
	}
	entries, e = os.ReadDir(outer)
	if e != nil {
		return p, e
	}
	for _, entry := range entries {
		if entry.Name() != "website.tar.gz" && entry.Name() != "database.sql" {
			return p, errors.New("Unsupported history backup contents")
		}
	}
	p.Files, p.Bytes, e = unpackHistoryTar(ctx, filepath.Join(outer, "website.tar.gz"), files)
	if e != nil {
		return p, e
	}
	p.ID = id
	p.Platform = "LiteCP manual ZIP"
	p.Roots = []string{"public_html"}
	p.Databases = []string{}
	p.CreatedAt = time.Now().UTC()
	sql := filepath.Join(outer, "database.sql")
	if stat, e := os.Stat(sql); e == nil && stat.Mode().IsRegular() {
		if stat.Size() > migrationExtractLimit-p.Bytes {
			return p, errors.New("Backup exceeds extraction limit")
		}
		if e = os.Rename(sql, filepath.Join(root, "files", "database.sql")); e != nil {
			return p, e
		}
		p.Databases = append(p.Databases, "database.sql")
	}
	content, _ := json.Marshal(p)
	if e = writeAtomic(filepath.Join(root, "manifest.json"), content, 0600); e != nil {
		return p, e
	}
	if e = writeAtomic(filepath.Join(root, "backup-owner"), []byte(b.SiteID), 0600); e != nil {
		return p, e
	}
	if e = writeAtomic(filepath.Join(root, "backup-origin"), []byte("history:"+b.ID), 0600); e != nil {
		return p, e
	}
	return p, nil
}
