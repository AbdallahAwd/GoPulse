package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/AbdallahAwd/GoPulse/internal/models"
)

func TestLegacyServerSchemaMigratesWithoutInventingTrust(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "gopulse.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE servers (id TEXT PRIMARY KEY,name TEXT NOT NULL,host TEXT NOT NULL,port INTEGER NOT NULL,user TEXT NOT NULL,auth_type TEXT NOT NULL,key_path TEXT NOT NULL,password TEXT NOT NULL,tags TEXT NOT NULL,is_db_only INTEGER NOT NULL); INSERT INTO servers VALUES ('legacy','Legacy','host',22,'root','password','','keep-password','[]',0)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	servers, err := NewServerStore(filepath.Join(dir, "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := servers.GetServer("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if saved.TrustedHostKey != "" || saved.Password != "keep-password" {
		t.Fatal("migration changed trust/credentials")
	}
	if err := servers.TrustHostKey("legacy", "host", 22, "approved-public-key"); err != nil {
		t.Fatal(err)
	}
	if err := migrateSQLiteSchema(servers.db); err != nil {
		t.Fatal(err)
	}
	saved, err = servers.GetServer("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if saved.TrustedHostKey != "approved-public-key" || servers.ListServers()[0].TrustedHostKey != saved.TrustedHostKey {
		t.Fatal("migration or read lost pin")
	}
	if err := servers.TrustHostKey("legacy", "other", 22, "wrong-key"); err == nil {
		t.Fatal("stale address approved")
	}
	saved, err = servers.GetServer("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if saved.TrustedHostKey != "approved-public-key" {
		t.Fatal("failed trust changed pin")
	}
}

func TestLegacyJSONImportKeepsExplicitHostKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	writeLegacyJSON(t, path, []models.Server{{ID: "one", TrustedHostKey: "approved-public-key"}})
	servers, err := NewServerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := servers.GetServer("one")
	if err != nil {
		t.Fatal(err)
	}
	if saved.TrustedHostKey != "approved-public-key" {
		t.Fatal("legacy JSON lost explicit pin")
	}
}
