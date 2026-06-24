package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var sqliteRegistry = struct {
	sync.Mutex
	dbs map[string]*sql.DB
}{
	dbs: make(map[string]*sql.DB),
}

func sqlitePathForLegacyJSON(legacyPath string) string {
	return filepath.Join(filepath.Dir(legacyPath), "gopulse.db")
}

func openSQLiteStore(legacyPath string) (*sql.DB, error) {
	dbPath := sqlitePathForLegacyJSON(legacyPath)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, err
	}

	sqliteRegistry.Lock()
	defer sqliteRegistry.Unlock()

	if db := sqliteRegistry.dbs[dbPath]; db != nil {
		return db, nil
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	pragmas := []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to apply %s: %w", pragma, err)
		}
	}

	if err := migrateSQLiteSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	sqliteRegistry.dbs[dbPath] = db
	return db, nil
}

func migrateSQLiteSchema(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS json_imports (
			name TEXT PRIMARY KEY,
			source_path TEXT NOT NULL,
			imported_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS servers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			host TEXT NOT NULL,
			port INTEGER NOT NULL,
			user TEXT NOT NULL,
			auth_type TEXT NOT NULL,
			key_path TEXT NOT NULL,
			password TEXT NOT NULL,
			tags TEXT NOT NULL,
			is_db_only INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password TEXT NOT NULL,
			role TEXT NOT NULL,
			permissions TEXT NOT NULL,
			allowed_servers TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			source_type TEXT NOT NULL,
			source_url TEXT NOT NULL,
			build_type TEXT NOT NULL,
			port INTEGER NOT NULL,
			host_port INTEGER NOT NULL,
			status TEXT NOT NULL,
			path TEXT NOT NULL,
			container_id TEXT NOT NULL,
			process_id INTEGER NOT NULL,
			domain TEXT NOT NULL,
			created_at TEXT NOT NULL,
			start_command TEXT NOT NULL,
			build_command TEXT NOT NULL,
			last_build TEXT NOT NULL,
			webhook_secret TEXT NOT NULL,
			webhook_enabled INTEGER NOT NULL,
			last_deploy TEXT NOT NULL,
			env_vars TEXT NOT NULL,
			restart_on_boot INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_projects_server_id ON projects(server_id)`,
		`CREATE TABLE IF NOT EXISTS domains (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			domain TEXT NOT NULL,
			port INTEGER NOT NULL,
			ssl INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(server_id, domain)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_domains_server_id ON domains(server_id)`,
		`CREATE TABLE IF NOT EXISTS databases (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			database_name TEXT NOT NULL,
			username TEXT NOT NULL,
			password TEXT NOT NULL,
			host_port INTEGER NOT NULL,
			container_id TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at TEXT NOT NULL,
			auto_backup INTEGER NOT NULL,
			retention_days INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_databases_server_id ON databases(server_id)`,
	}

	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to migrate SQLite schema: %w", err)
		}
	}
	return nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func intToBool(value int) bool {
	return value != 0
}

func encodeJSON(value interface{}, fallback string) string {
	if value == nil {
		return fallback
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fallback
	}
	return string(data)
}

func decodeJSON(raw string, target interface{}) error {
	if strings.TrimSpace(raw) == "" {
		raw = "null"
	}
	return json.Unmarshal([]byte(raw), target)
}

func timeToText(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func timeFromText(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return t
	}
	t, err = time.Parse(time.RFC3339, value)
	if err == nil {
		return t
	}
	return time.Time{}
}

func importJSONOnce[T any](db *sql.DB, name string, sourcePath string, countSQL string, insert func(*sql.Tx, T) error) error {
	data, err := os.ReadFile(sourcePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read legacy %s JSON: %w", name, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil
	}

	var records []T
	if err := json.Unmarshal(data, &records); err != nil {
		return fmt.Errorf("failed to parse legacy %s JSON: %w", name, err)
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var alreadyImported int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM json_imports WHERE name = ?`, name).Scan(&alreadyImported); err != nil {
		return err
	}
	if alreadyImported > 0 {
		return tx.Commit()
	}

	var existingRows int
	if err := tx.QueryRow(countSQL).Scan(&existingRows); err != nil {
		return err
	}
	if existingRows == 0 {
		for _, record := range records {
			if err := insert(tx, record); err != nil {
				return fmt.Errorf("failed to import legacy %s JSON: %w", name, err)
			}
		}
	}

	_, err = tx.Exec(
		`INSERT OR REPLACE INTO json_imports (name, source_path, imported_at) VALUES (?, ?, ?)`,
		name,
		sourcePath,
		timeToText(time.Now()),
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}
