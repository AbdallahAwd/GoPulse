package services

import (
	"context"
	"errors"
	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/store"
	"path/filepath"
	"strings"
	"testing"
)

type healthExecutor struct {
	run func(context.Context, string, string) (string, error)
}

func (e healthExecutor) Run(ctx context.Context, server, command string) (string, error) {
	return e.run(ctx, server, command)
}
func healthFixture(t *testing.T, engine string, run func(context.Context, string, string) (string, error)) (*DatabaseService, *store.DatabaseStore) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.NewDatabaseStore(filepath.Join(dir, "databases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddDatabase(models.Database{ID: "db-test", ServerID: "server", Type: engine, Username: "user", Password: "SECRET", DatabaseName: "main", Status: models.DatabaseStatusRunning}); err != nil {
		t.Fatal(err)
	}
	return NewDatabaseService(st, NewServiceManagerWithExecutor(nil, healthExecutor{run})), st
}
func TestHealthFailurePreservesLastState(t *testing.T) {
	service, st := healthFixture(t, "postgres", func(context.Context, string, string) (string, error) { return "", errors.New("SSH unavailable") })
	status, err := service.UpdateStatus(context.Background(), "db-test")
	db, _ := st.GetDatabase("db-test")
	if err == nil || status != models.DatabaseStatusRunning || db.Status != models.DatabaseStatusRunning {
		t.Fatal("transport failure fabricated stopped/error state")
	}
}
func TestAuthenticatedPostgresCheckAndBoundedContext(t *testing.T) {
	calls := 0
	service, _ := healthFixture(t, "postgres", func(ctx context.Context, _ string, command string) (string, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded health request")
		}
		if calls == 1 {
			return "true", nil
		}
		if !strings.Contains(command, "PGPASSWORD='SECRET'") || !strings.Contains(command, "SELECT 1") {
			t.Fatal("database check does not authenticate")
		}
		return "", nil
	})
	status, err := service.UpdateStatus(context.Background(), "db-test")
	if err != nil || status != models.DatabaseStatusRunning || calls != 2 {
		t.Fatalf("health check failed: %v", err)
	}
}
func TestStoppedContainerIsConfirmedWithoutCredentialProbe(t *testing.T) {
	calls := 0
	service, _ := healthFixture(t, "mysql", func(context.Context, string, string) (string, error) { calls++; return "false", nil })
	status, err := service.UpdateStatus(context.Background(), "db-test")
	if err != nil || status != models.DatabaseStatusStopped || calls != 1 {
		t.Fatal("confirmed stopped container was mishandled")
	}
}
func TestHealthProbeErrorDoesNotPersistCredentialsOrFalseState(t *testing.T) {
	calls := 0
	service, st := healthFixture(t, "mysql", func(context.Context, string, string) (string, error) {
		calls++
		if calls == 1 {
			return "true", nil
		}
		return "", errors.New("failed SECRET")
	})
	_, err := service.UpdateStatus(context.Background(), "db-test")
	db, _ := st.GetDatabase("db-test")
	if err == nil || strings.Contains(err.Error(), "SECRET") || db.Status != models.DatabaseStatusRunning {
		t.Fatal("health failure leaked secret or overwrote state")
	}
}
func TestAutoStartDoesNotRestartHealthyOrUnverifiableProcesses(t *testing.T) {
	for _, result := range []struct {
		out  string
		err  error
		want bool
	}{{"1234", nil, false}, {"1234\n5678", nil, false}, {"0", nil, true}, {"", errors.New("SSH failed"), false}, {"unexpected", nil, false}} {
		manager := NewServiceManagerWithExecutor(nil, healthExecutor{func(context.Context, string, string) (string, error) { return result.out, result.err }})
		service := NewProjectService(nil, manager, "")
		start, _ := service.shouldAutoStart(context.Background(), models.Project{Name: "web", ServerID: "server", BuildType: "process"})
		if start != result.want {
			t.Fatalf("incorrect automatic restart decision for %q", result.out)
		}
	}
}
