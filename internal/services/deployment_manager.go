package services

import (
	"context"
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

const deploymentTimeout = 30 * time.Minute

// DeploymentManager runs durable, deduplicated deployments outside the webhook
// request lifecycle. A per-project lock prevents concurrent pushes from racing
// over the same working tree and runtime.
type DeploymentManager struct {
	projectService *ProjectService
	store          *store.DeploymentStore
	ctx            context.Context
	cancel         context.CancelFunc
	projectLocks   sync.Map
	stateMu        sync.RWMutex
	closed         bool
}

var ErrDeploymentManagerClosed = errors.New("deployment manager is closed")

func NewDeploymentManager(projectService *ProjectService, deploymentStore *store.DeploymentStore) *DeploymentManager {
	ctx, cancel := context.WithCancel(context.Background())
	manager := &DeploymentManager{
		projectService: projectService,
		store:          deploymentStore,
		ctx:            ctx,
		cancel:         cancel,
	}
	queued, err := deploymentStore.RecoverInterrupted("GoPulse restarted while the deployment was running")
	if err != nil {
		log.Printf("Failed to recover interrupted deployments: %v", err)
	} else {
		for _, deployment := range queued {
			go manager.run(deployment.ID)
		}
	}
	return manager
}

func (m *DeploymentManager) Close() {
	m.stateMu.Lock()
	if m.closed {
		m.stateMu.Unlock()
		return
	}
	m.closed = true
	m.stateMu.Unlock()
	m.cancel()
}

// Enqueue persists a deployment before starting it. The returned boolean is
// false when the delivery was already accepted, making webhook retries safe.
func (m *DeploymentManager) Enqueue(deployment models.Deployment) (models.Deployment, bool, error) {
	m.stateMu.RLock()
	closed := m.closed
	m.stateMu.RUnlock()
	if closed {
		return models.Deployment{}, false, ErrDeploymentManagerClosed
	}
	if strings.TrimSpace(deployment.ProjectID) == "" {
		return models.Deployment{}, false, fmt.Errorf("project ID is required")
	}
	if strings.TrimSpace(deployment.DeliveryID) == "" {
		deployment.DeliveryID = uuid.NewString()
	}
	if deployment.ID == "" {
		deployment.ID = uuid.NewString()
	}
	if deployment.Source == "" {
		deployment.Source = "manual"
	}
	deployment.Status = models.DeploymentStatusQueued
	deployment.QueuedAt = time.Now().UTC()
	deployment.StartedAt = time.Time{}
	deployment.FinishedAt = time.Time{}
	deployment.Error = ""

	if _, err := m.projectService.GetProject(context.Background(), deployment.ProjectID); err != nil {
		return models.Deployment{}, false, err
	}

	if err := m.store.CreateDeployment(deployment); err != nil {
		if errors.Is(err, store.ErrDeploymentAlreadyExists) {
			existing, getErr := m.store.GetDeploymentByDeliveryID(deployment.DeliveryID)
			return existing, false, getErr
		}
		return models.Deployment{}, false, err
	}

	go m.run(deployment.ID)
	return deployment, true, nil
}

func (m *DeploymentManager) run(deploymentID string) {
	deployment, err := m.store.GetDeployment(deploymentID)
	if err != nil {
		log.Printf("Failed to load queued deployment %s: %v", deploymentID, err)
		return
	}

	lockValue, _ := m.projectLocks.LoadOrStore(deployment.ProjectID, &sync.Mutex{})
	projectLock := lockValue.(*sync.Mutex)
	projectLock.Lock()
	defer projectLock.Unlock()

	select {
	case <-m.ctx.Done():
		m.fail(deployment, "GoPulse stopped before the deployment started")
		return
	default:
	}

	deployment.Status = models.DeploymentStatusRunning
	deployment.StartedAt = time.Now().UTC()
	if err := m.store.UpdateDeployment(deployment); err != nil {
		log.Printf("Failed to mark deployment %s running: %v", deployment.ID, err)
		return
	}

	ctx, cancel := context.WithTimeout(m.ctx, deploymentTimeout)
	defer cancel()
	err = m.projectService.RedeployProjectRevision(ctx, deployment.ProjectID, deployment.Branch, deployment.CommitSHA)
	deployment.FinishedAt = time.Now().UTC()
	if err != nil {
		deployment.Status = models.DeploymentStatusFailed
		deployment.Error = err.Error()
		log.Printf("Deployment failed: deployment=%s project=%s commit=%s error=%v",
			deployment.ID, deployment.ProjectID, deployment.CommitSHA, err)
	} else {
		deployment.Status = models.DeploymentStatusSucceeded
		log.Printf("Deployment succeeded: deployment=%s project=%s commit=%s",
			deployment.ID, deployment.ProjectID, deployment.CommitSHA)
	}
	if updateErr := m.store.UpdateDeployment(deployment); updateErr != nil {
		log.Printf("Failed to finish deployment %s: %v", deployment.ID, updateErr)
	}
}

func (m *DeploymentManager) fail(deployment models.Deployment, reason string) {
	deployment.Status = models.DeploymentStatusFailed
	deployment.FinishedAt = time.Now().UTC()
	deployment.Error = reason
	if err := m.store.UpdateDeployment(deployment); err != nil {
		log.Printf("Failed to mark deployment %s failed: %v", deployment.ID, err)
	}
}

func (m *DeploymentManager) GetDeployment(id string) (models.Deployment, error) {
	return m.store.GetDeployment(id)
}

func (m *DeploymentManager) ListProjectDeployments(projectID string, limit int) ([]models.Deployment, error) {
	return m.store.ListProjectDeployments(projectID, limit)
}
