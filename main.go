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
	"net/mail"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata"
)

var version = "0.5.1-dev"

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
	store      *Store
	system     *System
	sessions   map[string]session
	attempts   map[string]loginAttempt
	mu         sync.Mutex
	mutationMu sync.Mutex
	loginMu    sync.Mutex
}

func main() {
	dataDir := envOr("LITECP_DATA_DIR", "/var/lib/litecp")
	bind := envOr("LITECP_BIND", "127.0.0.1:9090")
	if len(os.Args) > 1 && os.Args[1] == "host-resolvers" {
		if os.Geteuid() != 0 {
			log.Fatal("host-resolvers requires root")
		}
		if err := NewSystem(dataDir).applyHostResolvers(context.Background(), os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}

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
		if os.Getenv("LITECP_CONFIGURE_RESOLVERS") == "0" {
			if err := store.UpdateSettings(func(settings *Settings) { settings.Resolvers = []string{} }); err != nil {
				log.Fatal(err)
			}
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
	if len(os.Args) > 1 && os.Args[1] == "sync-mail" {
		if os.Geteuid() != 0 {
			log.Fatal("sync-mail requires root")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		domains, mailboxes, aliases := store.MailConfig()
		if err := NewSystem(dataDir).SyncMailConfig(ctx, domains, mailboxes, aliases); err != nil {
			log.Fatal(err)
		}
		return
	}

	app := &App{
		store:    store,
		system:   NewSystem(dataDir),
		sessions: make(map[string]session),
		attempts: make(map[string]loginAttempt),
	}
	reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	if err := app.system.ReconcileSites(reconcileCtx, store.Sites()); err != nil {
		reconcileCancel()
		log.Fatalf("reconcile isolated website runtimes: %v", err)
	}
	reconcileCancel()

	server := &http.Server{
		Addr:              bind,
		Handler:           app.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       45 * time.Minute,
		WriteTimeout:      45 * time.Minute,
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
	mux.HandleFunc("/api/activities", only(http.MethodGet, a.withAuth(a.activities)))
	mux.HandleFunc("/api/php/extensions", a.withAuth(a.phpExtensions))
	mux.HandleFunc("/api/security/scan", only(http.MethodPost, a.withAuth(a.malwareScan)))
	mux.HandleFunc("/api/sites/waf", only(http.MethodPut, a.withAuth(a.setSiteWAF)))
	mux.HandleFunc("/api/system/hostname", only(http.MethodPut, a.withAuth(a.systemHostname)))
	mux.HandleFunc("/api/system/resolvers", only(http.MethodPut, a.withAuth(a.systemResolvers)))
	mux.HandleFunc("/api/system/reboot", only(http.MethodPost, a.withAuth(a.systemReboot)))
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
	mux.HandleFunc("/api/files/trash", a.withAuth(a.trash))
	mux.HandleFunc("/api/migrations", a.withAuth(a.migrations))
	mux.HandleFunc("/api/phpmyadmin/", a.withAuth(a.phpMyAdmin))
	mux.HandleFunc("/api/live-migrations", a.withAuth(a.liveMigrations))
	mux.HandleFunc("/api/live-migrations/download", only(http.MethodGet, a.withAuth(a.liveMigrationDownload)))
	mux.HandleFunc("/api/migrations/database", only(http.MethodPost, a.withAuth(a.migrationDatabaseImport)))
	mux.HandleFunc("/api/migrations/import", only(http.MethodPost, a.withAuth(a.migrationImport)))
	mux.HandleFunc("/api/migrations/", only(http.MethodDelete, a.withAuth(a.migrationDelete)))
	mux.HandleFunc("/api/files/create", only(http.MethodPost, a.withAuth(a.createFile)))
	mux.HandleFunc("/api/files/transfer", only(http.MethodPost, a.withAuth(a.transferFiles)))
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
	mux.HandleFunc("/api/firewall/", a.withAuth(a.firewallRuleAction))
	mux.HandleFunc("/api/mail/hosting", only(http.MethodPut, a.withAuth(a.toggleMailHosting)))
	mux.HandleFunc("/api/mail/dns", only(http.MethodGet, a.withAuth(a.mailDNS)))
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
		// Serialize state-changing operations so file, service and config writes cannot race.
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.URL.Path != "/api/logout" {
			a.mutationMu.Lock()
			defer a.mutationMu.Unlock()
			if !a.validSession(cookie.Value) {
				writeError(w, http.StatusUnauthorized, "Your session has expired. Please sign in again.")
				return
			}
		}
		next(w, r)
	}
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if !validOrigin(r) {
		writeError(w, http.StatusForbidden, "Request origin was rejected.")
		return
	}
	ip := clientIP(r)
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSONWithLimit(r, &input, 16<<10); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Bound expensive password work and reserve attempts before checking them.
	// Concurrent requests must not bypass the per-address failure limit.
	if !a.loginMu.TryLock() {
		writeError(w, http.StatusTooManyRequests, "Another sign-in is being checked. Please retry shortly.")
		return
	}
	defer a.loginMu.Unlock()
	if !a.reserveLoginAttempt(ip) {
		writeError(w, http.StatusTooManyRequests, "Too many sign-in attempts. Try again in 15 minutes.")
		return
	}
	if !a.store.CheckPassword(input.Username, input.Password) {
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
	a.rememberSession(token, expires)
	secure := requestIsHTTPS(r)
	http.SetCookie(w, &http.Cookie{Name: "litecp_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{
		Name: "litecp_session", Value: token, Path: "/api/", Expires: expires,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode,
	})
	_ = a.store.AddActivity("Signed in", "Administrator session started", "success")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("litecp_session"); err == nil {
		a.mu.Lock()
		delete(a.sessions, cookie.Value)
		a.mu.Unlock()
	}
	secure := requestIsHTTPS(r)
	http.SetCookie(w, &http.Cookie{Name: "litecp_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{Name: "litecp_session", Value: "", Path: "/api/", MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
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

func (a *App) reserveLoginAttempt(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for address, attempt := range a.attempts {
		if now.Sub(attempt.Since) >= 15*time.Minute {
			delete(a.attempts, address)
		}
	}
	attempt, exists := a.attempts[ip]
	if !exists {
		if len(a.attempts) >= 4096 {
			return false
		}
		if a.attempts == nil {
			a.attempts = make(map[string]loginAttempt)
		}
		attempt = loginAttempt{Since: now}
	}
	if attempt.Count >= 5 {
		return false
	}
	attempt.Count++
	a.attempts[ip] = attempt
	return true
}

func (a *App) rememberSession(token string, expires time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	oldest := ""
	for key, current := range a.sessions {
		if !current.Expires.After(now) {
			delete(a.sessions, key)
			continue
		}
		if oldest == "" || current.Expires.Before(a.sessions[oldest].Expires) {
			oldest = key
		}
	}
	if len(a.sessions) >= 128 {
		delete(a.sessions, oldest)
	}
	if a.sessions == nil {
		a.sessions = make(map[string]session)
	}
	a.sessions[token] = session{Expires: expires}
}

func (a *App) clearLoginFailures(ip string) {
	a.mu.Lock()
	delete(a.attempts, ip)
	a.mu.Unlock()
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	parsed := net.ParseIP(strings.TrimSpace(host))
	if parsed != nil && parsed.IsLoopback() {
		forwarded := strings.TrimSpace(r.Header.Get("X-Real-IP"))
		if candidate := net.ParseIP(forwarded); candidate != nil {
			return candidate.String()
		}
	}
	if parsed != nil {
		return parsed.String()
	}
	return host
}

func (a *App) state(w http.ResponseWriter, r *http.Request) {
	state := a.store.PublicState()
	for i := range state.Sites {
		state.Sites[i].SSL = a.system.SiteSSLHealthy(state.Sites[i])
	}
	state.System = a.system.Snapshot(r.Context(), state.Sites)
	if !state.Settings.EmailHostingEnabled {
		for i := range state.System.Services {
			if state.System.Services[i].Key == "mail" {
				state.System.Services[i].State = "disabled"
			}
		}
	}
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
	if input.Service == "mail" && !a.store.Settings().EmailHostingEnabled {
		writeError(w, http.StatusConflict, "Enable email hosting from the Email page first.")
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
	appVersion := normalizeAppVersion(appType, input.AppVersion)
	if err := validateSiteOptions(appType, appVersion, phpVersion); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.AppType, input.AppVersion, input.PHPVersion = appType, appVersion, phpVersion
	if a.store.SiteByDomain(input.Domain) != nil {
		writeError(w, http.StatusConflict, "That domain is already managed by LiteCP.")
		return
	}
	site, credentials, err := a.system.CreateSite(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var database *ManagedDatabase
	if credentials != nil {
		databaseID, _ := randomToken(8)
		value := ManagedDatabase{
			ID: databaseID, Name: credentials.Name, Username: credentials.Username,
			SiteID: site.ID, Domain: site.Domain, LegacyAuth: legacyPHPDatabaseAuth(site.PHPVersion), CreatedAt: site.CreatedAt,
		}
		database = &value
	}
	if err := a.store.AddSiteWithDatabase(site, database); err != nil {
		rollbackErrors := []string{}
		if rollbackErr := a.system.RemoveSiteConfig(context.Background(), site); rollbackErr != nil {
			rollbackErrors = append(rollbackErrors, "configuration: "+rollbackErr.Error())
		}
		if database != nil {
			if rollbackErr := a.system.DropManagedDatabase(context.Background(), *database); rollbackErr != nil {
				rollbackErrors = append(rollbackErrors, "database: "+rollbackErr.Error())
			}
		}
		if rollbackErr := a.system.CleanupFailedSiteFiles(site); rollbackErr != nil {
			rollbackErrors = append(rollbackErrors, "files: "+rollbackErr.Error())
		}
		if len(rollbackErrors) > 0 {
			writeError(w, http.StatusInternalServerError, "Panel state could not be saved and website rollback was incomplete: "+strings.Join(rollbackErrors, "; "))
			return
		}
		writeError(w, http.StatusInternalServerError, "Website was rolled back because panel state could not be saved.")
		return
	}
	warning := ""
	if input.EnableCron {
		if err := a.system.SetCron(site, true); err != nil {
			warning = "Website created, but the application cron could not be enabled: " + err.Error()
		} else {
			site.Cron = true
			if err := a.store.UpdateSite(site.ID, func(stored *Site) { stored.Cron = true }); err != nil {
				rollbackErr := a.system.SetCron(site, false)
				site.Cron = false
				warning = "Website created, but cron state could not be saved: " + err.Error()
				if rollbackErr != nil {
					warning += "; cron rollback also failed: " + rollbackErr.Error()
				}
			}
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
	if err := a.store.UpdateSite(input.ID, func(site *Site) { site.SSL = true }); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not save the SSL state.")
		return
	}
	if err := a.system.EnableSSL(r.Context(), *site, input.Email); err != nil {
		if rollbackErr := a.store.UpdateSite(input.ID, func(site *Site) { site.SSL = false }); rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, "SSL setup failed and panel state rollback also failed: "+rollbackErr.Error())
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
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
	if err := a.store.UpdateSite(input.ID, func(site *Site) { site.Cron = input.Enabled }); err != nil {
		if rollbackErr := a.system.SetCron(*site, !input.Enabled); rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, "Panel state could not be saved, and cron rollback failed: "+rollbackErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "Cron change was rolled back because panel state could not be saved.")
		return
	}
	label := "disabled"
	if input.Enabled {
		label = "enabled"
	}
	a.store.AddActivity(applicationName(site.AppType)+" cron "+label, site.Domain+" automation schedule updated", "success")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) changeSitePHP(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID               string `json:"id"`
		Version          string `json:"version"`
		DatabasePassword string `json:"databasePassword"`
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
	if err := validateSiteOptions(site.AppType, site.AppVersion, strings.TrimSpace(input.Version)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var authenticationChanged bool
	database := a.store.DatabaseBySiteID(site.ID)
	if legacyPHPDatabaseAuth(input.Version) && database != nil && !database.LegacyAuth {
		if err := validateDatabasePassword(input.DatabasePassword); err != nil {
			writeError(w, http.StatusBadRequest, "Switching a site database to legacy PHP requires its current database password.")
			return
		}
		compatibleDatabase := *database
		compatibleDatabase.LegacyAuth = true
		if err := a.system.ResetDatabasePassword(r.Context(), compatibleDatabase, input.DatabasePassword); err != nil {
			writeError(w, http.StatusBadGateway, "Could not make the database compatible with legacy PHP: "+err.Error())
			return
		}
		if err := a.store.UpdateDatabase(database.ID, func(value *ManagedDatabase) { value.LegacyAuth = true }); err != nil {
			if rollbackErr := a.system.ResetDatabasePassword(context.Background(), *database, input.DatabasePassword); rollbackErr != nil {
				writeError(w, http.StatusInternalServerError, "Panel state could not be saved, and database-authentication rollback failed: "+rollbackErr.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "Database authentication was rolled back because panel state could not be saved.")
			return
		}
		authenticationChanged = true
	}
	rollbackAuthentication := func() error {
		if !authenticationChanged || database == nil {
			return nil
		}
		if err := a.system.ResetDatabasePassword(context.Background(), *database, input.DatabasePassword); err != nil {
			return err
		}
		return a.store.UpdateDatabase(database.ID, func(value *ManagedDatabase) { value.LegacyAuth = database.LegacyAuth })
	}
	if err := a.system.SetPHPVersion(r.Context(), *site, input.Version); err != nil {
		if rollbackErr := rollbackAuthentication(); rollbackErr != nil {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("PHP change failed (%v), and database-authentication rollback failed (%v).", err, rollbackErr))
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := a.store.UpdateSite(input.ID, func(site *Site) { site.PHPVersion = strings.TrimSpace(input.Version) }); err != nil {
		changed := *site
		changed.PHPVersion = strings.TrimSpace(input.Version)
		phpRollbackErr := a.system.SetPHPVersion(context.Background(), changed, site.PHPVersion)
		authRollbackErr := rollbackAuthentication()
		if phpRollbackErr != nil || authRollbackErr != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Panel state could not be saved, and PHP recovery was incomplete (runtime: %v; database authentication: %v).", phpRollbackErr, authRollbackErr))
			return
		}
		writeError(w, http.StatusInternalServerError, "PHP change was rolled back because panel state could not be saved.")
		return
	}
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
	backup, err := a.system.snapshotSiteConfig(*site)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := a.system.RemoveSiteConfig(r.Context(), *site); err != nil {
		if rollbackErr := a.system.restoreSiteConfig(context.Background(), *site, backup); rollbackErr != nil {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("Website detach failed (%v), and configuration rollback failed (%v).", err, rollbackErr))
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := a.store.RemoveSite(id); err != nil {
		if rollbackErr := a.system.restoreSiteConfig(context.Background(), *site, backup); rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, "Website detach failed and its configuration rollback also failed: "+rollbackErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "Website detach was rolled back because panel state could not be saved.")
		return
	}
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
			if saveErr := a.store.AddBackup(backup); saveErr != nil {
				writeError(w, http.StatusInternalServerError, "Local backup completed, but its history could not be saved.")
				return
			}
			_ = a.pruneExpiredBackups()
			a.store.AddActivity("Remote backup failed", site.Domain+" was saved locally but could not upload to "+destination.Name, "warning")
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		backup.DestinationID = destination.ID
		backup.DestinationName = destination.Name
		backup.RemotePath = remotePath
	}
	if err := a.store.AddBackup(backup); err != nil {
		writeError(w, http.StatusInternalServerError, "Backup completed, but its history could not be saved.")
		return
	}
	_ = a.pruneExpiredBackups()
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
	if settings.Email != "" {
		address, err := mail.ParseAddress(settings.Email)
		if err != nil || address.Address != settings.Email {
			writeError(w, http.StatusBadRequest, "Enter a valid administrator email address.")
			return
		}
	}
	if _, err := time.LoadLocation(settings.Timezone); err != nil {
		writeError(w, http.StatusBadRequest, "Choose a valid display timezone.")
		return
	}
	if settings.BackupRetention < 1 || settings.BackupRetention > 90 {
		writeError(w, http.StatusBadRequest, "Backup retention must be between 1 and 90 days.")
		return
	}
	if err := a.store.UpdateSettings(func(value *Settings) {
		value.Email, value.Timezone, value.BackupRetention = settings.Email, settings.Timezone, settings.BackupRetention
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not save panel settings.")
		return
	}
	if err := a.pruneExpiredBackups(); err != nil {
		writeError(w, http.StatusInternalServerError, "Settings were saved, but expired backups could not be pruned: "+err.Error())
		return
	}
	a.store.AddActivity("Settings saved", "Panel preferences were updated", "success")
	writeJSON(w, http.StatusOK, settings)
}

func (a *App) pruneExpiredBackups() error {
	retention := a.store.Settings().BackupRetention
	if retention < 1 {
		return nil
	}
	expired := a.store.ExpiredBackups(time.Now().UTC().Add(-time.Duration(retention) * 24 * time.Hour))
	removed := map[string]bool{}
	for _, backup := range expired {
		if err := a.system.RemoveBackupFile(backup.Path); err != nil {
			return err
		}
		removed[backup.ID] = true
	}
	return a.store.RemoveBackups(removed)
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
	a.mu.Lock()
	a.sessions = make(map[string]session)
	a.mu.Unlock()
	a.store.AddActivity("Password changed", "Administrator credentials were updated", "success")
	http.SetCookie(w, &http.Cookie{Name: "litecp_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{Name: "litecp_session", Value: "", Path: "/api/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true, "signedOut": true})
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback() && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func validOrigin(r *http.Request) bool {
	// Same-site subdomains are not the same origin as the administrator panel.
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		if referer := r.Referer(); referer != "" {
			parsed, err := url.Parse(referer)
			if err != nil || parsed.User != nil {
				return false
			}
			origin = parsed.Scheme + "://" + parsed.Host
		} else {
			// Preserve non-browser API clients without browser-origin headers.
			return true
		}
	}
	scheme := "http"
	if requestIsHTTPS(r) {
		scheme = "https"
	}
	return origin == scheme+"://"+r.Host
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
