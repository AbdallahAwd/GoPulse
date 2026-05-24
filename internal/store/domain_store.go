package store

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/amaa/gopulse/internal/models"
)

// DomainStore handles storage and retrieval of domain configurations
type DomainStore struct {
	filePath string
	mu       sync.RWMutex
	domains  []models.Domain
}

// NewDomainStore creates a new DomainStore
func NewDomainStore(filePath string) (*DomainStore, error) {
	store := &DomainStore{
		filePath: filePath,
		domains:  []models.Domain{},
	}

	if _, err := os.Stat(filePath); err == nil {
		if err := store.load(); err != nil {
			return nil, fmt.Errorf("failed to load domains: %w", err)
		}
	}

	return store, nil
}

func (s *DomainStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &s.domains)
}

func (s *DomainStore) save() error {
	data, err := json.MarshalIndent(s.domains, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

// AddDomain adds a new domain
func (s *DomainStore) AddDomain(domain models.Domain) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.domains {
		if existing.ID == domain.ID {
			return fmt.Errorf("domain with ID %s already exists", domain.ID)
		}
		if existing.ServerID == domain.ServerID && existing.Domain == domain.Domain {
			return fmt.Errorf("domain %s already configured for this server", domain.Domain)
		}
	}

	domain.CreatedAt = time.Now()
	s.domains = append(s.domains, domain)
	return s.save()
}

// RemoveDomain removes a domain by ID
func (s *DomainStore) RemoveDomain(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, domain := range s.domains {
		if domain.ID == id {
			s.domains = append(s.domains[:i], s.domains[i+1:]...)
			return s.save()
		}
	}
	return fmt.Errorf("domain not found")
}

// GetDomain returns a domain by ID
func (s *DomainStore) GetDomain(id string) (models.Domain, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, domain := range s.domains {
		if domain.ID == id {
			return domain, nil
		}
	}
	return models.Domain{}, fmt.Errorf("domain not found")
}

// ListDomains returns all domains
func (s *DomainStore) ListDomains() []models.Domain {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]models.Domain, len(s.domains))
	copy(result, s.domains)
	return result
}

// ListDomainsByServer returns all domains for a specific server
func (s *DomainStore) ListDomainsByServer(serverID string) []models.Domain {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []models.Domain
	for _, domain := range s.domains {
		if domain.ServerID == serverID {
			result = append(result, domain)
		}
	}
	return result
}
