package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func relocateMigrationHomeText(content, kind, sourceHome, destinationHome string) string {
	replace := func(value string) string {
		if strings.HasPrefix(value, sourceHome+"/") {
			return destinationHome + value[len(sourceHome):]
		}
		return value
	}
	if kind == ".php" {
		return migrationPHPStringsAndComments.ReplaceAllStringFunc(content, func(token string) string {
			if len(token) < 2 || (token[0] != '\'' && token[0] != '"') {
				return token
			}
			return token[:1] + replace(token[1:len(token)-1]) + token[len(token)-1:]
		})
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(strings.TrimSpace(key), "#") || strings.HasPrefix(strings.TrimSpace(key), ";") {
			continue
		}
		trimmed := strings.TrimLeft(value, " \t")
		prefix := value[:len(value)-len(trimmed)]
		if len(trimmed) > 0 && (trimmed[0] == '\'' || trimmed[0] == '"') {
			trimmed = trimmed[:1] + replace(trimmed[1:])
		} else {
			trimmed = replace(trimmed)
		}
		lines[i] = key + "=" + prefix + trimmed
	}
	return strings.Join(lines, "\n")
}

// Relocate literal source-home references, preserving originals outside the
// document root. This never evaluates PHP, changes SQL, or relaxes open_basedir.
// roots are either fresh staging trees or the exact committed migration site.
func relocateMigrationHomePaths(roots []string, sourceHome, destinationHome, recoveryParent string, uid, gid int) error {
	if sourceHome == "" || sourceHome == destinationHome {
		return nil
	}
	if !path.IsAbs(sourceHome) || path.Clean(sourceHome) != sourceHome || sourceHome == "/" || destinationHome == "" || strings.ContainsAny(sourceHome, "\x00\r\n\\") {
		return fmt.Errorf("unsupported source home path")
	}
	type change struct {
		root, relative, text string
		original             []byte
		mode                 os.FileMode
	}
	var changes []change
	var scanned int64
	for _, root := range roots {
		if root == "" {
			continue
		}
		writeRoot := root
		if info, err := os.Lstat(root); err != nil {
			return err
		} else if info.Mode().IsRegular() {
			writeRoot = filepath.Dir(root)
		}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("symbolic link in application path repair")
			}
			name := strings.ToLower(entry.Name())
			if entry.IsDir() {
				if name == "vendor" || name == "node_modules" || name == ".git" || name == "cache" || name == "storage" {
					return filepath.SkipDir
				}
				return nil
			}
			kind := strings.ToLower(filepath.Ext(name))
			if kind != ".php" && kind != ".ini" && name != ".env" {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("special application configuration")
			}
			relative, err := filepath.Rel(writeRoot, path)
			if err != nil {
				return err
			}
			// Most application code (including large encoded PHP files) needs no
			// relocation. Search with bounded memory before decoding/re-writing it.
			matches, err := migrationSourceContainsHome(writeRoot, relative, sourceHome)
			if err != nil {
				return err
			}
			if !matches && relative != "index.php" {
				return nil
			}
			if !matches && info.Size() > 2<<20 {
				return nil
			}
			scanned += info.Size()
			if info.Size() > 2<<20 || scanned > 64<<20 {
				return fmt.Errorf("application path repair requires review for %s: configuration rewrite size limit reached", relative)
			}
			data, info, err := readMigrationSource(writeRoot, relative)
			if err != nil {
				return err
			}
			text := relocateMigrationHomeText(string(data), kind, sourceHome, destinationHome)
			if relative == "index.php" {
				text = repairMigrationLocalBootstrap(text, writeRoot, destinationHome)
			}
			if text == string(data) {
				return nil
			}
			changes = append(changes, change{writeRoot, relative, text, data, info.Mode().Perm()})
			return nil
		})
		if err != nil {
			return err
		}
	}
	if len(changes) == 0 {
		return nil
	}
	recovery, err := os.MkdirTemp(recoveryParent, ".path-recovery-")
	if err != nil {
		return err
	}
	// Preserve every original before modifying any active file.
	for i, item := range changes {
		if err = os.WriteFile(filepath.Join(recovery, fmt.Sprintf("%06d.original", i)), item.original, 0600); err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(recovery, fmt.Sprintf("%06d.path", i)), []byte(filepath.Join(item.root, item.relative)), 0600); err != nil {
			return err
		}
	}
	for _, item := range changes {
		if _, err = secureWriteManaged(item.root, item.relative, strings.NewReader(item.text), int64(len(item.text)), item.mode, uid, gid); err != nil {
			return err
		}
	}
	return nil
}

func migrationSourceContainsHome(root, relative, sourceHome string) (bool, error) {
	file, err := secureOpenManaged(root, filepath.ToSlash(relative), os.O_RDONLY, 0)
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("special application configuration")
	}
	needle := []byte(sourceHome + "/")
	buffer := make([]byte, 64<<10+len(needle))
	reader := io.LimitReader(file, info.Size()+1)
	carry := 0
	for {
		n, readErr := reader.Read(buffer[carry:])
		data := buffer[:carry+n]
		if bytes.Contains(data, needle) {
			return true, nil
		}
		if readErr != nil {
			if readErr == io.EOF {
				return false, nil
			}
			return false, readErr
		}
		carry = len(needle) - 1
		if carry > len(data) {
			carry = len(data)
		}
		copy(buffer, data[len(data)-carry:])
	}
}

func (a *App) repairCommittedMigrationPaths(job LiveMigrationJob, stagedRoot string, site Site, sourceHome string) error {
	roots := []string{site.Root}
	entries, err := os.ReadDir(filepath.Join(stagedRoot, "files", "private_home"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if err := validateMigrationPrivateEntry(entry); err != nil {
			return err
		}
		target := filepath.Join(managedSiteRoot(site), entry.Name())
		if _, err := os.Lstat(target); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		roots = append(roots, target)
	}
	recovery, err := a.liveJobRoot(job.ID)
	if err != nil {
		return err
	}
	return relocateMigrationHomePaths(roots, sourceHome, filepath.ToSlash(managedSiteRoot(site)), recovery, site.RuntimeUID, site.RuntimeGID)
}

// Repair only the known literal bootstrap layout, without evaluating source PHP.
// The existing path-recovery transaction preserves the original entry point.
func repairMigrationLocalBootstrap(content, root, destinationHome string) string {
	if _, err := os.Lstat(filepath.Join(destinationHome, "app", "bootstrap.php")); !os.IsNotExist(err) {
		return content
	}
	if _, _, err := readMigrationSource(root, "app/bootstrap.php"); err != nil {
		return content
	}
	for _, quote := range []string{"'", "\""} {
		old := "require dirname(__DIR__) . " + quote + "/app/bootstrap.php" + quote + ";"
		content = strings.Replace(content, old, "require __DIR__ . "+quote+"/app/bootstrap.php"+quote+";", 1)
	}
	return content
}
