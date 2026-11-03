package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Accept only inert local MySQL PDO connections. Never evaluate source PHP or
// silently redirect an external database to this server.
func migrationMySQLDSN(dsn string) (map[string]string, error) {
	parts := map[string]string{}
	if !strings.HasPrefix(dsn, "mysql:") || strings.ContainsAny(dsn, "\\$\r\n\x00") {
		return nil, fmt.Errorf("database DSN requires a literal local MySQL connection")
	}
	for _, part := range strings.Split(strings.TrimPrefix(dsn, "mysql:"), ";") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		pair := strings.SplitN(part, "=", 2)
		if len(pair) != 2 {
			return nil, fmt.Errorf("invalid database DSN")
		}
		key, value := strings.TrimSpace(pair[0]), strings.TrimSpace(pair[1])
		if _, exists := parts[key]; exists {
			return nil, fmt.Errorf("duplicate database DSN option")
		}
		switch key {
		case "host":
			if value != "localhost" && value != "127.0.0.1" && value != "::1" {
				return nil, fmt.Errorf("external database DSN requires explicit mapping")
			}
		case "dbname":
			if !regexp.MustCompile(`^[A-Za-z0-9_$-]{1,64}$`).MatchString(value) {
				return nil, fmt.Errorf("invalid database DSN name")
			}
		case "port":
			if value != "3306" {
				return nil, fmt.Errorf("non-default database DSN port requires explicit mapping")
			}
		case "charset":
			if !regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`).MatchString(value) {
				return nil, fmt.Errorf("invalid database DSN charset")
			}
		default:
			return nil, fmt.Errorf("unsupported database DSN option")
		}
		parts[key] = value
	}
	if parts["host"] == "" || parts["dbname"] == "" {
		return nil, fmt.Errorf("database DSN needs literal host and database name")
	}
	return parts, nil
}

var migrationDatabaseReference = regexp.MustCompile(`(?i)\b(?:PDO|mysqli?\w*|pg_connect|sqlite\w*|odbc\w*|DB_[A-Z_]+|DATABASE_URL|DBAL|Eloquent)\b|(?:mysql|pgsql|sqlite):|['"](?:dsn|database|db_name|dbhost|dbuser)['"]\s*=>`)

var migrationLiteralInclude = regexp.MustCompile(`(?i)\b(?:require_once|include_once|require|include)\s*\(?\s*['"]([^'"\r\n]+)['"]`)

// Open relative to the managed tree, refusing links and special files even if
// the application changes a path while it is being inspected.
func readMigrationSource(root, relative string) ([]byte, os.FileInfo, error) {
	file, err := secureOpenManaged(root, filepath.ToSlash(relative), os.O_RDONLY, 0)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 2<<20 {
		return nil, nil, fmt.Errorf("unsupported application source or inspection limit")
	}
	data, err := io.ReadAll(io.LimitReader(file, (2<<20)+1))
	if err == nil && len(data) > 2<<20 {
		err = fmt.Errorf("application source inspection limit")
	}
	return data, info, err
}

// Some accounts retain unused SQL exports even though the site is static or
// file/session based. Preserve/import those dumps without inventing application
// credentials. Unknown frameworks, oversized source and links still need review.
func migrationHasNoDatabaseReferences(roots ...string) bool {
	var scanned int64
	files := 0
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
			name := strings.ToLower(entry.Name())
			if entry.IsDir() {
				if name == ".git" || name == "node_modules" || name == "perl5" {
					return filepath.SkipDir
				}
				if name == "vendor" {
					return fmt.Errorf("framework needs connection mapping")
				}
				return nil
			}
			files++
			if files > maxArchiveMembers || !entry.Type().IsRegular() {
				return fmt.Errorf("unsupported source")
			}
			if !strings.HasSuffix(name, ".php") && !strings.HasPrefix(name, ".env") && !strings.HasSuffix(name, ".ini") {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			scanned += info.Size()
			if info.Size() > 2<<20 || scanned > 64<<20 {
				return fmt.Errorf("source inspection limit")
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			data, _, err := readMigrationSource(root, relative)
			if err != nil {
				return err
			}
			if migrationDatabaseReference.Match(data) {
				return fmt.Errorf("database reference needs mapping")
			}
			// An incomplete application cannot be classified as a standalone
			// file-based site just because its missing configuration was not read.
			if strings.HasSuffix(name, ".php") {
				for _, match := range migrationLiteralInclude.FindAllSubmatch(data, -1) {
					include := string(match[1])
					if filepath.IsAbs(include) || strings.ContainsAny(include, "\\$:") {
						return fmt.Errorf("application dependency needs review")
					}
					dependency, err := filepath.Rel(root, filepath.Join(filepath.Dir(path), include))
					if err != nil {
						return err
					}
					f, err := secureOpenManaged(root, filepath.ToSlash(dependency), os.O_RDONLY, 0)
					if err != nil {
						return fmt.Errorf("application dependency needs review")
					}
					f.Close()
				}
			}
			return nil
		})
		if err != nil {
			return false
		}
	}
	return true
}
