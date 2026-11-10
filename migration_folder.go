package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	migrationFolderScanLimit = 5000
	migrationFolderListLimit = 250
)

type migrationFolderRequest struct {
	Platform   string    `json:"platform"`
	Folder     string    `json:"folder"`
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

type migrationFolderArchive struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

func migrationFolderPath(folder string) (root, relative string, err error) {
	folder = filepath.ToSlash(strings.TrimSpace(folder))
	invalid := errors.New("Enter an absolute local backup folder, for example /var/backups/migration.")
	if len(folder) > 4096 || !filepath.IsAbs(folder) || strings.HasPrefix(folder, "//") || strings.ContainsFunc(folder, unicode.IsControl) {
		return "", "", invalid
	}
	volume := filepath.VolumeName(folder)
	root = volume + string(os.PathSeparator)
	relative = strings.Trim(folder[len(volume):], "/")
	if relative == "" {
		return root, ".", nil
	}
	if strings.ContainsAny(relative, `\:`) {
		return "", "", invalid
	}
	parts := strings.Split(relative, "/")
	for _, part := range parts {
		if part == "." || part == ".." {
			return "", "", invalid
		}
	}
	switch parts[0] {
	case "proc", "sys", "dev", "run":
		return "", "", errors.New("Choose a backup storage folder outside virtual system directories.")
	}
	return root, filepath.FromSlash(relative), nil
}

func migrationFolderArchiveName(name string) bool {
	if name == "" || len(name) > 255 || !utf8.ValidString(name) || strings.ContainsAny(name, `/\:`) || strings.ContainsFunc(name, unicode.IsControl) || strings.HasPrefix(name, ".") {
		return false
	}
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".tar") || strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".tar.gz")
}

func openMigrationFolderArchive(root, relative, name string) (*os.File, os.FileInfo, error) {
	file, err := secureOpenManaged(root, filepath.Join(relative, name), os.O_RDONLY, 0)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		_ = file.Close()
		return nil, nil, errors.New("Archive must be a non-empty regular file.")
	}
	return file, info, nil
}

func readMigrationFolderRequest(w http.ResponseWriter, r *http.Request) (migrationFolderRequest, string, string, bool) {
	var input migrationFolderRequest
	if err := decodeJSONWithLimit(r, &input, 8192); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid backup folder request.")
		return input, "", "", false
	}
	if input.Platform != "cpanel" && input.Platform != "plesk" && input.Platform != "litecp" {
		writeError(w, http.StatusBadRequest, "Select cPanel, Plesk or LiteCP as the backup type.")
		return input, "", "", false
	}
	root, relative, err := migrationFolderPath(input.Folder)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return input, "", "", false
	}
	return input, root, relative, true
}

func (a *App) scanMigrationFolder(w http.ResponseWriter, r *http.Request) {
	if a.system == nil || a.system.demo {
		writeError(w, http.StatusConflict, "Folder restore requires an installed LiteCP server.")
		return
	}
	_, root, relative, ok := readMigrationFolderRequest(w, r)
	if !ok {
		return
	}
	directory, err := secureOpenManaged(root, relative, os.O_RDONLY, 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Cannot open this backup folder. Check its path and permissions and use a folder without symbolic links.")
		return
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil || !info.IsDir() {
		writeError(w, http.StatusBadRequest, "The backup location must be a folder.")
		return
	}
	names, err := directory.Readdirnames(migrationFolderScanLimit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Cannot read the backup folder.")
		return
	}
	truncated := len(names) > migrationFolderScanLimit
	if truncated {
		names = names[:migrationFolderScanLimit]
	}
	items := make([]migrationFolderArchive, 0)
	for _, name := range names {
		if r.Context().Err() != nil {
			return
		}
		if !migrationFolderArchiveName(name) {
			continue
		}
		file, stat, err := openMigrationFolderArchive(root, relative, name)
		if err != nil {
			continue
		}
		_ = file.Close()
		items = append(items, migrationFolderArchive{Name: name, Size: stat.Size(), ModifiedAt: stat.ModTime().UTC()})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ModifiedAt.Equal(items[j].ModifiedAt) {
			return items[i].Name < items[j].Name
		}
		return items[i].ModifiedAt.After(items[j].ModifiedAt)
	})
	if len(items) > migrationFolderListLimit {
		items = items[:migrationFolderListLimit]
		truncated = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "truncated": truncated})
}

func (a *App) restoreMigrationFolderArchive(w http.ResponseWriter, r *http.Request) {
	if a.system == nil || a.system.demo {
		writeError(w, http.StatusConflict, "Folder restore requires an installed LiteCP server.")
		return
	}
	input, root, relative, ok := readMigrationFolderRequest(w, r)
	if !ok {
		return
	}
	if !migrationFolderArchiveName(input.Name) || input.Size <= 0 || input.ModifiedAt.IsZero() {
		writeError(w, http.StatusBadRequest, "Scan a backup folder and select an archive first.")
		return
	}
	if !a.migrationFolderMu.TryLock() {
		writeError(w, http.StatusConflict, "Another folder backup is being inspected. Try again when it finishes.")
		return
	}
	defer a.migrationFolderMu.Unlock()
	archive, info, err := openMigrationFolderArchive(root, relative, input.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Cannot open the selected backup. Rescan the folder and select an available archive.")
		return
	}
	defer archive.Close()
	if info.Size() != input.Size || !info.ModTime().Equal(input.ModifiedAt) {
		writeError(w, http.StatusConflict, "The selected backup changed after the scan. Rescan and select it again.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	preview, err := a.stageFolderMigration(ctx, input.Platform, archive)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, errFolderMigrationStageLimit) || errors.Is(err, errFolderMigrationSpace) {
			status = http.StatusConflict
		}
		writeError(w, status, "Cannot inspect this backup. "+err.Error())
		return
	}
	_ = a.store.AddActivity("Folder backup inspected", input.Platform+": "+input.Name+" staged for review", "success")
	writeJSON(w, http.StatusCreated, preview)
}
