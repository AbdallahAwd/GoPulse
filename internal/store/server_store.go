package store

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/amaa/gopulse/internal/models"
)

// ServerStore handles storage and retrieval of server configurations
type ServerStore struct {
	filePath string
	mu       sync.RWMutex
	servers  []models.Server
}

// NewServerStore creates a new ServerStore
func NewServerStore(filePath string) (*ServerStore, error) {
	store := &ServerStore{
		filePath: filePath,
		servers:  []models.Server{},
	}

	// Load existing servers if file exists
	if _, err := os.Stat(filePath); err == nil {
		if err := store.load(); err != nil {
			return nil, fmt.Errorf("failed to load servers: %w", err)
		}
	}

	return store, nil
}

// load reads servers from the JSON file
func (s *ServerStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &s.servers)
}

// save writes servers to the JSON file
func (s *ServerStore) save() error {
	// Note: mu should be locked by caller

	data, err := json.MarshalIndent(s.servers, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, data, 0644)
}

// AddServer adds a new server
func (s *ServerStore) AddServer(server models.Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if ID already exists
	for _, existing := range s.servers {
		if existing.ID == server.ID {
			return fmt.Errorf("server with ID %s already exists", server.ID)
		}
	}

	s.servers = append(s.servers, server)
	return s.save()
}

// RemoveServer removes a server by ID
func (s *ServerStore) RemoveServer(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, server := range s.servers {
		if server.ID == id {
			// Remove element at index i
			s.servers = append(s.servers[:i], s.servers[i+1:]...)
			return s.save()
		}
	}

	return fmt.Errorf("server not found")
}

// GetServer returns a server by ID
func (s *ServerStore) GetServer(id string) (models.Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, server := range s.servers {
		if server.ID == id {
			return server, nil
		}
	}

	return models.Server{}, fmt.Errorf("server not found")
}

// ListServers returns all servers
func (s *ServerStore) ListServers() []models.Server {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy to avoid race conditions if caller modifies the slice
	result := make([]models.Server, len(s.servers))
	copy(result, s.servers)
	return result
}

// UpdateServer updates an existing server
func (s *ServerStore) UpdateServer(server models.Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, existing := range s.servers {
		if existing.ID == server.ID {
			s.servers[i] = server
			return s.save()
		}
	}

	return fmt.Errorf("server not found")
}
