package main

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (a *App) requestSite(r *http.Request) (*Site, error) {
	siteID := strings.TrimSpace(r.URL.Query().Get("siteId"))
	if siteID == "" {
		return nil, errors.New("website is required")
	}
	site := a.store.SiteByID(siteID)
	if site == nil {
		return nil, errors.New("website not found")
	}
	return site, nil
}

func rollbackUploadedFiles(system *System, site Site, files []FileEntry) error {
	failures := []string{}
	for _, created := range files {
		if err := system.DeleteManagedPath(site, created.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, created.Path+": "+err.Error())
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func (a *App) files(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		site, err := a.requestSite(r)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		listing, err := a.system.ListFiles(*site, r.URL.Query().Get("path"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, listing)
	case http.MethodDelete:
		var input struct {
			SiteID string `json:"siteId"`
			Path   string `json:"path"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		site := a.store.SiteByID(input.SiteID)
		if site == nil {
			writeError(w, http.StatusNotFound, "Website not found.")
			return
		}
		entry, err := a.system.MoveManagedPathToTrash(*site, input.Path)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		a.store.AddActivity("File moved to trash", site.Domain+": "+input.Path, "warning")
		writeJSON(w, http.StatusOK, entry)
	default:
		w.Header().Set("Allow", "GET, DELETE")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
	}
}

func (a *App) fileContent(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		site, err := a.requestSite(r)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		content, info, err := a.system.ReadManagedFile(*site, r.URL.Query().Get("path"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"path": r.URL.Query().Get("path"), "content": content, "mode": fmt.Sprintf("%04o", info.Mode().Perm())})
	case http.MethodPut:
		var input struct {
			SiteID  string `json:"siteId"`
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := decodeJSONLimit(r, &input, maxEditableFileSize+64*1024); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		site := a.store.SiteByID(input.SiteID)
		if site == nil {
			writeError(w, http.StatusNotFound, "Website not found.")
			return
		}
		if err := a.system.WriteManagedFile(*site, input.Path, input.Content); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		a.store.AddActivity("File saved", site.Domain+": "+input.Path, "success")
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
	}
}

func (a *App) uploadFiles(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadRequestSize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "Upload is too large or malformed.")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	site := a.store.SiteByID(r.FormValue("siteId"))
	if site == nil {
		writeError(w, http.StatusNotFound, "Website not found.")
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, "Choose at least one file to upload.")
		return
	}
	if len(files) > maxUploadFiles {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Upload at most %d files per request.", maxUploadFiles))
		return
	}
	cleanNames := make([]string, len(files))
	seenNames := make(map[string]bool, len(files))
	for _, header := range files {
		if header.Size > maxUploadSize {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%s exceeds the %d MiB per-file limit.", filepath.Base(header.Filename), maxUploadSize>>20))
			return
		}
	}
	for index, header := range files {
		name := filepath.Base(header.Filename)
		if err := validateFileName(name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if seenNames[name] {
			writeError(w, http.StatusBadRequest, "An upload request cannot contain duplicate file names.")
			return
		}
		seenNames[name] = true
		cleanNames[index] = name
		if len(files) > 1 {
			relative := filepath.ToSlash(filepath.Join(r.FormValue("path"), name))
			exists, err := a.system.ManagedPathExists(*site, relative)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if exists {
				writeError(w, http.StatusConflict, "Multi-file uploads do not overwrite existing files. Rename or remove "+name+" first.")
				return
			}
		}
	}
	stored := make([]FileEntry, 0, len(files))
	for index, header := range files {
		file, err := header.Open()
		if err != nil {
			if rollbackErr := rollbackUploadedFiles(a.system, *site, stored); rollbackErr != nil {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("Upload failed (%v), and uploaded-file rollback failed (%v).", err, rollbackErr))
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		var entry FileEntry
		var saveErr error
		if len(files) > 1 {
			entry, saveErr = a.system.SaveNewUpload(*site, r.FormValue("path"), cleanNames[index], file)
		} else {
			entry, saveErr = a.system.SaveUpload(*site, r.FormValue("path"), cleanNames[index], file)
		}
		_ = file.Close()
		if saveErr != nil {
			if rollbackErr := rollbackUploadedFiles(a.system, *site, stored); rollbackErr != nil {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("Upload failed (%v), and uploaded-file rollback failed (%v).", saveErr, rollbackErr))
				return
			}
			writeError(w, http.StatusBadRequest, saveErr.Error())
			return
		}
		stored = append(stored, entry)
	}
	a.store.AddActivity("Files uploaded", fmt.Sprintf("%s: %d file(s)", site.Domain, len(stored)), "success")
	writeJSON(w, http.StatusCreated, map[string]any{"files": stored})
}

func (a *App) createDirectory(w http.ResponseWriter, r *http.Request) {
	var input struct {
		SiteID string `json:"siteId"`
		Path   string `json:"path"`
		Name   string `json:"name"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	site := a.store.SiteByID(input.SiteID)
	if site == nil {
		writeError(w, http.StatusNotFound, "Website not found.")
		return
	}
	if err := a.system.CreateManagedDirectory(*site, input.Path, input.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.store.AddActivity("Folder created", site.Domain+": "+input.Name, "success")
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (a *App) renameFile(w http.ResponseWriter, r *http.Request) {
	var input struct {
		SiteID      string `json:"siteId"`
		Path        string `json:"path"`
		Destination string `json:"destination"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	site := a.store.SiteByID(input.SiteID)
	if site == nil {
		writeError(w, http.StatusNotFound, "Website not found.")
		return
	}
	if err := a.system.RenameManagedPath(*site, input.Path, input.Destination); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.store.AddActivity("File renamed", site.Domain+": "+input.Path+" → "+input.Destination, "success")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) changeFilePermissions(w http.ResponseWriter, r *http.Request) {
	var input struct {
		SiteID string `json:"siteId"`
		Path   string `json:"path"`
		Mode   string `json:"mode"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	site := a.store.SiteByID(input.SiteID)
	if site == nil {
		writeError(w, http.StatusNotFound, "Website not found.")
		return
	}
	if err := a.system.ChmodManagedPath(*site, input.Path, input.Mode); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) archiveFiles(w http.ResponseWriter, r *http.Request) {
	var input struct {
		SiteID      string   `json:"siteId"`
		Paths       []string `json:"paths"`
		Destination string   `json:"destination"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	site := a.store.SiteByID(input.SiteID)
	if site == nil {
		writeError(w, http.StatusNotFound, "Website not found.")
		return
	}
	entry, err := a.system.ArchiveManagedPaths(r.Context(), *site, input.Paths, input.Destination)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.store.AddActivity("ZIP archive created", site.Domain+": "+entry.Path, "success")
	writeJSON(w, http.StatusCreated, entry)
}

func (a *App) extractArchive(w http.ResponseWriter, r *http.Request) {
	var input struct {
		SiteID      string `json:"siteId"`
		Path        string `json:"path"`
		Destination string `json:"destination"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	site := a.store.SiteByID(input.SiteID)
	if site == nil {
		writeError(w, http.StatusNotFound, "Website not found.")
		return
	}
	if err := a.system.ExtractManagedArchive(r.Context(), *site, input.Path, input.Destination); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.store.AddActivity("ZIP archive extracted", site.Domain+": "+input.Path, "success")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) downloadFile(w http.ResponseWriter, r *http.Request) {
	site, err := a.requestSite(r)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	file, err := secureOpenManaged(site.Root, r.URL.Query().Get("path"), os.O_RDONLY, 0)
	if err != nil {
		writeError(w, http.StatusNotFound, "File not found.")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusBadRequest, "Only regular files can be downloaded.")
		return
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": info.Name()})
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (a *App) createManagedDatabase(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name     string `json:"name"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Username = strings.TrimSpace(input.Username)
	if input.Password == "" {
		var err error
		input.Password, err = randomToken(24)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not generate a database password.")
			return
		}
	}
	if err := validateDatabaseObjects(input.Name, input.Username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateDatabasePassword(input.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.system.CreateManagedDatabase(r.Context(), input.Name, input.Username, input.Password); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	id, _ := randomToken(8)
	database := ManagedDatabase{ID: id, Name: input.Name, Username: input.Username, CreatedAt: time.Now().UTC()}
	if err := a.store.AddDatabase(database); err != nil {
		if rollbackErr := a.system.DropManagedDatabase(context.Background(), database); rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, "Panel state could not be saved, and database rollback failed: "+rollbackErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "Database was rolled back because panel state could not be saved.")
		return
	}
	a.store.AddActivity("Database created", input.Name+" with local-only access", "success")
	writeJSON(w, http.StatusCreated, map[string]any{"database": database, "password": input.Password, "host": "localhost"})
}

func (a *App) deleteManagedDatabase(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/databases/")
	database := a.store.DatabaseByID(id)
	if database == nil {
		writeError(w, http.StatusNotFound, "Database not found.")
		return
	}
	if err := a.store.RemoveDatabase(id); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not save database removal state.")
		return
	}
	if database.SiteID != "" {
		if err := a.store.UpdateSite(database.SiteID, func(site *Site) { site.Database, site.DBUser = "", "" }); err != nil {
			if rollbackErr := a.store.AddDatabase(*database); rollbackErr != nil {
				writeError(w, http.StatusInternalServerError, "Website detachment failed, and database-state rollback also failed: "+rollbackErr.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "Could not detach the database from its website.")
			return
		}
	}
	if err := a.system.DropManagedDatabase(r.Context(), *database); err != nil {
		rollbackErrors := []string{}
		if rollbackErr := a.store.AddDatabase(*database); rollbackErr != nil {
			rollbackErrors = append(rollbackErrors, "database state: "+rollbackErr.Error())
		}
		if database.SiteID != "" {
			if rollbackErr := a.store.UpdateSite(database.SiteID, func(site *Site) { site.Database, site.DBUser = database.Name, database.Username }); rollbackErr != nil {
				rollbackErrors = append(rollbackErrors, "website state: "+rollbackErr.Error())
			}
		}
		if len(rollbackErrors) > 0 {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("Database removal failed (%v), and state rollback was incomplete: %s", err, strings.Join(rollbackErrors, "; ")))
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.store.AddActivity("Database deleted", database.Name+" and its local user were removed", "warning")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) resetManagedDatabasePassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID       string `json:"id"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	database := a.store.DatabaseByID(input.ID)
	if database == nil {
		writeError(w, http.StatusNotFound, "Database not found.")
		return
	}
	if input.Password == "" {
		var err error
		input.Password, err = randomToken(24)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not generate a database password.")
			return
		}
	}
	if err := validateDatabasePassword(input.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	desiredLegacy := database.LegacyAuth
	if site := a.store.SiteByID(database.SiteID); site != nil {
		desiredLegacy = legacyPHPDatabaseAuth(site.PHPVersion)
	}
	updatedDatabase := *database
	updatedDatabase.LegacyAuth = desiredLegacy
	if desiredLegacy != database.LegacyAuth {
		if err := a.store.UpdateDatabase(database.ID, func(value *ManagedDatabase) { value.LegacyAuth = desiredLegacy }); err != nil {
			writeError(w, http.StatusInternalServerError, "Could not save database authentication state.")
			return
		}
	}
	if err := a.system.ResetDatabasePassword(r.Context(), updatedDatabase, input.Password); err != nil {
		if desiredLegacy != database.LegacyAuth {
			_ = a.store.UpdateDatabase(database.ID, func(value *ManagedDatabase) { value.LegacyAuth = database.LegacyAuth })
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.store.AddActivity("Database password reset", database.Username+" credentials changed", "success")
	writeJSON(w, http.StatusOK, map[string]string{"password": input.Password})
}

func (a *App) createBackupDestination(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name          string `json:"name"`
		Protocol      string `json:"protocol"`
		Host          string `json:"host"`
		Port          int    `json:"port"`
		Username      string `json:"username"`
		Password      string `json:"password"`
		RemotePath    string `json:"remotePath"`
		HostKeySHA256 string `json:"hostKeySha256"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Password) < 1 || len(input.Password) > 256 || strings.ContainsAny(input.Password, "\r\n") {
		writeError(w, http.StatusBadRequest, "Backup password must contain 1 to 256 characters.")
		return
	}
	ciphertext, err := a.system.EncryptSecret(input.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not protect the backup credential.")
		return
	}
	id, _ := randomToken(8)
	destination := BackupDestination{
		ID: id, Name: strings.TrimSpace(input.Name), Protocol: strings.ToLower(strings.TrimSpace(input.Protocol)),
		Host: strings.TrimSpace(input.Host), Port: input.Port, Username: strings.TrimSpace(input.Username),
		RemotePath: strings.TrimSpace(input.RemotePath), HostKeySHA256: strings.TrimSpace(input.HostKeySHA256),
		PasswordCipher: ciphertext, HasPassword: true, CreatedAt: time.Now().UTC(),
	}
	if err := validateBackupDestination(destination, true); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.AddBackupDestination(destination); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	destination.PasswordCipher = ""
	a.store.AddActivity("Backup destination added", destination.Name+" over "+strings.ToUpper(destination.Protocol), "success")
	writeJSON(w, http.StatusCreated, destination)
}

func (a *App) deleteBackupDestination(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/backup-destinations/")
	destination := a.store.BackupDestinationByID(id)
	if destination == nil {
		writeError(w, http.StatusNotFound, "Backup destination not found.")
		return
	}
	if err := a.store.RemoveBackupDestination(id); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not save backup destination removal.")
		return
	}
	a.store.AddActivity("Backup destination removed", destination.Name, "warning")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) createFirewallRule(w http.ResponseWriter, r *http.Request) {
	var input FirewallRule
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, _ := randomToken(8)
	rule := normalizeFirewallRule(input)
	rule.ID, rule.CreatedAt = id, time.Now().UTC()
	if err := validateFirewallRule(rule); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.store.HasFirewallRule(rule) {
		writeError(w, http.StatusConflict, "That firewall rule is already managed by LiteCP.")
		return
	}
	if err := a.system.ApplyFirewallRule(r.Context(), rule, false); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := a.store.AddFirewallRule(rule); err != nil {
		rollbackErr := a.system.ApplyFirewallRule(context.Background(), rule, true)
		if rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Panel state could not be saved, and the new firewall rule could not be removed (%v). Check the host firewall immediately.", rollbackErr))
			return
		}
		writeError(w, http.StatusInternalServerError, "Firewall rule was rolled back because panel state could not be saved.")
		return
	}
	a.store.AddActivity("Firewall rule added", fmt.Sprintf("%s %s %d/%s", rule.Direction, rule.Action, rule.Port, rule.Protocol), "success")
	writeJSON(w, http.StatusCreated, rule)
}

func (a *App) deleteFirewallRule(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/firewall/")
	rule := a.store.FirewallRuleByID(id)
	if rule == nil {
		writeError(w, http.StatusNotFound, "Firewall rule not found.")
		return
	}
	if err := a.system.ApplyFirewallRule(r.Context(), *rule, true); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := a.store.RemoveFirewallRule(id); err != nil {
		rollbackErr := a.system.ApplyFirewallRule(context.Background(), *rule, false)
		if rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Panel state could not be saved, and the removed firewall rule could not be restored (%v). Check the host firewall immediately.", rollbackErr))
			return
		}
		writeError(w, http.StatusInternalServerError, "Firewall removal was rolled back because panel state could not be saved.")
		return
	}
	a.store.AddActivity("Firewall rule removed", fmt.Sprintf("%d/%s", rule.Port, rule.Protocol), "warning")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) createMailDomain(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Domain string `json:"domain"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	domain := strings.ToLower(strings.TrimSpace(input.Domain))
	if err := validateMailDomain(domain); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.store.HasMailDomain(domain) {
		writeError(w, http.StatusConflict, "That mail domain already exists.")
		return
	}
	id, _ := randomToken(8)
	item := MailDomain{ID: id, Domain: domain, DKIMSelector: "default", CreatedAt: time.Now().UTC()}
	domains, mailboxes, aliases := a.store.MailConfig()
	domains = append(domains, item)
	if err := a.system.SyncMailConfig(r.Context(), domains, mailboxes, aliases); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := a.store.AddMailDomain(item); err != nil {
		if rollbackErr := a.system.SyncMailConfig(context.Background(), domains[:len(domains)-1], mailboxes, aliases); rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, "Panel state could not be saved, and mail rollback failed: "+rollbackErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "Mail configuration was rolled back because panel state could not be saved.")
		return
	}
	a.store.AddActivity("Mail domain added", domain+" is ready for mailboxes", "success")
	writeJSON(w, http.StatusCreated, item)
}

func (a *App) deleteMailDomain(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/mail/domains/")
	item := a.store.MailDomainByID(id)
	if item == nil {
		writeError(w, http.StatusNotFound, "Mail domain not found.")
		return
	}
	domains, mailboxes, aliases := a.store.MailConfig()
	domains = filterMailDomains(domains, func(value MailDomain) bool { return value.ID != id })
	mailboxes = filterMailboxes(mailboxes, func(value Mailbox) bool { return value.Domain != item.Domain })
	aliases = filterMailAliases(aliases, func(value MailAlias) bool { return !strings.HasSuffix(value.Source, "@"+item.Domain) })
	if err := a.system.SyncMailConfig(r.Context(), domains, mailboxes, aliases); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := a.store.RemoveMailDomain(id); err != nil {
		originalDomains, originalMailboxes, originalAliases := a.store.MailConfig()
		if rollbackErr := a.system.SyncMailConfig(context.Background(), originalDomains, originalMailboxes, originalAliases); rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, "Panel state could not be saved, and mail rollback failed: "+rollbackErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "Mail removal was rolled back because panel state could not be saved.")
		return
	}
	a.store.AddActivity("Mail domain removed", item.Domain+" routing disabled; Maildir data kept", "warning")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) createMailbox(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Domain    string `json:"domain"`
		LocalPart string `json:"localPart"`
		Password  string `json:"password"`
		QuotaMB   int    `json:"quotaMb"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	domain := strings.ToLower(strings.TrimSpace(input.Domain))
	address := strings.ToLower(strings.TrimSpace(input.LocalPart)) + "@" + domain
	if !a.store.HasMailDomain(domain) {
		writeError(w, http.StatusBadRequest, "Add the mail domain before creating mailboxes.")
		return
	}
	if err := validateMailboxAddress(address, domain); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.QuotaMB < 100 || input.QuotaMB > 102400 {
		writeError(w, http.StatusBadRequest, "Mailbox quota must be between 100 MB and 102400 MB.")
		return
	}
	domains, mailboxes, aliases := a.store.MailConfig()
	for _, mailbox := range mailboxes {
		if mailbox.Address == address {
			writeError(w, http.StatusConflict, "That mailbox already exists.")
			return
		}
	}
	for _, alias := range aliases {
		if alias.Source == address {
			writeError(w, http.StatusConflict, "That address is already used by a forwarding alias.")
			return
		}
	}
	hash, err := a.system.HashMailboxPassword(r.Context(), input.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, _ := randomToken(8)
	mailbox := Mailbox{ID: id, Domain: domain, Address: address, QuotaMB: input.QuotaMB, PasswordHash: hash, CreatedAt: time.Now().UTC()}
	prospective := append(mailboxes, mailbox)
	if err := a.system.SyncMailConfig(r.Context(), domains, prospective, aliases); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := a.store.AddMailbox(mailbox); err != nil {
		if rollbackErr := a.system.SyncMailConfig(context.Background(), domains, mailboxes, aliases); rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, "Panel state could not be saved, and mailbox rollback failed: "+rollbackErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "Mailbox creation was rolled back because panel state could not be saved.")
		return
	}
	mailbox.PasswordHash = ""
	a.store.AddActivity("Mailbox created", address, "success")
	writeJSON(w, http.StatusCreated, mailbox)
}

func (a *App) deleteMailbox(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/mail/mailboxes/")
	mailbox := a.store.MailboxByID(id)
	if mailbox == nil {
		writeError(w, http.StatusNotFound, "Mailbox not found.")
		return
	}
	domains, mailboxes, aliases := a.store.MailConfig()
	mailboxes = filterMailboxes(mailboxes, func(value Mailbox) bool { return value.ID != id })
	if err := a.system.SyncMailConfig(r.Context(), domains, mailboxes, aliases); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := a.store.RemoveMailbox(id); err != nil {
		originalDomains, originalMailboxes, originalAliases := a.store.MailConfig()
		if rollbackErr := a.system.SyncMailConfig(context.Background(), originalDomains, originalMailboxes, originalAliases); rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, "Panel state could not be saved, and mailbox rollback failed: "+rollbackErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "Mailbox removal was rolled back because panel state could not be saved.")
		return
	}
	a.store.AddActivity("Mailbox removed", mailbox.Address+" login disabled; Maildir data kept", "warning")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) resetMailboxPassword(w http.ResponseWriter, r *http.Request) {
	var input struct{ ID, Password string }
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	mailbox := a.store.MailboxByID(input.ID)
	if mailbox == nil {
		writeError(w, http.StatusNotFound, "Mailbox not found.")
		return
	}
	hash, err := a.system.HashMailboxPassword(r.Context(), input.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	domains, mailboxes, aliases := a.store.MailConfig()
	for i := range mailboxes {
		if mailboxes[i].ID == input.ID {
			mailboxes[i].PasswordHash = hash
		}
	}
	if err := a.system.SyncMailConfig(r.Context(), domains, mailboxes, aliases); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := a.store.UpdateMailbox(input.ID, func(value *Mailbox) { value.PasswordHash = hash }); err != nil {
		originalDomains, originalMailboxes, originalAliases := a.store.MailConfig()
		if rollbackErr := a.system.SyncMailConfig(context.Background(), originalDomains, originalMailboxes, originalAliases); rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, "Panel state could not be saved, and password rollback failed: "+rollbackErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "Password change was rolled back because panel state could not be saved.")
		return
	}
	a.store.AddActivity("Mailbox password reset", mailbox.Address, "success")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) createMailAlias(w http.ResponseWriter, r *http.Request) {
	var input struct{ Source, Destination string }
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Source = strings.ToLower(strings.TrimSpace(input.Source))
	input.Destination = strings.TrimSpace(input.Destination)
	if err := validateAliasAddress(input.Source, input.Destination); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	parts := strings.Split(input.Source, "@")
	if !a.store.HasMailDomain(parts[1]) {
		writeError(w, http.StatusBadRequest, "The alias source domain is not hosted by LiteCP.")
		return
	}
	domains, mailboxes, aliases := a.store.MailConfig()
	for _, alias := range aliases {
		if alias.Source == input.Source {
			writeError(w, http.StatusConflict, "That alias source already exists.")
			return
		}
	}
	for _, mailbox := range mailboxes {
		if mailbox.Address == input.Source {
			writeError(w, http.StatusConflict, "That source address is already a mailbox.")
			return
		}
	}
	id, _ := randomToken(8)
	alias := MailAlias{ID: id, Source: input.Source, Destination: input.Destination, CreatedAt: time.Now().UTC()}
	if err := a.system.SyncMailConfig(r.Context(), domains, mailboxes, append(aliases, alias)); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := a.store.AddMailAlias(alias); err != nil {
		if rollbackErr := a.system.SyncMailConfig(context.Background(), domains, mailboxes, aliases); rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, "Panel state could not be saved, and alias rollback failed: "+rollbackErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "Alias creation was rolled back because panel state could not be saved.")
		return
	}
	a.store.AddActivity("Mail alias created", alias.Source+" → "+alias.Destination, "success")
	writeJSON(w, http.StatusCreated, alias)
}

func (a *App) deleteMailAlias(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/mail/aliases/")
	alias := a.store.MailAliasByID(id)
	if alias == nil {
		writeError(w, http.StatusNotFound, "Mail alias not found.")
		return
	}
	domains, mailboxes, aliases := a.store.MailConfig()
	aliases = filterMailAliases(aliases, func(value MailAlias) bool { return value.ID != id })
	if err := a.system.SyncMailConfig(r.Context(), domains, mailboxes, aliases); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := a.store.RemoveMailAlias(id); err != nil {
		originalDomains, originalMailboxes, originalAliases := a.store.MailConfig()
		if rollbackErr := a.system.SyncMailConfig(context.Background(), originalDomains, originalMailboxes, originalAliases); rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, "Panel state could not be saved, and alias rollback failed: "+rollbackErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "Alias removal was rolled back because panel state could not be saved.")
		return
	}
	a.store.AddActivity("Mail alias removed", alias.Source, "warning")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func filterMailDomains(values []MailDomain, keep func(MailDomain) bool) []MailDomain {
	result := make([]MailDomain, 0, len(values))
	for _, value := range values {
		if keep(value) {
			result = append(result, value)
		}
	}
	return result
}

func filterMailboxes(values []Mailbox, keep func(Mailbox) bool) []Mailbox {
	result := make([]Mailbox, 0, len(values))
	for _, value := range values {
		if keep(value) {
			result = append(result, value)
		}
	}
	return result
}

func filterMailAliases(values []MailAlias, keep func(MailAlias) bool) []MailAlias {
	result := make([]MailAlias, 0, len(values))
	for _, value := range values {
		if keep(value) {
			result = append(result, value)
		}
	}
	return result
}

func decodeJSONLimit(r *http.Request, destination any, limit int64) error {
	return decodeJSONWithLimit(r, destination, limit)
}

func parsePort(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 || parsed > 65535 {
		return fallback
	}
	return parsed
}
