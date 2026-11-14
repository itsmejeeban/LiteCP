package main

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func nativeBackupOuterEntry(name string, kind byte) bool {
	if kind == tar.TypeDir {
		return name == "databases"
	}
	switch name {
	case "website.tar.gz", "home.tar.gz", "database.sql":
		return true
	}
	base := path.Base(name)
	return path.Dir(name) == "databases" && strings.HasSuffix(base, ".sql") &&
		len(base) <= 68 && databaseObjectPattern.MatchString(strings.TrimSuffix(base, ".sql"))
}

// Both Backup History and Restore via Folder use the same native archive layout.
func stageNativeBackup(ctx context.Context, source io.Reader, stage string, outerBudget int64, freeBytes func(context.Context, string) (int64, error)) (MigrationPreview, error) {
	preview := MigrationPreview{Platform: "LiteCP scheduled backup", Roots: []string{"public_html"}, Databases: []string{}, CreatedAt: time.Now().UTC()}
	outer := filepath.Join(stage, "outer")
	if err := os.Mkdir(outer, 0700); err != nil {
		return preview, err
	}
	if _, _, err := unpackHistoryTarValidated(ctx, source, outer, outerBudget, nativeBackupOuterEntry); err != nil {
		return preview, err
	}
	if info, err := os.Stat(filepath.Join(outer, "website.tar.gz")); err != nil || !info.Mode().IsRegular() {
		return preview, errors.New("Scheduled backup is missing website.tar.gz")
	}
	type tree struct {
		name, root string
		required   int64
	}
	trees := []tree{{name: "website.tar.gz", root: "public_html"}}
	if _, err := os.Stat(filepath.Join(outer, "home.tar.gz")); err == nil {
		trees = append(trees, tree{name: "home.tar.gz", root: "home"})
	} else if !os.IsNotExist(err) {
		return preview, err
	}
	available, err := freeBytes(ctx, stage)
	if err != nil || available <= folderMigrationReserve {
		return preview, fmt.Errorf("%w: no extraction space beyond the safety reserve", errFolderMigrationSpace)
	}
	budget := available - folderMigrationReserve
	for i := range trees {
		item := &trees[i]
		item.required, err = migrationArchiveBytesWithLimit(ctx, filepath.Join(outer, item.name), budget)
		if err != nil {
			return preview, fmt.Errorf("inspect %s: %w", item.name, err)
		}
		if item.required > budget {
			return preview, fmt.Errorf("%w: uncompressed website files exceed available staging space", errFolderMigrationSpace)
		}
		budget -= item.required
	}
	files := filepath.Join(stage, "files")
	homeAllowed := func(name string, _ byte) bool {
		for part := name; part != "."; part = path.Dir(part) {
			if excludedNativeHomeBackupPath(part) {
				return false
			}
		}
		return true
	}
	for _, item := range trees {
		destination := filepath.Join(files, item.root)
		if err := os.MkdirAll(destination, 0700); err != nil {
			return preview, err
		}
		input, err := os.Open(filepath.Join(outer, item.name))
		if err != nil {
			return preview, err
		}
		var allowed func(string, byte) bool
		if item.root == "home" {
			allowed = homeAllowed
		}
		count, size, err := unpackHistoryTarValidated(ctx, input, destination, item.required, allowed)
		input.Close()
		if err != nil {
			return preview, err
		}
		preview.Files += count
		preview.Bytes += size
	}
	addDump := func(relative string) error {
		source := filepath.Join(outer, filepath.FromSlash(relative))
		target := filepath.Join(files, filepath.FromSlash(relative))
		info, err := os.Stat(source)
		if err != nil {
			return err
		}
		if info.Size() > folderMigrationExtractLimit-preview.Bytes {
			return errors.New("Scheduled backup exceeds supported size accounting")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		if err := os.Rename(source, target); err != nil {
			return err
		}
		preview.Databases = append(preview.Databases, relative)
		preview.Files++
		preview.Bytes += info.Size()
		return nil
	}
	dumps, err := os.ReadDir(filepath.Join(outer, "databases"))
	if err != nil && !os.IsNotExist(err) {
		return preview, err
	}
	if _, err := os.Stat(filepath.Join(outer, "database.sql")); err == nil {
		if len(dumps) > 0 {
			return preview, errors.New("Backup mixes legacy and named database dumps")
		}
		if err := addDump("database.sql"); err != nil {
			return preview, err
		}
	} else if !os.IsNotExist(err) {
		return preview, err
	}
	for _, dump := range dumps {
		if err := addDump(path.Join("databases", dump.Name())); err != nil {
			return preview, err
		}
	}
	return preview, nil
}
