package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var databaseObjectPattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
var backupHostKeyPattern = regexp.MustCompile(`^(?:SHA256:)?[A-Za-z0-9+/]{43}=?$`)

func (s *System) EncryptSecret(plaintext string) (string, error) {
	key, err := s.secretKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), []byte("litecp-backup-v1"))
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *System) DecryptSecret(ciphertext string) (string, error) {
	key, err := s.secretKey()
	if err != nil {
		return "", err
	}
	sealed, err := base64.RawURLEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", errors.New("backup credential is corrupted")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", errors.New("backup credential is corrupted")
	}
	plaintext, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte("litecp-backup-v1"))
	if err != nil {
		return "", errors.New("backup credential could not be decrypted")
	}
	return string(plaintext), nil
}

func (s *System) secretKey() ([]byte, error) {
	s.secretMu.Lock()
	defer s.secretMu.Unlock()

	path := filepath.Join(s.dataDir, "secret.key")
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != 32 {
			return nil, errors.New("LiteCP secret key has an invalid length")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read secret key: %w", err)
	}
	if err := os.MkdirAll(s.dataDir, 0700); err != nil {
		return nil, err
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("create secret key: %w", err)
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return key, nil
}

func validateBackupDestination(destination BackupDestination, passwordRequired bool) error {
	destination.Name = strings.TrimSpace(destination.Name)
	destination.Protocol = strings.ToLower(strings.TrimSpace(destination.Protocol))
	destination.Host = strings.TrimSpace(destination.Host)
	destination.Username = strings.TrimSpace(destination.Username)
	destination.RemotePath = strings.TrimSpace(destination.RemotePath)
	if destination.Name == "" || len(destination.Name) > 64 {
		return errors.New("destination name must contain 1 to 64 characters")
	}
	if destination.Protocol != "ftp" && destination.Protocol != "ftps" && destination.Protocol != "sftp" {
		return errors.New("backup protocol must be FTP, FTPS or SFTP")
	}
	if destination.Host == "" || len(destination.Host) > 253 || strings.ContainsAny(destination.Host, "/\r\n\t ") {
		return errors.New("enter a valid backup server hostname")
	}
	if destination.Port < 1 || destination.Port > 65535 {
		return errors.New("backup server port must be between 1 and 65535")
	}
	if destination.Username == "" || len(destination.Username) > 128 || strings.ContainsAny(destination.Username, "\r\n") {
		return errors.New("enter a valid backup username")
	}
	if destination.RemotePath == "" || !strings.HasPrefix(destination.RemotePath, "/") || strings.ContainsAny(destination.RemotePath, "\r\n") {
		return errors.New("remote backup path must be an absolute path")
	}
	if passwordRequired && destination.PasswordCipher == "" {
		return errors.New("backup destination password is required")
	}
	if destination.Protocol == "sftp" && !backupHostKeyPattern.MatchString(strings.TrimSpace(destination.HostKeySHA256)) {
		return errors.New("SFTP requires the server SHA256 host-key fingerprint")
	}
	return nil
}

func (s *System) UploadBackup(ctx context.Context, destination BackupDestination, localPath string) (string, error) {
	if err := validateBackupDestination(destination, true); err != nil {
		return "", err
	}
	if s.demo {
		return destination.Protocol + "://" + destination.Host + path.Join(destination.RemotePath, filepath.Base(localPath)), nil
	}
	password, err := s.DecryptSecret(destination.PasswordCipher)
	if err != nil {
		return "", err
	}
	remotePath := path.Join(destination.RemotePath, filepath.Base(localPath))
	remoteURL := (&url.URL{Scheme: destination.Protocol, Host: net.JoinHostPort(destination.Host, strconv.Itoa(destination.Port)), Path: remotePath}).String()
	config, err := os.CreateTemp(s.dataDir, ".curl-backup-*.conf")
	if err != nil {
		return "", err
	}
	configPath := config.Name()
	defer os.Remove(configPath)
	_ = os.Chmod(configPath, 0600)
	lines := []string{
		`fail`, `silent`, `show-error`, `connect-timeout = 20`, `retry = 2`,
		`user = "` + curlConfigEscape(destination.Username+":"+password) + `"`,
		`url = "` + curlConfigEscape(remoteURL) + `"`,
		`upload-file = "` + curlConfigEscape(localPath) + `"`,
	}
	if destination.Protocol == "ftps" {
		lines = append(lines, "ssl-reqd")
	}
	if destination.Protocol == "sftp" {
		fingerprint := strings.TrimPrefix(destination.HostKeySHA256, "SHA256:")
		lines = append(lines, `hostpubsha256 = "`+curlConfigEscape(fingerprint)+`"`)
	}
	if _, err := config.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		_ = config.Close()
		return "", err
	}
	if err := config.Close(); err != nil {
		return "", err
	}
	if _, err := runCommand(ctx, 30*time.Minute, "curl", "--config", configPath); err != nil {
		return "", fmt.Errorf("upload backup to %s: %w", destination.Name, err)
	}
	return remoteURL, nil
}

