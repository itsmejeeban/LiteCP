package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type cpanelMailboxImport struct {
	Mailbox        Mailbox
	Source         string
	DefaultAccount bool
}
type cpanelServicesPlan struct {
	Mailboxes  []cpanelMailboxImport
	Cron       []WebsiteCronJob
	Warnings   []string
	SourceHome string
}

func migrationServiceID(job, kind, key string) string {
	sum := sha256.Sum256([]byte(job + "\x00" + kind + "\x00" + key))
	return fmt.Sprintf("cp-%x", sum[:12])
}

// Read only inert, bounded metadata. Never execute source PHP or a source cron
// command while discovering accounts, credentials or schedules.
func readCPanelMetadata(root, name string, optional bool) ([]byte, error) {
	f, err := secureOpenManaged(root, name, os.O_RDONLY, 0)
	if optional && errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read cPanel migration metadata %s", name)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 {
		return nil, fmt.Errorf("invalid cPanel migration metadata %s", name)
	}
	return io.ReadAll(io.LimitReader(f, (2<<20)+1))
}

var importedSHA512 = regexp.MustCompile(`^\$6\$(?:rounds=[0-9]+\$)?[./A-Za-z0-9]{1,16}\$[./A-Za-z0-9]{86}$`)

func planCPanelServices(root string, job LiveMigrationJob) (cpanelServicesPlan, error) {
	plan := cpanelServicesPlan{}
	base := filepath.Join(root, "cpanel_account")
	data, err := readCPanelMetadata(base, "format.json", false)
	if err != nil {
		return plan, errors.New("this older capture has no mailbox/cron export; reconnect and capture the cPanel account again")
	}
	var format struct {
		Version    int    `json:"version"`
		SourceHome string `json:"sourceHome"`
	}
	if json.Unmarshal(data, &format) != nil || format.Version != 1 || !strings.HasPrefix(format.SourceHome, "/") || path.Clean(format.SourceHome) != format.SourceHome || strings.ContainsAny(format.SourceHome, "\r\n\x00\\") || path.Base(format.SourceHome) != job.Account {
		return plan, errors.New("invalid cPanel source home metadata")
	}
	plan.SourceHome = strings.TrimRight(format.SourceHome, "/")
	domains, err := os.ReadDir(filepath.Join(base, "etc"))
	if err != nil && !os.IsNotExist(err) {
		return plan, err
	}
	seen := map[string]bool{}
	for _, domain := range domains {
		if !domain.IsDir() || validateMailDomain(domain.Name()) != nil {
			return plan, errors.New("invalid cPanel mail domain metadata")
		}
		shadow, err := readCPanelMetadata(base, "etc/"+domain.Name()+"/shadow", true)
		if err != nil {
			return plan, err
		}
		quotas, err := readCPanelMetadata(base, "etc/"+domain.Name()+"/quota", true)
		if err != nil {
			return plan, err
		}
		quotaByLocal := map[string]int{}
		for _, line := range strings.Split(string(quotas), "\n") {
			parts := strings.Split(strings.TrimSpace(line), ":")
			if len(parts) >= 2 {
				bytes, e := strconv.ParseInt(parts[1], 10, 64)
				if e != nil || bytes < 0 {
					return plan, errors.New("unsupported cPanel mailbox quota")
				}
				if bytes > 102400<<20 {
					return plan, errors.New("cPanel mailbox quota exceeds 100 GiB")
				}
				if bytes > 0 {
					quotaByLocal[parts[0]] = int((bytes + (1 << 20) - 1) >> 20)
				}
			}
		}
		for _, line := range strings.Split(string(shadow), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			parts := strings.Split(line, ":")
			if len(parts) < 2 {
				return plan, errors.New("invalid cPanel mailbox shadow entry")
			}
			address := parts[0] + "@" + domain.Name()
			if validateMailboxAddress(address, domain.Name()) != nil || seen[address] {
				return plan, errors.New("invalid or duplicate cPanel mailbox")
			}
			if !importedSHA512.MatchString(parts[1]) {
				return plan, fmt.Errorf("mailbox %s uses a locked or unsupported password hash; reset it to SHA512-CRYPT on cPanel and recapture", address)
			}
			quota := quotaByLocal[parts[0]]
			if quota > 102400 {
				return plan, fmt.Errorf("mailbox %s exceeds LiteCP's supported quota", address)
			}
			source := filepath.Join(base, "mail", domain.Name(), parts[0])
			if info, e := os.Lstat(source); e == nil {
				if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					return plan, errors.New("unsafe cPanel mailbox tree")
				}
			} else if !os.IsNotExist(e) {
				return plan, e
			} else {
				source = ""
			}
			plan.Mailboxes = append(plan.Mailboxes, cpanelMailboxImport{Mailbox: Mailbox{ID: migrationServiceID(job.ID, "mail", address), Domain: domain.Name(), Address: address, PasswordHash: "{SHA512-CRYPT}" + parts[1], QuotaMB: quota, CreatedAt: time.Now().UTC()}, Source: source})
			seen[address] = true
			if len(plan.Mailboxes) > 10000 {
				return plan, errors.New("cPanel mailbox limit exceeded")
			}
		}
	}
	// Orphan mail must not silently disappear from an apparently successful import.
	mailDomains, err := os.ReadDir(filepath.Join(base, "mail"))
	if err != nil && !os.IsNotExist(err) {
		return plan, err
	}
	defaultMail := false
	for _, d := range mailDomains {
		// cPanel writes this account-level format marker even for accounts
		// with no mailboxes. It is metadata, not a domain or a Maildir message.
		if d.Name() == "mailbox_format.cpanel" {
			data, e := readCPanelMetadata(base, "mail/mailbox_format.cpanel", false)
			if e != nil {
				return plan, e
			}
			if strings.TrimSpace(string(data)) != "maildir" {
				return plan, errors.New("unsupported cPanel mailbox format; convert source mail to Maildir and capture again")
			}
			continue
		}
		if cpanelDefaultMailEntry(d.Name()) {
			if d.IsDir() {
				e := filepath.Walk(filepath.Join(base, "mail", d.Name()), func(_ string, info os.FileInfo, e error) error {
					if e != nil {
						return e
					}
					if !info.IsDir() {
						defaultMail = true
					}
					return nil
				})
				if e != nil {
					return plan, e
				}
			}
			continue
		}
		if !d.IsDir() || validateMailDomain(d.Name()) != nil {
			return plan, errors.New("unsupported cPanel mail root entry; source mail was preserved for review")
		}
		users, e := os.ReadDir(filepath.Join(base, "mail", d.Name()))
		if e != nil {
			return plan, e
		}
		for _, u := range users {
			if !u.IsDir() || !seen[u.Name()+"@"+d.Name()] {
				return plan, errors.New("cPanel mail data has no matching supported mailbox account")
			}
		}
	}
	if defaultMail {
		domain := ""
		for _, account := range job.Accounts {
			if account.ID == job.Account {
				domain = account.Domain
				break
			}
		}
		address := job.Account + "@" + domain
		if validateMailDomain(domain) != nil || validateMailboxAddress(address, domain) != nil || seen[address] {
			return plan, errors.New("default account mail needs a valid, non-conflicting primary domain")
		}
		plan.Mailboxes = append(plan.Mailboxes, cpanelMailboxImport{Mailbox: Mailbox{ID: migrationServiceID(job.ID, "mail", address), Domain: domain, Address: address, PasswordHash: lockedMailboxPasswordHash, CreatedAt: time.Now().UTC()}, Source: filepath.Join(base, "mail"), DefaultAccount: true})
		plan.Warnings = append(plan.Warnings, "Default account mail preserved as "+address+"; set a mailbox password in LiteCP before login.")
	}
	cron, err := readCPanelMetadata(base, "cron/"+job.Account, true)
	if err != nil {
		return plan, err
	}
	var warnings []string
	plan.Cron, warnings, err = parseCPanelCron(string(cron), job.ID)
	plan.Warnings = append(plan.Warnings, warnings...)
	return plan, err
}

