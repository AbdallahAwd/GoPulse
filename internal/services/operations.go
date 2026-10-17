package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/google/uuid"
	"log"
	"sync"
	"time"
)

type OperationWork func(context.Context, func(string)) (interface{}, error)
type pendingWork struct {
	work       OperationWork
	permission string
}
type Operations struct {
	workers chan struct{}
	closed  bool
	store   *store.OperationStore
	mu      sync.Mutex
	pending map[string]pendingWork
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewOperations(st *store.OperationStore) (*Operations, error) {
	if err := st.Recover(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Operations{store: st, pending: make(map[string]pendingWork), workers: make(chan struct{}, 8), ctx: ctx, cancel: cancel}, nil
}
func (s *Operations) Close() { s.mu.Lock(); s.closed = true; s.cancel(); s.mu.Unlock(); s.wg.Wait() }
func (s *Operations) Submit(owner, server, resource, kind, permission string, approval bool, work OperationWork) (models.Operation, error) {
	return s.SubmitReview(owner, server, resource, kind, permission, approval, nil, nil, work)
}
func (s *Operations) SubmitReview(owner, server, resource, kind, permission string, approval bool, preview interface{}, targets []string, work OperationWork, origin ...string) (models.Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return models.Operation{}, fmt.Errorf("operation service closed")
	}
	s.expireLocked()
	status := "running"
	if approval {
		status = "awaiting_approval"
	}
	var agentID, agentName, requestKey, fingerprint string
	if len(origin) > 0 {
		agentID = origin[0]
	}
	if len(origin) > 1 {
		agentName = origin[1]
	}
	if len(origin) > 2 {
		requestKey = origin[2]
	}
	if len(origin) > 3 {
		fingerprint = origin[3]
	}
	if requestKey != "" {
		existing, err := s.store.GetByRequest(owner, requestKey)
		if err == nil {
			if existing.RequestFingerprint != fingerprint {
				return models.Operation{}, fmt.Errorf("request_id was already used for a different operation")
			}
			return existing, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return models.Operation{}, err
		}
	}
	if !approval {
		select {
		case s.workers <- struct{}{}:
		default:
			return models.Operation{}, fmt.Errorf("operation worker capacity reached; no action executed")
		}
	}
	op := models.Operation{AgentCredentialID: agentID, AgentName: agentName, RequestKey: requestKey, RequestFingerprint: fingerprint, Preview: preview, TargetServers: targets, ID: uuid.NewString(), Owner: owner, ServerID: server, Resource: resource, Kind: kind, Status: status, Steps: []string{}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.store.Create(op); err != nil {
		if !approval {
			<-s.workers
		}
		return models.Operation{}, fmt.Errorf("another operation is active for this resource, or operation persistence failed: %w", err)
	}
	if approval {
		s.pending[op.ID] = pendingWork{work, permission}
	} else {
		s.launch(op, work)
	}
	return op, nil
}
func (s *Operations) launch(op models.Operation, work OperationWork) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { <-s.workers }()
		ctx, cancel := context.WithTimeout(s.ctx, 8*time.Minute)
		defer cancel()
		var persistErr error
		result, err := executeOperation(ctx, work, func(step string) {
			op.Steps = append(op.Steps, step)
			op.UpdatedAt = time.Now()
			if e := s.store.Save(op); e != nil {
				persistErr = e
				cancel()
			}
		})
		if persistErr != nil {
			err = fmt.Errorf("progress persistence failed: %w", persistErr)
		}
		op.Result = result
		op.Status = "completed"
		if err != nil {
			op.Status = "failed"
			op.Error = err.Error()
		}
		op.UpdatedAt = time.Now()
		if err := s.store.Save(op); err != nil {
			log.Printf("operation %s final status persistence failed: %v", op.ID, err)
		}
		if err := s.store.AuditIdentity(op.Owner, op.AgentCredentialID, op.Kind, op.ServerID, op.Status, op.ID); err != nil {
			log.Printf("operation %s audit persistence failed: %v", op.ID, err)
		}
	}()
}
func executeOperation(ctx context.Context, work OperationWork, progress func(string)) (result interface{}, err error) {
	defer func() {
		if recover() != nil {
			result = nil
			err = fmt.Errorf("operation stopped after an internal failure; verify remote state before retrying")
		}
	}()
	return work(ctx, progress)
}
func (s *Operations) Approve(id, owner string, allowed func(string) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("operation service closed")
	}
	op, err := s.store.Get(id)
	if err != nil {
		return err
	}
	pending, ok := s.pending[id]
	if !ok || op.Status != "awaiting_approval" || op.Owner != owner {
		return fmt.Errorf("approval is unavailable for this user")
	}
	if time.Since(op.CreatedAt) > 15*time.Minute {
		op.Status = "expired"
		s.store.Save(op)
		delete(s.pending, id)
		return fmt.Errorf("approval expired")
	}
	if !allowed(pending.permission) {
		return fmt.Errorf("permission revoked")
	}
	select {
	case s.workers <- struct{}{}:
	default:
		return fmt.Errorf("operation worker capacity reached; try approval again later")
	}
	if err := s.store.Audit(owner, op.Kind, op.ServerID, "approved", op.ID); err != nil {
		<-s.workers
		return err
	}
	op.Status = "running"
	op.UpdatedAt = time.Now()
	if err := s.store.Save(op); err != nil {
		<-s.workers
		return err
	}
	delete(s.pending, id)
	s.launch(op, pending.work)
	return nil
}
func (s *Operations) Get(id string) (models.Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	return s.store.Get(id)
}
func (s *Operations) List() ([]models.Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	return s.store.List()
}
func (s *Operations) Audit(actor, tool, server, outcome, id string) error {
	return s.store.Audit(actor, tool, server, outcome, id)
}

func (s *Operations) expireLocked() {
	for id := range s.pending {
		op, err := s.store.Get(id)
		if err == nil && time.Since(op.CreatedAt) > 15*time.Minute {
			op.Status = "expired"
			op.Error = "Human approval expired"
			op.UpdatedAt = time.Now()
			if s.store.Save(op) == nil {
				delete(s.pending, id)
			}
		}
	}
}

func (s *Operations) Reject(id, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	op, err := s.store.Get(id)
	if err != nil {
		return err
	}
	if op.Owner != owner || op.Status != "awaiting_approval" {
		return fmt.Errorf("operation is not awaiting this user's approval")
	}
	if err := s.store.Audit(owner, op.Kind, op.ServerID, "rejected", op.ID); err != nil {
		return err
	}
	op.Status = "rejected"
	op.Error = "Rejected by the dashboard user"
	op.UpdatedAt = time.Now()
	if err := s.store.Save(op); err != nil {
		return err
	}
	delete(s.pending, id)
	return nil
}

func (s *Operations) AuditIdentity(owner, agentID, tool, server, outcome, id string) error {
	return s.store.AuditIdentity(owner, agentID, tool, server, outcome, id)
}
