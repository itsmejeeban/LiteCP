package main

import (
	"errors"
	"strings"
)

func (s *Store) AddDatabase(database ManagedDatabase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.cfg.Databases {
		if item.Name == database.Name || item.Username == database.Username {
			return errors.New("a database with that name or username already exists")
		}
	}
	s.cfg.Databases = append(s.cfg.Databases, database)
	if err := s.saveLocked(); err != nil {
		s.cfg.Databases = s.cfg.Databases[:len(s.cfg.Databases)-1]
		return err
	}
	return nil
}

func (s *Store) DatabaseByID(id string) *ManagedDatabase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.cfg.Databases {
		if item.ID == id {
			copy := item
			return &copy
		}
	}
	return nil
}

func (s *Store) DatabaseBySiteID(siteID string) *ManagedDatabase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.cfg.Databases {
		if item.SiteID == siteID {
			copy := item
			return &copy
		}
	}
	return nil
}

func (s *Store) UpdateDatabase(id string, update func(*ManagedDatabase)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Databases {
		if s.cfg.Databases[i].ID == id {
			original := s.cfg.Databases[i]
			update(&s.cfg.Databases[i])
			if err := s.saveLocked(); err != nil {
				s.cfg.Databases[i] = original
				return err
			}
			return nil
		}
	}
	return errors.New("database not found")
}

func (s *Store) RemoveDatabase(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Databases {
		if s.cfg.Databases[i].ID == id {
			original := append([]ManagedDatabase{}, s.cfg.Databases...)
			s.cfg.Databases = append(s.cfg.Databases[:i], s.cfg.Databases[i+1:]...)
			if err := s.saveLocked(); err != nil {
				s.cfg.Databases = original
				return err
			}
			return nil
		}
	}
	return nil
}

func (s *Store) AddBackupDestination(destination BackupDestination) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.cfg.BackupDestinations {
		if strings.EqualFold(item.Name, destination.Name) {
			return errors.New("a backup destination with that name already exists")
		}
	}
	s.cfg.BackupDestinations = append(s.cfg.BackupDestinations, destination)
	if err := s.saveLocked(); err != nil {
		s.cfg.BackupDestinations = s.cfg.BackupDestinations[:len(s.cfg.BackupDestinations)-1]
		return err
	}
	return nil
}

func (s *Store) BackupDestinationByID(id string) *BackupDestination {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.cfg.BackupDestinations {
		if item.ID == id {
			copy := item
			return &copy
		}
	}
	return nil
}

func (s *Store) RemoveBackupDestination(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.BackupDestinations {
		if s.cfg.BackupDestinations[i].ID == id {
			original := append([]BackupDestination{}, s.cfg.BackupDestinations...)
			s.cfg.BackupDestinations = append(s.cfg.BackupDestinations[:i], s.cfg.BackupDestinations[i+1:]...)
			if err := s.saveLocked(); err != nil {
				s.cfg.BackupDestinations = original
				return err
			}
			return nil
		}
	}
	return nil
}

func (s *Store) AddFirewallRule(rule FirewallRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.FirewallRules = append(s.cfg.FirewallRules, rule)
	if err := s.saveLocked(); err != nil {
		s.cfg.FirewallRules = s.cfg.FirewallRules[:len(s.cfg.FirewallRules)-1]
		return err
	}
	return nil
}

func (s *Store) HasFirewallRule(rule FirewallRule) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rule = canonicalFirewallRule(normalizeFirewallRule(rule))
	addresses := strings.Split(firewallAddresses(rule), ",")
	for _, item := range s.cfg.FirewallRules {
		if rule.ID != "" && item.ID == rule.ID {
			continue
		}
		item = canonicalFirewallRule(normalizeFirewallRule(item))
		if item.Port == rule.Port && item.Protocol == rule.Protocol && item.Direction == rule.Direction && item.Action == rule.Action {
			for _, address := range strings.Split(firewallAddresses(item), ",") {
				if containsString(addresses, address) {
					return true
				}
			}
		}
	}
	return false
}

