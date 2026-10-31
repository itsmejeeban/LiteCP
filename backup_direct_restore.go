package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type directBackupRestoreContext struct{}

func (a *App) directBackupRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		job, err := a.loadLiveJob(r.URL.Query().Get("id"))
		if err != nil || job.Source != "backup" {
			writeError(w, 404, "Restore job not found")
			return
		}
		writeJSON(w, 200, job)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "Method not allowed")
		return
	}
	var input struct {
		ID, SiteID, Kind, ConfirmDomain string
		Confirm                         bool
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	site := a.store.SiteByID(input.SiteID)
	if site == nil || !input.Confirm || input.ConfirmDomain != site.Domain {
		writeError(w, 400, "Confirm replacement of the selected website before restoring")
		return
	}
	if input.Kind != "history" && input.Kind != "website-zip" {
		writeError(w, 400, "Unsupported backup type")
		return
	}
	if site.Application != nil && site.Application.Enabled {
		writeError(w, 409, "Disable the application runtime before restoring")
		return
	}
	if input.Kind == "history" {
		found := false
		for _, b := range a.store.PublicState().Backups {
			if b.ID == input.ID && b.SiteID == site.ID && !backupInProgress(b.Status) {
				found = true
			}
		}
		if !found {
			writeError(w, 404, "Backup not found for this website")
			return
		}
	} else {
		if _, err := loadWebsiteZIPBackup(a.system.websiteBackupsRoot(*site), input.ID, site.ID); err != nil {
			writeError(w, 404, "Complete website backup not found")
			return
		}
	}
	id, err := randomToken(18)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	job := LiveMigrationJob{ID: safeSiteID(id), Source: "backup", Account: site.ID, Accounts: []LiveAccount{{ID: site.ID, Domain: site.Domain}}, Status: "running", Stage: "verify", Message: "Restore requested. Waiting for other server changes to finish.", CreatedAt: time.Now().UTC()}
	if err = a.saveLiveJob(&job); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	go func() {
		a.mutationMu.Lock()
		defer a.mutationMu.Unlock()
		fail := func(err error) {
			setLiveJobStage(&job, "restore", "restore-review", "Restore stopped: "+err.Error()+". Existing data and recovery files were retained.")
			_ = a.saveLiveJob(&job)
		}
		ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), directBackupRestoreContext{}, true), 2*time.Hour)
		defer cancel()
		setLiveJobStage(&job, "verify", "running", "Retrieving and inspecting the backup...")
		_ = a.saveLiveJob(&job)
		var preview MigrationPreview
		var err error
		if input.Kind == "history" {
			for _, b := range a.store.PublicState().Backups {
				if b.ID == input.ID {
					preview, err = a.prepareHistoryBackup(ctx, b)
					break
				}
			}
		} else {
			preview, err = a.stageWebsiteBackup(ctx, *site, input.ID)
		}
		if err != nil {
			fail(err)
			return
		}
		_, root, err := a.loadMigration(preview.ID)
		if err != nil {
			fail(err)
			return
		}
		// Each restore owns a fresh stage so concurrent requests never share mutable files.
		target := filepath.Join(a.system.dataDir, "migrations", job.ID)
		if err = os.Mkdir(target, 0700); err != nil {
			fail(err)
			return
		}
		if err = copyRestoreStage(ctx, root, target); err != nil {
			fail(err)
			return
		}
		preview.ID = job.ID
		// The automatic restore adapter handles one website root and the saved home tree.
		preview.Roots = []string{"public_html"}
		for _, config := range []struct{ name, app string }{{"wp-config.php", "wordpress"}, {"configuration.php", "whmcs"}} {
			content, e := os.ReadFile(filepath.Join(target, "files", "public_html", config.name))
			if e != nil {
				continue
			}
			db, e := migrationDBName(string(content), config.app)
			if e != nil {
				fail(e)
				return
			}
			if len(preview.Databases) == 1 && filepath.Base(preview.Databases[0]) == "database.sql" {
				old := filepath.Join(target, "files", filepath.FromSlash(preview.Databases[0]))
				name := db + ".sql"
				if e = os.Rename(old, filepath.Join(target, "files", name)); e != nil {
					fail(e)
					return
				}
				preview.Databases = []string{name}
			}
			break
		}
		data, _ := json.Marshal(preview)
		if err = writeAtomic(filepath.Join(target, "manifest.json"), data, 0600); err != nil {
			fail(err)
			return
		}
		job.Preview = &preview
		job.Status = "review"
		_ = a.saveLiveJob(&job)
		a.performAutoMigration(&job, true)
	}()
	writeJSON(w, http.StatusAccepted, job)
}
func copyRestoreStage(ctx context.Context, source, target string) error {
	return filepath.Walk(source, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(source, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dest := filepath.Join(target, rel)
		if info.IsDir() {
			return os.Mkdir(dest, 0700)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported backup stage file")
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		dst, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, src)
		closeErr := dst.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}
