package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

var (
	ErrCronNotFound = errors.New("cron job or run not found on this server")
	ErrCronCapacity = errors.New("all job workers are busy; try again shortly")
)

type CronValidationError struct{ Message string }

func (e *CronValidationError) Error() string { return e.Message }

type CronResult struct {
	Output    string
	ExitCode  *int
	Truncated bool
	TimedOut  bool
	Err       error
}

type CronExecutor interface {
	Execute(context.Context, models.CronJob) CronResult
}

// CronService is a single control-plane scheduler. Claims and run history are
// persisted, workers are bounded, and each job may have only one active run.
type CronService struct {
	store    *store.CronStore
	executor CronExecutor
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	active   map[string]context.CancelFunc
	workers  chan struct{}
	wg       sync.WaitGroup
	closed   bool
}

func NewCronService(st *store.CronStore, executor CronExecutor) (*CronService, error) {
	if err := st.Recover(); err != nil {
		return nil, err
	}
	jobs, err := st.List("")
	if err != nil {
		return nil, err
	}
	// Advance only missed slots on startup; never replay downtime side effects.
	for _, job := range jobs {
		if job.Enabled && !job.NextRunAt.After(time.Now()) {
			schedule, err := ParseCronSchedule(job.Schedule, job.Timezone)
			if err != nil {
				return nil, fmt.Errorf("saved job %s: %w", job.ID, err)
			}
			job.NextRunAt = schedule.Next(time.Now())
			if err := st.Update(job); err != nil {
				return nil, err
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &CronService{store: st, executor: executor, ctx: ctx, cancel: cancel, active: map[string]context.CancelFunc{}, workers: make(chan struct{}, 8)}, nil
}

func ParseCronSchedule(expression, timezone string) (cron.Schedule, error) {
	invalid := func(message string) (cron.Schedule, error) { return nil, &CronValidationError{message} }
	if len(expression) > 120 || len(strings.Fields(expression)) != 5 {
		return invalid("Use a five-field cron expression: minute hour day month weekday.")
	}
	if timezone == "" || timezone == "Local" || len(timezone) > 100 || strings.ContainsAny(timezone, " \t\r\n\x00") {
		return invalid("Choose an IANA timezone such as UTC or Africa/Cairo.")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return invalid("Unknown timezone; use an IANA timezone such as UTC or Africa/Cairo.")
	}
	schedule, err := cron.ParseStandard("CRON_TZ=" + timezone + " " + expression)
	if err != nil {
		return invalid("Invalid cron expression: " + err.Error())
	}
	if schedule.Next(time.Now()).IsZero() {
		return invalid("This schedule has no upcoming execution date.")
	}
	return schedule, nil
}

func CronPreview(expression, timezone string, now time.Time) ([]time.Time, error) {
	schedule, err := ParseCronSchedule(expression, timezone)
	if err != nil {
		return nil, err
	}
	next := make([]time.Time, 0, 5)
	for i := 0; i < 5; i++ {
		now = schedule.Next(now)
		if now.IsZero() {
			break
		}
		next = append(next, now)
	}
	return next, nil
}

func ValidateCronJob(job *models.CronJob) error {
	job.Name = strings.TrimSpace(job.Name)
	job.Command = strings.TrimSpace(job.Command)
	job.Schedule = strings.Join(strings.Fields(job.Schedule), " ")
	job.Timezone = strings.TrimSpace(job.Timezone)
	job.WorkingDir = strings.TrimSpace(job.WorkingDir)
	bad := func(message string) error { return &CronValidationError{message} }
	if job.Name == "" || len(job.Name) > 100 {
		return bad("Job name must contain 1–100 characters.")
	}
	if job.Command == "" || len(job.Command) > 16384 || strings.ContainsRune(job.Command, 0) {
		return bad("Provide a command of up to 16 KiB, without null characters.")
	}
	if len(job.WorkingDir) > 4096 || strings.ContainsAny(job.WorkingDir, "\x00\r\n") || (job.WorkingDir != "" && !strings.HasPrefix(job.WorkingDir, "/")) {
		return bad("Working directory must be an absolute path, or empty for the account's home directory.")
	}
	if job.TimeoutSeconds < 1 || job.TimeoutSeconds > 3600 {
		return bad("Timeout must be between 1 and 3600 seconds.")
	}
	_, err := ParseCronSchedule(job.Schedule, job.Timezone)
	return err
}

func (s *CronService) List(serverID string) ([]models.CronJob, error) { return s.store.List(serverID) }
func (s *CronService) Get(serverID, id string) (models.CronJob, error) {
	job, err := s.store.Get(serverID, id)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrCronNotFound
	}
	return job, err
}
func (s *CronService) Save(serverID, id, actor string, input models.CronJob) (models.CronJob, error) {
	if err := ValidateCronJob(&input); err != nil {
		return models.CronJob{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return models.CronJob{}, ErrCronCapacity
	}
	input.ServerID = serverID
	input.ID = id
	input.UpdatedAt = time.Now().UTC()
	if id == "" {
		jobs, err := s.store.List(serverID)
		if err != nil {
			return models.CronJob{}, err
		}
		if len(jobs) >= 500 {
			return models.CronJob{}, &CronValidationError{"Maximum of 500 jobs per server reached."}
		}
		input.ID = uuid.NewString()
		input.CreatedBy = actor
		input.CreatedAt = input.UpdatedAt
	} else {
		old, err := s.Get(serverID, id)
		if err != nil {
			return models.CronJob{}, err
		}
		input.CreatedBy = old.CreatedBy
		input.CreatedAt = old.CreatedAt
	}
	input.LastRun = nil
	input.NextRunAt = time.Time{}
	if input.Enabled {
		schedule, _ := ParseCronSchedule(input.Schedule, input.Timezone)
		input.NextRunAt = schedule.Next(input.UpdatedAt)
	}
	var err error
	if id == "" {
		err = s.store.Create(input)
	} else {
		err = s.store.Update(input)
	}
	return input, err
}
func (s *CronService) SetEnabled(serverID, id string, enabled bool) (models.CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.Get(serverID, id)
	if err != nil {
		return job, err
	}
	job.Enabled = enabled
	job.UpdatedAt = time.Now().UTC()
	job.NextRunAt = time.Time{}
	if enabled {
		schedule, err := ParseCronSchedule(job.Schedule, job.Timezone)
		if err != nil {
			return job, err
		}
		job.NextRunAt = schedule.Next(job.UpdatedAt)
	}
	return job, s.store.Update(job)
}
func (s *CronService) Delete(serverID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.Get(serverID, id); err != nil {
		return err
	}
	return s.store.Delete(serverID, id)
}

// ImportJob restores definitions through the scheduler boundary. Restored jobs
// are paused so importing a backup never executes a command on a new host.
func (s *CronService) ImportJob(job models.CronJob, mode string) (string, error) {
	if job.ID == "" || len(job.ID) > 100 || job.ServerID == "" {
		return "", &CronValidationError{"A job and server ID are required for import."}
	}
	if err := ValidateCronJob(&job); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.Get(job.ServerID, job.ID)
	if err != nil && !errors.Is(err, ErrCronNotFound) {
		return "", err
	}
	exists := err == nil
	if exists && mode == "skip" {
		return "skipped", nil
	}
	if _, active := s.active[job.ID]; active {
		return "", store.ErrCronBusy
	}
	job.Enabled = false
	job.NextRunAt = time.Time{}
	job.LastRun = nil
	job.UpdatedAt = time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = job.UpdatedAt
	}
	if exists {
		return "updated", s.store.Update(job)
	}
	jobs, err := s.store.List(job.ServerID)
	if err != nil {
		return "", err
	}
	if len(jobs) >= 500 {
		return "", &CronValidationError{"Maximum of 500 jobs per server reached."}
	}
	return "created", s.store.Create(job)
}
func (s *CronService) Runs(serverID, id string, limit, offset int) ([]models.CronRun, error) {
	if _, err := s.Get(serverID, id); err != nil {
		return nil, err
	}
	return s.store.Runs(serverID, id, limit, offset)
}
func (s *CronService) Run(serverID, id, runID string) (models.CronRun, error) {
	run, err := s.store.Run(serverID, id, runID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrCronNotFound
	}
	return run, err
}
func (s *CronService) RunNow(serverID, id, actor string) (models.CronRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.Get(serverID, id)
	if err != nil {
		return models.CronRun{}, err
	}
	if s.closed {
		return models.CronRun{}, ErrCronCapacity
	}
	if _, ok := s.active[id]; ok {
		return models.CronRun{}, store.ErrCronBusy
	}
	select {
	case s.workers <- struct{}{}:
	default:
		return models.CronRun{}, ErrCronCapacity
	}
	run := newCronRun(job, "manual", actor, time.Now().UTC())
	if err := s.store.Claim(job, run, false, time.Time{}); err != nil {
		<-s.workers
		return models.CronRun{}, err
	}
	s.launch(job, run)
	return run, nil
}
func (s *CronService) Cancel(serverID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.Get(serverID, id); err != nil {
		return err
	}
	cancel, ok := s.active[id]
	if !ok {
		return &CronValidationError{"This job has no active run to cancel."}
	}
	cancel()
	return nil
}
func newCronRun(job models.CronJob, trigger, actor string, now time.Time) models.CronRun {
	return models.CronRun{ID: uuid.NewString(), JobID: job.ID, ServerID: job.ServerID, Trigger: trigger, Actor: actor, Status: "running", Command: job.Command, WorkingDir: job.WorkingDir, StartedAt: now}
}

func (s *CronService) Tick(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	jobs, err := s.store.Due(now)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if !job.Enabled || job.NextRunAt.IsZero() || job.NextRunAt.After(now) {
			continue
		}
		expected := job.NextRunAt
		schedule, err := ParseCronSchedule(job.Schedule, job.Timezone)
		if err != nil {
			return err
		}
		job.NextRunAt = schedule.Next(now)
		run := newCronRun(job, "scheduled", "scheduler", now.UTC())
		reserved := false
		if _, busy := s.active[job.ID]; !busy {
			select {
			case s.workers <- struct{}{}:
				reserved = true
			default:
			}
		}
		if !reserved {
			run.Status = "skipped"
			run.FinishedAt = run.StartedAt
			run.Error = "Worker capacity reached or previous run is still active; execution was skipped."
		}
		if err := s.store.Claim(job, run, true, expected); err != nil {
			if reserved {
				<-s.workers
			}
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return err
		}
		// A concurrent scheduler may hold the persisted active claim.
		persisted, err := s.store.Run(job.ServerID, job.ID, run.ID)
		if err != nil {
			if reserved {
				<-s.workers
			}
			return err
		}
		if reserved && persisted.Status == "running" {
			s.launch(job, run)
		} else {
			if reserved {
				<-s.workers
			}
			if err := s.store.Prune(job.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// launch is called with mu held and a worker slot reserved.
func (s *CronService) launch(job models.CronJob, run models.CronRun) {
	ctx, cancel := context.WithCancel(s.ctx)
	s.active[job.ID] = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		result := func() (result CronResult) {
			defer func() {
				if value := recover(); value != nil {
					result.Err = fmt.Errorf("job executor panicked: %v", value)
				}
			}()
			return s.executor.Execute(ctx, job)
		}()
		run.Output = result.Output
		run.OutputTruncated = result.Truncated
		run.ExitCode = result.ExitCode
		run.FinishedAt = time.Now().UTC()
		run.DurationMS = run.FinishedAt.Sub(run.StartedAt).Milliseconds()
		run.Status = "succeeded"
		if result.Err != nil {
			run.Status = "failed"
			run.Error = result.Err.Error()
		}
		if result.TimedOut {
			run.Status = "timed_out"
		}
		if ctx.Err() != nil {
			run.Status = "cancelled"
			run.Error = "Execution cancelled. For remote commands, verify the outcome before retrying."
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.store.Finish(run); err != nil {
			log.Printf("Cron run %s persistence failed: %v", run.ID, err)
		}
		delete(s.active, job.ID)
		<-s.workers
	}()
}
func (s *CronService) Start(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.cancel()
			return
		case now := <-ticker.C:
			if err := s.Tick(now); err != nil {
				log.Printf("Cron scheduler: %v", err)
			}
		}
	}
}
func (s *CronService) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
}