func (s *Store) UpdateFirewallRule(id string, rule FirewallRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.FirewallRules {
		if s.cfg.FirewallRules[i].ID == id {
			original := s.cfg.FirewallRules[i]
			rule.ID, rule.CreatedAt = original.ID, original.CreatedAt
			s.cfg.FirewallRules[i] = rule
			if err := s.saveLocked(); err != nil {
				s.cfg.FirewallRules[i] = original
				return err
			}
			return nil
		}
	}
	return errors.New("firewall rule not found")
}

func (s *Store) FirewallRuleByID(id string) *FirewallRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.cfg.FirewallRules {
		if item.ID == id {
			copy := item
			return &copy
		}
	}
	return nil
}

func (s *Store) RemoveFirewallRule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.FirewallRules {
		if s.cfg.FirewallRules[i].ID == id {
			original := append([]FirewallRule{}, s.cfg.FirewallRules...)
			s.cfg.FirewallRules = append(s.cfg.FirewallRules[:i], s.cfg.FirewallRules[i+1:]...)
			if err := s.saveLocked(); err != nil {
				s.cfg.FirewallRules = original
				return err
			}
			return nil
		}
	}
	return nil
}

func (s *Store) MailConfig() ([]MailDomain, []Mailbox, []MailAlias) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]MailDomain{}, s.cfg.MailDomains...), append([]Mailbox{}, s.cfg.Mailboxes...), append([]MailAlias{}, s.cfg.MailAliases...)
}

func (s *Store) AddMailDomain(domain MailDomain) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.MailDomains = append(s.cfg.MailDomains, domain)
	if err := s.saveLocked(); err != nil {
		s.cfg.MailDomains = s.cfg.MailDomains[:len(s.cfg.MailDomains)-1]
		return err
	}
	return nil
}

func (s *Store) MailDomainByID(id string) *MailDomain {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.cfg.MailDomains {
		if item.ID == id {
			copy := item
			return &copy
		}
	}
	return nil
}

func (s *Store) HasMailDomain(domain string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.cfg.MailDomains {
		if item.Domain == domain {
			return true
		}
	}
	return false
}

func (s *Store) RemoveMailDomain(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	originalDomains := append([]MailDomain{}, s.cfg.MailDomains...)
	originalMailboxes := append([]Mailbox{}, s.cfg.Mailboxes...)
	originalAliases := append([]MailAlias{}, s.cfg.MailAliases...)
	domain := ""
	for i := range s.cfg.MailDomains {
		if s.cfg.MailDomains[i].ID == id {
			domain = s.cfg.MailDomains[i].Domain
			s.cfg.MailDomains = append(s.cfg.MailDomains[:i], s.cfg.MailDomains[i+1:]...)
			break
		}
	}
	if domain != "" {
		mailboxes := s.cfg.Mailboxes[:0]
		for _, item := range s.cfg.Mailboxes {
			if item.Domain != domain {
				mailboxes = append(mailboxes, item)
			}
		}
		s.cfg.Mailboxes = mailboxes
		aliases := s.cfg.MailAliases[:0]
		for _, item := range s.cfg.MailAliases {
			if !strings.HasSuffix(item.Source, "@"+domain) {
				aliases = append(aliases, item)
			}
		}
		s.cfg.MailAliases = aliases
	}
	if domain == "" {
		return nil
	}
	if err := s.saveLocked(); err != nil {
		s.cfg.MailDomains = originalDomains
		s.cfg.Mailboxes = originalMailboxes
		s.cfg.MailAliases = originalAliases
		return err
	}
	return nil
}

func (s *Store) AddMailbox(mailbox Mailbox) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Mailboxes = append(s.cfg.Mailboxes, mailbox)
	if err := s.saveLocked(); err != nil {
		s.cfg.Mailboxes = s.cfg.Mailboxes[:len(s.cfg.Mailboxes)-1]
		return err
	}
	return nil
}

func (s *Store) MailboxByID(id string) *Mailbox {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.cfg.Mailboxes {
		if item.ID == id {
			copy := item
			return &copy
		}
	}
	return nil
}

