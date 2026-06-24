package store

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/amaa/gopulse/internal/models"
)

// ServerStore handles storage and retrieval of server configurations.
type ServerStore struct {
	legacyPath string
	db         *sql.DB
	mu         sync.RWMutex
}

// NewServerStore creates a SQLite-backed ServerStore and imports legacy JSON once.
func NewServerStore(legacyPath string) (*ServerStore, error) {
	db, err := openSQLiteStore(legacyPath)
	if err != nil {
		return nil, err
	}

	store := &ServerStore{
		legacyPath: legacyPath,
		db:         db,
	}

	err = importJSONOnce[models.Server](db, "servers", legacyPath, `SELECT COUNT(*) FROM servers`, insertServerTx)
	if err != nil {
		return nil, err
	}

	return store, nil
}

func insertServerTx(tx *sql.Tx, server models.Server) error {
	_, err := tx.Exec(
		`INSERT OR IGNORE INTO servers (
			id, name, host, port, user, auth_type, key_path, password, tags, is_db_only
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		server.ID,
		server.Name,
		server.Host,
		server.Port,
		server.User,
		server.AuthType,
		server.KeyPath,
		server.Password,
		encodeJSON(server.Tags, "[]"),
		boolToInt(server.IsDbOnly),
	)
	return err
}

func scanServer(scanner interface {
	Scan(dest ...interface{}) error
}) (models.Server, error) {
	var server models.Server
	var tagsRaw string
	var isDbOnly int

	err := scanner.Scan(
		&server.ID,
		&server.Name,
		&server.Host,
		&server.Port,
		&server.User,
		&server.AuthType,
		&server.KeyPath,
		&server.Password,
		&tagsRaw,
		&isDbOnly,
	)
	if err != nil {
		return models.Server{}, err
	}

	if err := decodeJSON(tagsRaw, &server.Tags); err != nil {
		return models.Server{}, err
	}
	server.IsDbOnly = intToBool(isDbOnly)
	return server, nil
}

// AddServer adds a new server.
func (s *ServerStore) AddServer(server models.Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM servers WHERE id = ?`, server.ID).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return fmt.Errorf("server with ID %s already exists", server.ID)
	}

	_, err := s.db.Exec(
		`INSERT INTO servers (
			id, name, host, port, user, auth_type, key_path, password, tags, is_db_only
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		server.ID,
		server.Name,
		server.Host,
		server.Port,
		server.User,
		server.AuthType,
		server.KeyPath,
		server.Password,
		encodeJSON(server.Tags, "[]"),
		boolToInt(server.IsDbOnly),
	)
	return err
}

// RemoveServer removes a server by ID.
func (s *ServerStore) RemoveServer(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM servers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("server not found")
	}
	return nil
}

// GetServer returns a server by ID.
func (s *ServerStore) GetServer(id string) (models.Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(
		`SELECT id, name, host, port, user, auth_type, key_path, password, tags, is_db_only
		FROM servers WHERE id = ?`,
		id,
	)
	server, err := scanServer(row)
	if err == sql.ErrNoRows {
		return models.Server{}, fmt.Errorf("server not found")
	}
	return server, err
}

// ListServers returns all servers.
func (s *ServerStore) ListServers() []models.Server {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, name, host, port, user, auth_type, key_path, password, tags, is_db_only
		FROM servers ORDER BY rowid`,
	)
	if err != nil {
		return []models.Server{}
	}
	defer rows.Close()

	var servers []models.Server
	for rows.Next() {
		server, err := scanServer(rows)
		if err == nil {
			servers = append(servers, server)
		}
	}
	return servers
}

// UpdateServer updates an existing server.
func (s *ServerStore) UpdateServer(server models.Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		`UPDATE servers SET
			name = ?, host = ?, port = ?, user = ?, auth_type = ?, key_path = ?,
			password = ?, tags = ?, is_db_only = ?
		WHERE id = ?`,
		server.Name,
		server.Host,
		server.Port,
		server.User,
		server.AuthType,
		server.KeyPath,
		server.Password,
		encodeJSON(server.Tags, "[]"),
		boolToInt(server.IsDbOnly),
		server.ID,
	)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("server not found")
	}
	return nil
}
