package main

import (
	"io/fs"
	"net/http"
	"path/filepath"
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
	writeJSON(w, http.StatusOK, map[string]any{"sizes": sizes, "unavailable": unavailable})
}
