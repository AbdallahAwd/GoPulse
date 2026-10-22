package services

import (
	"context"
	"fmt"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fixtureExecutor struct {
	commands []string
	run      func(string, string) (string, error)
}

func (f *fixtureExecutor) Run(_ context.Context, server, command string) (string, error) {
	f.commands = append(f.commands, command)
	return f.run(server, command)
}
func qdrantFixture(t *testing.T) (*DatabaseService, *fixtureExecutor) {
	t.Helper()
	st, err := store.NewDatabaseStore(filepath.Join(t.TempDir(), "databases.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixtureExecutor{run: func(_, cmd string) (string, error) {
		if strings.Contains(cmd, "urllib.request") {
			return `{"status":"ok","result":{"collections":[]}}`, nil
		}
		return "container", nil
	}}
	return NewDatabaseService(st, &ServiceManager{executor: f}), f
}
func TestQdrantInstallationExternalAndVerified(t *testing.T) {
	s, f := qdrantFixture(t)
	db, err := s.InstallQdrant(context.Background(), "srv", "vectors", "", models.QdrantConfig{}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if db.Status != models.DatabaseStatusRunning || db.Password != "" {
		t.Fatalf("unexpected state: %+v", db)
	}
	commands := strings.Join(f.commands, "\n")
	for _, part := range []string{"0.0.0.0:6333:6333", "0.0.0.0:6334:6334", "http://127.0.0.1:6333/collections", "/qdrant/storage", "/qdrant/snapshots", "production.yaml:ro", "0o600", "urllib.request"} {
		if !strings.Contains(commands, part) {
			t.Errorf("missing %s", part)
		}
	}
}
func TestQdrantValidation(t *testing.T) {
	for _, cfg := range []models.QdrantConfig{{BindAddress: "host;command"}, {BindAddress: "224.0.0.1"}, {BindAddress: "::"}, {Version: "latest"}, {StoragePath: "/var/lib/gopulse/databases/../evil"}, {HostPort: 80}, {GRPCPort: 6333}, {GRPCPort: 80}} {
		if ValidateQdrantConfig(&cfg) == nil {
			t.Fatalf("accepted %+v", cfg)
		}
	}
}
func TestQdrantCollectionValidationBeforeExecution(t *testing.T) {
	s, f := qdrantFixture(t)
	_, err := s.CreateQdrantCollection(context.Background(), "db", "x;reboot", 384, "Cosine")
	if err == nil || len(f.commands) != 0 {
		t.Fatal("invalid input executed")
	}
}
func TestQdrantFailedDeletePreservesMetadata(t *testing.T) {
	s, f := qdrantFixture(t)
	db, err := s.InstallQdrant(context.Background(), "srv", "vectors", "", models.QdrantConfig{}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	f.run = func(_, cmd string) (string, error) { return "", fmt.Errorf("offline") }
	if s.DeleteQdrant(context.Background(), db.ID, true) == nil {
		t.Fatal("expected failure")
	}
	if _, err := s.GetDatabase(db.ID); err != nil {
		t.Fatal("metadata lost")
	}
}

func TestQdrantCollectionCRUDAndSnapshots(t *testing.T) {
	s, f := qdrantFixture(t)
	db, err := s.InstallQdrant(context.Background(), "srv", "vectors", "", models.QdrantConfig{}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	f.run = func(_, command string) (string, error) {
		calls++
		if calls == 1 {
			return "", fmt.Errorf("404")
		}
		if calls == 2 {
			return `{"status":"ok","result":true}`, nil
		}
		return `{"status":"ok","result":{"config":{"params":{"vectors":{"size":384,"distance":"Cosine"}}}}}`, nil
	}
	if _, err := s.CreateQdrantCollection(context.Background(), db.ID, "documents", 384, "Cosine"); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatal("creation was not verified")
	}
	if _, err := s.CreateQdrantCollection(context.Background(), db.ID, "documents", 384, "Cosine"); err != nil {
		t.Fatal("idempotent existing collection failed")
	}
	if _, err := s.DeleteQdrantCollection(context.Background(), db.ID, "documents"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.QdrantSnapshots(context.Background(), db.ID, "documents", true); err != nil {
		t.Fatal(err)
	}
	before := len(f.commands)
	if _, err := s.RestoreQdrantSnapshot(context.Background(), db.ID, "documents", "../../secret.snapshot"); err == nil || len(f.commands) != before {
		t.Fatal("invalid snapshot executed")
	}
	if _, err := s.RestoreQdrantSnapshot(context.Background(), db.ID, "documents", "documents-2026.snapshot"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.commands[len(f.commands)-1], "file:///qdrant/snapshots/documents/documents-2026.snapshot") {
		t.Fatal("unexpected restore location")
	}
}
func TestQdrantPartialInstallRemainsVisibleAndExistingDirectoryIsNotAdopted(t *testing.T) {
	s, f := qdrantFixture(t)
	f.run = func(_, cmd string) (string, error) {
		if strings.Contains(cmd, "docker run") {
			return "", fmt.Errorf("daemon failure")
		}
		return "ok", nil
	}
	db, err := s.InstallQdrant(context.Background(), "srv", "vectors", "", models.QdrantConfig{}, func(string) {})
	if err == nil || db == nil || db.Status != models.DatabaseStatusError {
		t.Fatal("partial installation was lost")
	}
	saved, err := s.GetDatabase(db.ID)
	if err != nil || saved.Status != models.DatabaseStatusError {
		t.Fatal("partial state not persisted")
	}
	before := len(s.store.ListDatabases())
	f.run = func(_, cmd string) (string, error) {
		if strings.Contains(cmd, "test ! -e") {
			return "", fmt.Errorf("existing directory")
		}
		return "ok", nil
	}
	if _, err := s.InstallQdrant(context.Background(), "srv", "other", "", models.QdrantConfig{StoragePath: "/var/lib/gopulse/databases/existing"}, func(string) {}); err == nil {
		t.Fatal("existing directory adopted")
	}
	if len(s.store.ListDatabases()) != before {
		t.Fatal("existing directory became removable managed state")
	}
}
func TestQdrantRemovalShellHandlesMissingOwnedAndReplacedContainers(t *testing.T) {
	for _, scenario := range []string{"missing", "owned", "replaced", "offline"} {
		t.Run(scenario, func(t *testing.T) {
			s, f := qdrantFixture(t)
			db, err := s.InstallQdrant(context.Background(), "srv", "vectors", "", models.QdrantConfig{}, func(string) {})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			logPath := filepath.Join(dir, "removed")
			shim := "#!/bin/sh\ncase \"$1 $2\" in\n'ps -a') [ \"$GOPULSE_FIXTURE_OFFLINE\" = yes ] && exit 1; printf '%s' \"$GOPULSE_FIXTURE_CONTAINER\";;\n'rm -f') printf '%s' \"$3\" > \"$GOPULSE_FIXTURE_LOG\";;\n*) exit 1;;\nesac\n"
			if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(shim), 0700); err != nil {
				t.Fatal(err)
			}
			container, offline := "", "no"
			if scenario == "owned" {
				container = "gopulse-" + db.ID + "|" + db.ID
			}
			if scenario == "replaced" {
				container = "gopulse-" + db.ID + "|another-owner"
			}
			if scenario == "offline" {
				offline = "yes"
			}
			f.run = func(_, command string) (string, error) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "sh", "-c", command)
				cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "GOPULSE_FIXTURE_CONTAINER="+container, "GOPULSE_FIXTURE_OFFLINE="+offline, "GOPULSE_FIXTURE_LOG="+logPath)
				output, err := cmd.CombinedOutput()
				return string(output), err
			}
			err = s.DeleteQdrant(context.Background(), db.ID, false)
			mustFail := scenario == "replaced" || scenario == "offline"
			if (err != nil) != mustFail {
				t.Fatalf("unexpected removal result: %v", err)
			}
			_, savedErr := s.GetDatabase(db.ID)
			if (savedErr == nil) != mustFail {
				t.Fatal("metadata did not match verified removal")
			}
			_, logErr := os.Stat(logPath)
			if (logErr == nil) != (scenario == "owned") {
				t.Fatal("unowned container was removed, or owned container was retained")
			}
		})
	}
}