func curlConfigEscape(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func validateDatabaseObjects(name, username string) error {
	if name == "" || len(name) > 64 || !databaseObjectPattern.MatchString(name) {
		return errors.New("database name may contain only letters, numbers and underscores and must be at most 64 characters")
	}
	if username == "" || len(username) > 32 || !databaseObjectPattern.MatchString(username) {
		return errors.New("database username may contain only letters, numbers and underscores and must be at most 32 characters")
	}
	return nil
}

func validateDatabasePassword(password string) error {
	if len(password) < 12 || len(password) > 128 || strings.ContainsRune(password, 0) {
		return errors.New("database password must contain 12 to 128 characters")
	}
	return nil
}

func (s *System) CreateManagedDatabase(ctx context.Context, name, username, password string) error {
	if err := validateDatabaseObjects(name, username); err != nil {
		return err
	}
	if err := validateDatabasePassword(password); err != nil {
		return err
	}
	if s.demo {
		return nil
	}
	sql := fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci; CREATE USER '%s'@'localhost' IDENTIFIED BY '%s'; GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost'; FLUSH PRIVILEGES;", name, username, escapeSQLString(password), name, username)
	client := firstAvailableCommand("mariadb", "mysql")
	if _, err := runCommand(ctx, 30*time.Second, client, "--protocol=socket", "--user=root", "--execute", sql); err != nil {
		return fmt.Errorf("create database: %w", err)
	}
	return nil
}

func (s *System) DropManagedDatabase(ctx context.Context, database ManagedDatabase) error {
	if err := validateDatabaseObjects(database.Name, database.Username); err != nil {
		return err
	}
	if s.demo {
		return nil
	}
	sql := fmt.Sprintf("DROP DATABASE IF EXISTS `%s`; DROP USER IF EXISTS '%s'@'localhost'; FLUSH PRIVILEGES;", database.Name, database.Username)
	client := firstAvailableCommand("mariadb", "mysql")
	if _, err := runCommand(ctx, 30*time.Second, client, "--protocol=socket", "--user=root", "--execute", sql); err != nil {
		return fmt.Errorf("delete database: %w", err)
	}
	return nil
}

func (s *System) ResetDatabasePassword(ctx context.Context, database ManagedDatabase, password string) error {
	if err := validateDatabaseObjects(database.Name, database.Username); err != nil {
		return err
	}
	if err := validateDatabasePassword(password); err != nil {
		return err
	}
	if s.demo {
		return nil
	}
	sql := fmt.Sprintf("ALTER USER '%s'@'localhost' IDENTIFIED BY '%s'; FLUSH PRIVILEGES;", database.Username, escapeSQLString(password))
	client := firstAvailableCommand("mariadb", "mysql")
	if _, err := runCommand(ctx, 30*time.Second, client, "--protocol=socket", "--user=root", "--execute", sql); err != nil {
		return fmt.Errorf("reset database password: %w", err)
	}
	return nil
}

func escapeSQLString(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `'`, `''`)
}

