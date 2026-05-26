package store

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/amaa/gopulse/internal/models"
	"github.com/google/uuid"
)

// UserStore handles storage and retrieval of user credentials
type UserStore struct {
	filePath string
	mu       sync.RWMutex
	users    []models.User
}

// NewUserStore creates a new UserStore
func NewUserStore(filePath string) (*UserStore, error) {
	store := &UserStore{
		filePath: filePath,
		users:    []models.User{},
	}

	// Load existing users if file exists
	if _, err := os.Stat(filePath); err == nil {
		if err := store.load(); err != nil {
			return nil, fmt.Errorf("failed to load users: %w", err)
		}
	}

	return store, nil
}

// load reads users from the JSON file
func (s *UserStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &s.users)
}

// save writes users to the JSON file
func (s *UserStore) save() error {
	data, err := json.MarshalIndent(s.users, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, data, 0644)
}

// AddUser adds a new user
func (s *UserStore) AddUser(user models.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if user already exists
	for _, existing := range s.users {
		if existing.Username == user.Username {
			return fmt.Errorf("user %s already exists", user.Username)
		}
	}

	if user.ID == "" {
		user.ID = uuid.New().String()
	}
	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now()
	}

	// Bootstrap configuration for the first user
	if len(s.users) == 0 {
		user.Role = "admin"
		user.Permissions = []string{"*"}
		user.AllowedServers = []string{}
	} else if user.Role == "" {
		user.Role = "viewer"
	}

	s.users = append(s.users, user)
	return s.save()
}

// GetUser returns a user by Username
func (s *UserStore) GetUser(username string) (models.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, user := range s.users {
		if user.Username == username {
			return user, nil
		}
	}

	return models.User{}, fmt.Errorf("user not found")
}

// ListUsers returns a copy of all registered users
func (s *UserStore) ListUsers() []models.User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	usersCopy := make([]models.User, len(s.users))
	copy(usersCopy, s.users)
	return usersCopy
}

// DeleteUserByID deletes a user from the pool by ID
func (s *UserStore) DeleteUserByID(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, user := range s.users {
		if user.ID == id {
			s.users = append(s.users[:i], s.users[i+1:]...)
			return s.save()
		}
	}

	return fmt.Errorf("user not found")
}

// UpdateUser updates an existing user
func (s *UserStore) UpdateUser(user models.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, existing := range s.users {
		if existing.ID == user.ID {
			if user.Password == "" {
				user.Password = existing.Password
			}
			s.users[i] = user
			return s.save()
		}
	}

	return fmt.Errorf("user not found")
}

// HasUsers returns true if there is at least one registered user
func (s *UserStore) HasUsers() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users) > 0
}

// Count returns the number of registered users
func (s *UserStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users)
}