func (s *Store) UpdateMailbox(id string, update func(*Mailbox)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Mailboxes {
		if s.cfg.Mailboxes[i].ID == id {
			original := s.cfg.Mailboxes[i]
			update(&s.cfg.Mailboxes[i])
			if err := s.saveLocked(); err != nil {
				s.cfg.Mailboxes[i] = original
				return err
			}
			return nil
		}
	}
	return nil
}

func (s *Store) RemoveMailbox(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Mailboxes {
		if s.cfg.Mailboxes[i].ID == id {
			original := append([]Mailbox{}, s.cfg.Mailboxes...)
			s.cfg.Mailboxes = append(s.cfg.Mailboxes[:i], s.cfg.Mailboxes[i+1:]...)
			if err := s.saveLocked(); err != nil {
				s.cfg.Mailboxes = original
				return err
			}
			return nil
		}
	}
	return nil
}

func (s *Store) AddMailAlias(alias MailAlias) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.MailAliases = append(s.cfg.MailAliases, alias)
	if err := s.saveLocked(); err != nil {
		s.cfg.MailAliases = s.cfg.MailAliases[:len(s.cfg.MailAliases)-1]
		return err
	}
	return nil
}

func (s *Store) MailAliasByID(id string) *MailAlias {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.cfg.MailAliases {
		if item.ID == id {
			copy := item
			return &copy
		}
	}
	return nil
}

func (s *Store) RemoveMailAlias(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.MailAliases {
		if s.cfg.MailAliases[i].ID == id {
			original := append([]MailAlias{}, s.cfg.MailAliases...)
			s.cfg.MailAliases = append(s.cfg.MailAliases[:i], s.cfg.MailAliases[i+1:]...)
			if err := s.saveLocked(); err != nil {
				s.cfg.MailAliases = original
				return err
			}
			return nil
		}
	}
	return nil
}

// Commit the website's first mail routing domain and mailbox together.
func (s *Store) AddMailboxWithDomain(mailbox Mailbox, domain *MailDomain) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.cfg.Mailboxes {
		if item.Address == mailbox.Address {
			return errors.New("mailbox already exists")
		}
	}
	originalDomains := append([]MailDomain{}, s.cfg.MailDomains...)
	originalMailboxes := append([]Mailbox{}, s.cfg.Mailboxes...)
	if domain != nil {
		for _, item := range s.cfg.MailDomains {
			if item.Domain == domain.Domain {
				return errors.New("mail domain already exists")
			}
		}
		s.cfg.MailDomains = append(s.cfg.MailDomains, *domain)
	}
	s.cfg.Mailboxes = append(s.cfg.Mailboxes, mailbox)
	if err := s.saveLocked(); err != nil {
		s.cfg.MailDomains = originalDomains
		s.cfg.Mailboxes = originalMailboxes
		return err
	}
	return nil
}

func (s *Store) UpdateBackupDestination(destination BackupDestination) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.cfg.BackupDestinations {
		if item.ID != destination.ID && strings.EqualFold(item.Name, destination.Name) {
			return errors.New("a backup destination with that name already exists")
		}
	}
	for i, item := range s.cfg.BackupDestinations {
		if item.ID == destination.ID {
			s.cfg.BackupDestinations[i] = destination
			if err := s.saveLocked(); err != nil {
				s.cfg.BackupDestinations[i] = item
				return err
			}
			return nil
		}
	}
	return errors.New("backup destination not found")
}

func (s *Store) UpdateMailAlias(alias MailAlias) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.cfg.MailAliases {
		if item.ID != alias.ID && item.Source == alias.Source {
			return errors.New("source already has a forward")
		}
	}
	for i, item := range s.cfg.MailAliases {
		if item.ID == alias.ID {
			s.cfg.MailAliases[i] = alias
			if err := s.saveLocked(); err != nil {
				s.cfg.MailAliases[i] = item
				return err
			}
			return nil
		}
	}
	return errors.New("mail forward not found")
}
