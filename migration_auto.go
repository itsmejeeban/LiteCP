package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Configuration is inspected as text; source PHP is never executed as root.
func migrationDBName(content, app string) (string, error) {
	pattern := `(?m)define\s*\(\s*['"]DB_NAME['"]\s*,\s*['"]([A-Za-z0-9_$-]+)['"]\s*\)`
	if app == "whmcs" {
		pattern = `(?m)\$db_name\s*=\s*['"]([A-Za-z0-9_$-]+)['"]\s*;`
	}
	match := regexp.MustCompile(pattern).FindStringSubmatch(content)
	if len(match) != 2 {
		return "", fmt.Errorf("application database name is not a supported literal; manual configuration required")
	}
	return match[1], nil
}
func rewriteMigrationDB(content, app string, credentials DBCredentials) (string, error) {
	values := map[string]string{"DB_NAME": credentials.Name, "DB_USER": credentials.Username, "DB_PASSWORD": credentials.Password, "DB_HOST": "localhost"}
	if app == "whmcs" {
		values = map[string]string{"db_name": credentials.Name, "db_username": credentials.Username, "db_password": credentials.Password, "db_host": "localhost"}
	}
	for key, value := range values {
		escaped := strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `'`, `\'`)
		pattern := `(?m)define\s*\(\s*['"]` + key + `['"]\s*,\s*(?:'[^'\\]*(?:\\.[^'\\]*)*'|"[^"\\]*(?:\\.[^"\\]*)*")\s*\)`
		replacement := "define('" + key + "', '" + escaped + "')"
		if app == "whmcs" {
			pattern = `(?m)\$` + key + `\s*=\s*(?:'[^'\\]*(?:\\.[^'\\]*)*'|"[^"\\]*(?:\\.[^"\\]*)*")\s*;`
			replacement = "$" + key + " = '" + escaped + "';"
		}
		re := regexp.MustCompile(pattern)
		if len(re.FindAllStringIndex(content, -1)) != 1 {
			return "", fmt.Errorf("unsupported or ambiguous %s configuration; manual review required", key)
		}
		content = re.ReplaceAllStringFunc(content, func(string) string { return replacement })
	}
	return content, nil
}

