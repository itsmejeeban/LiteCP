package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var version = "0.3.0"

//go:embed web/*
var webAssets embed.FS

type session struct {
	Expires time.Time
}

type loginAttempt struct {
	Count int
	Since time.Time
}

type App struct {
	store    *Store
	system   *System
	sessions map[string]session
	attempts map[string]loginAttempt
	mu       sync.Mutex
}

func main() {
	dataDir := envOr("LITECP_DATA_DIR", "/var/lib/litecp")
	bind := envOr("LITECP_BIND", "127.0.0.1:9090")

	if len(os.Args) > 1 && os.Args[1] == "init" {
		initFlags := flag.NewFlagSet("init", flag.ExitOnError)
		username := initFlags.String("username", "admin", "administrator username")
		_ = initFlags.Parse(os.Args[2:])
		password := os.Getenv("LITECP_ADMIN_PASSWORD")
		if len(password) < 12 {
			log.Fatal("LITECP_ADMIN_PASSWORD must contain at least 12 characters")
		}
		store, err := OpenStore(dataDir)
		if err != nil {
			log.Fatal(err)
		}
		if err := store.Initialize(*username, password); err != nil {
			log.Fatal(err)
		}
		fmt.Println("LiteCP administrator created.")
		return
	}

	store, err := OpenStore(dataDir)
	if err != nil {
		log.Fatal(err)
	}
	if !store.Initialized() {
		log.Fatal("LiteCP is not initialized; run `LITECP_ADMIN_PASSWORD=... litecp init`")
	}

	app := &App{
		store:    store,
		system:   NewSystem(dataDir),
		sessions: make(map[string]session),
		attempts: make(map[string]loginAttempt),
	}

	server := &http.Server{
		Addr:              bind,
		Handler:           app.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Minute,
		WriteTimeout:      20 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	go func() {
		log.Printf("LiteCP %s listening on %s", version, bind)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", only(http.MethodPost, a.login))
	mux.HandleFunc("/api/logout", only(http.MethodPost, a.withAuth(a.logout)))
	mux.HandleFunc("/api/state", only(http.MethodGet, a.withAuth(a.state)))
	mux.HandleFunc("/api/services/action", only(http.MethodPost, a.withAuth(a.serviceAction)))
	mux.HandleFunc("/api/sites", only(http.MethodPost, a.withAuth(a.createSite)))
	mux.HandleFunc("/api/sites/ssl", only(http.MethodPost, a.withAuth(a.enableSSL)))
	mux.HandleFunc("/api/sites/cron", only(http.MethodPost, a.withAuth(a.toggleCron)))
	mux.HandleFunc("/api/sites/php", only(http.MethodPost, a.withAuth(a.changeSitePHP)))
	mux.HandleFunc("/api/sites/", only(http.MethodDelete, a.withAuth(a.deleteSite)))
	mux.HandleFunc("/api/backups", only(http.MethodPost, a.withAuth(a.createBackup)))
	mux.HandleFunc("/api/backup-destinations", only(http.MethodPost, a.withAuth(a.createBackupDestination)))
	mux.HandleFunc("/api/backup-destinations/", only(http.MethodDelete, a.withAuth(a.deleteBackupDestination)))
	mux.HandleFunc("/api/files", a.withAuth(a.files))
	mux.HandleFunc("/api/files/content", a.withAuth(a.fileContent))
	mux.HandleFunc("/api/files/upload", only(http.MethodPost, a.withAuth(a.uploadFiles)))
	mux.HandleFunc("/api/files/directory", only(http.MethodPost, a.withAuth(a.createDirectory)))
	mux.HandleFunc("/api/files/rename", only(http.MethodPost, a.withAuth(a.renameFile)))
	mux.HandleFunc("/api/files/permissions", only(http.MethodPut, a.withAuth(a.changeFilePermissions)))
	mux.HandleFunc("/api/files/archive", only(http.MethodPost, a.withAuth(a.archiveFiles)))
	mux.HandleFunc("/api/files/extract", only(http.MethodPost, a.withAuth(a.extractArchive)))
	mux.HandleFunc("/api/files/download", only(http.MethodGet, a.withAuth(a.downloadFile)))
	mux.HandleFunc("/api/databases", only(http.MethodPost, a.withAuth(a.createManagedDatabase)))
	mux.HandleFunc("/api/databases/password", only(http.MethodPut, a.withAuth(a.resetManagedDatabasePassword)))
	mux.HandleFunc("/api/databases/", only(http.MethodDelete, a.withAuth(a.deleteManagedDatabase)))
	mux.HandleFunc("/api/firewall", only(http.MethodPost, a.withAuth(a.createFirewallRule)))
	mux.HandleFunc("/api/firewall/", only(http.MethodDelete, a.withAuth(a.deleteFirewallRule)))
	mux.HandleFunc("/api/mail/domains", only(http.MethodPost, a.withAuth(a.createMailDomain)))
	mux.HandleFunc("/api/mail/domains/", only(http.MethodDelete, a.withAuth(a.deleteMailDomain)))
	mux.HandleFunc("/api/mail/mailboxes", only(http.MethodPost, a.withAuth(a.createMailbox)))
	mux.HandleFunc("/api/mail/mailboxes/password", only(http.MethodPut, a.withAuth(a.resetMailboxPassword)))
	mux.HandleFunc("/api/mail/mailboxes/", only(http.MethodDelete, a.withAuth(a.deleteMailbox)))
	mux.HandleFunc("/api/mail/aliases", only(http.MethodPost, a.withAuth(a.createMailAlias)))
	mux.HandleFunc("/api/mail/aliases/", only(http.MethodDelete, a.withAuth(a.deleteMailAlias)))
	mux.HandleFunc("/api/logs", only(http.MethodGet, a.withAuth(a.logs)))
	mux.HandleFunc("/api/settings", only(http.MethodPut, a.withAuth(a.updateSettings)))
	mux.HandleFunc("/api/password", only(http.MethodPut, a.withAuth(a.changePassword)))
	mux.HandleFunc("/api/version", only(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": version})
	}))

	webFS, err := fs.Sub(webAssets, "web")
	if err != nil {
		panic(err)
	}
	static := http.FileServer(http.FS(webFS))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-store")
		}
		static.ServeHTTP(w, r)
	}))

	return a.securityHeaders(mux)
}

