package main

import (
	"context"
	"io/fs"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (a *App) siteSizes(w http.ResponseWriter, r *http.Request) {
	sizes := make(map[string]int64)
	unavailable := make([]string, 0)
	for _, site := range a.store.Sites() {
		root := managedSiteRoot(site)
		var total int64
		err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := r.Context().Err(); err != nil {
				return err
			}
			if entry.Type()&fs.ModeSymlink != 0 || !entry.Type().IsRegular() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			total += info.Size()
			return nil
		})
		if err != nil {
			unavailable = append(unavailable, site.ID)
			continue
		}
		sizes[site.ID] = total
	}
	databaseSizes := a.websiteDatabaseSizes(r.Context())
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"sizes": sizes, "unavailable": unavailable, "databaseSizes": databaseSizes})
}

// Sum each owned schema once, including the legacy primary database field.
func sumWebsiteDatabaseSizes(site Site, databases []ManagedDatabase, schemas map[string]int64) (int64, bool) {
	names := map[string]bool{}
	if site.Database != "" {
		names[site.Database] = true
	}
	for _, database := range databases {
		if database.SiteID == site.ID {
			names[database.Name] = true
		}
	}
	var total int64
	for name := range names {
		size, ok := schemas[name]
		if !ok {
			return 0, false
		}
		total += size
	}
	return total, true
}

func (a *App) websiteDatabaseSizes(ctx context.Context) map[string]int64 {
	schemas := map[string]int64{}
	if !a.system.demo {
		// LEFT JOIN includes empty databases; absent schemas remain unavailable.
		query := "SELECT s.SCHEMA_NAME, COALESCE(SUM(t.DATA_LENGTH + t.INDEX_LENGTH),0) FROM information_schema.SCHEMATA s LEFT JOIN information_schema.TABLES t ON t.TABLE_SCHEMA=s.SCHEMA_NAME GROUP BY s.SCHEMA_NAME;"
		output, err := runCommandInput(ctx, 15*time.Second, query, firstAvailableCommand("mysql", "mariadb"), "--protocol=socket", "--user=root", "--batch", "--skip-column-names")
		if err == nil {
			for _, line := range strings.Split(output, "\n") {
				fields := strings.Split(strings.TrimSpace(line), "\t")
				if len(fields) != 2 {
					continue
				}
				size, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil && size >= 0 {
					schemas[fields[0]] = size
				}
			}
		}
	}
	result := map[string]int64{}
	databases := a.store.PublicState().Databases
	for _, site := range a.store.Sites() {
		if size, ok := sumWebsiteDatabaseSizes(site, databases, schemas); ok {
			result[site.ID] = size
		}
	}
	return result
}