var cpanelCronLine = regexp.MustCompile(`^\s*(\S+\s+\S+\s+\S+\s+\S+\s+\S+)\s+(.+)$`)
var cpanelPHPCommand = regexp.MustCompile(`/(?:opt/cpanel/ea-php[0-9]+/root/usr/bin/php|usr/local/bin/(?:ea-php[0-9]+|php)|usr/bin/php)(\b)`)
var cpanelEnvKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func parseCPanelCron(text, jobID string) ([]WebsiteCronJob, []string, error) {
	jobs := []WebsiteCronJob{}
	warnings := []string{}
	env := []string{}
	unsafeEnv := false
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 4096), 65536)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if eq := strings.Index(line, "="); eq > 0 && !strings.ContainsAny(line[:eq], " \t") {
			key, value := line[:eq], strings.TrimSpace(line[eq+1:])
			if key == "MAILTO" {
				if value != "" && value != `""` && value != "''" {
					warnings = append(warnings, "Cron notification email is not imported; configure delivery separately.")
				}
				continue
			}
			if key == "SHELL" && value == "/bin/sh" {
				continue
			}
			if !cpanelEnvKey.MatchString(key) || strings.ContainsAny(value, "$`\\\x00") || key == "CRON_TZ" || key == "TZ" || key == "SHELL" {
				unsafeEnv = true
				continue
			}
			value = strings.Trim(value, `"'`)
			env = append(env, key+"="+cronShellQuote(value))
			continue
		}
		schedule, command := "", ""
		if strings.HasPrefix(line, "@") {
			separator := strings.IndexAny(line, " \t")
			if separator < 0 {
				return nil, nil, errors.New("invalid cPanel cron macro")
			}
			schedule = map[string]string{"@yearly": "0 0 1 1 *", "@annually": "0 0 1 1 *", "@monthly": "0 0 1 * *", "@weekly": "0 0 * * 0", "@daily": "0 0 * * *", "@midnight": "0 0 * * *", "@hourly": "0 * * * *"}[line[:separator]]
			command = strings.TrimSpace(line[separator+1:])
		} else if match := cpanelCronLine.FindStringSubmatch(line); len(match) == 3 {
			schedule, command = match[1], match[2]
		} else {
			return nil, nil, errors.New("unsupported cPanel cron table line")
		}
		enabled := true
		if schedule == "" {
			schedule = "* * * * *"
			enabled = false
		}
		if unsafeEnv || strings.Contains(command, "%") {
			enabled = false
		}
		if len(env) > 0 {
			command = "export " + strings.Join(env, " ") + "; " + command
		}
		item := WebsiteCronJob{ID: migrationServiceID(jobID, "cron", line+strings.Join(env, " ")), Name: "Imported cPanel cron", Schedule: schedule, Command: command, Enabled: enabled, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		if !enabled {
			item.Name = "Imported cPanel cron - review required"
			warnings = append(warnings, "A cron job was imported disabled: unsupported macro, time zone/environment or percent-input syntax. Review it in Website Tools > Cron Jobs.")
		}
		if err := validateWebsiteCronJob(item); err != nil {
			return nil, nil, fmt.Errorf("unsupported cPanel cron job: %w", err)
		}
		duplicate := false
		for _, old := range jobs {
			if old.ID == item.ID {
				duplicate = true
			}
		}
		if !duplicate {
			jobs = append(jobs, item)
		}
		if len(jobs) > 100 {
			return nil, nil, errors.New("cPanel cron job limit exceeds 100")
		}
	}
	return jobs, warnings, scanner.Err()
}

