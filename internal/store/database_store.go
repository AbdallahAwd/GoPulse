package store

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/amaa/gopulse/internal/models"
)

// DatabaseStore handles storage and retrieval of database configurations
type DatabaseStore struct {
	filePath  string
	mu        sync.RWMutex
	databases []models.Database
}

// NewDatabaseStore creates a new DatabaseStore
func NewDatabaseStore(filePath string) (*DatabaseStore, error) {
	store := &DatabaseStore{
		filePath:  filePath,
		databases: []models.Database{},
	}

	// Load existing databases if file exists
	if _, err := os.Stat(filePath); err == nil {
		if err := store.load(); err != nil {
			return nil, fmt.Errorf("failed to load databases: %w", err)
		}
	}

	return store, nil
}

// load reads databases from the JSON file
func (s *DatabaseStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &s.databases)
}

// save writes databases to the JSON file
func (s *DatabaseStore) save() error {
	// Note: mu should be locked by caller

	data, err := json.MarshalIndent(s.databases, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, data, 0644)
}

// AddDatabase adds a new database record
func (s *DatabaseStore) AddDatabase(db models.Database) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if ID already exists
	for _, existing := range s.databases {
		if existing.ID == db.ID {
			return fmt.Errorf("database with ID %s already exists", db.ID)
		}
	}

	db.CreatedAt = time.Now()
	s.databases = append(s.databases, db)
	return s.save()
}

// RemoveDatabase removes a database record by ID
func (s *DatabaseStore) RemoveDatabase(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, db := range s.databases {
		if db.ID == id {
			s.databases = append(s.databases[:i], s.databases[i+1:]...)
			return s.save()
		}
	}

	return fmt.Errorf("database not found")
}

// GetDatabase returns a database record by ID
func (s *DatabaseStore) GetDatabase(id string) (models.Database, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, db := range s.databases {
		if db.ID == id {
			return db, nil
		}
	}

	return models.Database{}, fmt.Errorf("database not found")
}

// ListDatabases returns all databases
func (s *DatabaseStore) ListDatabases() []models.Database {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]models.Database, len(s.databases))
	copy(result, s.databases)
	return result
}

// UpdateDatabase updates an existing database record
func (s *DatabaseStore) UpdateDatabase(db models.Database) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, existing := range s.databases {
		if existing.ID == db.ID {
			if db.CreatedAt.IsZero() {
				db.CreatedAt = existing.CreatedAt
			}
			s.databases[i] = db
			return s.save()
		}
	}

	return fmt.Errorf("database not found")
}
