package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type migrationConfigPlan struct {
	Path     string
	Private  bool
	Content  string
	Kind     string
	Database string
	Fields   map[string][]int
}

var migrationPHPStringsAndComments = regexp.MustCompile(`(?s)'(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"|/\*.*?\*/|//[^\r\n]*|#[^\r\n]*`)
var migrationConfigField = regexp.MustCompile(`(?i)(?:\$([a-z_][a-z0-9_]*)\s*=\s*|['"]([a-z_][a-z0-9_]*)['"]\s*=>\s*|define\s*\(\s*['"]([a-z_][a-z0-9_]*)['"]\s*,\s*|\$db\s*\[\s*['"]default['"]\s*\]\s*\[\s*['"]([a-z_][a-z0-9_]*)['"]\s*\]\s*=\s*)('(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*")`)
var migrationEnvField = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?(DB_DATABASE|DB_NAME|DB_USERNAME|DB_USER|DB_PASSWORD|DB_HOST|DB_PORT|DB_SOCKET)[ \t]*=[ \t]*("[^"\r\n]*"|'[^'\r\n]*'|[^\r\n#]*)(?:[ \t]*#[^\r\n]*)?\r?$`)

func migrationConfigKey(key string) string {
	switch strings.ToLower(key) {
	case "db_database", "db_name", "dbname", "db_name_value", "database", "database_name":
		return "name"
	case "db_username", "db_user", "dbuser", "dbusername", "username", "user":
		return "user"
	case "db_password", "db_pass", "dbpass", "dbpassword", "password", "pass":
		return "password"
	case "db_host", "dbhost", "host", "hostname", "servername", "db_server":
		return "host"
	case "db_port", "port":
		return "port"
	case "db_socket", "unix_socket":
		return "socket"
	}
	return ""
}

func inspectMigrationConfig(content, kind string) (migrationConfigPlan, error) {
	result := migrationConfigPlan{Content: content, Kind: kind, Fields: map[string][]int{}}
	if kind == "env" {
		for _, match := range regexp.MustCompile(`(?m)^[ \t]*(?:DB_URL|DATABASE_URL)[ \t]*=([^\r\n]*)`).FindAllStringSubmatch(content, -1) {
			value := strings.Trim(strings.TrimSpace(match[1]), "\"'")
			if value != "" && !strings.HasPrefix(value, "#") {
				return result, fmt.Errorf("database URL needs explicit mapping before migration")
			}
		}
		if match := regexp.MustCompile(`(?m)^[ \t]*DB_CONNECTION[ \t]*=[ \t]*([A-Za-z0-9_]+)`).FindStringSubmatch(content); len(match) == 2 && match[1] != "mysql" && match[1] != "mariadb" {
			return result, fmt.Errorf("non-MySQL Laravel connection needs explicit mapping")
		}
	}

	searchable := content
	if kind == "php" {
		searchable = migrationPHPStringsAndComments.ReplaceAllStringFunc(content, func(value string) string {
			if strings.HasPrefix(value, "/*") || strings.HasPrefix(value, "//") || strings.HasPrefix(value, "#") {
				return strings.Repeat(" ", len(value))
			}
			return value
		})
	}
	scopeStart, scopeEnd := 0, len(searchable)
	if kind == "php" {
		scopeStart, scopeEnd = migrationPHPDatabaseScope(searchable)
		if scopeStart < 0 {
			return result, fmt.Errorf("multiple database arrays require explicit mapping")
		}
	}
	matches := migrationConfigField.FindAllStringSubmatchIndex(searchable, -1)
	if kind == "env" {
		matches = migrationEnvField.FindAllStringSubmatchIndex(searchable, -1)
	}
	values := map[string]string{}
	for _, match := range matches {
		if match[0] < scopeStart || match[0] >= scopeEnd {
			continue
		}
		if kind == "php" {
			insideString := false
			for _, token := range migrationPHPStringsAndComments.FindAllStringIndex(searchable, -1) {
				if token[0] < match[0] && match[0] < token[1] {
					insideString = true
					break
				}
			}
			if insideString {
				continue
			}
		}
		key := ""
		start, end := 0, 0
		if kind == "env" {
			key = content[match[2]:match[3]]
			start, end = match[4], match[5]
		} else {
			for group := 1; group <= 4; group++ {
				if match[group*2] >= 0 {
					key = content[match[group*2]:match[group*2+1]]
					break
				}
			}
			start, end = match[10], match[11]
		}
		field := migrationConfigKey(key)
		if kind == "php" && strings.EqualFold(key, "name") && scopeStart > 0 {
			field = "name"
		}
		if field == "" {
			continue
		}
		if kind == "php" {
			tail := strings.TrimLeft(searchable[end:], " \t\r\n")
			if tail != "" && !strings.ContainsRune(",;)]", rune(tail[0])) {
				return result, fmt.Errorf("dynamic database expression requires explicit mapping")
			}
		}
		value := strings.TrimSpace(content[start:end])
		if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') {
			quote := value[0]
			value = value[1 : len(value)-1]
			if quote == '"' && strings.Contains(value, "$") {
				return result, fmt.Errorf("dynamic database configuration requires manual mapping")
			}
		}
		if _, exists := result.Fields[field]; exists {
			return result, fmt.Errorf("multiple database connection values require explicit mapping")
		}
		result.Fields[field] = []int{start, end}
		values[field] = value
	}
	if values["name"] == "" {
		return result, nil
	}
	for _, key := range []string{"name", "user", "password", "host"} {
		if _, ok := result.Fields[key]; !ok {
			return result, fmt.Errorf("database connection needs literal name, username, password and host")
		}
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_$-]{1,64}$`).MatchString(values["name"]) {
		return result, fmt.Errorf("database name requires manual mapping")
	}
	host := strings.ToLower(values["host"])
	if host != "localhost" && host != "127.0.0.1" && host != "::1" && host != "localhost:3306" && host != "127.0.0.1:3306" {
		return result, fmt.Errorf("external database host requires explicit mapping")
	}
	result.Database = values["name"]
	return result, nil
}

func rewriteGenericMigrationConfig(plan migrationConfigPlan, credentials DBCredentials) string {
	values := map[string]string{"name": credentials.Name, "user": credentials.Username, "password": credentials.Password, "host": "localhost", "port": "3306", "socket": ""}
	type edit struct {
		start, end int
		value      string
	}
	edits := []edit{}
	for key, span := range plan.Fields {
		value := values[key]
		if plan.Kind == "env" {
			value = "\"" + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`).Replace(value) + "\""
		} else {
			value = "'" + strings.NewReplacer(`\`, `\\`, "'", `\'`).Replace(value) + "'"
		}
		edits = append(edits, edit{span[0], span[1], value})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	result := plan.Content
	for _, edit := range edits {
		result = result[:edit.start] + edit.value + result[edit.end:]
	}
	return result
}