func (a *App) startAutoMigrationRestores() {
	if a.system.demo {
		return
	}
	// Restore workers belong to this process. A saved restoring state
	// at startup has no worker and may contain a partially applied restore.
	entries, err := os.ReadDir(filepath.Join(a.system.dataDir, "live-migrations"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("Cannot inspect migration jobs at startup: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		job, err := a.loadLiveJob(entry.Name())
		if err != nil || job.Status != "restoring" {
			continue
		}
		setLiveJobStage(&job, "restore", "restore-review", "The panel restarted during restore. Review the destination and retained recovery data before retrying. Some changes may already have been applied.")
		if err := a.saveLiveJob(&job); err != nil {
			log.Printf("Cannot record interrupted restore %s: %v", job.ID, err)
		}
	}
	go func() {
		timer := time.NewTicker(10 * time.Second)
		defer timer.Stop()
		for range timer.C {
			if !a.mutationMu.TryLock() {
				continue
			}
			entries, _ := os.ReadDir(filepath.Join(a.system.dataDir, "live-migrations"))
			for _, entry := range entries {
				job, err := a.loadLiveJob(entry.Name())
				if err != nil || job.Source.Platform == "backup" || job.Status != "review" || job.Preview == nil {
					continue
				}
				// Compatible staged captures continue automatically, including jobs
				// created by older builds before AutoRestore was persisted.
				if !job.AutoRestore {
					job.AutoRestore = true
					_ = a.saveLiveJob(&job)
				}
				a.performAutoMigration(&job, false)
			}
			a.startNextQueuedMigration()
			a.mutationMu.Unlock()
		}
	}()
}

func (a *App) autoMigrationAction(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID            string `json:"id"`
		Overwrite     bool   `json:"overwrite"`
		ConfirmDomain string `json:"confirmDomain"`
		Background    bool   `json:"background"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	job, err := a.loadLiveJob(input.ID)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	if job.ArtifactsCleaned || (!job.NativeReady && job.Preview == nil) {
		writeError(w, 409, "Migration files were cleaned. Capture the source account again before restoring.")
		return
	}
	domain := migrationJobDomain(job)
	if incomplete := cpanelDatabaseCaptureIncomplete(job); incomplete || job.Status == "restored-review" {
		if !migrationCommittedCPanelWebsite(job, a.store.SiteByDomain(domain)) {
			if incomplete {
				writeError(w, 409, cpanelIncompleteDatabaseCaptureMessage)
			} else {
				writeError(w, 409, "Health retry requires the original committed migration website.")
			}
			return
		}
	}
	if job.Status == "captured" {
		if err = a.stageRetainedCapture(&job); err != nil {
			writeError(w, 409, err.Error())
			return
		}
	}
	if job.Status != "review" && job.Status != "restore-review" && job.Status != "restored-review" && job.Status != "awaiting-overwrite" {
		writeError(w, 409, "Capture must finish and be ready for restore first.")
		return
	}
	if input.Overwrite && input.ConfirmDomain != domain {
		writeError(w, 400, "Type the exact domain to confirm overwrite.")
		return
	}
	job.AutoRestore = true
	if input.Background {
		// The persistent restore worker serializes work independently of HTTP.
		// Overwrites still need the separate domain-confirmation workflow.
		if input.Overwrite {
			writeError(w, 400, "Use the individual overwrite confirmation.")
			return
		}
		setLiveJobStage(&job, "preflight", "review", "Restore retry queued. Waiting for the background restore worker.")
		if err := a.saveLiveJob(&job); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	a.performAutoMigration(&job, input.Overwrite)
	writeJSON(w, 200, job)
}
func migrationJobDomain(job LiveMigrationJob) string {
	for _, account := range job.Accounts {
		if account.ID == job.Account {
			return strings.ToLower(account.Domain)
		}
	}
	return ""
}

func migrationCommittedCPanelWebsite(job LiveMigrationJob, existing *Site) bool {
	return job.Source.Platform == "cpanel" && existing != nil && job.CPanelWebsiteID != "" && job.CPanelWebsiteID == existing.ID
}

func migrationCanRecreateEmptyDatabases(job LiveMigrationJob, dumps []string) bool {
	return job.Source.Platform == "cpanel" && job.RecreateEmptyDatabases &&
		job.CPanelDatabaseInventoryEmpty && len(dumps) == 0 && !cpanelDatabaseCaptureIncomplete(job)
}

func planEmptyMigrationAppDatabase(content, database, filename string) (migrationConfigPlan, error) {
	plan, err := inspectMigrationConfig(content, "php")
	if err != nil {
		return migrationConfigPlan{}, err
	}
	if plan.Database == "" || plan.Database != database {
		return migrationConfigPlan{}, fmt.Errorf("application needs a supported literal local database configuration")
	}
	if err = validateEmptyMigrationDatabaseConfig(plan); err != nil {
		return migrationConfigPlan{}, err
	}
	plan.Path = filename
	return plan, nil
}

func (a *App) performAutoMigration(job *LiveMigrationJob, overwrite bool) {
	fail := func(err error) {
		if errors.Is(err, context.Canceled) || a.liveMigrationCancelled(job.ID) {
			setLiveJobStage(job, job.Stage, "cancelled", "Migration cancelled. Captured files and source credentials were retained and were not deleted.")
			_ = a.saveLiveJob(job)
			return
		}
		message := "Automatic restore stopped: " + err.Error() + ". Retained backup and existing data were preserved."
		if job.Source.Platform == "backup" {
			message = "Restore stopped: " + err.Error() + ". Backup and recovery files were retained. Review the recovery backup before retrying if database restoration had started."
		} else if job.Source.Platform == "cpanel" && errors.Is(err, errMigrationDatabaseDumpMissing) {
			message += " Verify that the configured database exists on the source, is assigned to this cPanel account, and is included in its backup. Then retry capture."
		}
		setLiveJobStage(job, "restore", "restore-review", message)
		_ = a.saveLiveJob(job)
	}
	if a.liveMigrationCancelled(job.ID) {
		fail(context.Canceled)
		return
	}
	domain := migrationJobDomain(*job)
	if !domainPattern.MatchString(domain) {
		fail(fmt.Errorf("source domain requires manual mapping"))
		return
	}
	existing := a.store.SiteByDomain(domain)
	committed := migrationCommittedCPanelWebsite(*job, existing)
	if cpanelDatabaseCaptureIncomplete(*job) && !committed {
		setLiveJobStage(job, "preflight", "restore-review", cpanelIncompleteDatabaseCaptureMessage)
		_ = a.saveLiveJob(job)
		return
	}
	if existing != nil && !overwrite && !committed {
		setLiveJobStage(job, "preflight", "awaiting-overwrite", "Domain already exists. Review the conflict and explicitly confirm overwrite before migration continues.")
		_ = a.saveLiveJob(job)
		return
	}
	preview, root, err := a.loadMigration(job.ID)
	if err != nil {
		fail(err)
		return
	}
	if job.Source.Platform != "backup" {
		// Retained manifests from older versions may include plugin schema assets.
		// Reclassify those entries before planning imports on a retry.
		exports := make([]string, 0, len(preview.Databases))
		for _, dump := range preview.Databases {
			if isMigrationDatabaseDump(dump, job.Source.Platform) {
				exports = append(exports, dump)
			}
		}
		preview.Databases = exports
	}
	// Resume only the services phase of this exact website after a partial
	// commit. Never re-import SQL or replace newly delivered mail on retry.
	if committed {
		plan, e := planCPanelServices(filepath.Join(root, "files"), *job)
		if e != nil {
			fail(e)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()
		liveJobMu.Lock()
		liveCancels[job.ID] = cancel
		liveJobMu.Unlock()
		defer func() {
			liveJobMu.Lock()
			delete(liveCancels, job.ID)
			liveJobMu.Unlock()
		}()
		setLiveJobStage(job, "restore", "restoring", "Resuming cPanel mailbox and cron import. Website files and databases will not be replaced again.")
		_ = a.saveLiveJob(job)
		if job.Status == "cancelled" || a.liveMigrationCancelled(job.ID) {
			fail(context.Canceled)
			return
		}
		if e = a.repairCommittedMigrationPaths(*job, root, *existing, plan.SourceHome); e != nil {
			fail(e)
			return
		}
		if e = a.system.labelSiteHome(ctx, *existing); e != nil {
			fail(e)
			return
		}
		if e = a.importCPanelServices(ctx, plan, *existing); e != nil {
			fail(e)
			return
		}
		job.Warnings = append(job.Warnings, plan.Warnings...)
		if e = a.importCPanelDNS(ctx, filepath.Join(root, "files"), job, *existing); e != nil {
			fail(e)
			return
		}
		a.finishMigrationHealth(ctx, job, *existing, existing.Root, filepath.Join(root, "files", "private_home"))
		a.finishSuccessfulMigrationCleanup(job)
		return
	}
	if len(preview.Roots) != 1 {
		fail(fmt.Errorf("multiple or missing website roots require manual mapping"))
		return
	}
	recreateEmpty := migrationCanRecreateEmptyDatabases(*job, preview.Databases)
	source := filepath.Join(root, "files", filepath.FromSlash(preview.Roots[0]))
	app, configName := "regular", ""
	if info, e := os.Lstat(filepath.Join(source, "wp-config.php")); e == nil && info.Mode().IsRegular() {
		app, configName = "wordpress", "wp-config.php"
	} else if info, e := os.Lstat(filepath.Join(source, "configuration.php")); e == nil && info.Mode().IsRegular() {
		app, configName = "whmcs", "configuration.php"
	}
	content := ""
	sqlPath := ""
	nativeDatabases := map[string]ManagedDatabase{}
	var customConfigs []migrationConfigPlan
	var customDatabases map[string]string
	if configName != "" {
		data, e := os.ReadFile(filepath.Join(source, configName))
		if e != nil {
			fail(e)
			return
		}
		content = string(data)
		db, e := migrationDBName(content, app)
		if e != nil {
			fail(e)
			return
		}
		for _, dump := range preview.Databases {
			if filepath.Base(dump) == db+".sql" {
				if sqlPath != "" {
					fail(fmt.Errorf("ambiguous database dump"))
					return
				}
				sqlPath = filepath.Join(root, "files", filepath.FromSlash(dump))
			}
		}
		if sqlPath == "" {
			if !recreateEmpty {
				fail(fmt.Errorf("configuration %q has %w", configName, errMigrationDatabaseDumpMissing))
				return
			}
			plan, e := planEmptyMigrationAppDatabase(content, db, configName)
			if e != nil {
				fail(fmt.Errorf("configuration %q: %w", configName, e))
				return
			}
			customConfigs = []migrationConfigPlan{plan}
			customDatabases = map[string]string{db: ""}
		}
	} else if len(preview.Databases) > 0 && job.Source.Platform == "backup" && existing != nil {
		nativeDatabases, err = a.nativeBackupDatabasePlan(*existing, preview.Databases)
		if err != nil {
			fail(err)
			return
		}
	} else {
		private := filepath.Join(root, "files", "private_home")
		if recreateEmpty {
			customConfigs, customDatabases, err = planGenericMigrationDatabases(source, private, preview.Databases, true)
		} else {
			customConfigs, customDatabases, err = genericMigrationDatabasePlan(source, private, preview.Databases)
		}
		if recreateEmpty && errors.Is(err, errMigrationDatabaseConfigMissing) {
			warning := "No supported local database configuration was found in this cPanel account. Website files will be restored with their original configuration and no database mapping."
			if !containsString(job.Warnings, warning) {
				job.Warnings = append(job.Warnings, warning)
			}
		} else if err != nil {
			fail(err)
			return
		}
	}
	var cpanelPlan cpanelServicesPlan
	if job.Source.Platform == "cpanel" {
		cpanelPlan, err = planCPanelServices(filepath.Join(root, "files"), *job)
		if err == nil {
			err = a.preflightCPanelServices(cpanelPlan)
		}
		if err != nil {
			fail(err)
			return
		}
	}
	baseCtx, timeoutCancel := context.WithTimeout(context.Background(), 45*time.Minute)
	ctx, cancel := context.WithCancel(baseCtx)
	liveJobMu.Lock()
	liveCancels[job.ID] = cancel
	liveJobMu.Unlock()
	defer func() {
		cancel()
		timeoutCancel()
		liveJobMu.Lock()
		delete(liveCancels, job.ID)
		liveJobMu.Unlock()
	}()
	setLiveJobStage(job, "restore", "restoring", "Restoring website files and database. DNS remains unchanged.")
	_ = a.saveLiveJob(job)
	if existing != nil {
		backup, e := a.system.CreateBackup(ctx, *existing)
		if e != nil {
			fail(fmt.Errorf("recovery backup failed: %w", e))
			return
		}
		if e = a.store.AddBackup(backup); e != nil {
			fail(e)
			return
		}
	}
	var site Site
	if existing == nil {
		site, _, err = a.system.CreateSite(ctx, CreateSiteInput{Domain: domain, AppType: app, AppVersion: "9.0", PHPVersion: "8.3"})
		if err != nil {
			fail(err)
			return
		}
		if err = a.store.AddSiteWithDatabase(site, nil); err != nil {
			fail(err)
			return
		}
	} else {
		site = *existing
	}
	stage, err := os.MkdirTemp(filepath.Dir(site.Root), ".auto-migration-")
	if err != nil {
		fail(err)
		return
	}
	if err = os.Chmod(stage, 0750); err != nil {
		fail(err)
		return
	}
	if err = os.Chown(stage, site.RuntimeUID, site.RuntimeGID); err != nil {
		fail(err)
		return
	}
	staged := site
	staged.Root = stage
	if err = copyMigrationTree(ctx, source, staged, site.RuntimeUID, site.RuntimeGID); err != nil {
		fail(err)
		return
	}
	// cPanel applications may keep non-public source beside public_html. The
	// exporter places only application-owned sibling directories here; account
	// mail, SSH material, logs and other cPanel-managed data are excluded.
	privateStage := ""
	privateEntries := []os.DirEntry{}
	privateSource := filepath.Join(root, "files", "private_home")
	if job.Source.Platform == "backup" {
		privateSource = filepath.Join(root, "files", "home")
	}
	if privateInfo, privateErr := os.Lstat(privateSource); privateErr == nil {
		if !privateInfo.IsDir() || privateInfo.Mode()&os.ModeSymlink != 0 {
			fail(fmt.Errorf("private application data has an unsafe layout"))
			return
		}
		privateEntries, err = os.ReadDir(privateSource)
		if err != nil {
			fail(err)
			return
		}
		home := filepath.Dir(site.Root)
		for _, entry := range privateEntries {
			if err := validateMigrationPrivateEntry(entry); err != nil {
				fail(err)
				return
			}
			if _, statErr := os.Lstat(filepath.Join(home, entry.Name())); statErr == nil && job.Source.Platform != "backup" {
				fail(fmt.Errorf("private application directory %s already exists; existing data was preserved", entry.Name()))
				return
			} else if statErr != nil && !os.IsNotExist(statErr) {
				fail(statErr)
				return
			}
		}
		privateStage, err = os.MkdirTemp(home, ".auto-private-migration-")
		if err != nil {
			fail(err)
			return
		}
		privateSite := site
		privateSite.Root = privateStage
		if err = copyMigrationTree(ctx, privateSource, privateSite, site.RuntimeUID, site.RuntimeGID); err != nil {
			fail(err)
			return
		}
		if err = chownSiteTree(privateStage, site, a.system.webUser()); err != nil {
			fail(err)
			return
		}
	} else if !os.IsNotExist(privateErr) {
		fail(privateErr)
		return
	}
	var database *ManagedDatabase
	if sqlPath != "" {
		credentials, e := a.system.createDatabase(ctx, domain, site.PHPVersion)
		if e != nil {
			fail(e)
			return
		}
		id, e := randomToken(8)
		if e != nil {
			fail(e)
			return
		}
		value := ManagedDatabase{ID: id, SiteID: site.ID, Domain: domain, Name: credentials.Name, Username: credentials.Username, CreatedAt: time.Now().UTC()}
		database = &value
		if e = a.store.AddDatabase(value); e != nil {
			fail(e)
			return
		}
		if e = a.system.importScopedSQL(ctx, sqlPath, value, credentials.Password); e != nil {
			fail(e)
			return
		}
		rewritten, e := rewriteMigrationDB(content, app, credentials)
		if e != nil {
			fail(e)
			return
		}
		if e = writeAtomic(filepath.Join(stage, configName), []byte(rewritten), 0640); e != nil {
			fail(e)
			return
		}
		if e = os.Chown(filepath.Join(stage, configName), site.RuntimeUID, site.RuntimeGID); e != nil {
			fail(e)
			return
		}
	}
	// Each migrated schema gets a fresh scoped account; original source
	// credentials are never copied into the destination database server.
	if len(customDatabases) > 0 {
		credentialsByName := map[string]DBCredentials{}
		names := make([]string, 0, len(customDatabases))
		for name := range customDatabases {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			credentials, e := a.system.createDatabase(ctx, domain, site.PHPVersion)
			if e != nil {
				fail(e)
				return
			}
			id, e := randomToken(8)
			if e != nil {
				fail(e)
				return
			}
			value := ManagedDatabase{ID: id, SiteID: site.ID, Domain: domain, Name: credentials.Name, Username: credentials.Username, CreatedAt: time.Now().UTC()}
			if e = a.store.AddDatabase(value); e != nil {
				fail(e)
				return
			}
			if dump := customDatabases[name]; dump != "" {
				if e = a.system.importScopedSQL(ctx, filepath.Join(root, "files", filepath.FromSlash(dump)), value, credentials.Password); e != nil {
					fail(e)
					return
				}
			}
			credentialsByName[name] = credentials
			if len(customConfigs) > 0 && name == customConfigs[0].Database {
				copy := value
				database = &copy
			}
		}
		for _, plan := range customConfigs {
			targetRoot := stage
			if plan.Private {
				targetRoot = privateStage
			}
			if targetRoot == "" {
				fail(fmt.Errorf("private configuration staging is unavailable"))
				return
			}
			target := filepath.Join(targetRoot, plan.Path)
			rewritten := rewriteGenericMigrationConfig(plan, credentialsByName[plan.Database])
			if err = writeAtomic(target, []byte(rewritten), 0640); err != nil {
				fail(err)
				return
			}
			if err = os.Chown(target, site.RuntimeUID, site.RuntimeGID); err != nil {
				fail(err)
				return
			}
		}
		if recreateEmpty {
			warning := fmt.Sprintf("Recreated %d local database(s) as empty with new credentials. The source cPanel database inventory was verified empty and contained no SQL to import.", len(customDatabases))
			if !containsString(job.Warnings, warning) {
				job.Warnings = append(job.Warnings, warning)
			}
		}
	}
	if job.Source.Platform == "cpanel" {
		recovery, e := a.liveJobRoot(job.ID)
		if e == nil {
			e = relocateMigrationHomePaths([]string{stage, privateStage}, cpanelPlan.SourceHome, filepath.ToSlash(managedSiteRoot(site)), recovery, site.RuntimeUID, site.RuntimeGID)
		}
		if e != nil {
			fail(e)
			return
		}
	}
	if err = preserveLaravelConfigCache(stage, customConfigs, false); err != nil {
		fail(err)
		return
	}
	if privateStage != "" {
		if err = preserveLaravelConfigCache(privateStage, customConfigs, true); err != nil {
			fail(err)
			return
		}
	}
	for dump, targetDB := range nativeDatabases {
		if err = a.system.restoreNativeBackupSQL(ctx, filepath.Join(root, "files", filepath.FromSlash(dump)), targetDB); err != nil {
			fail(err)
			return
		}
	}
	if app == "whmcs" {
		if err = a.restoreWHMCSPrivate(ctx, job.ID, site, filepath.Join(stage, configName)); err != nil {
			fail(err)
			return
		}
	}
	if err = chownSiteTree(stage, site, a.system.webUser()); err != nil {
		fail(err)
		return
	}
	if err = ctx.Err(); err != nil || a.liveMigrationCancelled(job.ID) {
		fail(context.Canceled)
		return
	}
	if job.Source.Platform == "backup" && privateStage != "" {
		if err = preserveRestoreBackupRepositories(filepath.Dir(site.Root), privateStage); err != nil {
			fail(fmt.Errorf("cannot preserve existing backup repositories: %w", err))
			return
		}
	}
	// The remaining renames and state updates form the atomic commit. Do not
	// interrupt them, because stopping halfway could leave the website split
	// between its old and restored trees.
	setLiveJobStage(job, "restore", "committing", "Applying the verified website restore.")
	if err = a.saveLiveJob(job); err != nil || job.Status == "cancelled" {
		if err == nil {
			err = context.Canceled
		}
		fail(err)
		return
	}
	liveJobMu.Lock()
	delete(liveCancels, job.ID)
	liveJobMu.Unlock()
	// Rename rather than delete: previous files remain available for recovery.
	previous := stage + "-previous"
	if err = os.Rename(site.Root, previous); err != nil {
		fail(err)
		return
	}
	if err = os.Rename(stage, site.Root); err != nil {
		_ = os.Rename(previous, site.Root)
		fail(err)
		return
	}
	movedPrivate := []string{}
	privatePrevious := map[string]string{}
	if job.Source.Platform == "backup" && len(privateEntries) > 0 {
		recovery, e := os.MkdirTemp(filepath.Dir(site.Root), ".restore-home-previous-")
		if e != nil {
			_ = os.Rename(site.Root, stage)
			_ = os.Rename(previous, site.Root)
			fail(e)
			return
		}
		for _, entry := range privateEntries {
			current := filepath.Join(filepath.Dir(site.Root), entry.Name())
			if _, e = os.Lstat(current); os.IsNotExist(e) {
				continue
			} else if e != nil {
				for name, old := range privatePrevious {
					_ = os.Rename(old, filepath.Join(filepath.Dir(site.Root), name))
				}
				_ = os.Rename(site.Root, stage)
				_ = os.Rename(previous, site.Root)
				fail(e)
				return
			}
			saved := filepath.Join(recovery, entry.Name())
			if e = os.Rename(current, saved); e != nil {
				for name, old := range privatePrevious {
					_ = os.Rename(old, filepath.Join(filepath.Dir(site.Root), name))
				}
				_ = os.Rename(site.Root, stage)
				_ = os.Rename(previous, site.Root)
				fail(e)
				return
			}
			privatePrevious[entry.Name()] = saved
		}
	}
	for _, entry := range privateEntries {
		from := filepath.Join(privateStage, entry.Name())
		to := filepath.Join(filepath.Dir(site.Root), entry.Name())
		if err = os.Rename(from, to); err != nil {
			for index := len(movedPrivate) - 1; index >= 0; index-- {
				name := movedPrivate[index]
				_ = os.Rename(filepath.Join(filepath.Dir(site.Root), name), filepath.Join(privateStage, name))
			}
			_ = os.Rename(site.Root, stage)
			for name, old := range privatePrevious {
				_ = os.Rename(old, filepath.Join(filepath.Dir(site.Root), name))
			}
			_ = os.Rename(previous, site.Root)
			fail(fmt.Errorf("restore private application directory %s: %w", entry.Name(), err))
			return
		}
		movedPrivate = append(movedPrivate, entry.Name())
	}
	if err = a.system.labelSiteHome(ctx, site); err != nil {
		fail(err)
		return
	}
	if err = a.store.UpdateSite(site.ID, func(value *Site) {
		if database != nil {
			value.Database = database.Name
			value.DBUser = database.Username
		}
		value.AppType = app
	}); err != nil {
		fail(err)
		return
	}
	site.AppType = app
	if job.Source.Platform == "cpanel" {
		job.CPanelWebsiteID = site.ID
		if err = a.saveLiveJob(job); err != nil {
			fail(err)
			return
		}
	}

	if app != "regular" && len(cpanelPlan.Cron) == 0 {
		if err = a.system.SetCron(site, true); err != nil {
			fail(err)
			return
		}
		_ = a.store.UpdateSite(site.ID, func(value *Site) { value.Cron = true })
	}
	if job.Source.Platform == "cpanel" {
		if err = a.importCPanelServices(ctx, cpanelPlan, site); err != nil {
			setLiveJobStage(job, "restore", "restore-review", "Website restored; mailbox/cron import needs attention: "+err.Error()+". Destination data and recovery files were retained.")
			_ = a.saveLiveJob(job)
			return
		}
		job.Warnings = append(job.Warnings, cpanelPlan.Warnings...)
		job.Warnings = append(job.Warnings, cpanelServiceSummary(cpanelPlan))
	}
	if job.Source.Platform == "cpanel" {
		if err = a.importCPanelDNS(ctx, filepath.Join(root, "files"), job, site); err != nil {
			fail(err)
			return
		}
	}
	a.finishMigrationHealth(ctx, job, site, source, privateSource)
	a.finishSuccessfulMigrationCleanup(job)
	a.store.AddActivity("Automatic website restore", domain+": "+job.Status, "info")
}

func migrationHealthCurl(site Site) (string, []string) {
	scheme, port := "http", "80"
	if site.SSL {
		scheme, port = "https", "443"
	}
	// Pin the virtual host to loopback and let curl validate the TLS hostname
	// and certificate chain. Ignore proxy settings and curl configuration.
	return strings.ToUpper(scheme), []string{
		"--disable", "--silent", "--noproxy", "*", "--output", "/dev/null",
		"--write-out", "%{http_code}", "--max-time", "20",
		"--resolve", site.Domain + ":" + port + ":127.0.0.1", scheme + "://" + site.Domain + "/",
	}
}

func (a *App) finishMigrationHealth(ctx context.Context, job *LiveMigrationJob, site Site, roots ...string) {
	protocol, args := migrationHealthCurl(site)
	code, e := retryMigrationHealth(ctx, 2*time.Second, func() (string, error) {
		return runCommand(ctx, 30*time.Second, "curl", args...)
	})
	recordMigrationHealth(job, protocol, code, e, roots...)
}

func recordMigrationHealth(job *LiveMigrationJob, protocol, code string, probeErr error, roots ...string) {
	status, parseErr := strconv.Atoi(strings.TrimSpace(code))
	// A truly blank root has no index; Nginx correctly refuses directory listing.
	if probeErr == nil && status == 403 && len(roots) > 0 && blankMigrationWebsite(roots...) {
		setLiveJobStage(job, "result", "restored", "Blank website and included databases restored. HTTP 403 is expected because the website has no index file; directory listing remains disabled.")
		return
	}
	if probeErr != nil || parseErr != nil || status < 200 || status >= 400 {
		message := "Website files and database restored. The local " + protocol + " health check did not receive a valid response."
		if probeErr == nil && status >= 400 && status <= 599 {
			message = fmt.Sprintf("Website files and database restored. The local %s health check returned %d.", protocol, status)
		}
		if protocol == "HTTP" && probeErr == nil && status == 400 {
			message += " If the application requires HTTPS, configure SSL for the restored website and retry the check."
		} else if protocol == "HTTPS" && probeErr != nil {
			message += " Check the destination TLS listener and certificate before retrying."
		}
		setLiveJobStage(job, "result", "restored-review", message+" Review application logs before DNS cutover. Previous files are retained for recovery.")
		return
	}
	setLiveJobStage(job, "result", "restored", fmt.Sprintf("Website files and database restored. Local %s status: %d. Review application pages, mail delivery, DNS delegation and SSL before cutover. Previous files are retained for recovery.", protocol, status))
	if job.Source.Platform != "cpanel" && job.Source.Platform != "backup" {
		job.Message += " Mail and source cron jobs require separate configuration."
	}
}

// The exporter captures both private application directories and standalone
// files beside public_html. Links and device/socket entries remain forbidden.
func validateMigrationPrivateEntry(entry os.DirEntry) error {
	if entry.Name() == "" || entry.Name() == "." || entry.Name() == ".." || entry.Type()&os.ModeSymlink != 0 || (!entry.IsDir() && !entry.Type().IsRegular()) {
		return fmt.Errorf("private application data contains an unsupported top-level entry: %s", entry.Name())
	}
	return nil
}

// Nginx reloads and restored application workers may need a short settling
// period. Never treat a persistent error as success or rewrite website data.
func retryMigrationHealth(ctx context.Context, delay time.Duration, probe func() (string, error)) (string, error) {
	var code string
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		if ctx.Err() != nil {
			return code, ctx.Err()
		}
		code, err = probe()
		status, parseErr := strconv.Atoi(strings.TrimSpace(code))
		if err == nil && parseErr == nil && status >= 200 && status < 400 {
			return code, nil
		}
		if attempt < 3 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return code, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return code, err
}
