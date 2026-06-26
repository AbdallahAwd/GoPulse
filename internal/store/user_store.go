package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/amaa/gopulse/internal/models"
	"github.com/google/uuid"
)

// UserStore handles storage and retrieval of user credentials.
type UserStore struct {
	legacyPath string
	db         *sql.DB
	mu         sync.RWMutex
}

// NewUserStore creates a SQLite-backed UserStore and imports legacy JSON once.
func NewUserStore(legacyPath string) (*UserStore, error) {
	db, err := openSQLiteStore(legacyPath)
	if err != nil {
		return nil, err
	}

	store := &UserStore{
		legacyPath: legacyPath,
		db:         db,
	}

	err = importJSONOnce[models.User](db, "users", legacyPath, `SELECT COUNT(*) FROM users`, insertUserTx)
	if err != nil {
		return nil, err
	}

	return store, nil
}

func insertUserTx(tx *sql.Tx, user models.User) error {
	_, err := tx.Exec(
		`INSERT OR IGNORE INTO users (
			id, username, password, role, permissions, allowed_servers, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		user.ID,
		user.Username,
		user.Password,
		user.Role,
		encodeJSON(user.Permissions, "[]"),
		encodeJSON(user.AllowedServers, "[]"),
		timeToText(user.CreatedAt),
	)
	return err
}

func scanUser(scanner interface {
	Scan(dest ...interface{}) error
}) (models.User, error) {
	var user models.User
	var permissionsRaw string
	var allowedServersRaw string
	var createdAtRaw string

	err := scanner.Scan(
		&user.ID,
		&user.Username,
		&user.Password,
		&user.Role,
		&permissionsRaw,
		&allowedServersRaw,
		&createdAtRaw,
	)
	if err != nil {
		return models.User{}, err
	}

	if err := decodeJSON(permissionsRaw, &user.Permissions); err != nil {
		return models.User{}, err
	}
	if err := decodeJSON(allowedServersRaw, &user.AllowedServers); err != nil {
		return models.User{}, err
	}
	user.CreatedAt = timeFromText(createdAtRaw)
	return user, nil
}

// AddUser adds a new user.
func (s *UserStore) AddUser(user models.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = ?`, user.Username).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return fmt.Errorf("user %s already exists", user.Username)
	}

	if user.ID == "" {
		user.ID = uuid.New().String()
	}
	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now()
	}

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		user.Role = "admin"
		user.Permissions = []string{"*"}
		user.AllowedServers = []string{}
	} else if user.Role == "" {
		user.Role = "viewer"
	}

	_, err := s.db.Exec(
		`INSERT INTO users (
			id, username, password, role, permissions, allowed_servers, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		user.ID,
		user.Username,
		user.Password,
		user.Role,
		encodeJSON(user.Permissions, "[]"),
		encodeJSON(user.AllowedServers, "[]"),
		timeToText(user.CreatedAt),
	)
	return err
}

// GetUser returns a user by username.
func (s *UserStore) GetUser(username string) (models.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(
		`SELECT id, username, password, role, permissions, allowed_servers, created_at
		FROM users WHERE username = ?`,
		username,
	)
	user, err := scanUser(row)
	if err == sql.ErrNoRows {
		return models.User{}, fmt.Errorf("user not found")
	}
	return user, err
}

// ListUsers returns a copy of all registered users.
func (s *UserStore) ListUsers() []models.User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, username, password, role, permissions, allowed_servers, created_at
		FROM users ORDER BY rowid`,
	)
	if err != nil {
		return []models.User{}
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		user, err := scanUser(rows)
		if err == nil {
			users = append(users, user)
		}
	}
	return users
}

// DeleteUserByID deletes a user from the pool by ID.
func (s *UserStore) DeleteUserByID(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

// UpdateUser updates an existing user.
func (s *UserStore) UpdateUser(user models.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if user.Password == "" {
		var existingPassword string
		err := s.db.QueryRow(`SELECT password FROM users WHERE id = ?`, user.ID).Scan(&existingPassword)
		if err == sql.ErrNoRows {
			return fmt.Errorf("user not found")
		}
		if err != nil {
			return err
		}
		user.Password = existingPassword
	}

	result, err := s.db.Exec(
		`UPDATE users SET
			username = ?, password = ?, role = ?, permissions = ?, allowed_servers = ?, created_at = ?
		WHERE id = ?`,
		user.Username,
		user.Password,
		user.Role,
		encodeJSON(user.Permissions, "[]"),
		encodeJSON(user.AllowedServers, "[]"),
		timeToText(user.CreatedAt),
		user.ID,
	)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

// GetUserByID returns a user by their unique ID.
func (s *UserStore) GetUserByID(id string) (models.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(
		`SELECT id, username, password, role, permissions, allowed_servers, created_at
		FROM users WHERE id = ?`,
		id,
	)
	user, err := scanUser(row)
	if err == sql.ErrNoRows {
		return models.User{}, fmt.Errorf("user not found")
	}
	return user, err
}

// AddUserRaw inserts a user record exactly as provided, without modifying
// the role, permissions, password hash, or timestamps. This is used by
// the import flow where the data is already fully formed.
func (s *UserStore) AddUserRaw(user models.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, user.ID).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return fmt.Errorf("user with ID %s already exists", user.ID)
	}

	// Also check by username to avoid UNIQUE constraint violation.
	var usernameExists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = ?`, user.Username).Scan(&usernameExists); err != nil {
		return err
	}
	if usernameExists > 0 {
		return fmt.Errorf("user %s already exists", user.Username)
	}

	if user.ID == "" {
		user.ID = uuid.New().String()
	}
	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now()
	}

	_, err := s.db.Exec(
		`INSERT INTO users (
			id, username, password, role, permissions, allowed_servers, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		user.ID,
		user.Username,
		user.Password,
		user.Role,
		encodeJSON(user.Permissions, "[]"),
		encodeJSON(user.AllowedServers, "[]"),
		timeToText(user.CreatedAt),
	)
	return err
}

// HasUsers returns true if there is at least one registered user.
func (s *UserStore) HasUsers() bool {
	return s.Count() > 0
}

// Count returns the number of registered users.
func (s *UserStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return 0
	}
	return count
}