func (s *System) DatabaseEngine() string {
	if s.demo {
		return "MariaDB"
	}
	for _, command := range []string{"mariadb", "mysql"} {
		path, err := exec.LookPath(command)
		if err != nil {
			continue
		}
		output, err := exec.Command(path, "--version").CombinedOutput()
		if err == nil && strings.Contains(strings.ToLower(string(output)), "mariadb") {
			return "MariaDB"
		}
	}
	return "MySQL"
}

func (s *System) databaseUnit() string {
	for _, unit := range []string{"mariadb", "mysql", "mysqld"} {
		if unitFileExists(unit) {
			return unit
		}
	}
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		return "mysql"
	}
	return "mariadb"
}

func validateFirewallRule(rule FirewallRule) error {
	if rule.Port < 1 || rule.Port > 65535 {
		return errors.New("firewall port must be between 1 and 65535")
	}
	if rule.Protocol != "tcp" && rule.Protocol != "udp" {
		return errors.New("firewall protocol must be TCP or UDP")
	}
	if len(rule.Label) > 64 || strings.ContainsAny(rule.Label, "\r\n") {
		return errors.New("firewall rule label must be at most 64 characters")
	}
	if rule.Source != "" && rule.Source != "any" {
		if net.ParseIP(rule.Source) == nil {
			if _, _, err := net.ParseCIDR(rule.Source); err != nil {
				return errors.New("source must be an IP address, CIDR network or 'any'")
			}
		}
	}
	if rule.Source == "" || rule.Source == "any" {
		for _, protected := range []int{22, 25, 80, 443, 465, 587, 993, 8443} {
			if rule.Port == protected && rule.Protocol == "tcp" {
				return errors.New("that essential LiteCP port is already protected by the installer")
			}
		}
	}
	return nil
}

func (s *System) ApplyFirewallRule(ctx context.Context, rule FirewallRule, remove bool) error {
	if err := validateFirewallRule(rule); err != nil {
		return err
	}
	if s.demo {
		return nil
	}
	verb := "--add-port="
	if remove {
		verb = "--remove-port="
	}
	if s.firewallBackend() == "firewalld" {
		var args []string
		if rule.Source == "" || rule.Source == "any" {
			args = []string{"--permanent", "--zone=public", verb + strconv.Itoa(rule.Port) + "/" + rule.Protocol}
		} else {
			family := "ipv4"
			if strings.Contains(rule.Source, ":") {
				family = "ipv6"
			}
			richRule := fmt.Sprintf(`rule family="%s" source address="%s" port port="%d" protocol="%s" accept`, family, rule.Source, rule.Port, rule.Protocol)
			richVerb := "--add-rich-rule="
			if remove {
				richVerb = "--remove-rich-rule="
			}
			args = []string{"--permanent", "--zone=public", richVerb + richRule}
		}
		if _, err := runCommand(ctx, 20*time.Second, "firewall-cmd", args...); err != nil {
			return err
		}
		_, err := runCommand(ctx, 20*time.Second, "firewall-cmd", "--reload")
		return err
	}
	action := "allow"
	if remove {
		action = "delete"
	}
	args := []string{}
	if remove {
		args = append(args, "--force", action, "allow")
	} else {
		args = append(args, action)
	}
	if rule.Source != "" && rule.Source != "any" {
		args = append(args, "from", rule.Source, "to", "any", "port", strconv.Itoa(rule.Port), "proto", rule.Protocol)
	} else {
		args = append(args, strconv.Itoa(rule.Port)+"/"+rule.Protocol)
	}
	if !remove && rule.Label != "" {
		args = append(args, "comment", "LiteCP: "+rule.Label)
	}
	_, err := runCommand(ctx, 20*time.Second, "ufw", args...)
	return err
}

func (s *System) firewallBackend() string {
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		return "ufw"
	}
	return "firewalld"
}

func (s *System) FirewallBackend() string {
	if s.demo {
		return "firewalld"
	}
	return s.firewallBackend()
}
