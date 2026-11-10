package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const folderMigrationArchiveLimit = liveNativeLimit
const folderMigrationReserve = int64(1 << 30)

var errFolderMigrationStageLimit = errors.New("three restore or migration stages are already retained. Review the existing stages before inspecting another backup")
var errFolderMigrationSpace = errors.New("insufficient free disk space to stage this backup safely")

// stageFolderMigration only reads the already-open backup. No later read uses
// its pathname, and any files written here belong to the new private stage.
func (a *App) stageFolderMigration(ctx context.Context, platform string, source *os.File) (MigrationPreview, error) {
	var preview MigrationPreview
	if platform != "cpanel" && platform != "plesk" && platform != "litecp" {
		return preview, errors.New("select a supported backup type")
	}
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > folderMigrationArchiveLimit {
		return preview, errors.New("select a nonempty regular backup file no larger than 8 GiB")
	}
	staged, err := a.retainedMigrationStages()
	if err != nil {
		return preview, fmt.Errorf("inspect retained migration stages: %w", err)
	}
	if staged >= 3 {
		return preview, errFolderMigrationStageLimit
	}
	parent := filepath.Join(a.system.dataDir, "migrations")
	if err := os.MkdirAll(parent, 0700); err != nil {
		return preview, err
	}
	input := io.NewSectionReader(source, 0, info.Size())
	magic := make([]byte, 4)
	if _, err := input.ReadAt(magic, 0); err != nil {
		return preview, errors.New("backup header cannot be read")
	}
	isZIP := bytes.Equal(magic, []byte{'P', 'K', 3, 4})
	if platform == "litecp" && !isZIP && (magic[0] != 0x1f || magic[1] != 0x8b) {
		return preview, errors.New("select a LiteCP manual website ZIP or scheduled .tar.gz backup")
	}
	var entries []*zip.File
	var required int64
	if isZIP {
		archive, err := zip.NewReader(input, input.Size())
		if err != nil {
			return preview, fmt.Errorf("read ZIP backup: %w", err)
		}
		entries = archive.File
		required, err = migrationFolderZIPSpace(entries)
		if err != nil {
			return preview, err
		}
	} else {
		required, err = migrationArchiveReaderBytes(ctx, input)
		if err != nil {
			return preview, err
		}
		if _, err := input.Seek(0, io.SeekStart); err != nil {
			return preview, err
		}
	}
	if err := a.checkFolderMigrationSpace(ctx, parent, required); err != nil {
		return preview, err
	}
	id, err := randomToken(18)
	if err != nil {
		return preview, err
	}
	id = safeSiteID(id)
	stage := filepath.Join(parent, id)
	if err := os.Mkdir(stage, 0700); err != nil {
		return preview, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(stage)
		}
	}()

	switch {
	case platform == "litecp" && isZIP:
		preview, err = inspectManualBackupMembers(ctx, entries, stage, a.system.manualBackupFreeBytes)
		// The generic migration form publishes its selected root as web
		// content. The private home archive must never be a selectable root.
		preview.Roots = []string{"public_html"}
	case platform == "litecp":
		preview, err = a.stageFolderScheduledBackup(ctx, input, stage)
	default:
		files := filepath.Join(stage, "files")
		if err := os.Mkdir(files, 0700); err != nil {
			return preview, err
		}
		preview, err = extractMigrationReader(ctx, input, files, platform, liveMigrationExtractLimit)
		if err == nil && platform == "cpanel" {
			preview.Roots = folderCPanelRoots(preview.Roots)
			if len(preview.Roots) == 0 {
				err = errors.New("no standard public_html website root found in the cPanel archive")
			}
		}
	}
	if err != nil {
		return MigrationPreview{}, err
	}
	if current, err := source.Stat(); err != nil || current.Size() != info.Size() || !current.ModTime().Equal(info.ModTime()) {
		return MigrationPreview{}, errors.New("backup changed during inspection. Scan the folder again after the file is ready")
	}
	if err := ctx.Err(); err != nil {
		return MigrationPreview{}, err
	}
	preview.ID = id
	content, err := json.Marshal(preview)
	if err != nil {
		return MigrationPreview{}, err
	}
	if err := writeAtomic(filepath.Join(stage, "manifest.json"), content, 0600); err != nil {
		return MigrationPreview{}, err
	}
	keep = true
	return preview, nil
}

