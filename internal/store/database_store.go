package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/amaa/gopulse/internal/models"
)

// DatabaseStore handles storage and retrieval of database configurations.
type DatabaseStore struct {
	legacyPath string
	db         *sql.DB
	mu         sync.RWMutex
}

// NewDatabaseStore creates a SQLite-backed DatabaseStore and imports legacy JSON once.
func NewDatabaseStore(legacyPath string) (*DatabaseStore, error) {
	db, err := openSQLiteStore(legacyPath)
	if err != nil {
		return nil, err
	}

	store := &DatabaseStore{
		legacyPath: legacyPath,
		db:         db,
	}

	err = importJSONOnce[models.Database](db, "databases", legacyPath, `SELECT COUNT(*) FROM databases`, insertDatabaseTx)
	if err != nil {
		return nil, err
	}

	return store, nil
}

func insertDatabaseTx(tx *sql.Tx, db models.Database) error {
	_, err := tx.Exec(
		`INSERT OR IGNORE INTO databases (
			id, server_id, name, type, database_name, username, password, host_port,
			container_id, status, created_at, auto_backup, retention_days
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		db.ID,
		db.ServerID,
		db.Name,
		db.Type,
		db.DatabaseName,
		db.Username,
		db.Password,
		db.HostPort,
		db.ContainerID,
		string(db.Status),
		timeToText(db.CreatedAt),
		boolToInt(db.AutoBackup),
		db.RetentionDays,
	)
	return err
}

func scanDatabase(scanner interface {
	Scan(dest ...interface{}) error
}) (models.Database, error) {
	var db models.Database
	var status string
	var createdAtRaw string
	var autoBackup int

	err := scanner.Scan(
		&db.ID,
		&db.ServerID,
		&db.Name,
		&db.Type,
		&db.DatabaseName,
		&db.Username,
		&db.Password,
		&db.HostPort,
		&db.ContainerID,
		&status,
		&createdAtRaw,
		&autoBackup,
		&db.RetentionDays,
	)
	if err != nil {
		return models.Database{}, err
	}

	db.Status = models.DatabaseStatus(status)
	db.CreatedAt = timeFromText(createdAtRaw)
	db.AutoBackup = intToBool(autoBackup)
	return db, nil
}

const databaseSelectColumns = `id, server_id, name, type, database_name, username, password, host_port,
	container_id, status, created_at, auto_backup, retention_days`

// AddDatabase adds a new database record.
func (s *DatabaseStore) AddDatabase(db models.Database) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM databases WHERE id = ?`, db.ID).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return fmt.Errorf("database with ID %s already exists", db.ID)
	}

	db.CreatedAt = time.Now()
	_, err := s.db.Exec(
		`INSERT INTO databases (
			id, server_id, name, type, database_name, username, password, host_port,
			container_id, status, created_at, auto_backup, retention_days
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		db.ID,
		db.ServerID,
		db.Name,
		db.Type,
		db.DatabaseName,
		db.Username,
		db.Password,
		db.HostPort,
		db.ContainerID,
		string(db.Status),
		timeToText(db.CreatedAt),
		boolToInt(db.AutoBackup),
		db.RetentionDays,
	)
	return err
}

// RemoveDatabase removes a database record by ID.
func (s *DatabaseStore) RemoveDatabase(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM databases WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("database not found")
	}
	return nil
}

// GetDatabase returns a database record by ID.
func (s *DatabaseStore) GetDatabase(id string) (models.Database, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`SELECT `+databaseSelectColumns+` FROM databases WHERE id = ?`, id)
	db, err := scanDatabase(row)
	if err == sql.ErrNoRows {
		return models.Database{}, fmt.Errorf("database not found")
	}
	return db, err
}

// ListDatabases returns all databases.
func (s *DatabaseStore) ListDatabases() []models.Database {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT ` + databaseSelectColumns + ` FROM databases ORDER BY rowid`)
	if err != nil {
		return []models.Database{}
	}
	defer rows.Close()

	var databases []models.Database
	for rows.Next() {
		db, err := scanDatabase(rows)
		if err == nil {
			databases = append(databases, db)
		}
	}
	return databases
}

// UpdateDatabase updates an existing database record.
func (s *DatabaseStore) UpdateDatabase(db models.Database) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if db.CreatedAt.IsZero() {
		var existingCreatedAt string
		err := s.db.QueryRow(`SELECT created_at FROM databases WHERE id = ?`, db.ID).Scan(&existingCreatedAt)
		if err == sql.ErrNoRows {
			return fmt.Errorf("database not found")
		}
		if err != nil {
			return err
		}
		db.CreatedAt = timeFromText(existingCreatedAt)
	}

	result, err := s.db.Exec(
		`UPDATE databases SET
			server_id = ?, name = ?, type = ?, database_name = ?, username = ?,
			password = ?, host_port = ?, container_id = ?, status = ?, created_at = ?,
			auto_backup = ?, retention_days = ?
		WHERE id = ?`,
		db.ServerID,
		db.Name,
		db.Type,
		db.DatabaseName,
		db.Username,
		db.Password,
		db.HostPort,
		db.ContainerID,
		string(db.Status),
		timeToText(db.CreatedAt),
		boolToInt(db.AutoBackup),
		db.RetentionDays,
		db.ID,
	)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("database not found")
	}
	return nil
}
