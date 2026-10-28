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
	sql := fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci; CREATE USER '%s'@'localhost' %s; GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost'; FLUSH PRIVILEGES;", name, username, s.databaseUserIdentification(password, false), name, username)
	client := firstAvailableCommand("mysql", "mariadb")
	if _, err := runCommandInput(ctx, 30*time.Second, sql, client, "--protocol=socket", "--user=root", "--batch"); err != nil {
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
	client := firstAvailableCommand("mysql", "mariadb")
	if _, err := runCommandInput(ctx, 30*time.Second, sql, client, "--protocol=socket", "--user=root", "--batch"); err != nil {
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
	sql := fmt.Sprintf("ALTER USER '%s'@'localhost' %s; FLUSH PRIVILEGES;", database.Username, s.databaseUserIdentification(password, database.LegacyAuth))
	client := firstAvailableCommand("mysql", "mariadb")
	if _, err := runCommandInput(ctx, 30*time.Second, sql, client, "--protocol=socket", "--user=root", "--batch"); err != nil {
		return fmt.Errorf("reset database password: %w", err)
	}
	return nil
}

func escapeSQLString(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `'`, `''`)
}

func (s *System) databaseUserIdentification(password string, legacy bool) string {
	clause := "IDENTIFIED BY"
	if legacy && strings.EqualFold(s.DatabaseEngine(), "MySQL") {
		// PHP 7.3's bundled mysqlnd cannot authenticate with caching_sha2_password.
		// LiteCP's database server is bound to localhost and uses strong generated
		// passwords, so MySQL's legacy plugin is limited to local application users.
		clause = "IDENTIFIED WITH mysql_native_password BY"
	}
	return fmt.Sprintf("%s '%s'", clause, escapeSQLString(password))
}

func legacyPHPDatabaseAuth(version string) bool {
	return version == "7.3" || version == "7.4"
}

