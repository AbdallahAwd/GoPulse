package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/amaa/gopulse/internal/models"
)

// DomainStore handles storage and retrieval of domain configurations.
type DomainStore struct {
	legacyPath string
	db         *sql.DB
	mu         sync.RWMutex
}

// NewDomainStore creates a SQLite-backed DomainStore and imports legacy JSON once.
func NewDomainStore(legacyPath string) (*DomainStore, error) {
	db, err := openSQLiteStore(legacyPath)
	if err != nil {
		return nil, err
	}

	store := &DomainStore{
		legacyPath: legacyPath,
		db:         db,
	}

	err = importJSONOnce[models.Domain](db, "domains", legacyPath, `SELECT COUNT(*) FROM domains`, insertDomainTx)
	if err != nil {
		return nil, err
	}

	return store, nil
}

func insertDomainTx(tx *sql.Tx, domain models.Domain) error {
	_, err := tx.Exec(
		`INSERT OR IGNORE INTO domains (id, server_id, domain, port, ssl, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		domain.ID,
		domain.ServerID,
		domain.Domain,
		domain.Port,
		boolToInt(domain.SSL),
		timeToText(domain.CreatedAt),
	)
	return err
}

func scanDomain(scanner interface {
	Scan(dest ...interface{}) error
}) (models.Domain, error) {
	var domain models.Domain
	var ssl int
	var createdAtRaw string

	err := scanner.Scan(
		&domain.ID,
		&domain.ServerID,
		&domain.Domain,
		&domain.Port,
		&ssl,
		&createdAtRaw,
	)
	if err != nil {
		return models.Domain{}, err
	}

	domain.SSL = intToBool(ssl)
	domain.CreatedAt = timeFromText(createdAtRaw)
	return domain, nil
}

// AddDomain adds a new domain.
func (s *DomainStore) AddDomain(domain models.Domain) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var idExists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM domains WHERE id = ?`, domain.ID).Scan(&idExists); err != nil {
		return err
	}
	if idExists > 0 {
		return fmt.Errorf("domain with ID %s already exists", domain.ID)
	}

	var domainExists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM domains WHERE server_id = ? AND domain = ?`, domain.ServerID, domain.Domain).Scan(&domainExists); err != nil {
		return err
	}
	if domainExists > 0 {
		return fmt.Errorf("domain %s already configured for this server", domain.Domain)
	}

	if domain.CreatedAt.IsZero() {
		domain.CreatedAt = time.Now()
	}
	_, err := s.db.Exec(
		`INSERT INTO domains (id, server_id, domain, port, ssl, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		domain.ID,
		domain.ServerID,
		domain.Domain,
		domain.Port,
		boolToInt(domain.SSL),
		timeToText(domain.CreatedAt),
	)
	return err
}

// RemoveDomain removes a domain by ID.
func (s *DomainStore) RemoveDomain(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM domains WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("domain not found")
	}
	return nil
}

// GetDomain returns a domain by ID.
func (s *DomainStore) GetDomain(id string) (models.Domain, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(
		`SELECT id, server_id, domain, port, ssl, created_at FROM domains WHERE id = ?`,
		id,
	)
	domain, err := scanDomain(row)
	if err == sql.ErrNoRows {
		return models.Domain{}, fmt.Errorf("domain not found")
	}
	return domain, err
}

// ListDomains returns all domains.
func (s *DomainStore) ListDomains() []models.Domain {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT id, server_id, domain, port, ssl, created_at FROM domains ORDER BY rowid`)
	if err != nil {
		return []models.Domain{}
	}
	defer rows.Close()

	var domains []models.Domain
	for rows.Next() {
		domain, err := scanDomain(rows)
		if err == nil {
			domains = append(domains, domain)
		}
	}
	return domains
}

// ListDomainsByServer returns all domains for a specific server.
func (s *DomainStore) ListDomainsByServer(serverID string) []models.Domain {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, server_id, domain, port, ssl, created_at FROM domains
		WHERE server_id = ? ORDER BY rowid`,
		serverID,
	)
	if err != nil {
		return []models.Domain{}
	}
	defer rows.Close()

	var domains []models.Domain
	for rows.Next() {
		domain, err := scanDomain(rows)
		if err == nil {
			domains = append(domains, domain)
		}
	}
	return domains
}
