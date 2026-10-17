package services

import (
	"context"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestOperationsApprovalDeduplicationRecoveryAndOwnership(t *testing.T) {
	st, err := store.NewOperationStore(filepath.Join(t.TempDir(), "ops.json"))
	if err != nil {
		t.Fatal(err)
	}
	ops, err := NewOperations(st)
	if err != nil {
		t.Fatal(err)
	}
	defer ops.Close()
	var writes atomic.Int32
	work := func(ctx context.Context, step func(string)) (interface{}, error) {
		writes.Add(1)
		step("verified")
		return map[string]bool{"verified": true}, nil
	}
	op, err := ops.Submit("alice", "server", "resource", "restart", "services:write", true, work)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.Submit("alice", "server", "resource", "restart", "services:write", true, work); err == nil {
		t.Fatal("duplicate operation accepted")
	}
	if ops.Approve(op.ID, "bob", func(string) bool { return true }) == nil {
		t.Fatal("cross-user approval")
	}
	if ops.Approve(op.ID, "alice", func(string) bool { return false }) == nil {
		t.Fatal("permission revoked approval")
	}
	if writes.Load() != 0 {
		t.Fatal("work executed before approval")
	}
	if err := ops.Approve(op.ID, "alice", func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		saved, _ := ops.Get(op.ID)
		if saved.Status == "completed" {
			if len(saved.Steps) != 1 {
				t.Fatal("progress missing")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("work did not complete")
		}
		time.Sleep(time.Millisecond)
	}
	if ops.Approve(op.ID, "alice", func(string) bool { return true }) == nil || writes.Load() != 1 {
		t.Fatal("approval replay")
	}
	pending, err := ops.Submit("alice", "server", "resource", "restart", "services:write", true, work)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := NewOperations(st)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	saved, _ := recovered.Get(pending.ID)
	if saved.Status != "interrupted" {
		t.Fatalf("lost restart state: %+v", saved)
	}
	if writes.Load() != 1 {
		t.Fatal("interrupted operation replayed")
	}
}
func TestOperationsDeadlineFailure(t *testing.T) {
	st, _ := store.NewOperationStore(filepath.Join(t.TempDir(), "ops.json"))
	ops, _ := NewOperations(st)
	op, err := ops.Submit("alice", "server", "resource", "test", "", false, func(ctx context.Context, step func(string)) (interface{}, error) { <-ctx.Done(); return nil, ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	ops.Close()
	saved, _ := ops.Get(op.ID)
	if saved.Status != "failed" || saved.Error == "" {
		t.Fatalf("cancel failure not recorded: %+v", saved)
	}
}
func TestOperationPanicIsRecordedWithoutExposingDetails(t *testing.T) {
	st, _ := store.NewOperationStore(filepath.Join(t.TempDir(), "ops.json"))
	ops, _ := NewOperations(st)
	op, err := ops.Submit("alice", "server", "resource", "test", "", false, func(context.Context, func(string)) (interface{}, error) { panic("secret must not be exposed") })
	if err != nil {
		t.Fatal(err)
	}
	ops.Close()
	saved, _ := ops.Get(op.ID)
	if saved.Status != "failed" || saved.Error != "operation stopped after an internal failure; verify remote state before retrying" {
		t.Fatalf("unexpected failure: %+v", saved)
	}
}
func TestManagedServiceValidationAndVerification(t *testing.T) {
	runner := &fixtureExecutor{run: func(_, cmd string) (string, error) {
		if cmd == "systemctl is-active -- 'nginx'" {
			return "active", nil
		}
		return "", nil
	}}
	manager := &ServiceManager{executor: runner}
	if manager.RestartManagedService(context.Background(), "srv", "nginx;shutdown") == nil || len(runner.commands) != 0 {
		t.Fatal("invalid name executed")
	}
	if err := manager.RestartManagedService(context.Background(), "srv", "nginx"); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 2 {
		t.Fatal("restart was not verified")
	}
}
