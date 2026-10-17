package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/google/uuid"
)

type functionFixture struct {
	mu                      sync.Mutex
	block                   chan struct{}
	failDeploy, failCleanup bool
	versions                []models.FunctionVersion
	events                  []string
}

func (b *functionFixture) Prepare(ctx context.Context, f models.Function, r models.FunctionRun, w io.Writer) (string, error) {
	b.mu.Lock()
	fail := b.failDeploy
	block := b.block
	b.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if fail {
		io.WriteString(w, "SyntaxError: fixture")
		return "", errors.New("syntax check failed")
	}
	io.WriteString(w, "syntax checked\n")
	return "sha256:" + strings.Repeat("a", 64), nil
}
func (b *functionFixture) Execute(ctx context.Context, v models.FunctionVersion, r models.FunctionRun, event json.RawMessage, w io.Writer) (int, error) {
	b.mu.Lock()
	b.versions = append(b.versions, v)
	b.events = append(b.events, string(event))
	block := b.block
	b.mu.Unlock()
	io.WriteString(w, "hello\n")
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return -1, ctx.Err()
		}
	}
	return 0, nil
}
func (b *functionFixture) Cleanup(context.Context, models.FunctionRun) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failCleanup {
		return errors.New("offline")
	}
	return nil
}
func newFunctionTest(t *testing.T) (*FunctionService, *functionFixture, models.FunctionSpec) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "state.json")
	servers, e := store.NewServerStore(file)
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"a", "b"} {
		if e = servers.AddServer(models.Server{ID: id, Host: id + ".invalid"}); e != nil {
			t.Fatal(e)
		}
	}
	users, e := store.NewUserStore(file)
	if e != nil {
		t.Fatal(e)
	}
	if e = users.AddUserRaw(models.User{ID: "owner", Username: "owner", Role: "admin"}); e != nil {
		t.Fatal(e)
	}
	st, e := store.NewFunctionStore(file)
	if e != nil {
		t.Fatal(e)
	}
	backend := &functionFixture{}
	s, e := NewFunctionService(st, servers, users, backend)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	return s, backend, models.FunctionSpec{Name: "Hello", Runtime: "python", Code: "def handler(event, context):\n return event\n", TimeoutSeconds: 2, MemoryMB: 256, CPUMillis: 500, Concurrency: 1, Timezone: "UTC"}
}
func waitFunction(t *testing.T, s *FunctionService, r models.FunctionRun) models.FunctionRun {
	t.Helper()
	waitLimit := 5 * time.Second
	if os.Getenv("GOPULSE_TEST_FUNCTION_DOCKER") == "1" {
		waitLimit = 270 * time.Second
	}
	deadline := time.Now().Add(waitLimit)
	for time.Now().Before(deadline) {
		got, e := s.Store.Run(r.FunctionID, r.ID)
		if e != nil {
			t.Fatal(e)
		}
		if got.Status != "running" {
			// Final persistence precedes releasing the worker; wait for that release.
			s.mu.Lock()
			_, active := s.active[r.ID]
			s.mu.Unlock()
			if !active {
				return got
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("function did not finish")
	return r
}
func deployedFunction(t *testing.T, s *FunctionService, spec models.FunctionSpec) models.Function {
	t.Helper()
	f, e := s.Save("a", "", "owner", spec)
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Begin("a", f.ID, "owner", "deploy", "manual", nil)
	if e != nil {
		t.Fatal(e)
	}
	r = waitFunction(t, s, r)
	if r.Status != "succeeded" {
		t.Fatal(r)
	}
	f, e = s.Store.Get("a", f.ID)
	if e != nil {
		t.Fatal(e)
	}
	return f
}
func TestFunctionsImmutableDeploymentsAndFailure(t *testing.T) {
	s, b, spec := newFunctionTest(t)
	f := deployedFunction(t, s, spec)
	original := f.Deployed.ID
	spec.Code = "changed draft"
	if _, e := s.Save("a", f.ID, "owner", spec); e != nil {
		t.Fatal(e)
	}
	r, e := s.Begin("a", f.ID, "owner", "invoke", "manual", json.RawMessage(`{"name":"Ada"}`))
	if e != nil {
		t.Fatal(e)
	}
	r = waitFunction(t, s, r)
	if r.Status != "succeeded" || r.VersionID != original || !strings.Contains(r.Logs, "hello") {
		t.Fatal(r)
	}
	b.mu.Lock()
	if b.versions[0].Spec.Code == spec.Code {
		t.Fatal("executed undeployed draft")
	}
	b.failDeploy = true
	b.mu.Unlock()
	r, e = s.Begin("a", f.ID, "owner", "deploy", "manual", nil)
	if e != nil {
		t.Fatal(e)
	}
	r = waitFunction(t, s, r)
	if r.Status != "failed" {
		t.Fatal(r)
	}
	f, e = s.Store.Get("a", f.ID)
	if e != nil || f.Deployed.ID != original {
		t.Fatal("failed deployment replaced active version", e)
	}
	versions, e := s.Store.Versions(f.ID)
	if e != nil || len(versions) != 1 {
		t.Fatal(e, versions)
	}
	if e = s.Servers.RemoveServer("a"); e == nil {
		t.Fatal("deleted referenced server")
	}
	if _, e = s.Store.Get("b", f.ID); !errors.Is(e, store.ErrFunctionNotFound) {
		t.Fatal("cross-server lookup", e)
	}
}
func TestFunctionsConcurrencyCancellationAndCleanup(t *testing.T) {
	s, b, spec := newFunctionTest(t)
	f := deployedFunction(t, s, spec)
	b.mu.Lock()
	b.block = make(chan struct{})
	b.failCleanup = true
	b.mu.Unlock()
	r, e := s.Begin("a", f.ID, "owner", "invoke", "manual", nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Begin("a", f.ID, "owner", "invoke", "manual", nil); !errors.Is(e, store.ErrFunctionBusy) {
		t.Fatal("overlap", e)
	}
	if e = s.Delete("a", f.ID); !errors.Is(e, store.ErrFunctionBusy) {
		t.Fatal("deleted active function", e)
	}
	if e = s.Cancel("b", f.ID, r.ID); !errors.Is(e, store.ErrFunctionNotFound) {
		t.Fatal("cross-server cancel", e)
	}
	if e = s.Cancel("a", f.ID, r.ID); e != nil {
		t.Fatal(e)
	}
	r = waitFunction(t, s, r)
	if r.Status != "cancelled" || !r.NeedsCleanup {
		t.Fatal(r)
	}
	if _, e = s.Begin("a", f.ID, "owner", "invoke", "manual", nil); !errors.Is(e, store.ErrFunctionBusy) {
		t.Fatal("cleanup overlap", e)
	}
	b.mu.Lock()
	b.failCleanup = false
	b.block = nil
	b.mu.Unlock()
	s.RetryCleanup()
	r, e = s.Store.Run(f.ID, r.ID)
	if e != nil || r.NeedsCleanup {
		t.Fatal(r, e)
	}
	if e = s.Delete("a", f.ID); e != nil {
		t.Fatal(e)
	}
}
func TestFunctionsHTTPTriggersRotationPauseAndRevocation(t *testing.T) {
	s, _, spec := newFunctionTest(t)
	spec.HTTPEnabled = true
	f := deployedFunction(t, s, spec)
	token, e := s.RotateToken("a", f.ID)
	if e != nil {
		t.Fatal(e)
	}
	hash, e := s.Store.TokenHash("a", f.ID)
	if e != nil || hash == token || len(hash) != 64 {
		t.Fatal("token not hashed", e)
	}
	if _, e = s.InvokeHTTP("a", f.ID, "bad", nil); !errors.Is(e, ErrFunctionAccess) {
		t.Fatal(e)
	}
	r, e := s.InvokeHTTP("a", f.ID, token, json.RawMessage(`{"test":true}`))
	if e != nil {
		t.Fatal(e)
	}
	r = waitFunction(t, s, r)
	if r.Trigger != "http" || strings.Contains(r.Logs, token) {
		t.Fatal(r)
	}
	if _, e = s.Pause("a", f.ID, true); e != nil {
		t.Fatal(e)
	}
	if _, e = s.InvokeHTTP("a", f.ID, token, nil); e == nil {
		t.Fatal("paused HTTP accepted")
	}
	if _, e = s.Pause("a", f.ID, false); e != nil {
		t.Fatal(e)
	}
	newToken, e := s.RotateToken("a", f.ID)
	if e != nil || newToken == token {
		t.Fatal(e)
	}
	if _, e = s.InvokeHTTP("a", f.ID, token, nil); !errors.Is(e, ErrFunctionAccess) {
		t.Fatal("old token accepted", e)
	}
	if e = s.Users.UpdateUser(models.User{ID: "owner", Username: "owner", Role: "viewer", AllowedServers: []string{"a"}}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.InvokeHTTP("a", f.ID, newToken, nil); !errors.Is(e, ErrFunctionAccess) {
		t.Fatal("revoked owner accepted", e)
	}
}
func TestFunctionsScheduleSkipAndRestart(t *testing.T) {
	s, b, spec := newFunctionTest(t)
	spec.Schedule = "* * * * *"
	spec.ScheduleEnabled = true
	f := deployedFunction(t, s, spec)
	slot := f.NextRunAt
	b.mu.Lock()
	b.block = make(chan struct{})
	b.mu.Unlock()
	if e := s.Tick(slot); e != nil {
		t.Fatal(e)
	}
	if e := s.Tick(slot); e != nil {
		t.Fatal(e)
	}
	if e := s.Tick(slot.Add(time.Minute)); e != nil {
		t.Fatal(e)
	}
	runs, e := s.Store.Runs(f.ID)
	if e != nil {
		t.Fatal(e)
	}
	skips, active := 0, 0
	var running models.FunctionRun
	for _, r := range runs {
		if r.Status == "skipped" {
			skips++
		}
		if r.Status == "running" {
			active++
			running = r
		}
	}
	if skips != 1 || active != 1 {
		t.Fatal(runs)
	}
	if e = s.Cancel("a", f.ID, running.ID); e != nil {
		t.Fatal(e)
	}
	waitFunction(t, s, running)
	s.Close()
	b.mu.Lock()
	b.block = nil
	b.mu.Unlock()
	interrupted := models.FunctionRun{ID: uuid.NewString(), FunctionID: f.ID, ServerID: "a", Kind: "invoke", Status: "running", NeedsCleanup: true, StartedAt: time.Now()}
	if e = s.Store.Claim(f, interrupted); e != nil {
		t.Fatal(e)
	}
	restarted, e := NewFunctionService(s.Store, s.Servers, s.Users, b)
	if e != nil {
		t.Fatal(e)
	}
	defer restarted.Close()
	got, e := s.Store.Run(f.ID, interrupted.ID)
	if e != nil || got.Status != "interrupted" || !got.NeedsCleanup {
		t.Fatal(got, e)
	}
	restarted.RetryCleanup()
	got, _ = s.Store.Run(f.ID, interrupted.ID)
	if got.NeedsCleanup {
		t.Fatal("restart cleanup not retried")
	}
}
func TestFunctionsValidationBounds(t *testing.T) {
	s, _, valid := newFunctionTest(t)
	for _, mutate := range []func(*models.FunctionSpec){func(v *models.FunctionSpec) { v.Name = "" }, func(v *models.FunctionSpec) { v.Runtime = "shell" }, func(v *models.FunctionSpec) { v.Code = strings.Repeat("a", 32769) }, func(v *models.FunctionSpec) { v.TimeoutSeconds = 901 }, func(v *models.FunctionSpec) { v.MemoryMB = 0 }, func(v *models.FunctionSpec) { v.CPUMillis = 2001 }, func(v *models.FunctionSpec) { v.Concurrency = 5 }, func(v *models.FunctionSpec) { v.ScheduleEnabled = true }, func(v *models.FunctionSpec) { v.Schedule = "* * * * *"; v.Timezone = "evil" }} {
		spec := valid
		mutate(&spec)
		if _, e := s.Save("a", "", "owner", spec); e == nil {
			t.Fatal("invalid spec accepted", spec)
		}
	}
	f, e := s.Save("a", "", "owner", valid)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Begin("a", f.ID, "owner", "invoke", "manual", nil); e == nil {
		t.Fatal("invoked draft")
	}
}
func TestFunctionLogsBoundedAndConcurrent(t *testing.T) {
	var logs FunctionLogs
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); logs.Write([]byte(strings.Repeat("λ", FunctionLogLimit))) }()
	}
	wg.Wait()
	text, truncated := logs.Snapshot()
	if !truncated || len(text) > FunctionLogLimit {
		t.Fatal(len(text), truncated)
	}
}

func TestFunctionsDraftEditsDuringDeploymentKeepSnapshot(t *testing.T) {
	s, b, spec := newFunctionTest(t)
	f, e := s.Save("a", "", "owner", spec)
	if e != nil {
		t.Fatal(e)
	}
	b.mu.Lock()
	block := make(chan struct{})
	b.block = block
	b.mu.Unlock()
	r, e := s.Begin("a", f.ID, "owner", "deploy", "manual", nil)
	if e != nil {
		t.Fatal(e)
	}
	changed := spec
	changed.Code = "def handler(event, context):\n return 'new draft'\n"
	if _, e = s.Save("a", f.ID, "owner", changed); e != nil {
		t.Fatal(e)
	}
	close(block)
	waitFunction(t, s, r)
	got, e := s.Store.Get("a", f.ID)
	if e != nil || got.Revision != 2 || got.Draft.Code != changed.Code || got.Deployed.Revision != 1 || got.Deployed.Spec.Code != spec.Code {
		t.Fatal(got, e)
	}
}
func TestFunctionsGlobalAndPerServerCapacity(t *testing.T) {
	s, b, spec := newFunctionTest(t)
	spec.Concurrency = 4
	fs := []models.Function{deployedFunction(t, s, spec)}
	if e := s.Servers.AddServer(models.Server{ID: "c", Host: "c.invalid"}); e != nil {
		t.Fatal(e)
	}
	for _, server := range []string{"b", "c"} {
		f, e := s.Save(server, "", "owner", spec)
		if e != nil {
			t.Fatal(e)
		}
		r, e := s.Begin(server, f.ID, "owner", "deploy", "manual", nil)
		if e != nil {
			t.Fatal(e)
		}
		waitFunction(t, s, r)
		f, _ = s.Store.Get(server, f.ID)
		fs = append(fs, f)
	}
	b.mu.Lock()
	b.block = make(chan struct{})
	b.mu.Unlock()
	for _, f := range fs[:2] {
		for range 4 {
			if _, e := s.Begin(f.ServerID, f.ID, "owner", "invoke", "manual", nil); e != nil {
				t.Fatal(e)
			}
		}
	}
	for _, f := range fs {
		if _, e := s.Begin(f.ServerID, f.ID, "owner", "invoke", "manual", nil); !errors.Is(e, ErrFunctionCapacity) {
			t.Fatal("capacity exceeded", e)
		}
	}
}
func TestFunctionRunningLogsPersistBeforeCompletion(t *testing.T) {
	s, b, spec := newFunctionTest(t)
	f := deployedFunction(t, s, spec)
	b.mu.Lock()
	b.block = make(chan struct{})
	b.mu.Unlock()
	r, e := s.Begin("a", f.ID, "owner", "invoke", "manual", nil)
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(3 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		got, _ := s.Store.Run(f.ID, r.ID)
		if got.Status == "running" && got.Logs == "hello\n" {
			found = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !found {
		t.Fatal("live logs not persisted")
	}
	s.Cancel("a", f.ID, r.ID)
	waitFunction(t, s, r)
}
