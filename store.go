package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Site struct {
	Captcha        *CaptchaPolicy         `json:"captcha,omitempty"`
	Redis          *WebsiteRedis          `json:"redis,omitempty"`
	DNSZone        *WebsiteDNSZone        `json:"dnsZone,omitempty"`
	ResourceLimits *WebsiteResourceLimits `json:"resourceLimits,omitempty"`
	PHPSettings    map[string]string      `json:"phpSettings,omitempty"`
	Suspended      bool                   `json:"suspended,omitempty"`
	Application    *ApplicationHosting    `json:"application,omitempty"`
	ID             string                 `json:"id"`
	Domain         string                 `json:"domain"`
	Root           string                 `json:"root"`
	AppType        string                 `json:"appType"`
	AppVersion     string                 `json:"appVersion,omitempty"`
	PHPVersion     string                 `json:"phpVersion"`
	RuntimeUID     int                    `json:"runtimeUid,omitempty"`
	RuntimeGID     int                    `json:"runtimeGid,omitempty"`
	SSL            bool                   `json:"ssl"`
	Cron           bool                   `json:"cron"`
	WAFMode        string                 `json:"wafMode,omitempty"`
	Database       string                 `json:"database,omitempty"`
	DBUser         string                 `json:"dbUser,omitempty"`
	CreatedAt      time.Time              `json:"createdAt"`

	// Provisioning markers are request-scoped and never persisted. They allow
	// the API to remove only files it created if durable state saving fails.
	provisionedRoot        bool
	provisionedPlaceholder bool
}

