package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var mailboxLocalPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._+\-]{0,62}[a-z0-9])?$`)

type mailFileSpec struct {
	name    string
	content string
	mode    os.FileMode
}

type mailFileSnapshot struct {
	path    string
	content []byte
	mode    os.FileMode
	exists  bool
}

func snapshotMailFiles(paths []string) ([]mailFileSnapshot, error) {
	result := make([]mailFileSnapshot, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			result = append(result, mailFileSnapshot{path: path})
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("mail configuration path is not a regular file: %s", path)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		result = append(result, mailFileSnapshot{path: path, content: content, mode: info.Mode().Perm(), exists: true})
	}
	return result, nil
}

func restoreMailFiles(snapshots []mailFileSnapshot) error {
	errorsFound := []string{}
	for _, snapshot := range snapshots {
		var err error
		if snapshot.exists {
			err = writeAtomic(snapshot.path, snapshot.content, snapshot.mode)
		} else {
			err = os.Remove(snapshot.path)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		}
		if err != nil {
			errorsFound = append(errorsFound, snapshot.path+": "+err.Error())
		}
	}
	if len(errorsFound) > 0 {
		return errors.New(strings.Join(errorsFound, "; "))
	}
	return nil
}

func validateMailDomain(domain string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if len(domain) > 253 || !domainPattern.MatchString(domain) {
		return errors.New("enter a valid mail domain")
	}
	return nil
}

func validateMailboxAddress(address, domain string) error {
	address = strings.ToLower(strings.TrimSpace(address))
	domain = strings.ToLower(strings.TrimSpace(domain))
	if err := validateMailDomain(domain); err != nil {
		return err
	}
	parts := strings.Split(address, "@")
	if len(parts) != 2 || parts[1] != domain || !mailboxLocalPattern.MatchString(parts[0]) {
		return errors.New("enter a valid mailbox address for the selected domain")
	}
	return nil
}

func validateAliasAddress(source, destination string) error {
	parsed, err := mail.ParseAddress(strings.TrimSpace(destination))
	if err != nil || parsed.Address != strings.TrimSpace(destination) {
		return errors.New("enter a valid alias destination address")
	}
	parts := strings.Split(strings.ToLower(strings.TrimSpace(source)), "@")
	if len(parts) != 2 || !mailboxLocalPattern.MatchString(parts[0]) || validateMailDomain(parts[1]) != nil {
		return errors.New("enter a valid alias source address")
	}
	return nil
}

func (s *System) HashMailboxPassword(ctx context.Context, password string) (string, error) {
	if len(password) < 12 || len(password) > 128 || strings.ContainsRune(password, 0) {
		return "", errors.New("mailbox password must contain 12 to 128 characters")
	}
	if s.demo {
		return "{SHA512-CRYPT}$6$litecp-demo", nil
	}
	commandCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(commandCtx, "doveadm", "pw", "-s", "SHA512-CRYPT")
	command.Stdin = strings.NewReader(password + "\n" + password + "\n")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("hash mailbox password: %s", message)
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "{SHA512-CRYPT}") {
			return line, nil
		}
	}
	return "", errors.New("Dovecot did not return a password hash")
}

func (s *System) SyncMailConfig(ctx context.Context, domains []MailDomain, mailboxes []Mailbox, aliases []MailAlias) error {
	if s.demo {
		return nil
	}
	if _, err := exec.LookPath("postmap"); err != nil {
		return errors.New("email hosting is not installed; rerun the LiteCP installer")
	}
	if _, err := exec.LookPath("doveadm"); err != nil {
		return errors.New("Dovecot is not installed; rerun the LiteCP installer")
	}
	mapType, err := s.mailMapType(ctx)
	if err != nil {
		return err
	}
	mailDir := "/etc/litecp/mail"
	if err := os.MkdirAll(mailDir, 0750); err != nil {
		return fmt.Errorf("create mail configuration directory: %w", err)
	}
	// Sorting must not mutate caller-owned slices: handlers retain the original
	// ordering for durable-state rollback if a later save fails.
	domains = append([]MailDomain{}, domains...)
	mailboxes = append([]Mailbox{}, mailboxes...)
	aliases = append([]MailAlias{}, aliases...)
	sort.Slice(domains, func(i, j int) bool { return domains[i].Domain < domains[j].Domain })
	sort.Slice(mailboxes, func(i, j int) bool { return mailboxes[i].Address < mailboxes[j].Address })
	sort.Slice(aliases, func(i, j int) bool { return aliases[i].Source < aliases[j].Source })
	var domainMap, mailboxMap, aliasMap, passwordFile, senderMap strings.Builder
	for _, domain := range domains {
		if err := validateMailDomain(domain.Domain); err != nil {
			return err
		}
		domainMap.WriteString(domain.Domain + " OK\n")
	}
	for _, mailbox := range mailboxes {
		if err := validateMailboxAddress(mailbox.Address, mailbox.Domain); err != nil {
			return err
		}
		if !strings.HasPrefix(mailbox.PasswordHash, "{SHA512-CRYPT}") {
			return fmt.Errorf("mailbox %s has an invalid password hash", mailbox.Address)
		}
		mailboxMap.WriteString(mailbox.Address + " OK\n")
		senderMap.WriteString(mailbox.Address + " " + mailbox.Address + "\n")
		passwordFile.WriteString(fmt.Sprintf("%s:%s::::::userdb_quota_rule=*:storage=%dM\n", mailbox.Address, mailbox.PasswordHash, mailbox.QuotaMB))
		parts := strings.Split(mailbox.Address, "@")
		domainRoot := filepath.Join("/var/vmail", mailbox.Domain)
		mailboxRoot := filepath.Join(domainRoot, parts[0])
		mailRoot := filepath.Join(mailboxRoot, "Maildir")
		ownedPaths := []string{domainRoot, mailboxRoot, mailRoot}
		for _, folder := range []string{"cur", "new", "tmp"} {
			folderPath := filepath.Join(mailRoot, folder)
			if err := os.MkdirAll(folderPath, 0700); err != nil {
				return fmt.Errorf("create mailbox: %w", err)
			}
			ownedPaths = append(ownedPaths, folderPath)
		}
		for _, ownedPath := range ownedPaths {
			if err := chownWebTree(ownedPath, "vmail"); err != nil {
				return err
			}
		}
	}
	for _, alias := range aliases {
		if err := validateAliasAddress(alias.Source, alias.Destination); err != nil {
			return err
		}
		aliasMap.WriteString(alias.Source + " " + alias.Destination + "\n")
	}
	files := []mailFileSpec{
		{"domains", domainMap.String(), 0644},
		{"mailboxes", mailboxMap.String(), 0644},
		{"aliases", aliasMap.String(), 0644},
		{"senders", senderMap.String(), 0644},
		{"users", passwordFile.String(), 0600},
	}
	staging, err := os.MkdirTemp(mailDir, ".litecp-mail-sync-")
	if err != nil {
		return fmt.Errorf("create mail configuration staging: %w", err)
	}
	defer os.RemoveAll(staging)
	for _, file := range files {
		if err := writeAtomic(filepath.Join(staging, file.name), []byte(file.content), file.mode); err != nil {
			return fmt.Errorf("write mail %s: %w", file.name, err)
		}
	}
	if err := chownWebTree(filepath.Join(staging, "users"), "dovecot"); err != nil {
		return fmt.Errorf("secure mailbox password file: %w", err)
	}
	for _, name := range []string{"domains", "mailboxes", "aliases", "senders"} {
		if _, err := runCommand(ctx, 20*time.Second, "postmap", mapType+":"+filepath.Join(staging, name)); err != nil {
			return err
		}
	}
	mapSuffix := ".db"
	if mapType == "lmdb" {
		mapSuffix = ".lmdb"
	}
	commitNames := []string{"domains", "mailboxes", "aliases", "senders", "users"}
	for _, name := range []string{"domains", "mailboxes", "aliases", "senders"} {
		commitNames = append(commitNames, name+mapSuffix)
	}
	snapshotPaths := make([]string, 0, len(commitNames)+2)
	for _, name := range commitNames {
		snapshotPaths = append(snapshotPaths, filepath.Join(mailDir, name))
	}
	snapshotPaths = append(snapshotPaths, filepath.Join(mailDir, "KeyTable"), filepath.Join(mailDir, "SigningTable"))
	snapshots, err := snapshotMailFiles(snapshotPaths)
	if err != nil {
		return fmt.Errorf("snapshot mail configuration: %w", err)
	}
	rollback := func(cause error) error {
		restoreErr := restoreMailFiles(snapshots)
		if _, statErr := os.Stat(filepath.Join(mailDir, "users")); statErr == nil {
			if ownerErr := chownWebTree(filepath.Join(mailDir, "users"), "dovecot"); ownerErr != nil && restoreErr == nil {
				restoreErr = ownerErr
			}
		}
		units := append([]string{}, s.mailUnits()...)
		for _, optionalUnit := range []string{"opendkim", "rspamd"} {
			if unitFileExists(optionalUnit) {
				units = append(units, optionalUnit)
			}
		}
		if !s.mailHostingDisabled() {
			for _, unit := range units {
				if _, reloadErr := runCommand(context.Background(), 30*time.Second, "systemctl", "reload-or-restart", unit); reloadErr != nil && restoreErr == nil {
					restoreErr = reloadErr
				}
			}
		}
		if restoreErr != nil {
			return fmt.Errorf("%v; mail configuration rollback failed: %v", cause, restoreErr)
		}
		return cause
	}
	for _, name := range commitNames {
		source := filepath.Join(staging, name)
		content, readErr := os.ReadFile(source)
		if readErr != nil {
			return rollback(fmt.Errorf("read staged mail %s: %w", name, readErr))
		}
		mode := os.FileMode(0644)
		if name == "users" {
			mode = 0600
		}
		if err := writeAtomic(filepath.Join(mailDir, name), content, mode); err != nil {
			return rollback(fmt.Errorf("activate mail %s: %w", name, err))
		}
	}
	if err := chownWebTree(filepath.Join(mailDir, "users"), "dovecot"); err != nil {
		return rollback(fmt.Errorf("secure active mailbox password file: %w", err))
	}
	if err := s.syncDKIM(ctx, domains); err != nil {
		return rollback(err)
	}
	if !s.mailHostingDisabled() {
		for _, unit := range s.mailUnits() {
			if _, err := runCommand(ctx, 30*time.Second, "systemctl", "reload-or-restart", unit); err != nil {
				return rollback(err)
			}
		}
	}
	return nil
}

func (s *System) mailMapType(ctx context.Context) (string, error) {
	if s.demo {
		return "hash", nil
	}
	output, err := runCommand(ctx, 10*time.Second, "postconf", "-m")
	if err != nil {
		return "", fmt.Errorf("detect Postfix map support: %w", err)
	}
	available := strings.Fields(strings.ToLower(output))
	for _, preferred := range []string{"hash", "lmdb"} {
		for _, candidate := range available {
			if candidate == preferred {
				return preferred, nil
			}
		}
	}
	return "", errors.New("Postfix provides neither hash nor LMDB lookup maps")
}

func (s *System) mailUnits() []string {
	units := make([]string, 0, 2)
	for _, unit := range []string{"postfix", "dovecot"} {
		if s.demo || unitFileExists(unit) {
			units = append(units, unit)
		}
	}
	return units
}

func (s *System) mailStackState(ctx context.Context) string {
	units := s.mailUnits()
	if len(units) < 2 {
		return "not installed"
	}
	for _, unit := range units {
		if s.serviceState(ctx, unit) != "running" {
			return "stopped"
		}
	}
	if !probeTCP("127.0.0.1:25") || !probeTCP("127.0.0.1:143") {
		return "degraded"
	}
	return "running"
}
