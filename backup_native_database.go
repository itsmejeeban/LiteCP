package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Only an existing site's own registered databases may be replaced by this path.
func (a *App) nativeBackupDatabasePlan(site Site, dumps []string) (map[string]ManagedDatabase, error) {
	registered := map[string]ManagedDatabase{}
	for _, db := range a.store.PublicState().Databases {
		if db.SiteID == site.ID {
			registered[db.Name] = db
		}
	}
	if site.Database != "" && site.DBUser != "" {
		registered[site.Database] = ManagedDatabase{Name: site.Database, Username: site.DBUser, SiteID: site.ID}
	}
	plan := map[string]ManagedDatabase{}
	seen := map[string]bool{}
	for _, dump := range dumps {
		name := strings.TrimSuffix(filepath.Base(dump), ".sql")
		if name == "database" && len(dumps) == 1 && site.Database != "" {
			name = site.Database
		}
		db, ok := registered[name]
		if !ok || seen[name] {
			return nil, fmt.Errorf("backup database %s cannot be mapped uniquely to this website's registered databases", filepath.Base(dump))
		}
		if err := validateDatabaseObjects(db.Name, db.Username); err != nil {
			return nil, err
		}
		seen[name] = true
		plan[dump] = db
	}
	return plan, nil
}
func (s *System) restoreNativeBackupSQL(ctx context.Context, sqlPath string, database ManagedDatabase) (resultErr error) {
	token, err := randomToken(10)
	if err != nil {
		return err
	}
	password, err := randomToken(32)
	if err != nil {
		return err
	}
	user := "restore_" + strings.ReplaceAll(safeSiteID(token), "-", "_")
	if err = validateDatabaseObjects(database.Name, user); err != nil {
		return err
	}
	// An ephemeral scoped account keeps the original application user/password intact.
	query := fmt.Sprintf("CREATE USER '%s'@'localhost' IDENTIFIED BY '%s'; GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost';", user, password, database.Name, user)
	if _, err = runCommandInput(ctx, time.Minute, query, firstAvailableCommand("mysql", "mariadb"), "--protocol=socket", "--user=root"); err != nil {
		return fmt.Errorf("cannot authorize scoped database restore")
	}
	defer func() {
		_, cleanupErr := runCommandInput(context.Background(), time.Minute, fmt.Sprintf("DROP USER IF EXISTS '%s'@'localhost';", user), firstAvailableCommand("mysql", "mariadb"), "--protocol=socket", "--user=root")
		if cleanupErr != nil {
			resultErr = fmt.Errorf("database restore result: %v; temporary restore account cleanup requires administrator review", resultErr)
		}
	}()
	database.Username = user
	return s.importScopedSQLMode(ctx, sqlPath, database, password, true)
}