type Backup struct {
	ScheduleID      string    `json:"scheduleId,omitempty"`
	ID              string    `json:"id"`
	SiteID          string    `json:"siteId"`
	Domain          string    `json:"domain"`
	Path            string    `json:"path"`
	DestinationID   string    `json:"destinationId,omitempty"`
	DestinationName string    `json:"destinationName,omitempty"`
	RemotePath      string    `json:"remotePath,omitempty"`
	Size            int64     `json:"size"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
}

type BackupDestination struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Protocol       string    `json:"protocol"`
	Host           string    `json:"host"`
	Port           int       `json:"port"`
	Username       string    `json:"username"`
	RemotePath     string    `json:"remotePath"`
	HostKeySHA256  string    `json:"hostKeySha256,omitempty"`
	PasswordCipher string    `json:"passwordCipher,omitempty"`
	HasPassword    bool      `json:"hasPassword"`
	CreatedAt      time.Time `json:"createdAt"`
}

type ManagedDatabase struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Username   string    `json:"username"`
	SiteID     string    `json:"siteId,omitempty"`
	Domain     string    `json:"domain,omitempty"`
	LegacyAuth bool      `json:"legacyAuth,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

type FirewallRule struct {
	ID          string    `json:"id"`
	Direction   string    `json:"direction"`
	Action      string    `json:"action"`
	Port        int       `json:"port"`
	Protocol    string    `json:"protocol"`
	Source      string    `json:"source"`
	Destination string    `json:"destination,omitempty"`
	Label       string    `json:"label"`
	Remarks     string    `json:"remarks,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type MailDomain struct {
	ID           string    `json:"id"`
	Domain       string    `json:"domain"`
	DKIMSelector string    `json:"dkimSelector"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Mailbox struct {
	ID           string    `json:"id"`
	Domain       string    `json:"domain"`
	Address      string    `json:"address"`
	QuotaMB      int       `json:"quotaMb"`
	PasswordHash string    `json:"passwordHash,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

type MailAlias struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"`
	Destination string    `json:"destination"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Activity struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"createdAt"`
}

type Settings struct {
	ApplicationHosting    map[string]bool `json:"applicationHosting,omitempty"`
	ActivityRetentionDays int             `json:"activityRetentionDays"`
	Email                 string          `json:"email"`
	Timezone              string          `json:"timezone"`
	BackupRetention       int             `json:"backupRetention"`
	EmailHostingEnabled   bool            `json:"emailHostingEnabled"`
	Resolvers             []string        `json:"resolvers,omitempty"`
}

type Admin struct {
	Username     string          `json:"username"`
	PasswordHash string          `json:"passwordHash"`
	TwoFactor    twoFactorConfig `json:"twoFactor,omitempty"`
}

type Config struct {
	APIRules           []APIRule           `json:"apiRules,omitempty"`
	APICredentials     []APICredential     `json:"apiCredentials,omitempty"`
	BackupSchedules    []BackupSchedule    `json:"backupSchedules,omitempty"`
	SchemaVersion      int                 `json:"schemaVersion"`
	Admin              Admin               `json:"admin"`
	Settings           Settings            `json:"settings"`
	Sites              []Site              `json:"sites"`
	Backups            []Backup            `json:"backups"`
	BackupDestinations []BackupDestination `json:"backupDestinations,omitempty"`
	Databases          []ManagedDatabase   `json:"databases,omitempty"`
	FirewallRules      []FirewallRule      `json:"firewallRules,omitempty"`
	MailDomains        []MailDomain        `json:"mailDomains,omitempty"`
	Mailboxes          []Mailbox           `json:"mailboxes,omitempty"`
	MailAliases        []MailAlias         `json:"mailAliases,omitempty"`
	Activities         []Activity          `json:"activities"`
}

type PublicState struct {
	Version            string              `json:"version"`
	Username           string              `json:"username"`
	Settings           Settings            `json:"settings"`
	Sites              []Site              `json:"sites"`
	Backups            []Backup            `json:"backups"`
	BackupDestinations []BackupDestination `json:"backupDestinations"`
	Databases          []ManagedDatabase   `json:"databases"`
	FirewallRules      []FirewallRule      `json:"firewallRules"`
	MailDomains        []MailDomain        `json:"mailDomains"`
	Mailboxes          []Mailbox           `json:"mailboxes"`
	MailAliases        []MailAlias         `json:"mailAliases"`
	Activities         []Activity          `json:"activities"`
	System             SystemSnapshot      `json:"system"`
}

type Store struct {
	path string
	mu   sync.RWMutex
	cfg  Config
}

func OpenStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	store := &Store{path: filepath.Join(dataDir, "config.json")}
	content, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}
	if err := json.Unmarshal(content, &store.cfg); err != nil {
		return nil, fmt.Errorf("parse configuration: %w", err)
	}
	changed := false
	if store.cfg.SchemaVersion < 5 {
		store.cfg.SchemaVersion = 5
		store.cfg.Settings.EmailHostingEnabled = true
		if len(store.cfg.Settings.Resolvers) == 0 {
			// An upgrade has not applied DNS settings. Do not display Google
			// resolvers as though the provider's current configuration changed.
			store.cfg.Settings.Resolvers = []string{}
		}
		for i := range store.cfg.FirewallRules {
			if store.cfg.FirewallRules[i].Direction == "" {
				store.cfg.FirewallRules[i].Direction = "inbound"
			}
			if store.cfg.FirewallRules[i].Action == "" {
				store.cfg.FirewallRules[i].Action = "allow"
			}
		}
		for i := range store.cfg.MailDomains {
			if store.cfg.MailDomains[i].DKIMSelector == "" {
				store.cfg.MailDomains[i].DKIMSelector = "default"
			}
		}
		changed = true
	}
	for i := range store.cfg.Sites {
		if store.cfg.Sites[i].AppType == "" {
			store.cfg.Sites[i].AppType = "whmcs"
			changed = true
		}
		if store.cfg.Sites[i].PHPVersion == "" {
			store.cfg.Sites[i].PHPVersion = "8.3"
			changed = true
		}
		if store.cfg.Sites[i].AppType == "whmcs" && store.cfg.Sites[i].AppVersion == "" {
			// Existing installations used the WHMCS 8.13 compatibility matrix.
			store.cfg.Sites[i].AppVersion = "8.13"
			changed = true
		}
		if store.cfg.Sites[i].RuntimeUID == 0 || store.cfg.Sites[i].RuntimeGID == 0 {
			runtimeID := siteRuntimeID(store.cfg.Sites[i].ID)
			store.cfg.Sites[i].RuntimeUID = runtimeID
			store.cfg.Sites[i].RuntimeGID = runtimeID
			changed = true
		}
		if store.cfg.Sites[i].Database != "" {
			found := false
			for databaseIndex := range store.cfg.Databases {
				if store.cfg.Databases[databaseIndex].Name == store.cfg.Sites[i].Database {
					found = true
					legacy := legacyPHPDatabaseAuth(store.cfg.Sites[i].PHPVersion)
					if store.cfg.Databases[databaseIndex].LegacyAuth != legacy {
						store.cfg.Databases[databaseIndex].LegacyAuth = legacy
						changed = true
					}
					break
				}
			}
			if !found {
				store.cfg.Databases = append(store.cfg.Databases, ManagedDatabase{
					ID: "site-" + store.cfg.Sites[i].ID, Name: store.cfg.Sites[i].Database,
					Username: store.cfg.Sites[i].DBUser, SiteID: store.cfg.Sites[i].ID,
					Domain: store.cfg.Sites[i].Domain, LegacyAuth: legacyPHPDatabaseAuth(store.cfg.Sites[i].PHPVersion), CreatedAt: store.cfg.Sites[i].CreatedAt,
				})
				changed = true
			}
		}
	}
	if changed {
		if err := store.saveLocked(); err != nil {
			return nil, fmt.Errorf("migrate configuration: %w", err)
		}
	}
	return store, nil
}