func (a *App) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func only(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
			return
		}
		next(w, r)
	}
}

func (a *App) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("litecp_session")
		if err != nil || !a.validSession(cookie.Value) {
			writeError(w, http.StatusUnauthorized, "Your session has expired. Please sign in again.")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !validOrigin(r) {
			writeError(w, http.StatusForbidden, "Request origin was rejected.")
			return
		}
		next(w, r)
	}
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if !validOrigin(r) {
		writeError(w, http.StatusForbidden, "Request origin was rejected.")
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	if a.rateLimited(ip) {
		writeError(w, http.StatusTooManyRequests, "Too many sign-in attempts. Try again in 15 minutes.")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !a.store.CheckPassword(input.Username, input.Password) {
		a.noteLoginFailure(ip)
		time.Sleep(350 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "The username or password is incorrect.")
		return
	}
	a.clearLoginFailures(ip)
	token, err := randomToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not create a session.")
		return
	}
	expires := time.Now().Add(12 * time.Hour)
	a.mu.Lock()
	a.sessions[token] = session{Expires: expires}
	a.mu.Unlock()
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name: "litecp_session", Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode,
	})
	a.store.AddActivity("Signed in", "Administrator session started", "success")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("litecp_session"); err == nil {
		a.mu.Lock()
		delete(a.sessions, cookie.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "litecp_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) validSession(token string) bool {
	if token == "" {
		return false
	}
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	current, ok := a.sessions[token]
	if !ok || now.After(current.Expires) {
		delete(a.sessions, token)
		return false
	}
	return true
}

func (a *App) rateLimited(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	attempt := a.attempts[ip]
	if time.Since(attempt.Since) > 15*time.Minute {
		delete(a.attempts, ip)
		return false
	}
	return attempt.Count >= 5
}

func (a *App) noteLoginFailure(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	attempt := a.attempts[ip]
	if attempt.Since.IsZero() || time.Since(attempt.Since) > 15*time.Minute {
		attempt = loginAttempt{Since: time.Now()}
	}
	attempt.Count++
	a.attempts[ip] = attempt
}

func (a *App) clearLoginFailures(ip string) {
	a.mu.Lock()
	delete(a.attempts, ip)
	a.mu.Unlock()
}

func (a *App) state(w http.ResponseWriter, r *http.Request) {
	state := a.store.PublicState()
	state.System = a.system.Snapshot(r.Context())
	state.Version = version
	writeJSON(w, http.StatusOK, state)
}

func (a *App) serviceAction(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Service string `json:"service"`
		Action  string `json:"action"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.system.ServiceAction(r.Context(), input.Service, input.Action); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.store.AddActivity("Service "+input.Action, input.Service+" completed successfully", "success")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) createSite(w http.ResponseWriter, r *http.Request) {
	var input CreateSiteInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	appType := strings.ToLower(strings.TrimSpace(input.AppType))
	if appType == "" {
		appType = "whmcs"
	}
	phpVersion := strings.TrimSpace(input.PHPVersion)
	if phpVersion == "" {
		phpVersion = defaultPHPVersion(appType)
	}
	if err := validateSiteOptions(appType, phpVersion); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.AppType, input.PHPVersion = appType, phpVersion
	if a.store.SiteByDomain(input.Domain) != nil {
		writeError(w, http.StatusConflict, "That domain is already managed by LiteCP.")
		return
	}
	site, credentials, err := a.system.CreateSite(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.store.AddSite(site)
	if credentials != nil {
		databaseID, _ := randomToken(8)
		_ = a.store.AddDatabase(ManagedDatabase{
			ID: databaseID, Name: credentials.Name, Username: credentials.Username,
			SiteID: site.ID, Domain: site.Domain, CreatedAt: site.CreatedAt,
		})
	}
	warning := ""
	if input.EnableCron {
		if err := a.system.SetCron(site, true); err != nil {
			warning = "Website created, but the application cron could not be enabled: " + err.Error()
		} else {
			site.Cron = true
			a.store.UpdateSite(site.ID, func(stored *Site) { stored.Cron = true })
		}
	}
	a.store.AddActivity("Website added", site.Domain+" is ready for "+applicationName(site.AppType), "success")
	writeJSON(w, http.StatusCreated, map[string]any{"site": site, "database": credentials, "warning": warning})
}

func (a *App) enableSSL(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	site := a.store.SiteByID(input.ID)
	if site == nil {
		writeError(w, http.StatusNotFound, "Website not found.")
		return
	}
	if err := a.system.EnableSSL(r.Context(), *site, input.Email); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.store.UpdateSite(input.ID, func(site *Site) { site.SSL = true })
	a.store.AddActivity("SSL enabled", site.Domain+" now redirects to HTTPS", "success")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) toggleCron(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	site := a.store.SiteByID(input.ID)
	if site == nil {
		writeError(w, http.StatusNotFound, "Website not found.")
		return
	}
	if err := a.system.SetCron(*site, input.Enabled); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.store.UpdateSite(input.ID, func(site *Site) { site.Cron = input.Enabled })
	label := "disabled"
	if input.Enabled {
		label = "enabled"
	}
	a.store.AddActivity(applicationName(site.AppType)+" cron "+label, site.Domain+" automation schedule updated", "success")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) changeSitePHP(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	site := a.store.SiteByID(input.ID)
	if site == nil {
		writeError(w, http.StatusNotFound, "Website not found.")
		return
	}
	if err := validateSiteOptions(site.AppType, strings.TrimSpace(input.Version)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.system.SetPHPVersion(r.Context(), *site, input.Version); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.store.UpdateSite(input.ID, func(site *Site) { site.PHPVersion = strings.TrimSpace(input.Version) })
	a.store.AddActivity("PHP version changed", site.Domain+" now uses PHP "+strings.TrimSpace(input.Version), "success")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) deleteSite(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/sites/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "Website not found.")
		return
	}
	site := a.store.SiteByID(id)
	if site == nil {
		writeError(w, http.StatusNotFound, "Website not found.")
		return
	}
	if err := a.system.RemoveSiteConfig(r.Context(), *site); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.store.RemoveSite(id)
	a.store.AddActivity("Website detached", site.Domain+" configuration removed; files and database were kept", "warning")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) createBackup(w http.ResponseWriter, r *http.Request) {
	var input struct {
		SiteID        string `json:"siteId"`
		DestinationID string `json:"destinationId"`
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
	var destination *BackupDestination
	if input.DestinationID != "" {
		destination = a.store.BackupDestinationByID(input.DestinationID)
		if destination == nil {
			writeError(w, http.StatusNotFound, "Backup destination not found.")
			return
		}
	}
	backup, err := a.system.CreateBackup(r.Context(), *site)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if destination != nil {
		remotePath, err := a.system.UploadBackup(r.Context(), *destination, backup.Path)
		if err != nil {
			backup.DestinationID = destination.ID
			backup.DestinationName = destination.Name
			backup.Status = "remote failed"
			a.store.AddBackup(backup)
			a.store.AddActivity("Remote backup failed", site.Domain+" was saved locally but could not upload to "+destination.Name, "warning")
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		backup.DestinationID = destination.ID
		backup.DestinationName = destination.Name
		backup.RemotePath = remotePath
	}
	a.store.AddBackup(backup)
	a.store.AddActivity("Backup completed", site.Domain+" files and database saved", "success")
	writeJSON(w, http.StatusCreated, backup)
}

func (a *App) logs(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("target")
	lines, err := a.system.Logs(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"target": name, "lines": lines})
}

func (a *App) updateSettings(w http.ResponseWriter, r *http.Request) {
	var settings Settings
	if err := decodeJSON(r, &settings); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	settings.Email = strings.TrimSpace(settings.Email)
	settings.Timezone = strings.TrimSpace(settings.Timezone)
	if settings.BackupRetention < 1 || settings.BackupRetention > 90 {
		writeError(w, http.StatusBadRequest, "Backup retention must be between 1 and 90 days.")
		return
	}
	a.store.SetSettings(settings)
	a.store.AddActivity("Settings saved", "Panel preferences were updated", "success")
	writeJSON(w, http.StatusOK, settings)
}

func (a *App) changePassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Next) < 12 {
		writeError(w, http.StatusBadRequest, "The new password must contain at least 12 characters.")
		return
	}
	username := a.store.Username()
	if !a.store.CheckPassword(username, input.Current) {
		writeError(w, http.StatusUnauthorized, "The current password is incorrect.")
		return
	}
	if err := a.store.SetPassword(input.Next); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update the password.")
		return
	}
	a.store.AddActivity("Password changed", "Administrator credentials were updated", "success")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func validOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	host := r.Host
	return origin == "https://"+host || origin == "http://"+host
}

func decodeJSON(r *http.Request, dst any) error {
	return decodeJSONWithLimit(r, dst, 1<<20)
}

func decodeJSONWithLimit(r *http.Request, dst any, limit int64) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, limit+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("invalid request: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("invalid request: only one JSON object is allowed")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func randomToken(bytes int) (string, error) {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	const iterations = 600000
	derived := pbkdf2SHA256([]byte(password), salt, iterations, 32)
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", iterations, hex.EncodeToString(salt), hex.EncodeToString(derived)), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 100000 || iterations > 2000000 {
		return false
	}
	salt, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := hex.DecodeString(parts[3])
	if err != nil || len(expected) != 32 {
		return false
	}
	actual := pbkdf2SHA256([]byte(password), salt, iterations, len(expected))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func pbkdf2SHA256(password, salt []byte, iterations, length int) []byte {
	result := make([]byte, 0, length)
	for block := 1; len(result) < length; block++ {
		counter := []byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)}
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write(counter)
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		result = append(result, t...)
	}
	return result[:length]
}
