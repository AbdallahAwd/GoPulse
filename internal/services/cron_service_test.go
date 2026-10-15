package services

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/store"
)

type cronTestExecutor func(context.Context, models.CronJob) CronResult

func (f cronTestExecutor) Execute(ctx context.Context, job models.CronJob) CronResult {
	return f(ctx, job)
}
func cronFixture(t *testing.T, executor CronExecutor) (*CronService, *store.CronStore, *store.ServerStore) {
	t.Helper()
	dir := t.TempDir()
	servers, err := store.NewServerStore(filepath.Join(dir, "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err := servers.AddServer(models.Server{ID: id, Host: "localhost", Port: 22}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := store.NewCronStore(filepath.Join(dir, "cron.json"))
	if err != nil {
		t.Fatal(err)
	}
	if executor == nil {
		executor = cronTestExecutor(func(context.Context, models.CronJob) CronResult {
			code := 0
			return CronResult{Output: "ok\n", ExitCode: &code}
		})
	}
	s, err := NewCronService(st, executor)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, st, servers
}
func cronJob() models.CronJob {
	return models.CronJob{Name: "Daily job", Command: "printf 'ok\\n'", Schedule: "* * * * *", Timezone: "UTC", TimeoutSeconds: 10}
}
func saveCron(t *testing.T, s *CronService, input models.CronJob) models.CronJob {
	t.Helper()
	job, err := s.Save("a", "", "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	return job
}
func completedCron(t *testing.T, s *CronService, jobID, runID string) models.CronRun {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := s.Run("a", jobID, runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status != "running" {
			return run
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("run did not complete")
	return models.CronRun{}
}

func TestCronSchedulePreviewAndValidation(t *testing.T) {
	for _, test := range []struct{ expression, zone string }{{"*/5 * * * *", "UTC"}, {"0 9 * * MON-FRI", "Africa/Cairo"}, {"0 0 1 * *", "America/New_York"}} {
		next, err := CronPreview(test.expression, test.zone, time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC))
		if err != nil || len(next) != 5 {
			t.Fatalf("%+v: %v", test, err)
		}
		for i := 1; i < len(next); i++ {
			if !next[i].After(next[i-1]) {
				t.Fatal("dates not increasing")
			}
		}
	}
	for _, test := range []struct{ expression, zone string }{{"* * * * * *", "UTC"}, {"@every 1s", "UTC"}, {"61 * * * *", "UTC"}, {"*/0 * * * *", "UTC"}, {"0 0 31 2 *", "UTC"}, {"* * * * *", "Mars/Now"}, {"* * * * *", "Local"}, {"TZ=UTC * * * * *", "UTC"}} {
		if _, err := ParseCronSchedule(test.expression, test.zone); err == nil {
			t.Fatalf("accepted invalid: %+v", test)
		}
	}
	utc, _ := CronPreview("0 9 * * *", "UTC", time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC))
	cairo, _ := CronPreview("0 9 * * *", "Africa/Cairo", time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC))
	if utc[0].Equal(cairo[0]) {
		t.Fatal("timezone ignored")
	}
}
func TestCronInvalidJobsCannotBeSaved(t *testing.T) {
	s, _, _ := cronFixture(t, nil)
	for _, mutate := range []func(*models.CronJob){func(j *models.CronJob) { j.Name = "" }, func(j *models.CronJob) { j.Command = "" }, func(j *models.CronJob) { j.Command = "echo\x00hi" }, func(j *models.CronJob) { j.TimeoutSeconds = 0 }, func(j *models.CronJob) { j.TimeoutSeconds = 3601 }, func(j *models.CronJob) { j.WorkingDir = "relative" }, func(j *models.CronJob) { j.WorkingDir = "/bad\npath" }} {
		job := cronJob()
		mutate(&job)
		if _, err := s.Save("a", "", "alice", job); err == nil {
			t.Fatal("accepted invalid job")
		}
	}
	jobs, _ := s.List("a")
	if len(jobs) != 0 {
		t.Fatal("invalid jobs persisted")
	}
}
func TestCronCRUDPauseAndServerIsolation(t *testing.T) {
	s, _, _ := cronFixture(t, nil)
	job := saveCron(t, s, cronJob())
	if job.CreatedBy != "alice" || !job.NextRunAt.IsZero() {
		t.Fatal("paused creation metadata incorrect")
	}
	if _, err := s.Get("b", job.ID); !errors.Is(err, ErrCronNotFound) {
		t.Fatal("cross-server read allowed")
	}
	if _, err := s.RunNow("b", job.ID, "alice"); !errors.Is(err, ErrCronNotFound) {
		t.Fatal("cross-server execution allowed")
	}
	if err := s.Delete("b", job.ID); !errors.Is(err, ErrCronNotFound) {
		t.Fatal("cross-server delete allowed")
	}
	job, err := s.SetEnabled("a", job.ID, true)
	if err != nil || !job.NextRunAt.After(time.Now()) {
		t.Fatal("resume did not schedule", err)
	}
	job.Name = "Changed"
	job.CreatedBy = "spoofed"
	updated, err := s.Save("a", job.ID, "other", job)
	if err != nil || updated.CreatedBy != "alice" || !updated.CreatedAt.Equal(job.CreatedAt) {
		t.Fatal("update corrupted origin", err)
	}
	paused, err := s.SetEnabled("a", job.ID, false)
	if err != nil || !paused.NextRunAt.IsZero() {
		t.Fatal("pause not applied", err)
	}
	if err := s.Delete("a", job.ID); err != nil {
		t.Fatal(err)
	}
	jobs, _ := s.List("a")
	if len(jobs) != 0 {
		t.Fatal("job not deleted")
	}
}
func TestCronManualRunOnPausedJobStoresOutputAndActor(t *testing.T) {
	s, _, _ := cronFixture(t, nil)
	job := saveCron(t, s, cronJob())
	run, err := s.RunNow("a", job.ID, "bob")
	if err != nil {
		t.Fatal(err)
	}
	done := completedCron(t, s, job.ID, run.ID)
	if done.Status != "succeeded" || done.Output != "ok\n" || done.Actor != "bob" || done.Trigger != "manual" || done.ExitCode == nil || *done.ExitCode != 0 {
		t.Fatalf("bad result: %+v", done)
	}
	jobs, _ := s.List("a")
	if jobs[0].LastRun == nil || jobs[0].LastRun.Output != "" {
		t.Fatal("inventory should not load output")
	}
	runs, _ := s.Runs("a", job.ID, 25, 0)
	if len(runs) != 1 || runs[0].Output != "" {
		t.Fatal("history should not load output")
	}
	if _, err := s.Run("b", job.ID, run.ID); !errors.Is(err, ErrCronNotFound) {
		t.Fatal("run leaked across server")
	}
}
func TestCronConcurrentRunClaimsAndCancellation(t *testing.T) {
	started := make(chan models.CronJob, 1)
	s, _, _ := cronFixture(t, cronTestExecutor(func(ctx context.Context, j models.CronJob) CronResult {
		started <- j
		<-ctx.Done()
		return CronResult{Err: ctx.Err()}
	}))
	job := saveCron(t, s, cronJob())
	var wg sync.WaitGroup
	var mu sync.Mutex
	var runs []models.CronRun
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run, err := s.RunNow("a", job.ID, "actor")
			if err == nil {
				mu.Lock()
				runs = append(runs, run)
				mu.Unlock()
			} else if !errors.Is(err, store.ErrCronBusy) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(runs) != 1 {
		t.Fatalf("%d commands claimed", len(runs))
	}
	<-started
	if err := s.Delete("a", job.ID); !errors.Is(err, store.ErrCronBusy) {
		t.Fatal("deleted active job")
	}
	if err := s.Cancel("a", job.ID); err != nil {
		t.Fatal(err)
	}
	if done := completedCron(t, s, job.ID, runs[0].ID); done.Status != "cancelled" {
		t.Fatal(done.Status)
	}
}
func TestCronScheduledOverlapSkippedAndPauseDoesNotCancel(t *testing.T) {
	started := make(chan struct{}, 1)
	s, st, _ := cronFixture(t, cronTestExecutor(func(ctx context.Context, j models.CronJob) CronResult {
		started <- struct{}{}
		<-ctx.Done()
		return CronResult{Err: ctx.Err()}
	}))
	input := cronJob()
	input.Enabled = true
	job := saveCron(t, s, input)
	if err := s.Tick(job.NextRunAt); err != nil {
		t.Fatal(err)
	}
	<-started
	job, _ = s.Get("a", job.ID)
	if err := s.Tick(job.NextRunAt); err != nil {
		t.Fatal(err)
	}
	runs, _ := st.Runs("a", job.ID, 25, 0)
	if len(runs) != 2 || runs[0].Status != "skipped" || runs[1].Status != "running" {
		t.Fatalf("overlap: %+v", runs)
	}
	jobs, _ := s.List("a")
	if jobs[0].LastRun.Status != "running" {
		t.Fatal("running state hidden by skipped run")
	}
	if _, err := s.SetEnabled("a", job.ID, false); err != nil {
		t.Fatal(err)
	}
	run, _ := s.Run("a", job.ID, runs[1].ID)
	if run.Status != "running" {
		t.Fatal("pause cancelled active command")
	}
	if err := s.Tick(time.Now().Add(24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	after, _ := st.Runs("a", job.ID, 25, 0)
	if len(after) != 2 {
		t.Fatal("paused schedule fired")
	}
}
func TestCronWorkerCapacityAndSnapshotEdits(t *testing.T) {
	commands := make(chan string, 8)
	s, _, _ := cronFixture(t, cronTestExecutor(func(ctx context.Context, j models.CronJob) CronResult {
		commands <- j.Command
		<-ctx.Done()
		return CronResult{Err: ctx.Err()}
	}))
	var jobs []models.CronJob
	for i := 0; i < 9; i++ {
		input := cronJob()
		input.Name = fmt.Sprint(i)
		jobs = append(jobs, saveCron(t, s, input))
	}
	for _, job := range jobs[:8] {
		if _, err := s.RunNow("a", job.ID, "actor"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.RunNow("a", jobs[8].ID, "actor"); !errors.Is(err, ErrCronCapacity) {
		t.Fatal("worker limit ignored")
	}
	jobs[0].Command = "echo changed"
	if _, err := s.Save("a", jobs[0].ID, "actor", jobs[0]); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if command := <-commands; command != cronJob().Command {
			t.Fatal("active snapshot mutated")
		}
	}
}
func TestCronExecutorFailureAndPanicAreRecorded(t *testing.T) {
	for _, panicRun := range []bool{false, true} {
		t.Run(fmt.Sprint(panicRun), func(t *testing.T) {
			s, _, _ := cronFixture(t, cronTestExecutor(func(context.Context, models.CronJob) CronResult {
				if panicRun {
					panic("test")
				}
				code := 7
				return CronResult{Err: errors.New("failed command"), Output: "failure\n", ExitCode: &code}
			}))
			job := saveCron(t, s, cronJob())
			run, err := s.RunNow("a", job.ID, "actor")
			if err != nil {
				t.Fatal(err)
			}
			if done := completedCron(t, s, job.ID, run.ID); done.Status != "failed" || done.Error == "" {
				t.Fatalf("failure not recorded: %+v", done)
			}
		})
	}
}
func TestCronRecoveryDoesNotReplayMissedOrInterruptedJobs(t *testing.T) {
	s, st, _ := cronFixture(t, nil)
	input := cronJob()
	input.Enabled = true
	job := saveCron(t, s, input)
	job.NextRunAt = time.Now().Add(-24 * time.Hour)
	if err := st.Update(job); err != nil {
		t.Fatal(err)
	}
	run := newCronRun(job, "manual", "actor", time.Now().Add(-time.Minute))
	if err := st.Claim(job, run, false, time.Time{}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	recovered, err := NewCronService(st, cronTestExecutor(func(context.Context, models.CronJob) CronResult { t.Error("replayed old command"); return CronResult{} }))
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	job, _ = recovered.Get("a", job.ID)
	if !job.NextRunAt.After(time.Now()) {
		t.Fatal("missed schedule not advanced")
	}
	done, _ := recovered.Run("a", job.ID, run.ID)
	if done.Status != "interrupted" {
		t.Fatal("run not recovered")
	}
	if err := recovered.Tick(time.Now()); err != nil {
		t.Fatal(err)
	}
}
func TestCronLocalExecutionOutputExitDirectoryAndLimits(t *testing.T) {
	_, _, servers := cronFixture(t, nil)
	executor := &ServerCronExecutor{Servers: servers}
	job := cronJob()
	job.ServerID = "a"
	job.WorkingDir = t.TempDir()
	job.Command = "printf '%s\\n' \"$PWD\"; printf 'stderr\\n' >&2; exit 7"
	result := executor.Execute(context.Background(), job)
	if result.Err == nil || result.ExitCode == nil || *result.ExitCode != 7 || !strings.Contains(result.Output, job.WorkingDir) || !strings.Contains(result.Output, "stderr") {
		t.Fatalf("local result: %+v", result)
	}
	job.Command = "yes output | /usr/bin/head -c 100000"
	result = executor.Execute(context.Background(), job)
	if !result.Truncated || len(result.Output) > cronOutputLimit {
		t.Fatal("output not bounded")
	}
	job.Command = "printf started; sleep 5"
	job.TimeoutSeconds = 1
	start := time.Now()
	result = executor.Execute(context.Background(), job)
	if !result.TimedOut || time.Since(start) > 3*time.Second || !strings.Contains(result.Output, "started") {
		t.Fatal("timeout/output handling failed", result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result = executor.Execute(ctx, job)
	if !errors.Is(result.Err, context.Canceled) {
		t.Fatal("cancelled context executed command")
	}
}
func TestCronShellQuoteAndConcurrentOutput(t *testing.T) {
	value := "path 'quotes'; $HOME `command`"
	if !strings.HasPrefix(cronShellQuote(value), "'") || !strings.Contains(cronShellQuote(value), "'\"'\"'") {
		t.Fatal("shell quoting failed")
	}
	buffer := &cronOutput{}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				buffer.Write([]byte(strings.Repeat("x", 1000)))
			}
		}()
	}
	wg.Wait()
	text, truncated := buffer.result()
	if len(text) != cronOutputLimit || !truncated {
		t.Fatal("concurrent output was not bounded")
	}
}

func TestCronBackupImportIsValidatedPausedAndConflictAware(t *testing.T) {
	s, st, _ := cronFixture(t, nil)
	job := cronJob()
	job.ID = "restored"
	job.ServerID = "a"
	job.Enabled = true
	exports := &store.ExportStore{CronJobs: st, ImportCronJob: s.ImportJob}
	data := &models.GoPulseExport{CronJobs: []models.CronJob{job}}
	report, err := exports.ImportAll(data, "skip")
	if err != nil || report.CronJobsCreated != 1 || len(report.Errors) != 0 {
		t.Fatal(report, err)
	}
	restored, err := s.Get("a", job.ID)
	if err != nil || restored.Enabled || !restored.NextRunAt.IsZero() {
		t.Fatal("import automatically enabled execution", restored, err)
	}
	data.CronJobs[0].Name = "Updated"
	report, err = exports.ImportAll(data, "skip")
	if err != nil || report.CronJobsSkipped != 1 {
		t.Fatal(report, err)
	}
	report, err = exports.ImportAll(data, "overwrite")
	if err != nil || report.CronJobsUpdated != 1 {
		t.Fatal(report, err)
	}
	restored, _ = s.Get("a", job.ID)
	if restored.Name != "Updated" || restored.Enabled {
		t.Fatal("overwrite incorrect")
	}
	data.CronJobs[0].Schedule = "invalid"
	report, err = exports.ImportAll(data, "overwrite")
	if err != nil || len(report.Errors) != 1 || report.CronJobsUpdated != 0 {
		t.Fatal("invalid job restored", report, err)
	}
}