func (s *Store) Initialized() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Admin.PasswordHash != ""
}

func (s *Store) Initialize(username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 64 {
		return errors.New("administrator username must contain 1 to 64 characters")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Admin.PasswordHash != "" {
		return errors.New("LiteCP is already initialized")
	}
	s.cfg = Config{
		SchemaVersion:      5,
		Admin:              Admin{Username: username, PasswordHash: hash},
		Settings:           Settings{Timezone: "Asia/Kolkata", BackupRetention: 14, EmailHostingEnabled: true, Resolvers: []string{"8.8.8.8", "8.8.4.4"}},
		Sites:              []Site{},
		Backups:            []Backup{},
		BackupDestinations: []BackupDestination{},
		Databases:          []ManagedDatabase{},
		FirewallRules:      []FirewallRule{},
		MailDomains:        []MailDomain{},
		Mailboxes:          []Mailbox{},
		MailAliases:        []MailAlias{},
		Activities: []Activity{{
			ID: "initial", Title: "LiteCP is ready", Detail: "Server management initialized", Kind: "success", CreatedAt: time.Now().UTC(),
		}},
	}
	return s.saveLocked()
}

func (s *Store) Username() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Admin.Username
}

func (s *Store) CheckPassword(username, password string) bool {
	s.mu.RLock()
	admin := s.cfg.Admin
	s.mu.RUnlock()
	if !checkAdminCredentials(admin, username, password, verifyPassword) {
		return false
	}
	if !strings.HasPrefix(admin.PasswordHash, "pbkdf2-sha256$") {
		return true
	}
	// Upgrade only after correct authentication; retain legacy verification for
	// restored configurations and never overwrite a concurrent password change.
	next, err := hashPassword(password)
	if err != nil {
		log.Print("Administrator password hash upgrade deferred: hashing failed")
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Admin.PasswordHash != admin.PasswordHash {
		return false
	}
	s.cfg.Admin.PasswordHash = next
	if err := s.saveLocked(); err != nil {
		s.cfg.Admin.PasswordHash = admin.PasswordHash
		log.Print("Administrator password hash upgrade deferred: configuration save failed")
	}
	return true
}

func checkAdminCredentials(admin Admin, username, password string, verify func(string, string) bool) bool {
	// Always do the password work, including for an unknown username. Otherwise
	// the expensive derivation reveals which administrator name exists.
	passwordOK := verify(admin.PasswordHash, password)
	return subtleStringEqual(admin.Username, strings.TrimSpace(username)) && passwordOK
}

func subtleStringEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := range a {
		result |= a[i] ^ b[i]
	}
	return result == 0
}

func (s *Store) SetPassword(password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Admin.PasswordHash = hash
	return s.saveLocked()
}

func (s *Store) PublicState() PublicState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	destinations := append([]BackupDestination{}, s.cfg.BackupDestinations...)
	for i := range destinations {
		destinations[i].HasPassword = destinations[i].PasswordCipher != ""
		destinations[i].PasswordCipher = ""
	}
	mailboxes := append([]Mailbox{}, s.cfg.Mailboxes...)
	for i := range mailboxes {
		mailboxes[i].PasswordHash = ""
	}
	activities := append([]Activity{}, s.cfg.Activities...)
	if len(activities) > 10 {
		activities = activities[:10]
	}
	return PublicState{
		Username: s.cfg.Admin.Username, Settings: s.cfg.Settings,
		Sites: append([]Site{}, s.cfg.Sites...), Backups: append([]Backup{}, s.cfg.Backups...),
		BackupDestinations: destinations, Databases: append([]ManagedDatabase{}, s.cfg.Databases...),
		FirewallRules: append([]FirewallRule{}, s.cfg.FirewallRules...),
		MailDomains:   append([]MailDomain{}, s.cfg.MailDomains...), Mailboxes: mailboxes,
		MailAliases: append([]MailAlias{}, s.cfg.MailAliases...), Activities: activities,
	}
}