func (s *System) prepareCPanelCron(plan cpanelServicesPlan, site Site) []WebsiteCronJob {
	jobs := append([]WebsiteCronJob{}, plan.Cron...)
	for i := range jobs {
		command := strings.ReplaceAll(jobs[i].Command, plan.SourceHome+"/", filepath.ToSlash(managedSiteRoot(site))+"/")
		command = strings.ReplaceAll(command, cronShellQuote(plan.SourceHome), cronShellQuote(filepath.ToSlash(managedSiteRoot(site))))
		// Preserve existing quotes around PHP paths; adding shell quotes inside
		// a double-quoted executable would make the command invalid.
		command = cpanelPHPCommand.ReplaceAllStringFunc(command, func(string) string { return s.phpBinary(site.PHPVersion) })
		if strings.Contains(command, plan.SourceHome) || (s.usesPHP73Container(site.PHPVersion) && cpanelPHPCommand.MatchString(jobs[i].Command)) {
			jobs[i].Enabled = false
			jobs[i].Name = "Imported cPanel cron - review required"
		}
		jobs[i].SiteID = site.ID
		jobs[i].Command = command
	}
	return jobs
}

func (s *System) migrationMailRoot() string {
	if s.demo {
		return filepath.Join(s.dataDir, "vmail")
	}
	return "/var/vmail"
}

