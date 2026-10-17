package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/google/uuid"
)

type FunctionValidationError struct{ Message string }

func (e *FunctionValidationError) Error() string { return e.Message }

var ErrFunctionAccess = errors.New("server:write, docker:write and access to the server are required")
var ErrFunctionCapacity = errors.New("function worker capacity reached; try again shortly")

type FunctionService struct {
	Store        *store.FunctionStore
	Servers      *store.ServerStore
	Users        *store.UserStore
	backend      FunctionBackend
	mu           sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	active       map[string]context.CancelFunc
	serverActive map[string]int
	workers      chan struct{}
	wg           sync.WaitGroup
	closed       bool
}

func NewFunctionService(st *store.FunctionStore, servers *store.ServerStore, users *store.UserStore, backend FunctionBackend) (*FunctionService, error) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &FunctionService{Store: st, Servers: servers, Users: users, backend: backend, ctx: ctx, cancel: cancel, active: map[string]context.CancelFunc{}, serverActive: map[string]int{}, workers: make(chan struct{}, 8)}
	pending, err := st.Pending()
	if err != nil {
		cancel()
		return nil, err
	}
	for _, r := range pending {
		if r.Status == "running" {
			r.Status = "interrupted"
			r.Error = "GoPulse restarted. Execution was not replayed; remote cleanup is pending."
			r.FinishedAt = time.Now().UTC()
			if err = st.UpdateRun(r); err != nil {
				cancel()
				return nil, err
			}
		}
	}
	// Missed slots are intentionally not replayed after downtime.
	functions, err := st.List("")
	if err != nil {
		cancel()
		return nil, err
	}
	now := time.Now().UTC()
	for _, f := range functions {
		if !f.NextRunAt.IsZero() && !f.NextRunAt.After(now) && f.Deployed != nil {
			sch, e := ParseCronSchedule(f.Deployed.Spec.Schedule, f.Deployed.Spec.Timezone)
			if e != nil {
				cancel()
				return nil, e
			}
			f.NextRunAt = sch.Next(now)
			if err = st.Save(f, false); err != nil {
				cancel()
				return nil, err
			}
		}
	}
	return s, nil
}
func functionUserAllowed(u models.User, server string) bool {
	if u.Role == "admin" {
		return true
	}
	permission := func(want string) bool {
		for _, p := range u.Permissions {
			if p == "*" || p == want {
				return true
			}
		}
		return false
	}
	scope := false
	for _, id := range u.AllowedServers {
		if id == server {
			scope = true
		}
	}
	return scope && permission("server:write") && permission("docker:write")
}
func (s *FunctionService) Authorize(username, server string) (models.User, error) {
	u, e := s.Users.GetUser(username)
	if e != nil || !functionUserAllowed(u, server) {
		return models.User{}, ErrFunctionAccess
	}
	return u, nil
}
func (s *FunctionService) authorizeID(id, server string) error {
	u, e := s.Users.GetUserByID(id)
	if e != nil || !functionUserAllowed(u, server) {
		return ErrFunctionAccess
	}
	return nil
}
func (s *FunctionService) Save(server, id, actor string, spec models.FunctionSpec) (models.Function, error) {
	if err := validateFunctionSpec(spec); err != nil {
		return models.Function{}, err
	}
	u, err := s.Authorize(actor, server)
	if err != nil {
		return models.Function{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return models.Function{}, ErrFunctionCapacity
	}
	if _, err = s.Servers.GetServer(server); err != nil {
		return models.Function{}, store.ErrFunctionNotFound
	}
	now := time.Now().UTC()
	var f models.Function
	create := id == ""
	if create {
		all, e := s.Store.List(server)
		if e != nil {
			return f, e
		}
		if len(all) >= 100 {
			return f, &FunctionValidationError{"Limit of 100 functions per server reached."}
		}
		f = models.Function{ID: uuid.NewString(), ServerID: server, CreatedAt: now}
	} else {
		f, err = s.Store.Get(server, id)
		if err != nil {
			return f, err
		}
	}
	f.OwnerID = u.ID
	f.Draft = spec
	f.Revision++
	f.UpdatedAt = now
	err = s.Store.Save(f, create)
	return f, err
}
func (s *FunctionService) Begin(server, id, actor, kind, trigger string, event json.RawMessage) (models.FunctionRun, error) {
	u, err := s.Authorize(actor, server)
	if err != nil {
		return models.FunctionRun{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.beginLocked(server, id, u.ID, kind, trigger, event)
}
func (s *FunctionService) beginLocked(server, id, actorID, kind, trigger string, event json.RawMessage) (models.FunctionRun, error) {
	var r models.FunctionRun
	if s.closed {
		return r, ErrFunctionCapacity
	}
	f, err := s.Store.Get(server, id)
	if err != nil {
		return r, err
	}
	if kind != "deploy" && kind != "invoke" {
		return r, &FunctionValidationError{"Unknown function operation."}
	}
	if kind == "invoke" && (len(event) > 32768 || (len(event) > 0 && !json.Valid(event))) {
		return r, &FunctionValidationError{"Event must be valid JSON up to 32 KiB."}
	}
	if kind == "invoke" && f.Deployed == nil {
		return r, &FunctionValidationError{"Deploy a function version before running it."}
	}
	if err = s.authorizeID(actorID, server); err != nil {
		return r, err
	}
	if s.serverActive[server] >= 4 {
		return r, ErrFunctionCapacity
	}
	select {
	case s.workers <- struct{}{}:
	default:
		return r, ErrFunctionCapacity
	}
	r = models.FunctionRun{ID: uuid.NewString(), FunctionID: f.ID, ServerID: server, Kind: kind, Trigger: trigger, ActorID: actorID, Status: "running", Revision: f.Revision, StartedAt: time.Now().UTC(), NeedsCleanup: true}
	if kind == "invoke" {
		r.VersionID = f.Deployed.ID
		r.Revision = f.Deployed.Revision
	}
	if err = s.Store.Claim(f, r); err != nil {
		<-s.workers
		return r, err
	}
	limit := 250 * time.Second
	if kind == "invoke" {
		limit = time.Duration(f.Deployed.Spec.TimeoutSeconds+10) * time.Second
	}
	ctx, cancel := context.WithTimeout(s.ctx, limit)
	s.active[r.ID] = cancel
	s.serverActive[server]++
	s.wg.Add(1)
	event = append(json.RawMessage(nil), event...)
	go s.execute(ctx, cancel, f, r, event)
	return r, nil
}
func (s *FunctionService) execute(ctx context.Context, cancel context.CancelFunc, f models.Function, r models.FunctionRun, event json.RawMessage) {
	logs := &FunctionLogs{}
	flushDone := make(chan struct{})
	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-flushDone:
				return
			case <-ticker.C:
				text, truncated := logs.Snapshot()
				if err := s.Store.UpdateLogs(r.ID, text, truncated); err != nil {
					log.Printf("function log persistence failed: %v", err)
				}
			}
		}
	}()
	// Keep finalization in one defer, including a defensive panic path. Cleanup
	// gets a fresh deadline even when the function's execution context expired.
	defer func() {
		if recover() != nil {
			r.Status = "failed"
			r.Error = "Internal function execution failure; cleanup will be retried."
		}
		close(flushDone)
		<-flushed
		cancel()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 25*time.Second)
		cleanupErr := s.cleanup(cleanupCtx, r)
		cleanupCancel()
		r.NeedsCleanup = cleanupErr != nil
		if cleanupErr != nil {
			r.Error += functionCleanupPending
		}
		r.Logs, r.LogsTruncated = logs.Snapshot()
		r.Result = logs.Result()
		r.FinishedAt = time.Now().UTC()
		r.DurationMS = r.FinishedAt.Sub(r.StartedAt).Milliseconds()
		if err := s.Store.UpdateRun(r); err != nil {
			log.Printf("function result persistence failed: %v", err)
		}
		s.mu.Lock()
		delete(s.active, r.ID)
		s.serverActive[f.ServerID]--
		s.mu.Unlock()
		<-s.workers
		s.wg.Done()
	}()
	r.Status = "failed"
	if err := s.authorizeID(r.ActorID, f.ServerID); err != nil {
		r.Error = err.Error()
		return
	}
	if r.Kind == "deploy" {
		image, err := s.backend.Prepare(ctx, f, r, logs)
		if err != nil {
			r.Error = err.Error()
			s.executionStatus(ctx, &r)
			return
		}
		s.mu.Lock()
		current, e := s.Store.Get(f.ServerID, f.ID)
		if e == nil {
			e = s.authorizeID(r.ActorID, f.ServerID)
		}
		if e == nil && ctx.Err() == nil {
			sum := sha256.Sum256([]byte(f.Draft.Code))
			v := models.FunctionVersion{ID: uuid.NewString(), FunctionID: f.ID, Revision: f.Revision, Spec: f.Draft, ImageID: image, CodeSHA256: hex.EncodeToString(sum[:]), CreatedAt: time.Now().UTC()}
			current.Deployed = &v
			current.NextRunAt = time.Time{}
			if v.Spec.ScheduleEnabled {
				sch, _ := ParseCronSchedule(v.Spec.Schedule, v.Spec.Timezone)
				current.NextRunAt = sch.Next(time.Now())
			}
			e = s.Store.Activate(current, v)
			if e == nil {
				r.VersionID = v.ID
				r.Status = "succeeded"
				fmt.Fprintln(logs, "Deployed version "+v.ID)
			}
		} else if e == nil {
			e = ctx.Err()
		}
		s.mu.Unlock()
		if e != nil {
			r.Error = "Deployment was not activated; previous version remains available."
			s.executionStatus(ctx, &r)
		}
	} else {
		code, err := s.backend.Execute(ctx, *f.Deployed, r, event, logs)
		if code >= 0 {
			r.ExitCode = &code
		}
		if err == nil {
			r.Status = "succeeded"
		} else {
			r.Error = "Function execution failed; inspect logs."
			if code == 124 || code == 137 {
				r.Status = "timed_out"
				r.Error = "Function reached its timeout or was killed (including memory exhaustion)."
			}
			s.executionStatus(ctx, &r)
		}
	}
}
func (s *FunctionService) executionStatus(ctx context.Context, r *models.FunctionRun) {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		r.Status = "timed_out"
		r.Error = "Function execution deadline exceeded."
	} else if ctx.Err() != nil {
		r.Status = "cancelled"
		r.Error = "Function execution cancelled."
	}
}
func (s *FunctionService) Cancel(server, id, runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, e := s.Store.Run(id, runID)
	if e != nil || r.ServerID != server {
		return store.ErrFunctionNotFound
	}
	cancel, ok := s.active[runID]
	if !ok {
		return &FunctionValidationError{"This execution is no longer active."}
	}
	cancel()
	return nil
}
func (s *FunctionService) Delete(server, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Store.Delete(server, id)
}
func (s *FunctionService) Pause(server, id string, paused bool) (models.Function, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, e := s.Store.Get(server, id)
	if e != nil {
		return f, e
	}
	f.Paused = paused
	if !paused && f.Deployed != nil && f.Deployed.Spec.ScheduleEnabled {
		sch, err := ParseCronSchedule(f.Deployed.Spec.Schedule, f.Deployed.Spec.Timezone)
		if err != nil {
			return f, err
		}
		f.NextRunAt = sch.Next(time.Now())
	}
	return f, s.Store.Save(f, false)
}
func (s *FunctionService) RotateToken(server, id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, e := s.Store.Get(server, id); e != nil {
		return "", e
	}
	var secret [32]byte
	if _, e := rand.Read(secret[:]); e != nil {
		return "", e
	}
	token := base64.RawURLEncoding.EncodeToString(secret[:])
	sum := sha256.Sum256([]byte(token))
	return token, s.Store.SetTokenHash(server, id, hex.EncodeToString(sum[:]))
}
func (s *FunctionService) InvokeHTTP(server, id, token string, event json.RawMessage) (models.FunctionRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, e := s.Store.Get(server, id)
	if e != nil || len(token) != 43 {
		return models.FunctionRun{}, ErrFunctionAccess
	}
	stored, e := s.Store.TokenHash(server, id)
	sum := sha256.Sum256([]byte(token))
	if e != nil || subtle.ConstantTimeCompare([]byte(stored), []byte(hex.EncodeToString(sum[:]))) != 1 {
		return models.FunctionRun{}, ErrFunctionAccess
	}
	if f.Paused || f.Deployed == nil || !f.Deployed.Spec.HTTPEnabled {
		return models.FunctionRun{}, &FunctionValidationError{"HTTP trigger is paused or not deployed."}
	}
	return s.beginLocked(server, id, f.OwnerID, "invoke", "http", event)
}
func (s *FunctionService) Tick(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	functions, err := s.Store.List("")
	if err != nil {
		return err
	}
	for _, f := range functions {
		if f.Deployed == nil || f.Paused || !f.Deployed.Spec.ScheduleEnabled || f.NextRunAt.IsZero() || f.NextRunAt.After(now) {
			continue
		}
		sch, e := ParseCronSchedule(f.Deployed.Spec.Schedule, f.Deployed.Spec.Timezone)
		if e != nil {
			return e
		}
		slot := f.NextRunAt
		f.NextRunAt = sch.Next(now)
		if e = s.Store.Save(f, false); e != nil {
			return e
		}
		event, _ := json.Marshal(map[string]interface{}{"scheduled_at": slot.UTC(), "source": "gopulse.schedule"})
		if _, e = s.beginLocked(f.ServerID, f.ID, f.OwnerID, "invoke", "scheduled", event); e != nil {
			r := models.FunctionRun{ID: uuid.NewString(), FunctionID: f.ID, ServerID: f.ServerID, VersionID: f.Deployed.ID, Revision: f.Deployed.Revision, Kind: "invoke", Trigger: "scheduled", ActorID: f.OwnerID, Status: "skipped", Error: e.Error(), StartedAt: now.UTC(), FinishedAt: now.UTC()}
			if e = s.Store.RecordSkipped(r); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *FunctionService) RetryCleanup() {
	pending, err := s.Store.Pending()
	if err != nil {
		log.Printf("function cleanup scan failed: %v", err)
		return
	}
	for _, r := range pending {
		if r.Status == "running" {
			continue
		}
		ctx, cancel := context.WithTimeout(s.ctx, 25*time.Second)
		err = s.cleanup(ctx, r)
		cancel()
		if err != nil {
			continue
		}
		r.NeedsCleanup = false
		r.Error = strings.TrimSpace(strings.ReplaceAll(r.Error, functionCleanupPending, ""))
		r.Error = strings.ReplaceAll(r.Error, "remote cleanup is pending.", "remote cleanup completed.")
		if err = s.Store.UpdateRun(r); err != nil {
			log.Printf("function cleanup persistence failed: %v", err)
		}
	}
	if err = s.Store.Prune(time.Now()); err != nil {
		log.Printf("function history pruning failed: %v", err)
	}
}
func (s *FunctionService) Start(ctx context.Context) {
	// A separate cleanup loop ensures offline SSH hosts do not delay schedules.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		s.RetryCleanup()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				s.RetryCleanup()
			}
		}
	}()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.ctx.Done():
			return
		case now := <-ticker.C:
			if e := s.Tick(now); e != nil {
				log.Printf("function scheduler failed: %v", e)
			}
		}
	}
}
func (s *FunctionService) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
}

const functionCleanupPending = " Remote container cleanup pending; further runs are blocked until cleanup succeeds."

func (s *FunctionService) cleanup(ctx context.Context, r models.FunctionRun) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("internal cleanup failure")
		}
	}()
	return s.backend.Cleanup(ctx, r)
}