func (s *Store) SiteByDomain(domain string) *Site {
	s.mu.RLock()
	defer s.mu.RUnlock()
	domain = strings.ToLower(strings.TrimSpace(domain))
	for _, site := range s.cfg.Sites {
		if site.Domain == domain {
			copy := site
			return &copy
		}
	}
	return nil
}

func (s *Store) SiteByID(id string) *Site {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, site := range s.cfg.Sites {
		if site.ID == id {
			copy := site
			return &copy
		}
	}
	return nil
}

func (s *Store) Sites() []Site {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Site{}, s.cfg.Sites...)
}

func (s *Store) AddSite(site Site) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Sites = append(s.cfg.Sites, site)
	if err := s.saveLocked(); err != nil {
		s.cfg.Sites = s.cfg.Sites[:len(s.cfg.Sites)-1]
		return err
	}
	return nil
}

func (s *Store) AddSiteWithDatabase(site Site, database *ManagedDatabase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.cfg.Sites {
		if existing.ID == site.ID || existing.Domain == site.Domain {
			return errors.New("website already exists")
		}
		if existing.RuntimeUID == site.RuntimeUID || existing.RuntimeGID == site.RuntimeGID {
			return errors.New("website runtime identity collision; retry provisioning")
		}
	}
	originalSites := append([]Site{}, s.cfg.Sites...)
	originalDatabases := append([]ManagedDatabase{}, s.cfg.Databases...)
	s.cfg.Sites = append(s.cfg.Sites, site)
	if database != nil {
		for _, existing := range s.cfg.Databases {
			if existing.Name == database.Name || existing.Username == database.Username {
				s.cfg.Sites = originalSites
				return errors.New("database already exists")
			}
		}
		s.cfg.Databases = append(s.cfg.Databases, *database)
	}
	if err := s.saveLocked(); err != nil {
		s.cfg.Sites = originalSites
		s.cfg.Databases = originalDatabases
		return err
	}
	return nil
}

func (s *Store) UpdateSite(id string, update func(*Site)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Sites {
		if s.cfg.Sites[i].ID == id {
			original := s.cfg.Sites[i]
			update(&s.cfg.Sites[i])
			if err := s.saveLocked(); err != nil {
				s.cfg.Sites[i] = original
				return err
			}
			return nil
		}
	}
	return errors.New("website not found")
}

func (s *Store) UpdateSiteDomain(id, domain string, roots ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, site := range s.cfg.Sites {
		if site.ID != id && strings.EqualFold(site.Domain, domain) {
			return errors.New("website already exists")
		}
	}
	for i := range s.cfg.Sites {
		if s.cfg.Sites[i].ID != id {
			continue
		}
		originalSites := append([]Site{}, s.cfg.Sites...)
		originalDatabases := append([]ManagedDatabase{}, s.cfg.Databases...)
		originalBackups := append([]Backup{}, s.cfg.Backups...)
		if zone := renameWebsiteDNS(s.cfg.Sites[i].DNSZone, s.cfg.Sites[i].Domain, domain); zone != nil {
			if err := advanceDNSSerial(zone, s.cfg.Sites[i].DNSZone.Serial); err != nil {
				return err
			}
			s.cfg.Sites[i].DNSZone = zone
		}
		oldHome := siteHome(s.cfg.Sites[i])
		if len(roots) > 0 {
			s.cfg.Sites[i].Root = roots[0]
		}
		newHome := siteHome(s.cfg.Sites[i])
		for j := range s.cfg.Backups {
			if s.cfg.Backups[j].SiteID == id {
				s.cfg.Backups[j].Domain = domain
				if oldHome != "" && newHome != "" && strings.HasPrefix(s.cfg.Backups[j].Path, oldHome+string(filepath.Separator)) {
					s.cfg.Backups[j].Path = newHome + strings.TrimPrefix(s.cfg.Backups[j].Path, oldHome)
				}
			}
		}
		s.cfg.Sites[i].Domain = domain
		s.cfg.Sites[i].SSL = false
		if s.cfg.Sites[i].Application != nil {
			application := *s.cfg.Sites[i].Application
			application.Certificate = ""
			application.CertificateKey = ""
			s.cfg.Sites[i].Application = &application
		}
		for j := range s.cfg.Databases {
			if s.cfg.Databases[j].SiteID == id {
				s.cfg.Databases[j].Domain = domain
			}
		}
		if err := s.saveLocked(); err != nil {
			s.cfg.Sites = originalSites
			s.cfg.Databases = originalDatabases
			s.cfg.Backups = originalBackups
			return err
		}
		return nil
	}
	return errors.New("website not found")
}