func (a *App) preflightCPanelServices(plan cpanelServicesPlan) error {
	_, mailboxes, aliases := a.store.MailConfig()
	if len(plan.Mailboxes) > 0 && !a.store.Settings().EmailHostingEnabled {
		return errors.New("enable Email hosting on LiteCP before importing cPanel mailboxes")
	}
	if len(plan.Mailboxes) > 0 && !a.system.demo {
		for _, binary := range []string{"postmap", "doveadm"} {
			if _, err := exec.LookPath(binary); err != nil {
				return errors.New("install LiteCP email services before importing cPanel mailboxes")
			}
		}
	}
	for _, incoming := range plan.Mailboxes {
		target := filepath.Join(a.system.migrationMailRoot(), incoming.Mailbox.Domain, strings.Split(incoming.Mailbox.Address, "@")[0])
		if info, err := os.Lstat(target); err == nil {
			marker, e := readCPanelMetadata(target, ".litecp-import", false)
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || e != nil || string(marker) != incoming.Mailbox.ID {
				return fmt.Errorf("mail data for %s already exists; destination was preserved", incoming.Mailbox.Address)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		for _, old := range mailboxes {
			if old.Address == incoming.Mailbox.Address && old.ID != incoming.Mailbox.ID {
				return fmt.Errorf("mailbox %s already exists; existing mail was preserved", old.Address)
			}
		}
		for _, alias := range aliases {
			if alias.Source == incoming.Mailbox.Address {
				return fmt.Errorf("mailbox %s conflicts with an existing forwarder", incoming.Mailbox.Address)
			}
		}
	}
	return nil
}

// Commit mail accounts in one durable additive transaction. Existing passwords,
// aliases and unrelated mail domains are never replaced by a migration.
func (s *Store) addCPanelMailboxes(incoming []cpanelMailboxImport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	oldDomains, oldBoxes := s.cfg.MailDomains, s.cfg.Mailboxes
	domains := append([]MailDomain{}, oldDomains...)
	boxes := append([]Mailbox{}, oldBoxes...)
	for _, item := range incoming {
		found := false
		for _, old := range boxes {
			if old.Address == item.Mailbox.Address {
				if old.ID != item.Mailbox.ID {
					return errors.New("mailbox conflict during migration")
				}
				found = true
			}
		}
		if found {
			continue
		}
		known := false
		for _, d := range domains {
			if d.Domain == item.Mailbox.Domain {
				known = true
			}
		}
		if !known {
			domains = append(domains, MailDomain{ID: migrationServiceID(item.Mailbox.ID, "domain", item.Mailbox.Domain), Domain: item.Mailbox.Domain, DKIMSelector: "default", CreatedAt: time.Now().UTC()})
		}
		boxes = append(boxes, item.Mailbox)
	}
	s.cfg.MailDomains, s.cfg.Mailboxes = domains, boxes
	if err := s.saveLocked(); err != nil {
		s.cfg.MailDomains, s.cfg.Mailboxes = oldDomains, oldBoxes
		return err
	}
	return nil
}

func (a *App) importCPanelServices(ctx context.Context, plan cpanelServicesPlan, site Site) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.preflightCPanelServices(plan); err != nil {
		return err
	}
	if len(plan.Mailboxes) > 0 {
		base := a.system.migrationMailRoot()
		if err := os.MkdirAll(base, 0750); err != nil {
			return err
		}
		for _, incoming := range plan.Mailboxes {
			if err := ctx.Err(); err != nil {
				return err
			}
			local := strings.Split(incoming.Mailbox.Address, "@")[0]
			relative := incoming.Mailbox.Domain + "/" + local
			if err := secureEnsureManagedDirectory(base, incoming.Mailbox.Domain, 0700, os.Getuid(), os.Getgid()); err != nil {
				return err
			}
			target := filepath.Join(base, filepath.FromSlash(relative))
			if info, err := os.Lstat(target); err == nil {
				if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					return errors.New("unsafe destination mailbox")
				}
				marker, err := readCPanelMetadata(target, ".litecp-import", false)
				if err != nil || string(marker) != incoming.Mailbox.ID {
					return fmt.Errorf("mail data for %s already exists; destination was preserved", incoming.Mailbox.Address)
				}
				// A completed retry must not overwrite messages received since import.
			} else if !os.IsNotExist(err) {
				return err
			} else {
				stage, err := os.MkdirTemp(filepath.Dir(target), ".cpanel-mail-")
				if err != nil {
					return err
				}
				if incoming.Source != "" {
					if err = copyCPanelMaildirFiltered(ctx, incoming.Source, stage, incoming.DefaultAccount); err != nil {
						return err
					}
				}
				for _, name := range []string{"cur", "new", "tmp"} {
					if err = os.MkdirAll(filepath.Join(stage, "Maildir", name), 0700); err != nil {
						return err
					}
				}
				if err = writeAtomic(filepath.Join(stage, ".litecp-import"), []byte(incoming.Mailbox.ID), 0600); err != nil {
					return err
				}
				if !a.system.demo {
					if _, err = runCommand(ctx, 30*time.Second, "chown", "-R", "vmail:vmail", "--", stage); err != nil {
						return errors.New("cannot assign imported mail ownership")
					}
				}
				if err = secureRenameManaged(base, filepath.ToSlash(strings.TrimPrefix(stage, base+string(os.PathSeparator))), relative); err != nil {
					return err
				}
			}
		}
		oldDomains, oldBoxes, aliases := a.store.MailConfig()
		if err := ctx.Err(); err != nil {
			return err
		}
		// Persist before syncing so a failed service reload is safely retryable.
		if err := a.store.addCPanelMailboxes(plan.Mailboxes); err != nil {
			return err
		}
		domains, boxes, _ := a.store.MailConfig()
		if err := a.system.SyncMailConfig(ctx, domains, boxes, aliases); err != nil {
			_ = a.system.SyncMailConfig(context.Background(), oldDomains, oldBoxes, aliases)
			return fmt.Errorf("mailbox data retained; mail configuration needs retry: %w", err)
		}
	}
	if len(plan.Cron) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		old, err := a.system.websiteCronJobs(site.ID)
		if err != nil {
			return err
		}
		for _, job := range a.system.prepareCPanelCron(plan, site) {
			found := false
			for _, previous := range old {
				if previous.ID == job.ID {
					found = true
				}
			}
			if !found {
				old = append(old, job)
			}
		}
		if len(old) > 100 {
			return errors.New("combined cron jobs exceed the website limit")
		}
		if err := a.system.saveWebsiteCron(site, old); err != nil {
			return err
		}
	}
	return nil
}

