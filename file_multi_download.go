package main

import (
	"archive/zip"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
)

// Stream a selection without creating archives in the customer's website.
func (a *App) downloadSelectedFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "Use GET or POST for file downloads")
		return
	}
	// ZIP selections are confined to managed websites, never the server explorer.
	if r.URL.Query().Get("siteId") == "server-explorer" {
		writeError(w, http.StatusForbidden, "Combined downloads require a managed website")
		return
	}
	site, err := a.requestSite(r)
	if err != nil {
		writeError(w, 404, "Website not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if r.ParseForm() != nil {
		writeError(w, 400, "Invalid file selection")
		return
	}
	paths := r.PostForm["paths"]
	if r.Method == http.MethodGet {
		if len(r.URL.RawQuery) > 8192 {
			writeError(w, http.StatusRequestURITooLong, "Select fewer files or use Compress")
			return
		}
		paths = r.URL.Query()["paths"]
	}
	if len(paths) < 2 || len(paths) > 250 {
		writeError(w, 400, "Select between 2 and 250 files for a ZIP download")
		return
	}
	files := []*os.File{}
	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
	}()
	names := []string{}
	seen := map[string]bool{}
	for _, relative := range paths {
		clean := path.Clean(relative)
		if clean == "." || path.IsAbs(relative) || clean == ".." || strings.HasPrefix(clean, "../") || strings.ContainsAny(relative, "\\\x00") || seen[clean] {
			writeError(w, 400, "Invalid or duplicate selection")
			return
		}
		f, e := secureOpenManaged(managedSiteRoot(*site), clean, os.O_RDONLY, 0)
		if e != nil {
			writeError(w, 404, "A selected file is no longer available")
			return
		}
		files = append(files, f)
		info, e := f.Stat()
		if e != nil || !info.Mode().IsRegular() {
			writeError(w, 400, "Select regular files only; use Compress for folders")
			return
		}
		names = append(names, clean)
		seen[clean] = true
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="selected-files.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	archive := zip.NewWriter(w)
	for i, f := range files {
		if r.Context().Err() != nil {
			return
		}
		info, e := f.Stat()
		if e != nil {
			return
		}
		header, e := zip.FileInfoHeader(info)
		if e != nil {
			return
		}
		header.Name = names[i]
		header.Method = zip.Store
		member, e := archive.CreateHeader(header)
		if e != nil {
			return
		}
		if _, e = io.Copy(member, f); e != nil {
			return
		}
	}
	_ = archive.Close()
}
