package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/amaa/gopulse/internal/models"
	"github.com/google/uuid"
)

// Dashboard settings have their own namespace in the existing settings table.
type DashboardSettingsStore struct{ db *sql.DB }

func NewDashboardSettingsStore(path string) (*DashboardSettingsStore, error) {
	db, err := openSQLiteStore(path)
	if err != nil {
		return nil, err
	}
	return &DashboardSettingsStore{db}, nil
}

func (s *DashboardSettingsStore) Domain() (models.DashboardDomainSettings, error) {
	var settings models.DashboardDomainSettings
	var raw string
	err := s.db.QueryRow(`SELECT data FROM resource_settings WHERE id='dashboard:domain'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		settings.HTTPS = true
		return settings, nil
	}
	if err == nil {
		err = json.Unmarshal([]byte(raw), &settings)
	}
	return settings, err
}

func (s *DashboardSettingsStore) SaveDomain(settings models.DashboardDomainSettings) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO resource_settings(id,data) VALUES('dashboard:domain',?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, string(raw))
	return err
}

// This public, random instance identifier proves that a domain reaches this
// control plane. It is neither an authentication credential nor a machine ID.
func (s *DashboardSettingsStore) PublicIdentity() (string, error) {
	_, err := s.db.Exec(`INSERT INTO resource_settings(id,data) VALUES('dashboard:identity',?) ON CONFLICT(id) DO NOTHING`, uuid.NewString())
	if err != nil {
		return "", err
	}
	var id string
	err = s.db.QueryRow(`SELECT data FROM resource_settings WHERE id='dashboard:identity'`).Scan(&id)
	return id, err
}
