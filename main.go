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

var version = "5.0.23"
var buildRevision = ""

//go:embed web/*
var webAssets embed.FS

type session struct {
	Expires          time.Time
	OperatorID       string
	OperatorRevision string
}

type loginAttempt struct {
	Count int
	Since time.Time
}

type App struct {
	apiBuckets map[string]loginAttempt
	store      *Store
	system     *System
	sessions   map[string]session
	attempts   map[string]loginAttempt
	mu         sync.Mutex
	mutationMu sync.Mutex
	loginMu    sync.Mutex
	traffic    *trafficService
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "captcha-gateway" {
		if err := runCaptchaGateway(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "website-redis" {
		if err := redisRuntimeCommand(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "resource-runtime" {
		if err := resourceRuntimeCommand(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "resource-cron" {
		if err := resourceCronCommand(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "web" {
		if err := runUnprivilegedWeb(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "broker" {
		// Mark inherited activation descriptors before startup can launch any
		// website command or helper process.
		if err := prepareBrokerActivation(); err != nil {
			log.Fatal(err)
		}
	}
	if len(os.Args) > 1 && os.Args[1] == "doctor" {
		if err := runDoctor(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "capture-worker" {
		if err := runLiveCaptureWorker(os.Args[2]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "scan-server" {
		if err := runServerScan(); err != nil {
			log.Fatal(err)
		}
		return
	}
	dataDir := envOr("LITECP_DATA_DIR", "/var/lib/litecp")
	if len(os.Args) > 1 && os.Args[1] == "reset-admin-password" {
		if err := validateResetPasswordArguments(os.Args); err != nil {
			log.Fatal(err)
		}
		if err := resetAdminPasswordCommand(dataDir); err != nil {
			log.Fatal(err)
		}
		return
	}
	bind := envOr("LITECP_BIND", "127.0.0.1:9090")
	if len(os.Args) > 1 && os.Args[1] == "ensure-default-resolvers" {
		if os.Geteuid() != 0 {
			log.Fatal("ensure-default-resolvers requires root")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		wanted := []string{"8.8.8.8", "8.8.4.4"}
		if !defaultResolversMatch(currentSystemResolvers()) {
			if err := NewSystem(dataDir).applyHostResolvers(ctx, wanted); err != nil {
				log.Fatal(err)
			}
			if err := waitForDefaultResolvers(ctx, currentSystemResolvers); err != nil {
				log.Fatal(err)
			}
			log.Print("Restored Google DNS resolvers: 8.8.8.8, 8.8.4.4")
		}

		return
	}
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

	if len(os.Args) > 1 && os.Args[1] == "snapshot-reconcile" {
		if os.Geteuid() != 0 {
			log.Fatal("snapshot-reconcile requires root")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		system := NewSystem(dataDir)
		if err := reconcileSnapshotWAFHTTP(wafPolicyPath, wafHTTPPath); err != nil {
			log.Fatal(err)
		}
		if err := (&App{store: store, system: system}).syncCaptchaRuntime(store.Sites()); err != nil {
			log.Fatal(err)
		}
		if err := system.reconcileTrafficLoggingMode(ctx, store.Sites(), false); err != nil {
			log.Fatal(err)
		}
		if err := system.reconcileWebsiteCron(store.Sites()); err != nil {
			log.Fatal(err)
		}
		for _, site := range store.Sites() {
			if !domainPattern.MatchString(site.Domain) {
				log.Fatal("invalid snapshot domain")
			}
			path, enabled := system.nginxPaths(site.Domain)
			if enabled != "" {
				if _, err := os.Lstat(enabled); os.IsNotExist(err) {
					if err = os.Symlink(path, enabled); err != nil {
						log.Fatal(err)
					}
				}
			}
			if err := system.reconcileSiteRuntime(ctx, site, path); err != nil {
				log.Fatal(err)
			}
		}
		restoredApplications := false
		for _, site := range store.Sites() {
			if site.Application == nil || !site.Application.Enabled || site.Application.Stopped {
				continue
			}
			app := *site.Application
			if err := validateApplicationHosting(site, app); err != nil {
				log.Fatal(err)
			}
			if _, err := runCommand(ctx, 15*time.Second, "podman", "container", "exists", app.Container); err == nil {
				// Retained recovery containers must not claim the replacement
				// application ports on the next boot. Keep their data intact.
				if _, err = runCommand(ctx, 15*time.Second, "podman", "update", "--restart=no", app.Container); err != nil {
					log.Fatal(err)
				}
				_, _ = runCommand(ctx, 30*time.Second, "podman", "stop", app.Container)
				if _, err = runCommand(ctx, 15*time.Second, "podman", "rename", app.Container, app.Container+"-recovery-"+time.Now().Format("20060102150405")); err != nil {
					log.Fatal(err)
				}
			}
			if _, err := runCommand(ctx, 2*time.Minute, "podman", applicationContainerArgs(site, app, app.Container, app.Command, true)...); err != nil {
				log.Fatal(err)
			}
			if _, err := runCommand(ctx, 15*time.Second, "podman", "update", "--restart=always", app.Container); err != nil {
				log.Fatal(err)
			}
			restoredApplications = true
		}
		if restoredApplications {
			if _, err := runCommand(ctx, 15*time.Second, "systemctl", "enable", "podman-restart.service"); err != nil {
				log.Fatal(err)
			}
		}
		return
	}
	if runtimeRootHTTPForbidden(os.Args, NewSystem(dataDir).demo) {
		log.Fatal("Root HTTP serving is disabled. Start litecp.service (unprivileged web) and litecp-broker.service.")
	}
	if err := store.interruptBackupRequests(); err != nil {
		log.Fatal("Cannot reconcile backup requests: ", err)
	}
	app := &App{
		store:    store,
		system:   NewSystem(dataDir),
		sessions: make(map[string]session),
		attempts: make(map[string]loginAttempt),
	}
	reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	if err := app.syncCaptchaRuntime(store.Sites()); err != nil {
		// Protected websites fail closed while the administrator retains panel access.
		log.Printf("CAPTCHA runtime needs attention: %v", err)
	}
	app.startCaptchaReconcile()
	if err := app.system.ReconcileSites(reconcileCtx, store.Sites()); err != nil {
		reconcileCancel()
		log.Fatalf("reconcile isolated website runtimes: %v", err)
	}
	reconcileCancel()
	app.startUsageMonitor()
	app.startWebsiteTraffic()
	app.startBackupSchedules()
	app.startSnapshotAutomation()
	app.startCloudflareFirewallUpdates()
	app.startAutoMigrationRestores()
	app.startIntegrityMonitor()
	app.startOperationsMonitor()
	if len(os.Args) == 2 && os.Args[1] == "broker" {
		if err := runPrivilegeBroker(app); err != nil {
			log.Fatal(err)
		}
		return
	}

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
	return a.securityHeaders(a.routeMux())
}

func (a *App) routeMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/security/captcha", a.withAuth(a.captchaSettings))
	mux.HandleFunc("/api/access", a.withAuth(a.apiAccess))
	mux.HandleFunc("/api/v1", only(http.MethodPost, a.externalAPI))
	mux.HandleFunc("/api/v1/request", only(http.MethodPost, a.externalAPI))
	mux.HandleFunc("/api/login", only(http.MethodPost, a.login))
	mux.HandleFunc("/api/logout", only(http.MethodPost, a.withAuth(a.logout)))
	mux.HandleFunc("/api/state", only(http.MethodGet, a.withAuth(a.state)))
	mux.HandleFunc("/api/operations", a.withAuth(a.operationsAPI))
	mux.HandleFunc("/api/operators", a.withAuth(a.operatorsAPI))
	mux.HandleFunc("/api/activity-policy", a.withAuth(a.activityPolicy))
	mux.HandleFunc("/api/activities", only(http.MethodGet, a.withAuth(a.activities)))
	mux.HandleFunc("/api/php/extensions", a.withAuth(a.phpExtensions))
	mux.HandleFunc("/api/security/scan", only(http.MethodPost, a.withAuth(a.malwareScan)))
	mux.HandleFunc("/api/security/server-scan", a.withAuth(a.serverScan))
	mux.HandleFunc("/api/security/malware-files", a.withAuth(a.malwareFileAction))
	mux.HandleFunc("/api/live-migrations/auto-restore", only(http.MethodPost, a.withAuth(a.autoMigrationAction)))
	mux.HandleFunc("/api/backup-schedules", a.withAuth(a.backupSchedules))
	mux.HandleFunc("/api/security/posture", only(http.MethodGet, a.withAuth(a.securityPosture)))
	mux.HandleFunc("/api/security/integrity", a.withAuth(a.integrityAPI))
	mux.HandleFunc("/api/security/selinux", a.withAuth(a.selinuxRepair))
	mux.HandleFunc("/api/security/two-factor", a.withAuth(a.twoFactorSettings))
	mux.HandleFunc("/api/security/waf-events", only(http.MethodGet, a.withAuth(a.wafPolicyEvents)))
	mux.HandleFunc("/api/security/waf-policies", a.withAuth(a.wafPolicyMethods))
	mux.HandleFunc("/api/sites/waf-logs", only(http.MethodGet, a.withAuth(a.wafLogs)))
	mux.HandleFunc("/api/sites/waf", only(http.MethodPut, a.withAuth(a.setSiteWAF)))
	mux.HandleFunc("/api/system/hostname", only(http.MethodPut, a.withAuth(a.systemHostname)))
	mux.HandleFunc("/api/system/resolvers", only(http.MethodPut, a.withAuth(a.systemResolvers)))
	mux.HandleFunc("/api/system/reboot", only(http.MethodPost, a.withAuth(a.systemReboot)))
	mux.HandleFunc("/api/services/action", only(http.MethodPost, a.withAuth(a.serviceAction)))
	mux.HandleFunc("/api/sites", only(http.MethodPost, a.withAuth(a.createSite)))
	mux.HandleFunc("/api/sites/sizes", only(http.MethodGet, a.withAuth(a.siteSizes)))
	mux.HandleFunc("/api/sites/traffic", only(http.MethodGet, a.withAuth(a.websiteTraffic)))
	mux.HandleFunc("/api/sites/suspension", only(http.MethodPut, a.withAuth(a.siteSuspension)))
	mux.HandleFunc("/api/sites/wordpress/setup", only(http.MethodPost, a.withAuth(a.completeWordPressSetup)))
	mux.HandleFunc("/api/sites/ssl", only(http.MethodPost, a.withAuth(a.enableSSL)))
	mux.HandleFunc("/api/sites/cron", only(http.MethodPost, a.withAuth(a.toggleCron)))
	mux.HandleFunc("/api/sites/php", only(http.MethodPost, a.withAuth(a.changeSitePHP)))
	mux.HandleFunc("/api/sites/php-settings", a.withAuth(a.sitePHPSettings))
	mux.HandleFunc("/api/sites/redis", a.withAuth(a.websiteRedis))
	mux.HandleFunc("/api/sites/dns", a.withAuth(a.websiteDNS))
	mux.HandleFunc("/api/sites/resource-limits", a.withAuth(a.websiteResourceLimits))
	mux.HandleFunc("/api/sites/domain", only(http.MethodPut, a.withAuth(a.changeSiteDomain)))
	mux.HandleFunc("/api/sites/", only(http.MethodDelete, a.withAuth(a.deleteSite)))
	mux.HandleFunc("/api/overview/jobs", only(http.MethodGet, a.withAuth(a.overviewJobs)))
	mux.HandleFunc("/api/backups/counts", a.withAuth(a.backupCounts))
	mux.HandleFunc("/api/backups/restore-direct", a.withAuth(a.directBackupRestore))
	mux.HandleFunc("/api/backups/restorable", only(http.MethodGet, a.withAuth(a.backupRestoreInventory)))
	mux.HandleFunc("/api/backups/item", a.withAuth(a.backupHistoryItem))
	mux.HandleFunc("/api/backups", only(http.MethodPost, a.withAuth(a.createBackup)))
	mux.HandleFunc("/api/application-hosting", a.withAuth(a.applicationHosting))
	mux.HandleFunc("/api/system-updates", a.withAuth(a.systemUpdates))
	mux.HandleFunc("/api/environment-inventory", a.withAuth(a.environmentInventory))
	mux.HandleFunc("/api/analytics/processes", a.withAuth(a.liveProcesses))
	mux.HandleFunc("/api/analytics/usage", a.withAuth(a.usageAnalytics))
	mux.HandleFunc("/api/analytics/disk-usage", a.withAuth(a.directoryDiskUsage))
	mux.HandleFunc("/api/website-ftp", a.withAuth(a.websiteFTP))
	mux.HandleFunc("/api/website-cron/run", only(http.MethodPost, a.withAuth(a.runWebsiteCron)))
	mux.HandleFunc("/api/website-cron", a.withAuth(a.websiteCron))
	mux.HandleFunc("/api/website-backups", a.withAuth(a.websiteBackups))
	mux.HandleFunc("/api/website-backups/upload", only(http.MethodPost, a.withAuth(a.websiteBackupUpload)))
	mux.HandleFunc("/api/website-backups/restore", only(http.MethodPost, a.withAuth(a.websiteBackupRestore)))
	mux.HandleFunc("/api/backup-destinations", a.withAuth(a.createBackupDestination))
	mux.HandleFunc("/api/backup-destinations/test", only(http.MethodPost, a.withAuth(a.testBackupDestination)))
	mux.HandleFunc("/api/backup-destinations/storage", only(http.MethodPost, a.withAuth(a.backupDestinationStorage)))
	mux.HandleFunc("/api/backup-destinations/", only(http.MethodDelete, a.withAuth(a.deleteBackupDestination)))
	mux.HandleFunc("/api/files", a.withAuth(a.files))
	mux.HandleFunc("/api/files/search", only(http.MethodGet, a.withAuth(a.searchFiles)))
	mux.HandleFunc("/api/files/trash", a.withAuth(a.trash))
	mux.HandleFunc("/api/migrations", a.withAuth(a.migrations))
	mux.HandleFunc("/api/phpmyadmin/", a.withAuth(a.phpMyAdmin))
	mux.HandleFunc("/api/databases/phpmyadmin", only(http.MethodPost, a.withAuth(a.databaseAdminLaunch)))
	mux.HandleFunc("/internal/phpmyadmin-ticket", only(http.MethodPost, a.consumePMATicket))
	mux.HandleFunc("/api/mail/webmail-configure", a.withAuth(a.configureWebmail))
	mux.HandleFunc("/api/mail/webmail-login", only(http.MethodPost, a.withAuth(a.webmailLaunch)))
	mux.HandleFunc("/api/mail/delivery", only(http.MethodGet, a.withAuth(a.emailDeliveryLogs)))
	mux.HandleFunc("/api/mail/smtp-port-check", only(http.MethodPost, a.withAuth(a.smtpPortCheck)))
	mux.HandleFunc("/api/system-snapshots/settings", a.withAuth(a.snapshotSettingsAPI))
	mux.HandleFunc("/api/system-snapshots/remote", a.withAuth(a.snapshotRemoteAPI))
	mux.HandleFunc("/api/system-snapshots", a.withAuth(a.systemSnapshots))
	mux.HandleFunc("/api/system-snapshots/upload", only(http.MethodPost, a.withAuth(a.uploadSystemSnapshot)))
	mux.HandleFunc("/api/system-snapshots/inspect", only(http.MethodPost, a.withAuth(a.inspectSystemSnapshot)))
	mux.HandleFunc("/api/system-snapshots/restore", only(http.MethodPost, a.withAuth(a.restoreSystemSnapshot)))
	mux.HandleFunc("/internal/webmail-ticket", only(http.MethodPost, a.consumeWebmailTicket))
	mux.HandleFunc("/api/live-migrations", a.withAuth(a.liveMigrations))
	mux.HandleFunc("/api/live-migrations/cancel", only(http.MethodPost, a.withAuth(a.liveMigrationCancellation)))
	mux.HandleFunc("/api/live-migrations/download", only(http.MethodGet, a.withAuth(a.liveMigrationDownload)))
	mux.HandleFunc("/api/migrations/database", only(http.MethodPost, a.withAuth(a.migrationDatabaseImport)))
	mux.HandleFunc("/api/migrations/import", only(http.MethodPost, a.withAuth(a.migrationImport)))
	mux.HandleFunc("/api/migrations/", only(http.MethodDelete, a.withAuth(a.migrationDelete)))
	mux.HandleFunc("/api/files/create", only(http.MethodPost, a.withAuth(a.createFile)))
	mux.HandleFunc("/api/files/transfer", only(http.MethodPost, a.withAuth(a.transferFiles)))
	mux.HandleFunc("/api/files/upload-url", only(http.MethodPost, a.withAuth(a.uploadFileURL)))
	mux.HandleFunc("/api/files/download-selected", a.withAuth(a.downloadSelectedFiles))
	mux.HandleFunc("/api/files/content", a.withAuth(a.fileContent))
	mux.HandleFunc("/api/files/encodings", only(http.MethodGet, a.withAuth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"encodings": availableEditorEncodings(), "maxBytes": maxEditableFileSize})
	})))
	mux.HandleFunc("/api/files/upload", only(http.MethodPost, a.withAuth(a.uploadFiles)))
	mux.HandleFunc("/api/files/directory", only(http.MethodPost, a.withAuth(a.createDirectory)))
	mux.HandleFunc("/api/files/rename", only(http.MethodPost, a.withAuth(a.renameFile)))
	mux.HandleFunc("/api/files/permissions", only(http.MethodPut, a.withAuth(a.changeFilePermissions)))
	mux.HandleFunc("/api/files/archive", only(http.MethodPost, a.withAuth(a.archiveFiles)))
	mux.HandleFunc("/api/files/extract", only(http.MethodPost, a.withAuth(a.extractArchive)))
	mux.HandleFunc("/api/files/download", only(http.MethodGet, a.withAuth(a.downloadFile)))
	mux.HandleFunc("/api/databases", only(http.MethodPost, a.withAuth(a.createManagedDatabase)))
	mux.HandleFunc("/api/databases/sizes", only(http.MethodGet, a.withAuth(a.databaseSizes)))
	mux.HandleFunc("/api/databases/password", only(http.MethodPut, a.withAuth(a.resetManagedDatabasePassword)))
	mux.HandleFunc("/api/databases/", only(http.MethodDelete, a.withAuth(a.deleteManagedDatabase)))
	mux.HandleFunc("/api/firewall/apply-litecp", a.withAuth(a.litecpFirewallControl))
	mux.HandleFunc("/api/backup-destinations/host-key", only(http.MethodPost, a.withAuth(a.backupHostKey)))
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
	mux.HandleFunc("/api/terminal", only(http.MethodPost, a.withAuth(a.terminalCommand)))
	mux.HandleFunc("/api/terminal/session", a.withAuth(a.interactiveTerminal))
	mux.HandleFunc("/api/shell-capabilities", only(http.MethodGet, a.withAuth(a.terminalCapabilities)))
	mux.HandleFunc("/api/security/ssh-port", a.withAuth(a.sshPortSettings))
	mux.HandleFunc("/api/security/brute-force", a.withAuth(a.bruteForceProtection))
	mux.HandleFunc("/api/security/panel-port", a.withAuth(a.panelPortSettings))
	mux.HandleFunc("/api/mail/aliases/", a.withAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			a.updateMailAlias(w, r)
			return
		}
		only(http.MethodDelete, a.deleteMailAlias)(w, r)
	}))
	mux.HandleFunc("/api/logs", only(http.MethodGet, a.withAuth(a.logs)))
	mux.HandleFunc("/api/settings", only(http.MethodPut, a.withAuth(a.updateSettings)))
	mux.HandleFunc("/api/password", only(http.MethodPut, a.withAuth(a.changePassword)))
	mux.HandleFunc("/api/updates/install", only(http.MethodPost, a.withAuth(a.startPanelUpdate)))
	mux.HandleFunc("/api/developer-mode", a.withAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPut {
			writeError(w, 405, "Use GET or PUT")
			return
		}
		a.developerMode(w, r)
	}))
	mux.HandleFunc("/api/updates/status", only(http.MethodGet, a.withAuth(a.panelUpdateStatus)))
	mux.HandleFunc("/api/updates", only(http.MethodGet, a.withAuth(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, checkRelease(version)) })))
	mux.HandleFunc("/api/version", only(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": version, "build": buildRevision})
	}))

	webFS, err := fs.Sub(webAssets, "web")
	if err != nil {
		panic(err)
	}
	static := cachedImageAssets(http.FileServer(http.FS(webFS)), webFS)
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

	return mux
}

func (a *App) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		formSources := "'self'"
		if a.system != nil {
			if webmail, err := url.Parse(a.system.WebmailURL()); err == nil && webmail.Scheme == "https" && webmail.Host != "" && webmail.User == nil && !strings.ContainsAny(webmail.Host, " ;'\r\n") {
				formSources += " https://" + webmail.Host
			}
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action "+formSources)
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
		// Set only by the external API dispatcher after credential, permission,
		// expiry, IP and rate checks. It is bound to this exact route and method.
		if validExternalAPIGrant(r) {
			a.auditOperation(w, r, next)
			return
		}
		cookie, err := r.Cookie("litecp_session")
		operator, limited := a.operatorSession(r)
		if limited && operator == nil || !limited && (err != nil || !a.validSession(cookie.Value)) && !a.trustedRequest(r) {
			writeError(w, http.StatusUnauthorized, "Your session has expired. Please sign in again.")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !validOrigin(r) {
			writeError(w, http.StatusForbidden, "Request origin was rejected.")
			return
		}
		if limited && !operatorRouteAllowed(operator.Role, r.Method, r.URL.Path) {
			writeError(w, 403, "Your limited administrator role does not permit this operation.")
			return
		}
		// Serialize state-changing operations so file, service and config writes cannot race.
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.URL.Path != "/api/logout" && r.URL.Path != "/api/live-migrations/cancel" && r.URL.Path != "/api/terminal/session" && r.URL.Path != "/api/files/upload-url" {
			a.mutationMu.Lock()
			defer a.mutationMu.Unlock()
			if limited && !a.validSession(cookie.Value) || !limited && (cookie == nil || !a.validSession(cookie.Value)) && !a.trustedRequest(r) {
				writeError(w, http.StatusUnauthorized, "Your session has expired. Please sign in again.")
				return
			}
		}
		if limited {
			r = r.WithContext(context.WithValue(r.Context(), operatorContextKey{}, operator))
		}
		a.auditOperation(w, r, next)
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
		Code     string `json:"code"`
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
	var operator *PanelOperator
	input.Username = strings.TrimSpace(input.Username)
	owner := input.Username == a.store.Username()
	credentialsOK := false
	if owner {
		credentialsOK = a.store.CheckPassword(input.Username, input.Password)
	} else {
		operator, credentialsOK = a.authenticateOperator(input.Username, input.Password, input.Code)
	}
	if !credentialsOK {
		if operator != nil && strings.TrimSpace(input.Code) == "" {
			writeJSON(w, 401, map[string]any{"error": "Enter this operator's authenticator code.", "twoFactorRequired": true})
			return
		}
		a.authenticationEvent("failed", r)
		time.Sleep(350 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "The sign-in credentials or authentication code are incorrect.")
		return
	}
	if owner && a.store.twoFactor().Enabled && strings.TrimSpace(input.Code) == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Enter your authenticator or recovery code to finish signing in.", "twoFactorRequired": true})
		return
	}
	if owner && a.store.twoFactor().Enabled && a.verifySecondFactor(input.Code, true) != nil {
		a.authenticationEvent("failed", r)
		writeError(w, http.StatusUnauthorized, "The sign-in credentials or authentication code are incorrect.")
		return
	}
	a.clearLoginFailures(ip)
	a.authenticationEvent("password_verified", r)
	token, err := randomToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not create a session.")
		return
	}
	expires := time.Now().Add(12 * time.Hour)
	a.rememberSession(token, expires)
	if operator != nil {
		a.mu.Lock()
		a.sessions[token] = session{Expires: expires, OperatorID: operator.ID, OperatorRevision: operator.Revision}
		a.mu.Unlock()
	}
	if operator != nil {
		a.forgetBrowser(w, r)
	}
	secure := requestIsHTTPS(r)
	http.SetCookie(w, &http.Cookie{Name: "litecp_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{
		Name: "litecp_session", Value: token, Path: "/api/", Expires: expires,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode,
	})
	if owner {
		if _, trustErr := a.trustAuthenticatedBrowser(w, r); trustErr != nil {
			_ = a.store.AddActivity("Browser trust unavailable", "Signed in normally, but browser trust could not be persisted. Save Developer mode settings to retry.", "warning")
		}
	}
	_ = a.store.AddActivity("Signed in", "Administrator session started", "success")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	closeOwnedTerminals(terminalOwner(r))
	a.forgetBrowser(w, r)
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
	current, ok := a.sessions[token]
	if !ok || now.After(current.Expires) {
		delete(a.sessions, token)
		a.mu.Unlock()
		return false
	}
	a.mu.Unlock()
	if current.OperatorID != "" {
		a.store.mu.RLock()
		valid := false
		for _, op := range a.store.cfg.Operators {
			if op.ID == current.OperatorID && op.Enabled && op.Revision == current.OperatorRevision {
				valid = true
			}
		}
		a.store.mu.RUnlock()
		if !valid {
			a.mu.Lock()
			delete(a.sessions, token)
			a.mu.Unlock()
			return false
		}
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
	if op := a.requestOperator(r); op != nil {
		redactOperatorState(&state, *op)
	}
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
	if input.AppType == "wordpress" {
		if input.WordPressMode != "install" && input.WordPressMode != "migrate" {
			writeError(w, 400, "Choose Install new WordPress or Migrate existing WordPress")
			return
		}
		if err := validateWordPressInstall(input); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		if input.WordPressMode == "install" {
			input.CreateDatabase = true
		}
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
	wordpressInstalled := false
	wordpressAdminURL := ""
	if input.AppType == "wordpress" && input.WordPressMode == "install" {
		if credentials == nil {
			warning = "Website created, but WordPress requires a managed database."
		} else if err := a.system.installWordPress(r.Context(), site, *credentials, input); err != nil {
			warning = "Website created, but WordPress installation failed: " + err.Error()
		} else {
			wordpressInstalled = true
			scheme := "http"
			if site.SSL {
				scheme = "https"
			}
			wordpressAdminURL = scheme + "://" + site.Domain + "/wp-admin/"
		}
	}
	a.store.AddActivity("Website added", site.Domain+" is ready for "+applicationName(site.AppType), "success")
	writeJSON(w, http.StatusCreated, map[string]any{"site": site, "database": credentials, "warning": warning, "wordpressInstalled": wordpressInstalled, "wordpressAdminURL": wordpressAdminURL})
}

func (a *App) enableSSL(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Renew bool   `json:"renew"`
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
	if err := a.system.EnableSSL(r.Context(), *site, input.Email, input.Renew); err != nil {
		if rollbackErr := a.store.UpdateSite(input.ID, func(value *Site) { value.SSL = site.SSL }); rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, "SSL setup failed and panel state rollback also failed: "+rollbackErr.Error())
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	action := "SSL enabled"
	if input.Renew {
		action = "SSL renewed"
	}
	a.store.AddActivity(action, site.Domain+" now redirects to HTTPS", "success")
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
		if rollbackErr := a.system.SetCron(*site, site.Cron); rollbackErr != nil {
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
	if err := validatePHPVersionSwitch(strings.TrimSpace(input.Version)); err != nil {
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

func (a *App) changeSiteDomain(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID     string `json:"id"`
		Domain string `json:"domain"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.Domain = strings.ToLower(strings.TrimSpace(input.Domain))
	if len(input.Domain) > 253 || !domainPattern.MatchString(input.Domain) {
		writeError(w, http.StatusBadRequest, "Enter a valid domain name, such as example.com.")
		return
	}
	site := a.store.SiteByID(input.ID)
	if site == nil {
		writeError(w, http.StatusNotFound, "Website not found.")
		return
	}
	if site.Domain == input.Domain && (siteHome(*site) == "" || site.Root == defaultSiteRoot(input.Domain)) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if existing := a.store.SiteByDomain(input.Domain); existing != nil && existing.ID != site.ID {
		writeError(w, http.StatusConflict, "That domain is already managed by LiteCP.")
		return
	}
	updated := *site
	updated.Domain = input.Domain
	if siteHome(*site) != "" {
		updated.Root = defaultSiteRoot(input.Domain)
	}
	updated.SSL = false
	if updated.Application != nil {
		application := *updated.Application
		application.Certificate = ""
		application.CertificateKey = ""
		updated.Application = &application
	}
	updated.DNSZone = renameWebsiteDNS(site.DNSZone, site.Domain, updated.Domain)
	if updated.DNSZone != nil {
		if err := advanceDNSSerial(updated.DNSZone, site.DNSZone.Serial); err != nil {
			writeError(w, 409, err.Error())
			return
		}
	}
	rollbackDNS, err := a.prepareWebsiteDNSChange(r.Context(), *site, &updated)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	defer func() {
		if current := a.store.SiteByID(site.ID); current != nil && current.Domain == site.Domain {
			if e := rollbackDNS(nil); e != nil {
				log.Printf("DNS rollback for website %s failed: %v", site.ID, e)
			}
		}
	}()
	if err := a.system.ChangeSiteDomain(r.Context(), *site, updated); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := a.store.UpdateSiteDomain(site.ID, input.Domain, updated.Root); err != nil {
		if rollbackErr := a.system.ChangeSiteDomain(context.Background(), updated, *site); rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, "Domain change could not be saved and configuration rollback also failed: "+rollbackErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "Domain change was rolled back because panel state could not be saved.")
		return
	}
	a.store.AddActivity("Website domain changed", site.Domain+" changed to "+input.Domain+"; files and database were preserved", "success")
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
	if r.URL.Query().Get("deleteData") == "true" {
		var confirmation struct {
			Domain string `json:"domain"`
		}
		if decodeJSON(r, &confirmation) != nil || confirmation.Domain != site.Domain {
			writeError(w, 400, "Type the exact website domain to confirm permanent removal")
			return
		}
		if err := a.terminateWebsite(r.Context(), *site); err != nil {
			writeError(w, 409, err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	rollbackDNS, dnsErr := a.prepareWebsiteDNSChange(r.Context(), *site, nil)
	if dnsErr != nil {
		writeError(w, 502, dnsErr.Error())
		return
	}
	defer func() {
		if a.store.SiteByID(site.ID) != nil {
			if e := rollbackDNS(nil); e != nil {
				log.Printf("DNS rollback for website %s failed: %v", site.ID, e)
			}
		}
	}()
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
	job, err := a.enqueueBackup(*site, destination)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
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
	a.store.AddActivity("Settings saved", "Panel preferences were updated", "success")
	writeJSON(w, http.StatusOK, settings)
}

func (a *App) operationsRetentionReview(policy operationsState) map[string]bool {
	review := map[string]bool{}
	if a.system.demo || !policy.Settings.RetentionEnabled {
		return review
	}
	var available []Backup
	for _, backup := range a.store.PublicState().Backups {
		verified := policy.Verification["history:"+backup.ID]
		if verified.Status != "verified" || a.store.SiteByID(backup.SiteID) == nil || !availableHistoryLocal(backup) {
			continue
		}
		info, err := os.Lstat(backup.Path)
		if err == nil && info.Size() == verified.Size && info.ModTime().Equal(verified.ModifiedAt) {
			available = append(available, backup)
		}
	}
	for _, backup := range retentionCandidates(available, policy, time.Now().UTC()) {
		review["history:"+backup.ID] = true
	}
	return review
}

func (a *App) changePassword(w http.ResponseWriter, r *http.Request) {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	var input struct {
		Current string `json:"current"`
		Next    string `json:"next"`
		Code    string `json:"code"`
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
	if !a.reserveLoginAttempt(clientIP(r)) {
		writeError(w, 429, "Too many authentication attempts. Try again later.")
		return
	}
	if !a.store.CheckPassword(username, input.Current) {
		writeError(w, http.StatusUnauthorized, "The current password is incorrect.")
		return
	}
	if a.store.twoFactor().Enabled && a.verifySecondFactor(input.Code, true) != nil {
		writeError(w, http.StatusUnauthorized, "Authentication failed.")
		return
	}
	a.clearLoginFailures(clientIP(r))
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
	limited := &io.LimitedReader{R: r.Body, N: limit + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("invalid request: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("invalid request: only one JSON object is allowed")
	}
	if limited.N == 0 {
		return errors.New("invalid request: JSON body exceeds the size limit")
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

func verifyPassword(encoded, password string) bool {
	if strings.HasPrefix(encoded, "$argon2id$") {
		return verifyArgon2Password(encoded, password)
	}
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
