package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Site struct {
	ID         string    `json:"id"`
	Domain     string    `json:"domain"`
	Root       string    `json:"root"`
	PHPVersion string    `json:"phpVersion"`
	SSL        bool      `json:"ssl"`
	Cron       bool      `json:"cron"`
	Database   string    `json:"database,omitempty"`
	DBUser     string    `json:"dbUser,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

type Backup struct {
	ID        string    `json:"id"`
	SiteID    string    `json:"siteId"`
	Domain    string    `json:"domain"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type Activity struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"createdAt"`
}

type Settings struct {
	Email           string `json:"email"`
	Timezone        string `json:"timezone"`
	BackupRetention int    `json:"backupRetention"`
}

type Admin struct {
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
}

type Config struct {
	Admin      Admin      `json:"admin"`
	Settings   Settings   `json:"settings"`
	Sites      []Site     `json:"sites"`
	Backups    []Backup   `json:"backups"`
	Activities []Activity `json:"activities"`
}

type PublicState struct {
	Version    string         `json:"version"`
	Username   string         `json:"username"`
	Settings   Settings       `json:"settings"`
	Sites      []Site         `json:"sites"`
	Backups    []Backup       `json:"backups"`
	Activities []Activity     `json:"activities"`
	System     SystemSnapshot `json:"system"`
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
		Admin: Admin{Username: username, PasswordHash: hash},
		Settings: Settings{Timezone: "UTC", BackupRetention: 14},
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
	return subtleStringEqual(admin.Username, strings.TrimSpace(username)) && verifyPassword(admin.PasswordHash, password)
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
	return PublicState{
		Username: s.cfg.Admin.Username, Settings: s.cfg.Settings,
		Sites: append([]Site(nil), s.cfg.Sites...),
		Backups: append([]Backup(nil), s.cfg.Backups...),
		Activities: append([]Activity(nil), s.cfg.Activities...),
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

func (s *Store) AddSite(site Site) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Sites = append(s.cfg.Sites, site)
	_ = s.saveLocked()
}

func (s *Store) UpdateSite(id string, update func(*Site)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Sites {
		if s.cfg.Sites[i].ID == id {
			update(&s.cfg.Sites[i])
			_ = s.saveLocked()
			return
		}
	}
}

func (s *Store) RemoveSite(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Sites {
		if s.cfg.Sites[i].ID == id {
			s.cfg.Sites = append(s.cfg.Sites[:i], s.cfg.Sites[i+1:]...)
			_ = s.saveLocked()
			return
		}
	}
}

func (s *Store) AddBackup(backup Backup) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Backups = append([]Backup{backup}, s.cfg.Backups...)
	if len(s.cfg.Backups) > 100 {
		s.cfg.Backups = s.cfg.Backups[:100]
	}
	_ = s.saveLocked()
}

func (s *Store) AddActivity(title, detail, kind string) {
	id, _ := randomToken(8)
	s.mu.Lock()
	defer s.mu.Unlock()
	activity := Activity{ID: id, Title: title, Detail: detail, Kind: kind, CreatedAt: time.Now().UTC()}
	s.cfg.Activities = append([]Activity{activity}, s.cfg.Activities...)
	if len(s.cfg.Activities) > 40 {
		s.cfg.Activities = s.cfg.Activities[:40]
	}
	_ = s.saveLocked()
}

func (s *Store) SetSettings(settings Settings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Settings = settings
	_ = s.saveLocked()
}

func (s *Store) saveLocked() error {
	content, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, content, 0600); err != nil {
		return err
	}
	if err := os.Rename(temporary, s.path); err != nil {
		return err
	}
	return os.Chmod(s.path, 0600)
}