func folderCPanelRoots(roots []string) []string {
	var standard []string
	for _, root := range roots {
		parts := strings.Split(root, "/")
		if root == "public_html" || root == "homedir/public_html" || (len(parts) == 3 && parts[1] == "homedir" && parts[0] != "cpanel_account" && parts[0] != "private_home") {
			standard = append(standard, root)
		}
	}
	return standard
}

func migrationFolderZIPSpace(entries []*zip.File) (int64, error) {
	if len(entries) > liveMigrationMemberLimit {
		return 0, errors.New("backup contains too many entries")
	}
	var content, space int64
	for _, entry := range entries {
		if entry.UncompressedSize64 > uint64(liveMigrationExtractLimit-content) {
			return 0, errors.New("backup exceeds the 64 GiB extraction limit")
		}
		size := int64(entry.UncompressedSize64)
		content += size
		space += ((size+4095)/4096)*4096 + 4096
	}
	return space, nil
}

func (a *App) checkFolderMigrationSpace(ctx context.Context, directory string, required int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	available, err := a.system.manualBackupFreeBytes(ctx, directory)
	if err != nil {
		return fmt.Errorf("check available migration storage: %w", err)
	}
	if available < required+folderMigrationReserve {
		return errFolderMigrationSpace
	}
	return nil
}

func (a *App) stageFolderScheduledBackup(ctx context.Context, source io.Reader, stage string) (MigrationPreview, error) {
	preview := MigrationPreview{Platform: "LiteCP scheduled backup", Roots: []string{"public_html"}, Databases: []string{}, CreatedAt: time.Now().UTC()}
	outer := filepath.Join(stage, "outer")
	if err := os.Mkdir(outer, 0700); err != nil {
		return preview, err
	}
	if _, _, err := unpackHistoryTarReader(ctx, source, outer); err != nil {
		return preview, err
	}
	entries, err := os.ReadDir(outer)
	if err != nil {
		return preview, err
	}
	websiteArchive := ""
	for _, entry := range entries {
		if !entry.Type().IsRegular() || (entry.Name() != "website.tar.gz" && entry.Name() != "database.sql") {
			return preview, errors.New("scheduled LiteCP backup must contain website.tar.gz and optionally database.sql")
		}
		if entry.Name() == "website.tar.gz" {
			websiteArchive = filepath.Join(outer, entry.Name())
		}
	}
	if websiteArchive == "" {
		return preview, errors.New("scheduled LiteCP backup is missing website.tar.gz")
	}
	required, err := migrationArchiveBytes(ctx, websiteArchive)
	if err != nil {
		return preview, err
	}
	if err := a.checkFolderMigrationSpace(ctx, stage, required); err != nil {
		return preview, err
	}
	files := filepath.Join(stage, "files", "public_html")
	if err := os.MkdirAll(files, 0700); err != nil {
		return preview, err
	}
	preview.Files, preview.Bytes, err = unpackHistoryTar(ctx, websiteArchive, files)
	if err != nil {
		return preview, err
	}
	sql := filepath.Join(outer, "database.sql")
	if info, err := os.Stat(sql); err == nil {
		if info.Size() > migrationExtractLimit-preview.Bytes {
			return preview, errors.New("scheduled backup exceeds the combined 8 GiB extraction limit")
		}
		if err := os.Rename(sql, filepath.Join(stage, "files", "database.sql")); err != nil {
			return preview, err
		}
		preview.Databases = append(preview.Databases, "database.sql")
		preview.Files++
		preview.Bytes += info.Size()
	} else if !os.IsNotExist(err) {
		return preview, err
	}
	return preview, nil
}