func (s *Store) RemoveSite(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Sites {
		if s.cfg.Sites[i].ID == id {
			original := append([]Site{}, s.cfg.Sites...)
			s.cfg.Sites = append(s.cfg.Sites[:i], s.cfg.Sites[i+1:]...)
			if err := s.saveLocked(); err != nil {
				s.cfg.Sites = original
				return err
			}
			return nil
		}
	}
	return errors.New("website not found")
}

func (s *Store) AddBackup(backup Backup) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	original := append([]Backup{}, s.cfg.Backups...)
	s.cfg.Backups = append([]Backup{backup}, s.cfg.Backups...)
	if err := s.saveLocked(); err != nil {
		s.cfg.Backups = original
		return err
	}
	return nil
}

func (s *Store) ExpiredBackups(cutoff time.Time) []Backup {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []Backup{}
	for _, backup := range s.cfg.Backups {
		if backup.ScheduleID == "" && !backupInProgress(backup.Status) && backup.CreatedAt.Before(cutoff) {
			result = append(result, backup)
		}
	}
	return result
}

func (s *Store) RemoveBackups(ids map[string]bool) error {
	if len(ids) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	original := append([]Backup{}, s.cfg.Backups...)
	kept := make([]Backup, 0, len(s.cfg.Backups))
	for _, backup := range s.cfg.Backups {
		if !ids[backup.ID] {
			kept = append(kept, backup)
		}
	}
	s.cfg.Backups = kept
	if err := s.saveLocked(); err != nil {
		s.cfg.Backups = original
		return err
	}
	return nil
}

func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Settings
}

func (s *Store) AddActivity(title, detail, kind string) error {
	id, _ := randomToken(8)
	s.mu.Lock()
	defer s.mu.Unlock()
	original := append([]Activity{}, s.cfg.Activities...)
	activity := Activity{ID: id, Title: title, Detail: detail, Kind: kind, CreatedAt: time.Now().UTC()}
	s.cfg.Activities = append([]Activity{activity}, s.cfg.Activities...)
	if days := s.cfg.Settings.ActivityRetentionDays; days > 0 {
		cutoff := time.Now().UTC().AddDate(0, 0, -days)
		kept := make([]Activity, 0, len(s.cfg.Activities))
		for _, item := range s.cfg.Activities {
			if !item.CreatedAt.Before(cutoff) {
				kept = append(kept, item)
			}
		}
		s.cfg.Activities = kept
	}
	if err := s.saveLocked(); err != nil {
		s.cfg.Activities = original
		return err
	}
	return nil
}

func (s *Store) SetSettings(settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	original := s.cfg.Settings
	s.cfg.Settings = settings
	if err := s.saveLocked(); err != nil {
		s.cfg.Settings = original
		return err
	}
	return nil
}

func (s *Store) UpdateSettings(update func(*Settings)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	original := s.cfg.Settings
	update(&s.cfg.Settings)
	if err := s.saveLocked(); err != nil {
		s.cfg.Settings = original
		return err
	}
	return nil
}

func (s *Store) Activities(page, perPage int) ([]Activity, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 10
	}
	if perPage > 100 {
		perPage = 100
	}
	total := len(s.cfg.Activities)
	if total == 0 || page-1 > (total-1)/perPage {
		return []Activity{}, total
	}
	start := (page - 1) * perPage
	if start >= total {
		return []Activity{}, total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	return append([]Activity{}, s.cfg.Activities[start:end]...), total
}

func (s *Store) saveLocked() error {
	content, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".litecp-config-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return err
	}
	if err := os.Chmod(s.path, 0600); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(s.path))
	if err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}