func (s *System) DatabaseEngine() string {
	if s.demo {
		return "MySQL"
	}
	for _, command := range []string{"mysql", "mariadb"} {
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
	for _, unit := range []string{"mysql", "mysqld", "mariadb"} {
		if unitFileExists(unit) {
			return unit
		}
	}
	if s.os.ID == "ubuntu" || s.os.ID == "debian" {
		return "mysql"
	}
	return "mysqld"
}

func validateFirewallRule(rule FirewallRule) error {
	rule = normalizeFirewallRule(rule)
	if rule.Direction != "inbound" && rule.Direction != "outbound" {
		return errors.New("firewall direction must be inbound or outbound")
	}
	if rule.Action != "allow" && rule.Action != "deny" {
		return errors.New("firewall action must be allow or deny")
	}
	if rule.Port < 1 || rule.Port > 65535 {
		return errors.New("firewall port must be between 1 and 65535")
	}
	if rule.Protocol != "tcp" && rule.Protocol != "udp" {
		return errors.New("firewall protocol must be TCP or UDP")
	}
	if len(rule.Label) > 64 || strings.ContainsAny(rule.Label, "\r\n") {
		return errors.New("firewall rule label must be at most 64 characters")
	}
	address := rule.Source
	addressLabel := "source"
	if rule.Direction == "outbound" {
		address = rule.Destination
		addressLabel = "destination"
	}
	if address != "" && address != "any" {
		if net.ParseIP(address) == nil {
			if _, _, err := net.ParseCIDR(address); err != nil {
				return fmt.Errorf("%s must be an IP address, CIDR network or 'any'", addressLabel)
			}
		}
	}
	if rule.Direction == "inbound" && (rule.Source == "" || rule.Source == "any") {
		for _, protected := range []int{22, 25, 80, 443, 465, 587, 993, 4321, 8443} {
			if rule.Port == protected && rule.Protocol == "tcp" {
				return errors.New("unscoped changes to essential LiteCP ports are protected by the installer")
			}
		}
	}
	return nil
}

func (s *System) ApplyFirewallRule(ctx context.Context, rule FirewallRule, remove bool) error {
	rule = normalizeFirewallRule(rule)
	if err := validateFirewallRule(rule); err != nil {
		return err
	}
	if s.demo {
		return nil
	}
	if s.firewallBackend() == "firewalld" {
		richVerb := "--add-rich-rule="
		if remove {
			richVerb = "--remove-rich-rule="
		}
		decision := "accept"
		if rule.Action == "deny" {
			decision = "reject"
		}
		address := rule.Source
		addressTerm := "source"
		scopeArgs := []string{"--permanent", "--zone=public"}
		if rule.Direction == "outbound" {
			address, addressTerm = rule.Destination, "destination"
			policies, err := runCommand(ctx, 20*time.Second, "firewall-cmd", "--permanent", "--get-policies")
			if err != nil {
				return err
			}
			if !containsString(strings.Fields(policies), "litecp-outbound") {
				if _, err := runCommand(ctx, 20*time.Second, "firewall-cmd", "--permanent", "--new-policy", "litecp-outbound"); err != nil {
					return err
				}
				for _, args := range [][]string{{"--permanent", "--policy", "litecp-outbound", "--add-ingress-zone", "HOST"}, {"--permanent", "--policy", "litecp-outbound", "--add-egress-zone", "ANY"}, {"--permanent", "--policy", "litecp-outbound", "--set-priority", "-500"}} {
					if _, err := runCommand(ctx, 20*time.Second, "firewall-cmd", args...); err != nil {
						return err
					}
				}
			}
			scopeArgs = []string{"--permanent", "--policy", "litecp-outbound"}
		}
		family := ""
		if address != "" && address != "any" {
			family = "ipv4"
			if strings.Contains(address, ":") {
				family = "ipv6"
			}
		}
		parts := []string{"rule"}
		if family != "" {
			parts = append(parts, `family="`+family+`"`, addressTerm+` address="`+address+`"`)
		}
		parts = append(parts, fmt.Sprintf(`port port="%d" protocol="%s"`, rule.Port, rule.Protocol), decision)
		operation := richVerb + strings.Join(parts, " ")
		// Preserve the representation used by pre-0.5 unscoped allow rules.
		if rule.Direction == "inbound" && rule.Action == "allow" && rule.Source == "any" {
			verb := "--add-port="
			if remove {
				verb = "--remove-port="
			}
			operation = fmt.Sprintf("%s%d/%s", verb, rule.Port, rule.Protocol)
		}
		args := append(scopeArgs, operation)
		if _, err := runCommand(ctx, 20*time.Second, "firewall-cmd", args...); err != nil {
			return err
		}
		_, err := runCommand(ctx, 20*time.Second, "firewall-cmd", "--reload")
		if err != nil {
			inverse := strings.Replace(operation, "--add-", "--remove-", 1)
			if remove {
				inverse = strings.Replace(operation, "--remove-", "--add-", 1)
			}
			_, rollbackErr := runCommand(context.Background(), 20*time.Second, "firewall-cmd", append(scopeArgs, inverse)...)
			if rollbackErr != nil {
				return fmt.Errorf("firewall reload failed: %v; permanent configuration rollback also failed: %v", err, rollbackErr)
			}
		}
		return err
	}
	action := rule.Action
	args := []string{}
	if remove {
		args = append(args, "--force", "delete")
	}
	args = append(args, action)
	if rule.Direction == "outbound" {
		args = append(args, "out", "to")
		if rule.Destination == "" || rule.Destination == "any" {
			args = append(args, "any")
		} else {
			args = append(args, rule.Destination)
		}
		args = append(args, "port", strconv.Itoa(rule.Port), "proto", rule.Protocol)
	} else if rule.Source != "" && rule.Source != "any" {
		args = append(args, "from", rule.Source, "to", "any", "port", strconv.Itoa(rule.Port), "proto", rule.Protocol)
	} else {
		args = append(args, strconv.Itoa(rule.Port)+"/"+rule.Protocol)
	}
	if !remove && rule.Label != "" {
		args = append(args, "comment", "LiteCP: "+rule.Label)
	}
	_, err := runHostCommand(ctx, 30*time.Second, "ufw", args...)
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
