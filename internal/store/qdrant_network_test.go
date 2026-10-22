package store

import (
	"path/filepath"
	"testing"

	"github.com/AbdallahAwd/GoPulse/internal/models"
)

func TestQdrantNetworkMetadataCommitsAtomically(t *testing.T) {
	s, err := NewDatabaseStore(filepath.Join(t.TempDir(), "databases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddDatabase(models.Database{ID: "vectors", Type: "qdrant", HostPort: 6333, AutoBackup: true, RetentionDays: 7}); err != nil {
		t.Fatal(err)
	}
	old := models.QdrantConfig{BindAddress: "127.0.0.1", HostPort: 6333}
	if err := s.SetQdrantConfig("vectors", old); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_network BEFORE INSERT ON resource_settings BEGIN SELECT RAISE(ABORT,'fixture'); END`); err != nil {
		t.Fatal(err)
	}
	next := models.QdrantConfig{BindAddress: "0.0.0.0", HostPort: 16333, GRPCPort: 16334}
	if err := s.CommitQdrantNetwork("vectors", next); err == nil {
		t.Fatal("expected fixture write failure")
	}
	db, _ := s.GetDatabase("vectors")
	cfg, _ := s.GetQdrantConfig("vectors")
	if db.HostPort != 6333 || cfg != old {
		t.Fatal("partial network metadata committed")
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_network`); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitQdrantNetwork("vectors", next); err != nil {
		t.Fatal(err)
	}
	db, _ = s.GetDatabase("vectors")
	cfg, _ = s.GetQdrantConfig("vectors")
	if db.HostPort != 16333 || cfg != next || db.Status != models.DatabaseStatusRunning || !db.AutoBackup || db.RetentionDays != 7 {
		t.Fatal("network commit altered unrelated metadata")
	}
	if err := s.CommitQdrantNetwork("missing", next); err == nil {
		t.Fatal("orphan settings created")
	}
}
