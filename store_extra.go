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
			return nil
		}
	}
	s.cfg.Databases = append(s.cfg.Databases, database)
	return s.saveLocked()
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

func (s *Store) RemoveDatabase(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Databases {
		if s.cfg.Databases[i].ID == id {
			s.cfg.Databases = append(s.cfg.Databases[:i], s.cfg.Databases[i+1:]...)
			return s.saveLocked()
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
	return s.saveLocked()
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
			s.cfg.BackupDestinations = append(s.cfg.BackupDestinations[:i], s.cfg.BackupDestinations[i+1:]...)
			return s.saveLocked()
		}
	}
	return nil
}

func (s *Store) AddFirewallRule(rule FirewallRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.FirewallRules = append(s.cfg.FirewallRules, rule)
	return s.saveLocked()
}

func (s *Store) HasFirewallRule(rule FirewallRule) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.cfg.FirewallRules {
		if item.Port == rule.Port && item.Protocol == rule.Protocol && item.Source == rule.Source {
			return true
		}
	}
	return false
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
			s.cfg.FirewallRules = append(s.cfg.FirewallRules[:i], s.cfg.FirewallRules[i+1:]...)
			return s.saveLocked()
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
	return s.saveLocked()
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
	return s.saveLocked()
}

func (s *Store) AddMailbox(mailbox Mailbox) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Mailboxes = append(s.cfg.Mailboxes, mailbox)
	return s.saveLocked()
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
			update(&s.cfg.Mailboxes[i])
			return s.saveLocked()
		}
	}
	return nil
}

func (s *Store) RemoveMailbox(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Mailboxes {
		if s.cfg.Mailboxes[i].ID == id {
			s.cfg.Mailboxes = append(s.cfg.Mailboxes[:i], s.cfg.Mailboxes[i+1:]...)
			return s.saveLocked()
		}
	}
	return nil
}

func (s *Store) AddMailAlias(alias MailAlias) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.MailAliases = append(s.cfg.MailAliases, alias)
	return s.saveLocked()
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
			s.cfg.MailAliases = append(s.cfg.MailAliases[:i], s.cfg.MailAliases[i+1:]...)
			return s.saveLocked()
		}
	}
	return nil
}
