package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/amaa/gopulse/internal/models"
	"github.com/google/uuid"
)

func TestCronPersistenceRetentionClaimsAndServerCascade(t *testing.T) {
	dir := t.TempDir()
	servers, err := NewServerStore(filepath.Join(dir, "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := servers.AddServer(models.Server{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	s, err := NewCronStore(filepath.Join(dir, "cron.json"))
	if err != nil {
		t.Fatal(err)
	}
	job := models.CronJob{ID: "job", ServerID: "a", Enabled: true, Schedule: "* * * * *", Timezone: "UTC", Command: "echo ok", NextRunAt: time.Now().UTC().Truncate(time.Second)}
	if err := s.Create(job); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewCronStore(filepath.Join(dir, "cron.json"))
	if err != nil {
		t.Fatal(err)
	}
	if persisted, err := reopened.Get("a", "job"); err != nil || persisted.Command != job.Command {
		t.Fatal("job not persisted", err)
	}
	for i := 0; i < 105; i++ {
		run := models.CronRun{ID: uuid.NewString(), JobID: "job", ServerID: "a", Status: "running", StartedAt: time.Now(), Output: "large output"}
		if err := s.Claim(job, run, false, time.Time{}); err != nil {
			t.Fatal(err)
		}
		double := run
		double.ID = uuid.NewString()
		if err := s.Claim(job, double, false, time.Time{}); !errors.Is(err, ErrCronBusy) {
			t.Fatal("active claim duplicated", err)
		}
		run.Status = "succeeded"
		if err := s.Finish(run); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := s.Runs("a", "job", 100, 0)
	if err != nil || len(runs) != 100 {
		t.Fatal("retention failed", len(runs), err)
	}
	page, err := s.Runs("a", "job", 25, 25)
	if err != nil || len(page) != 25 || page[0].ID == runs[0].ID {
		t.Fatal("pagination failed")
	}
	run := models.CronRun{ID: uuid.NewString(), JobID: "job", ServerID: "a", Status: "running"}
	expected := job.NextRunAt
	job.NextRunAt = expected.Add(time.Minute)
	if err := s.Claim(job, run, true, expected); err != nil {
		t.Fatal(err)
	}
	run.ID = uuid.NewString()
	if err := s.Claim(job, run, true, expected); err == nil {
		t.Fatal("same schedule slot claimed twice")
	}
	if err := servers.RemoveServer("a"); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.List("")
	if err != nil || len(jobs) != 0 {
		t.Fatal("orphaned jobs after server delete")
	}
	remaining, err := s.Runs("a", "job", 100, 0)
	if err != nil || len(remaining) != 0 {
		t.Fatal("orphaned run history")
	}
}
