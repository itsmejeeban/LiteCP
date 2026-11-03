package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const migrationUploadLimit = int64(2 << 30)
const migrationExtractLimit = int64(8 << 30)
const liveMigrationExtractLimit = int64(64 << 30)
const liveMigrationMemberLimit = 1000000

type MigrationPreview struct {
	ID        string    `json:"id"`
	Platform  string    `json:"platform"`
	Roots     []string  `json:"roots"`
	Databases []string  `json:"databases"`
	Files     int       `json:"files"`
	Bytes     int64     `json:"bytes"`
	Skipped   int       `json:"skipped"`
	CreatedAt time.Time `json:"createdAt"`
}

func safeArchiveName(name string) (string, error) {
	if strings.ContainsAny(name, "\\\x00:") || strings.HasPrefix(name, "/") {
		return "", errors.New("archive contains an unsafe path")
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("archive path escapes the staging directory")
	}
	if len(clean) > 4096 {
		return "", errors.New("archive path is too long")
	}
	return clean, nil
}

// Windows-created plugin archives can leave backslash separators in a native
// Linux backup. Normalize before validation, never after it; traversal and
// duplicate normalized destinations are still rejected by the extractor.
func safeMigrationArchiveName(name string) (string, error) {
	return safeArchiveName(strings.ReplaceAll(name, `\`, "/"))
}

// Extract only inert regular files/directories, never symlinks, devices, hard
// links, ownership or permissions supplied by another hosting panel.
func extractMigration(ctx context.Context, archivePath, root, platform string) (MigrationPreview, error) {
	return extractMigrationWithLimit(ctx, archivePath, root, platform, migrationExtractLimit)
}

func extractMigrationWithLimit(ctx context.Context, archivePath, root, platform string, extractLimit int64) (MigrationPreview, error) {
	preview := MigrationPreview{Platform: platform, Roots: []string{}, Databases: []string{}, CreatedAt: time.Now().UTC()}
	roots := map[string]bool{}
	files := map[string]bool{}
	memberLimit := maxArchiveMembers
	if extractLimit == liveMigrationExtractLimit {
		memberLimit = liveMigrationMemberLimit
	}
	add := func(name string, size int64, directory bool, source io.Reader) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		clean, err := safeMigrationArchiveName(name)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if clean == "." {
			return nil
		}
		if wasDirectory, exists := files[clean]; exists {
			if wasDirectory && directory {
				return secureEnsureManagedDirectory(root, clean, 0700, os.Getuid(), os.Getgid())
			}
			return fmt.Errorf("duplicate archive entry: %s", clean)
		}
		files[clean] = directory
		if len(files) > memberLimit {
			return errors.New("archive contains too many entries")
		}
		if size < 0 || size > extractLimit-preview.Bytes {
			return fmt.Errorf("archive exceeds the %d GiB extraction limit", extractLimit>>30)
		}
		if directory {
			return secureEnsureManagedDirectory(root, clean, 0700, os.Getuid(), os.Getgid())
		}
		if err := secureEnsureManagedDirectory(root, path.Dir(clean), 0700, os.Getuid(), os.Getgid()); err != nil {
			return fmt.Errorf("archive parent conflict for %s: %w", clean, err)
		}
		written, err := secureWriteManagedNoReplace(root, clean, source, size, 0600, os.Getuid(), os.Getgid())
		if err != nil {
			return fmt.Errorf("cannot extract archive entry %s: %w", clean, err)
		}
		if written != size {
			return errors.New("archive member size does not match its header")
		}
		preview.Files++
		preview.Bytes += written
		// Mail folders and attachments must never be classified as web roots
		// or database exports, even when named public_html or mysql.
		if strings.HasPrefix(clean, "cpanel_account/") {
			return nil
		}
		parts := strings.Split(clean, "/")
		for i, part := range parts {
			if ((platform == "cpanel" || platform == "litecp") && part == "public_html") || (platform == "plesk" && part == "httpdocs") {
				roots[strings.Join(parts[:i+1], "/")] = true
				break
			}
		}
		if isMigrationDatabaseDump(clean, platform) {
			preview.Databases = append(preview.Databases, clean)
		}
		return nil
	}
	input, err := os.Open(archivePath)
	if err != nil {
		return preview, err
	}
	defer input.Close()
	magic := make([]byte, 4)
	n, _ := input.Read(magic)
	_, _ = input.Seek(0, io.SeekStart)
	if n >= 4 && bytes.Equal(magic, []byte{'P', 'K', 3, 4}) {
		info, _ := input.Stat()
		reader, err := zip.NewReader(input, info.Size())
		if err != nil {
			return preview, err
		}
		if len(reader.File) > memberLimit {
			return preview, errors.New("archive contains too many entries")
		}
		for _, member := range reader.File {
			if member.Mode()&os.ModeSymlink != 0 || (!member.FileInfo().IsDir() && !member.Mode().IsRegular()) {
				preview.Skipped++
				continue
			}
			if member.UncompressedSize64 > uint64(extractLimit) {
				return preview, errors.New("archive member exceeds extraction limit")
			}
			source, err := member.Open()
			if err != nil {
				return preview, err
			}
			err = add(member.Name, int64(member.UncompressedSize64), member.FileInfo().IsDir(), source)
			_ = source.Close()
			if err != nil {
				return preview, err
			}
		}
	} else {
		var stream io.Reader = input
		if n >= 2 && magic[0] == 0x1f && magic[1] == 0x8b {
			gz, err := gzip.NewReader(input)
			if err != nil {
				return preview, err
			}
			defer gz.Close()
			stream = gz
		}
		reader := tar.NewReader(stream)
		entries := 0
		for {
			header, err := reader.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return preview, err
			}
			entries++
			if entries > memberLimit {
				return preview, errors.New("archive contains too many entries")
			}
			if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir {
				preview.Skipped++
				continue
			}
			if err := add(header.Name, header.Size, header.Typeflag == tar.TypeDir, reader); err != nil {
				return preview, err
			}
		}
	}
	for root := range roots {
		preview.Roots = append(preview.Roots, root)
	}
	sort.Strings(preview.Roots)
	sort.Strings(preview.Databases)
	if len(preview.Roots) == 0 {
		return preview, errors.New("no website root found; upload a cPanel archive containing public_html or a Plesk website export containing httpdocs. Encrypted, incremental and nested Plesk server backups must be exported as a website archive first")
	}
	return preview, nil
}

func (a *App) migrations(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		a.migrationUpload(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, POST")
		writeError(w, 405, "Method not allowed.")
		return
	}
	items := []MigrationPreview{}
	entries, err := os.ReadDir(filepath.Join(a.system.dataDir, "migrations"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		writeError(w, 500, err.Error())
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if preview, _, err := a.loadMigration(entry.Name()); err == nil {
				items = append(items, preview)
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	writeJSON(w, 200, map[string]any{"items": items})
}

func (a *App) migrationUpload(w http.ResponseWriter, r *http.Request) {
	// Only completed stages appear in the review list. Retained partial work must
	// not permanently use every slot after a failed live capture or inspection.
	staged, err := a.retainedMigrationStages()
	if err != nil {
		writeError(w, 500, "Cannot inspect retained migration stages.")
		return
	}
	if staged >= 3 {
		writeError(w, 409, "Three migration archives are already staged. Delete an unused stage before uploading another.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, migrationUploadLimit+1<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, 400, "Upload a valid archive up to 2 GiB.")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	platform := r.FormValue("platform")
	if platform != "cpanel" && platform != "plesk" {
		writeError(w, 400, "Source platform must be cPanel or Plesk.")
		return
	}
	source, _, err := r.FormFile("archive")
	if err != nil {
		writeError(w, 400, "Choose a migration archive.")
		return
	}
	defer source.Close()
	id, err := randomToken(16)
	if err != nil {
		writeError(w, 500, "Could not generate migration ID.")
		return
	}
	id = safeSiteID(id)
	root := filepath.Join(a.system.dataDir, "migrations", id)
	if err := os.MkdirAll(filepath.Join(root, "files"), 0700); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(root)
		}
	}()
	file, err := os.OpenFile(filepath.Join(root, "archive"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	written, copyErr := io.Copy(file, io.LimitReader(source, migrationUploadLimit+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || written > migrationUploadLimit {
		writeError(w, 400, "Archive upload failed or exceeded 2 GiB.")
		return
	}
	preview, err := extractMigration(r.Context(), filepath.Join(root, "archive"), filepath.Join(root, "files"), platform)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	preview.ID = id
	content, _ := json.Marshal(preview)
	if err := writeAtomic(filepath.Join(root, "manifest.json"), content, 0600); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	_ = os.Remove(filepath.Join(root, "archive"))
	keep = true
	a.store.AddActivity("Migration archive inspected", platform+": staged for review; no live data changed", "success")
	writeJSON(w, 201, preview)
}

func (a *App) retainedMigrationStages() (int, error) {
	entries, err := os.ReadDir(filepath.Join(a.system.dataDir, "migrations"))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			if _, _, err := a.loadMigration(entry.Name()); err == nil {
				count++
			}
		}
	}
	return count, nil
}

func (a *App) loadMigration(id string) (MigrationPreview, string, error) {
	if id == "" || safeSiteID(id) != id {
		return MigrationPreview{}, "", errors.New("invalid migration ID")
	}
	root := filepath.Join(a.system.dataDir, "migrations", id)
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return MigrationPreview{}, "", errors.New("migration not found")
	}
	content, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return MigrationPreview{}, "", errors.New("migration not found")
	}
	var preview MigrationPreview
	if json.Unmarshal(content, &preview) != nil || preview.ID != id {
		return preview, "", errors.New("migration manifest is invalid")
	}
	return preview, root, nil
}

func copyMigrationTree(ctx context.Context, sourceRoot string, site Site, uid, gid int) error {
	return filepath.Walk(sourceRoot, func(source string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(sourceRoot, source)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "." {
			return nil
		}
		if info.IsDir() {
			return secureEnsureManagedDirectory(site.Root, relative, 0750, uid, gid)
		}
		if !info.Mode().IsRegular() {
			return errors.New("migration contains an unsafe file")
		}
		input, err := secureOpenManaged(sourceRoot, relative, os.O_RDONLY, 0)
		if err != nil {
			return err
		}
		_, writeErr := secureWriteManagedNoReplace(site.Root, relative, input, info.Size(), 0640, uid, gid)
		closeErr := input.Close()
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	})
}

func (a *App) migrationImport(w http.ResponseWriter, r *http.Request) {
	var input struct{ ID, SiteID, Root, SQL, Password string }
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	preview, root, err := a.loadMigration(input.ID)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	site := a.store.SiteByID(input.SiteID)
	if site == nil {
		writeError(w, 404, "Website not found.")
		return
	}
	if !containsString(preview.Roots, input.Root) {
		writeError(w, 400, "Select a website root from the inspected archive.")
		return
	}
	if input.SQL != "" && !containsString(preview.Databases, input.SQL) {
		writeError(w, 400, "Select an inspected SQL dump.")
		return
	}
	// Only import into an empty, explicitly chosen site. Existing installations
	// must be backed up and cleared by the administrator, never overwritten here.
	entries, err := os.ReadDir(site.Root)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if len(entries) != 0 {
		writeError(w, 409, "Destination website must be empty. Move its placeholder or existing files to Trash first.")
		return
	}
	uid, gid, err := a.system.siteFileOwnership(*site)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if available, err := migrationFreeBytes(r.Context(), filepath.Dir(site.Root)); err != nil || available < preview.Bytes+(1<<30) {
		writeError(w, 409, "Not enough free disk space to stage this website safely; target files were not changed.")
		return
	}
	staging, err := os.MkdirTemp(filepath.Dir(site.Root), ".litecp-migration-"+safeSiteID(input.ID)+"-")
	if err != nil {
		writeError(w, 500, "Create migration staging directory: "+err.Error())
		return
	}
	keepStaging := false
	defer func() {
		if !keepStaging {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := os.Chmod(staging, 0750); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if err := os.Chown(staging, uid, gid); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	stagedSite := *site
	stagedSite.Root = staging
	if err := copyMigrationTree(r.Context(), filepath.Join(root, "files", filepath.FromSlash(input.Root)), stagedSite, uid, gid); err != nil {
		writeError(w, 502, "Migration staging failed; the live website was not changed: "+err.Error())
		return
	}
	database := a.store.DatabaseBySiteID(site.ID)
	databaseImported := false
	if input.SQL != "" {
		if database == nil || validateDatabasePassword(input.Password) != nil {
			writeError(w, 400, "The target site needs a managed database and its current password.")
			return
		}
		if err := a.system.importScopedSQL(r.Context(), filepath.Join(root, "files", filepath.FromSlash(input.SQL)), *database, input.Password); err != nil {
			writeError(w, 502, "Database import failed and the empty target database was restored: "+err.Error())
			return
		}
		databaseImported = true
	}
	rollbackDatabase := func(cause error) error {
		if !databaseImported || database == nil {
			return cause
		}
		if rollbackErr := a.system.resetManagedDatabase(context.Background(), *database); rollbackErr != nil {
			return fmt.Errorf("%v; database rollback failed: %v", cause, rollbackErr)
		}
		return cause
	}
	previousRoot := staging + "-previous"
	if err := os.Rename(site.Root, previousRoot); err != nil {
		writeError(w, 502, rollbackDatabase(fmt.Errorf("prepare atomic website switch: %w", err)).Error())
		return
	}
	if err := os.Rename(staging, site.Root); err != nil {
		restoreErr := os.Rename(previousRoot, site.Root)
		cause := fmt.Errorf("activate staged website: %w", err)
		if restoreErr != nil {
			keepStaging = true
			cause = fmt.Errorf("%v; original website restore failed: %v; staged files remain at %s", cause, restoreErr, staging)
		}
		writeError(w, 502, rollbackDatabase(cause).Error())
		return
	}
	keepStaging = true
	if err := os.RemoveAll(previousRoot); err != nil {
		writeError(w, 500, "Migration was activated, but the previous empty document root could not be removed: "+err.Error())
		return
	}
	a.store.AddActivity("Website migration imported", preview.Platform+" → "+site.Domain, "success")
	writeJSON(w, 200, map[string]any{"ok": true, "note": "Website files were activated atomically. Update application database credentials and paths using the file manager. SSL, DNS, email accounts, server settings and arbitrary source cron jobs are not imported. Test the site before changing DNS. Delete the staged archive when verified."})
}

func (a *App) migrationDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/migrations/")
	_, root, err := a.loadMigration(id)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	if err := os.RemoveAll(root); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *System) importScopedSQL(ctx context.Context, sqlPath string, database ManagedDatabase, password string) error {
	return s.importScopedSQLMode(ctx, sqlPath, database, password, false)
}
func (s *System) importScopedSQLMode(ctx context.Context, sqlPath string, database ManagedDatabase, password string, replace bool) error {
	if err := validateDatabaseObjects(database.Name, database.Username); err != nil {
		return err
	}
	if err := validateDatabasePassword(password); err != nil {
		return err
	}
	if s.demo {
		return errors.New("database migration is unavailable in demo mode")
	}
	// No root DB credentials, no mysql client shell commands, no LOCAL INFILE.
	// The source dump must not contain CREATE DATABASE or USE for the old name.
	source, err := os.Open(sqlPath)
	if err != nil {
		return err
	}
	defer source.Close()
	credentials, err := os.CreateTemp(s.dataDir, ".migration-db-*.cnf")
	if err != nil {
		return err
	}
	defer os.Remove(credentials.Name())
	escaped := strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(password)
	_, err = fmt.Fprintf(credentials, "[client]\nuser=%s\npassword=\"%s\"\n", database.Username, escaped)
	if closeErr := credentials.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	commandCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	// Keep the host's configured client socket while adding scoped credentials.
	// --defaults-file suppresses /etc/my.cnf and breaks AlmaLinux's LiteCP socket.
	emptyQuery := "SELECT (SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()) + (SELECT COUNT(*) FROM information_schema.routines WHERE routine_schema = DATABASE()) + (SELECT COUNT(*) FROM information_schema.events WHERE event_schema = DATABASE())"
	check := exec.CommandContext(commandCtx, firstAvailableCommand("mysql", "mariadb"), "--defaults-extra-file="+credentials.Name(), "--user="+database.Username, "--protocol=socket", "--batch", "--skip-column-names", database.Name, "--execute", emptyQuery)
	var checkOutput limitedBuffer
	check.Stdout, check.Stderr = &checkOutput, &checkOutput
	if err := check.Run(); err != nil {
		return fmt.Errorf("verify target database: %s", checkOutput.String())
	}
	if !replace && strings.TrimSpace(checkOutput.String()) != "0" {
		return errors.New("the target database must be empty; existing tables, routines and events were not modified")
	}
	command := exec.CommandContext(commandCtx, firstAvailableCommand("mysql", "mariadb"), "--defaults-extra-file="+credentials.Name(), "--user="+database.Username, "--protocol=socket", "--binary-mode=1", "--local-infile=0", database.Name)
	command.Stdin = source
	var stderr limitedBuffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if replace {
			return fmt.Errorf("SQL restore failed; use the retained recovery backup: %s", stderr.String())
		}
		rollbackErr := s.resetManagedDatabase(context.Background(), database)
		if rollbackErr != nil {
			return fmt.Errorf("SQL import: %s (empty-database restore failed: %v)", stderr.String(), rollbackErr)
		}
		return fmt.Errorf("SQL import: %s", stderr.String())
	}
	return nil
}

func (s *System) resetManagedDatabase(ctx context.Context, database ManagedDatabase) error {
	if err := validateDatabaseObjects(database.Name, database.Username); err != nil {
		return err
	}
	if s.demo {
		return nil
	}
	statement := fmt.Sprintf("DROP DATABASE IF EXISTS `%s`; CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", database.Name, database.Name)
	client := firstAvailableCommand("mysql", "mariadb")
	if _, err := runCommandInput(ctx, 30*time.Second, statement, client, "--protocol=socket", "--user=root", "--batch"); err != nil {
		return fmt.Errorf("restore empty managed database: %w", err)
	}
	return nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(value []byte) (int, error) {
	n := len(value)
	remaining := (1 << 20) - b.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = b.Buffer.Write(value)
	}
	return n, nil
}

// Application SQL assets (plugins, migrations, templates) are website files,
// not database exports to import as independent schemas.
func isMigrationDatabaseDump(name, platform string) bool {
	if strings.HasPrefix(name, "cpanel_account/") {
		return false
	}
	if !strings.HasSuffix(strings.ToLower(name), ".sql") || strings.EqualFold(path.Base(name), "mysql.sql") {
		return false
	}
	parts := strings.Split(name, "/")
	for _, part := range parts[:len(parts)-1] {
		if part == "public_html" || part == "httpdocs" || part == "private_home" {
			return false
		}
	}
	for _, part := range parts[:len(parts)-1] {
		if part == "mysql" || part == "databases" {
			return true
		}
	}
	return false
}
