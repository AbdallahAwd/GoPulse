package store

import (
	"path/filepath"
	"testing"

	"github.com/AbdallahAwd/GoPulse/internal/models"
)

func TestDashboardSettingsPersistAndUseTheirOwnNamespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	st, err := NewDashboardSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := st.Domain()
	if err != nil || initial.Domain != "" || !initial.HTTPS {
		t.Fatal(initial, err)
	}
	identity, err := st.PublicIdentity()
	if err != nil {
		t.Fatal(err)
	}
	saved := models.DashboardDomainSettings{DashboardDomainInput: models.DashboardDomainInput{Domain: "dashboard.example.com", HTTPS: true}, Revision: "saved", AppliedRevision: "applied", AppliedDomain: "old.example.com", AppliedHTTPS: true}
	if err := st.SaveDomain(saved); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO resource_settings(id,data) VALUES('database-fixture','{"version":"fixture"}')`); err != nil {
		t.Fatal(err)
	}
	second, err := NewDashboardSettingsStore(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := second.Domain()
	if err != nil || loaded.Revision != saved.Revision || loaded.AppliedDomain != saved.AppliedDomain {
		t.Fatal(loaded, err)
	}
	next, err := second.PublicIdentity()
	if err != nil || next != identity {
		t.Fatal("instance identity did not persist", err)
	}
	var other string
	if err := st.db.QueryRow(`SELECT data FROM resource_settings WHERE id='database-fixture'`).Scan(&other); err != nil || other != "{\"version\":\"fixture\"}" {
		t.Fatal("unrelated settings were changed", other, err)
	}
}