func copyCPanelMaildir(ctx context.Context, source, target string) error {
	return copyCPanelMaildirFiltered(ctx, source, target, false)
}

func copyCPanelMaildirFiltered(ctx context.Context, source, target string, defaultAccount bool) error {
	return filepath.Walk(source, func(name string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if defaultAccount && !cpanelDefaultMailEntry(parts[0]) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		for i, part := range parts {
			parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "%3A", ":"), "%25", "%")
			if parts[i] == ".." || strings.ContainsAny(parts[i], "/\\\x00") {
				return errors.New("invalid imported mail filename")
			}
		}
		decoded := "Maildir/" + strings.Join(parts, "/")
		if info.IsDir() {
			return secureEnsureManagedDirectory(target, decoded, 0700, os.Getuid(), os.Getgid())
		}
		if !info.Mode().IsRegular() {
			return errors.New("mail import contains a link or special file")
		}
		if err := secureEnsureManagedDirectory(target, filepath.ToSlash(filepath.Dir(decoded)), 0700, os.Getuid(), os.Getgid()); err != nil {
			return err
		}
		f, err := secureOpenManaged(source, filepath.ToSlash(rel), os.O_RDONLY, 0)
		if err != nil {
			return err
		}
		_, err = secureWriteManagedNoReplace(target, decoded, f, info.Size(), 0600, os.Getuid(), os.Getgid())
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}

// Stable presentation and tests; password hashes are never returned in a job.
func cpanelServiceSummary(plan cpanelServicesPlan) string {
	return fmt.Sprintf("%d mailbox(es) and %d custom cron job(s) imported.", len(plan.Mailboxes), len(plan.Cron))
}

// cPanel creates these at account level even when there are no email accounts.
// They are Maildir contents, never domain/account pairs.
func cpanelDefaultMailEntry(name string) bool {
	return strings.HasPrefix(name, ".") || name == "cur" || name == "new" || name == "tmp" || name == "maildirsize" || name == "subscriptions" || strings.HasPrefix(name, "dovecot")
}