// Only inspect configuration-shaped text files, never execute source PHP.
func genericMigrationDatabasePlan(public, private string, dumps []string) ([]migrationConfigPlan, map[string]string, error) {
	databases := map[string]string{}
	for _, dump := range dumps {
		name := strings.TrimSuffix(filepath.Base(dump), ".sql")
		if _, ok := databases[name]; ok {
			return nil, nil, fmt.Errorf("ambiguous database dump %s", filepath.Base(dump))
		}
		databases[name] = dump
	}
	plans := []migrationConfigPlan{}
	files := 0
	for index, root := range []string{public, private} {
		if _, err := os.Lstat(root); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("symbolic link in staged configuration")
			}
			name := strings.ToLower(entry.Name())
			if entry.IsDir() {
				if name == "vendor" || name == "node_modules" || name == ".git" || name == "cache" || name == "upgrade" {
					return filepath.SkipDir
				}
				return nil
			}
			files++
			if files > maxArchiveMembers {
				return fmt.Errorf("too many configuration candidates")
			}
			kind := "php"
			if name == ".env" {
				kind = "env"
			} else if !strings.HasSuffix(name, ".php") || !(strings.Contains(name, "config") || strings.Contains(name, "connect") || strings.Contains(name, "database") || name == "db.php") {
				return nil
			}
			// Shipped example files are preserved, but must not override an
			// accompanying active configuration with placeholder credentials.
			if kind == "php" && migrationConfigExampleHasActiveSibling(path) {
				return nil
			}
			info, e := entry.Info()
			if e != nil {
				return e
			}
			if info.Size() > 2<<20 {
				return nil
			}
			data, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			plan, e := inspectMigrationConfig(string(data), kind)
			// Ignore unrelated config files without a connection. Never guess dynamic values.
			if e != nil {
				if len(plan.Fields) > 0 && plan.Fields["name"] != nil {
					return fmt.Errorf("configuration %s: %w", entry.Name(), e)
				}
				return nil
			}
			if plan.Database == "" {
				return nil
			}
			if _, ok := databases[plan.Database]; !ok {
				return fmt.Errorf("configuration %s has no matching database dump", entry.Name())
			}
			plan.Path, e = filepath.Rel(root, path)
			if e != nil {
				return e
			}
			plan.Private = index == 1
			plans = append(plans, plan)
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	if len(plans) == 0 && !blankMigrationWebsite(public, private) {
		return nil, nil, fmt.Errorf("no supported literal database configuration found; use a DB_DATABASE/DB_USERNAME/DB_PASSWORD/DB_HOST .env connection or a literal PHP configuration")
	}
	return plans, databases, nil
}

