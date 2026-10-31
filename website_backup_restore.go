package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func inspectManualBackup(ctx context.Context, archivePath, stage string) (MigrationPreview, error) {
	preview := MigrationPreview{Platform: "LiteCP manual ZIP", Roots: []string{"public_html"}, Databases: []string{}, CreatedAt: time.Now().UTC()}
	outer, err := zip.OpenReader(archivePath)
	if err != nil {
		return preview, errors.New("select a valid LiteCP manual website ZIP backup")
	}
	defer outer.Close()
	if len(outer.File) != 2 && len(outer.File) != 3 {
		return preview, errors.New("backup must contain public_html.zip, databases.zip and optionally home_directories.zip")
	}
	members := map[string]bool{}
	for _, member := range outer.File {
		if (member.Name != "public_html.zip" && member.Name != "databases.zip" && member.Name != "home_directories.zip") || members[member.Name] || !member.Mode().IsRegular() || member.UncompressedSize64 > uint64(migrationExtractLimit) {
			return preview, errors.New("unexpected, duplicate, oversized or unsafe backup member")
		}
		members[member.Name] = true
		input, err := member.Open()
		if err != nil {
			return preview, err
		}
		destination, err := os.OpenFile(filepath.Join(stage, member.Name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			input.Close()
			return preview, err
		}
		written, copyErr := io.Copy(destination, io.LimitReader(input, migrationExtractLimit+1))
		input.Close()
		closeErr := destination.Close()
		if copyErr != nil {
			return preview, copyErr
		}
		if closeErr != nil {
			return preview, closeErr
		}
		if written != int64(member.UncompressedSize64) {
			return preview, errors.New("nested backup size or checksum mismatch")
		}
	}
	if !members["public_html.zip"] || !members["databases.zip"] {
		return preview, errors.New("required backup components are missing")
	}
	filesRoot := filepath.Join(stage, "files")
	if err = os.MkdirAll(filesRoot, 0700); err != nil {
		return preview, err
	}
	if err = secureEnsureManagedDirectory(filesRoot, "public_html", 0700, os.Getuid(), os.Getgid()); err != nil {
		return preview, err
	}
	seen := map[string]bool{}
	count := 0
	names := []string{"public_html.zip", "databases.zip"}
	if members["home_directories.zip"] {
		names = append(names, "home_directories.zip")
		preview.Roots = append(preview.Roots, "home")
		if err = secureEnsureManagedDirectory(filesRoot, "home", 0700, os.Getuid(), os.Getgid()); err != nil {
			return preview, err
		}
	}
	for _, name := range names {
		inner, err := zip.OpenReader(filepath.Join(stage, name))
		if err != nil {
			return preview, fmt.Errorf("%s is not a valid ZIP", name)
		}
		required := int64(0)
		for _, member := range inner.File {
			if member.UncompressedSize64 > uint64(migrationExtractLimit-preview.Bytes-required) {
				inner.Close()
				return preview, errors.New("backup exceeds the combined 8 GiB extraction limit")
			}
			required += int64(member.UncompressedSize64)
		}
		if available, err := migrationFreeBytes(ctx, stage); err != nil || available < required+(256<<20) {
			inner.Close()
			return preview, errors.New("insufficient free space for extracted backup files")
		}
		for _, member := range inner.File {
			if err = ctx.Err(); err != nil {
				inner.Close()
				return preview, err
			}
			count++
			if count > maxArchiveMembers {
				inner.Close()
				return preview, errors.New("backup exceeds the 100,000-entry limit")
			}
			clean, err := safeArchiveName(member.Name)
			if err != nil {
				inner.Close()
				return preview, err
			}
			directory := member.FileInfo().IsDir()
			if !directory && !member.Mode().IsRegular() {
				inner.Close()
				return preview, errors.New("backup links and special files are not supported")
			}
			if name == "public_html.zip" {
				if clean != "public_html" && !strings.HasPrefix(clean, "public_html/") {
					inner.Close()
					return preview, errors.New("website archive must use a public_html root")
				}
			} else if name == "home_directories.zip" {
				if clean != "home" && !strings.HasPrefix(clean, "home/") {
					inner.Close()
					return preview, errors.New("home archive must use a home root")
				}
				relative := strings.TrimPrefix(clean, "home/")
				if relative == "public_html" || strings.HasPrefix(relative, "public_html/") || relative == "backups/manual" || strings.HasPrefix(relative, "backups/manual/") || relative == "backups/scheduled" || strings.HasPrefix(relative, "backups/scheduled/") {
					inner.Close()
					return preview, errors.New("home archive contains a reserved backup or public path")
				}
			} else {
				if directory || path.Base(clean) != clean || (clean != "README.txt" && (!strings.HasSuffix(clean, ".sql") || !databaseObjectPattern.MatchString(strings.TrimSuffix(clean, ".sql")))) {
					inner.Close()
					return preview, errors.New("database archive must contain SQL dumps or README.txt only")
				}
				clean = "databases/" + clean
			}
			if seen[clean] {
				inner.Close()
				return preview, errors.New("duplicate backup path")
			}
			seen[clean] = true
			if member.UncompressedSize64 > uint64(migrationExtractLimit-preview.Bytes) {
				inner.Close()
				return preview, errors.New("backup exceeds the combined 8 GiB extraction limit")
			}
			if directory {
				if err = secureEnsureManagedDirectory(filesRoot, clean, 0700, os.Getuid(), os.Getgid()); err != nil {
					inner.Close()
					return preview, err
				}
				continue
			}
			if err = secureEnsureManagedDirectory(filesRoot, path.Dir(clean), 0700, os.Getuid(), os.Getgid()); err != nil {
				inner.Close()
				return preview, err
			}
			input, err := member.Open()
			if err != nil {
				inner.Close()
				return preview, err
			}
			written, copyErr := secureWriteManagedNoReplace(filesRoot, clean, input, int64(member.UncompressedSize64), 0600, os.Getuid(), os.Getgid())
			input.Close()
			if copyErr != nil {
				inner.Close()
				return preview, copyErr
			}
			if written != int64(member.UncompressedSize64) {
				inner.Close()
				return preview, errors.New("backup member size mismatch")
			}
			preview.Bytes += written
			preview.Files++
			if name == "databases.zip" && strings.HasSuffix(clean, ".sql") {
				preview.Databases = append(preview.Databases, clean)
			}
		}
		inner.Close()
	}
	return preview, nil
}
func (a *App) stageWebsiteBackup(ctx context.Context, site Site, backupID string) (MigrationPreview, error) {
	var preview MigrationPreview
	if !liveJobIDPattern.MatchString(backupID) {
		return preview, errors.New("invalid backup ID")
	}
	backupRoot := filepath.Join(a.system.websiteBackupsRoot(site), backupID)
	if _, err := loadWebsiteZIPBackup(filepath.Dir(backupRoot), backupID, site.ID); err != nil {
		return preview, err
	}
	archive, cleanup, err := a.websiteBackupArchive(ctx, filepath.Dir(backupRoot), backupID, site.ID)
	if err != nil {
		return preview, err
	}
	defer cleanup()
	return a.prepareManualBackupStage(ctx, site, archive)
}
func (a *App) prepareManualBackupStage(ctx context.Context, site Site, archive string) (MigrationPreview, error) {
	var preview MigrationPreview
	parent := filepath.Join(a.system.dataDir, "migrations")
	if err := os.MkdirAll(parent, 0700); err != nil {
		return preview, err
	}
	entries, _ := os.ReadDir(parent)
	staged := 0
	for _, entry := range entries {
		if existing, root, err := a.loadMigration(entry.Name()); err == nil {
			staged++
			owner, _ := os.ReadFile(filepath.Join(root, "backup-owner"))
			origin, _ := os.ReadFile(filepath.Join(root, "backup-origin"))
			if string(owner) == site.ID && string(origin) == archive && existing.Platform == "LiteCP manual ZIP" {
				return existing, nil
			}
		}
	}
	if staged >= 3 && ctx.Value(directBackupRestoreContext{}) != true {
		return preview, errors.New("three restore/migration stages are already retained; review existing stages before inspecting another backup")
	}
	info, err := os.Stat(archive)
	if err != nil {
		return preview, err
	}
	if available, err := migrationFreeBytes(ctx, parent); err != nil || available < info.Size()*2+(512<<20) {
		return preview, errors.New("insufficient free space to inspect the uploaded backup safely")
	}
	id, err := randomToken(18)
	if err != nil {
		return preview, err
	}
	id = safeSiteID(id)
	root := filepath.Join(parent, id)
	if err = os.Mkdir(root, 0700); err != nil {
		return preview, err
	}
	preview, err = inspectManualBackup(ctx, archive, root)
	if err != nil {
		return preview, err
	}
	preview.ID = id
	content, _ := json.Marshal(preview)
	if err = writeAtomic(filepath.Join(root, "manifest.json"), content, 0600); err != nil {
		return preview, err
	}
	if err = writeAtomic(filepath.Join(root, "backup-owner"), []byte(site.ID), 0600); err != nil {
		return preview, err
	}
	if err = writeAtomic(filepath.Join(root, "backup-origin"), []byte(archive), 0600); err != nil {
		return preview, err
	}
	return preview, nil
}
func (a *App) websiteBackupUpload(w http.ResponseWriter, r *http.Request) {
	site := a.store.SiteByID(r.URL.Query().Get("siteId"))
	if site == nil {
		writeError(w, 404, "Website not found")
		return
	}
	if a.system.demo {
		writeError(w, 400, "Backup upload requires the installed Linux server")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, migrationUploadLimit+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, 400, "Select a valid backup ZIP up to 2 GiB")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	input, header, err := r.FormFile("backup")
	if err != nil {
		writeError(w, 400, "Choose a LiteCP manual website ZIP backup")
		return
	}
	defer input.Close()
	if header.Size <= 0 || header.Size > migrationUploadLimit {
		writeError(w, 400, "Backup ZIP must be nonempty and no larger than 2 GiB")
		return
	}
	id, err := randomToken(18)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	id = safeSiteID(id)
	root := filepath.Join(a.system.websiteBackupsRoot(*site), id)
	if err = os.MkdirAll(root, 0700); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	file, err := os.OpenFile(filepath.Join(root, "website.zip"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	written, err := io.Copy(file, io.LimitReader(input, migrationUploadLimit+1))
	closeErr := file.Close()
	if err != nil || closeErr != nil || written != header.Size {
		writeError(w, 400, "Backup upload interrupted; partial data retained privately")
		return
	}
	preview, err := a.prepareManualBackupStage(r.Context(), *site, filepath.Join(root, "website.zip"))
	if err != nil {
		writeError(w, 400, "Backup inspection failed: "+err.Error()+". Uploaded data retained privately; live website unchanged.")
		return
	}
	item := websiteZIPBackup{ID: id, SiteID: site.ID, Domain: site.Domain, Size: written, CreatedAt: time.Now().UTC()}
	content, _ := json.Marshal(item)
	if err = writeAtomic(filepath.Join(root, "manifest.json"), content, 0600); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	a.store.AddActivity("Manual backup uploaded", site.Domain+": inspected and staged; no live data changed", "success")
	writeJSON(w, 201, preview)
}
func (a *App) websiteBackupRestore(w http.ResponseWriter, r *http.Request) {
	if a.system.demo {
		writeError(w, 400, "Backup restore requires the installed Linux server")
		return
	}
	sourceSite := a.store.SiteByID(r.URL.Query().Get("siteId"))
	if sourceSite == nil {
		writeError(w, 404, "Website not found")
		return
	}
	var input struct {
		Action, ID, StageID, TargetSiteID, SQL, DatabaseID, Password string
		Confirm                                                      bool
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if input.Action == "inspect" {
		preview, err := a.stageWebsiteBackup(r.Context(), *sourceSite, input.ID)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, preview)
		return
	}
	if !input.Confirm {
		writeError(w, 400, "Confirm restore into the selected empty target")
		return
	}
	preview, root, err := a.loadMigration(input.StageID)
	if err != nil || preview.Platform != "LiteCP manual ZIP" {
		writeError(w, 404, "Inspected manual backup stage not found")
		return
	}
	owner, err := os.ReadFile(filepath.Join(root, "backup-owner"))
	if err != nil || string(owner) != sourceSite.ID {
		writeError(w, 403, "Backup stage belongs to another website")
		return
	}
	clone := r.Clone(r.Context())
	if input.Action == "files" {
		target := a.store.SiteByID(input.TargetSiteID)
		if target == nil {
			writeError(w, 404, "Target website not found")
			return
		}
		if target.Application != nil && target.Application.Enabled {
			writeError(w, 409, "Disable the target application runtime before restoring files")
			return
		}
		content, _ := json.Marshal(map[string]string{"ID": input.StageID, "SiteID": target.ID, "Root": "public_html"})
		clone.Body = io.NopCloser(bytes.NewReader(content))
		a.migrationImport(w, clone)
		return
	}
	if input.Action == "home" {
		target := a.store.SiteByID(input.TargetSiteID)
		if target == nil || siteHome(*target) == "" {
			writeError(w, 400, "Select a website with a managed home directory")
			return
		}
		if err := a.restoreBackupHome(r.Context(), *target, filepath.Join(root, "files", "home")); err != nil {
			writeError(w, 409, err.Error())
			return
		}
		a.store.AddActivity("Website home restored", target.Domain+": non-public directories restored", "success")
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if input.Action == "database" {
		content, _ := json.Marshal(map[string]any{"ID": input.StageID, "SQL": input.SQL, "DatabaseID": input.DatabaseID, "Password": input.Password, "Confirm": true})
		clone.Body = io.NopCloser(bytes.NewReader(content))
		a.migrationDatabaseImport(w, clone)
		return
	}
	writeError(w, 400, "Select files or database restore")
}

// Preflight all non-public files before writing. Existing files are never replaced.
func (a *App) restoreBackupHome(ctx context.Context, site Site, source string) error {
	home := siteHome(site)
	if home == "" {
		return errors.New("website home is unavailable")
	}
	type entry struct {
		path string
		info os.FileInfo
	}
	entries := []entry{}
	err := filepath.Walk(source, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(source, p)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		if relative == "public_html" || strings.HasPrefix(relative, "public_html/") || relative == "backups/manual" || strings.HasPrefix(relative, "backups/manual/") || relative == "backups/scheduled" || strings.HasPrefix(relative, "backups/scheduled/") {
			return errors.New("reserved home restore path")
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return errors.New("unsupported home backup entry")
		}
		if _, _, err := resolveManagedPath(home, relative, true); err != nil {
			return err
		}
		existing, err := secureOpenManaged(home, relative, os.O_RDONLY, 0)
		if err == nil {
			stat, e := existing.Stat()
			existing.Close()
			if e != nil {
				return e
			}
			if !info.IsDir() || !stat.IsDir() {
				return fmt.Errorf("restore destination already exists: %s; choose an empty target", relative)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		entries = append(entries, entry{relative, info})
		return nil
	})
	if err != nil {
		return err
	}
	uid, gid, err := a.system.siteFileOwnership(site)
	if err != nil {
		return err
	}
	for _, item := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if item.info.IsDir() {
			if err := secureEnsureManagedDirectory(home, item.path, 0750, uid, gid); err != nil {
				return err
			}
			continue
		}
		if err := secureEnsureManagedDirectory(home, filepath.ToSlash(filepath.Dir(item.path)), 0750, uid, gid); err != nil {
			return err
		}
		input, err := secureOpenManaged(source, item.path, os.O_RDONLY, 0)
		if err != nil {
			return err
		}
		_, err = secureWriteManagedNoReplace(home, item.path, input, item.info.Size(), 0640, uid, gid)
		input.Close()
		if err != nil {
			return fmt.Errorf("home restore stopped at %s: %w; copied files retained", item.path, err)
		}
	}
	return nil
}