func migrationConfigExampleHasActiveSibling(path string) bool {
	name := filepath.Base(path)
	lower := strings.ToLower(name)
	for _, suffix := range []string{".example.php", ".sample.php"} {
		if !strings.HasSuffix(lower, suffix) {
			continue
		}
		base := name[:len(name)-len(suffix)]
		for _, active := range []string{base + ".php", base + ".local.php"} {
			info, err := os.Lstat(filepath.Join(filepath.Dir(path), active))
			if err == nil && info.Mode().IsRegular() {
				return true
			}
		}
	}
	return false
}

// Laravel reads this generated file instead of .env when configuration is
// cached. Preserve the source cache under a unique name rather than executing
// arbitrary artisan code as root or deleting source data.
func preserveLaravelConfigCache(root string, plans []migrationConfigPlan, private bool) error {
	seen := map[string]bool{}
	for _, plan := range plans {
		if plan.Private != private || plan.Kind != "env" {
			continue
		}
		directory := filepath.Join(root, filepath.Dir(plan.Path))
		if seen[directory] {
			continue
		}
		seen[directory] = true
		if info, err := os.Lstat(filepath.Join(directory, "artisan")); err != nil || !info.Mode().IsRegular() {
			continue
		}
		cache := filepath.Join(directory, "bootstrap", "cache", "config.php")
		info, err := os.Lstat(cache)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Laravel configuration cache is not a regular file")
		}
		// Store the original cache outside public_html, where it cannot be
		// downloaded as a non-PHP file containing source credentials.
		recovery, err := os.MkdirTemp(filepath.Dir(root), ".laravel-cache-recovery-")
		if err != nil {
			return err
		}
		if err = os.Rename(cache, filepath.Join(recovery, "config.php")); err != nil {
			return fmt.Errorf("preserve Laravel configuration cache: %w", err)
		}
	}
	return nil
}

// Locate a literal db/database array without executing PHP. Ignore brackets inside
// strings and comments and preserve byte offsets used by credential replacement.
func migrationPHPDatabaseScope(content string) (int, int) {
	pattern := regexp.MustCompile(`(?i)['"](?:db|database)['"]\s*=>\s*\[`)
	scopeStart, scopeEnd := 0, len(content)
	for _, match := range pattern.FindAllStringIndex(content, -1) {
		masked := []byte(content)
		for _, token := range migrationPHPStringsAndComments.FindAllStringIndex(content, -1) {
			if token[0] < match[0] && match[0] < token[1] {
				masked = nil
				break
			}
			for i := token[0]; i < token[1]; i++ {
				masked[i] = ' '
			}
		}
		if masked == nil {
			continue
		}
		depth := 1
		for i := match[1]; i < len(masked); i++ {
			if masked[i] == '[' {
				depth++
			}
			if masked[i] == ']' {
				depth--
				if depth == 0 {
					if scopeStart != 0 {
						return -1, -1
					}
					scopeStart, scopeEnd = match[1], i
					break
				}
			}
		}
	}
	return scopeStart, scopeEnd
}

// An empty document root may legitimately retain PHP settings and unrelated SQL
// dumps. Do not treat an unrecognized application as a blank website.
func blankMigrationWebsite(roots ...string) bool {
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if os.IsNotExist(err) && path == root {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("link")
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("special file")
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			switch relative {
			case ".htaccess", ".user.ini", "php.ini":
				return nil
			}
			return fmt.Errorf("application file")
		})
		if err != nil {
			return false
		}
	}
	return true
}
