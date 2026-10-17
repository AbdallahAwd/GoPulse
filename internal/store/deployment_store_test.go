package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
)

func TestDeploymentStorePersistsAndDeduplicatesDeliveries(t *testing.T) {
	dir := t.TempDir()
	projectStore, err := NewProjectStore(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatalf("NewProjectStore: %v", err)
	}
	if err := projectStore.AddProject(models.Project{ID: "project-1", ServerID: "server-1", Name: "web"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	deploymentStore, err := NewDeploymentStore(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatalf("NewDeploymentStore: %v", err)
	}
	queuedAt := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	deployment := models.Deployment{
		ID:         "deployment-1",
		ProjectID:  "project-1",
		DeliveryID: "delivery-1",
		Source:     "github_actions",
		Repository: "acme/web",
		Ref:        "refs/heads/main",
		Branch:     "main",
		CommitSHA:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Status:     models.DeploymentStatusQueued,
		QueuedAt:   queuedAt,
	}
	if err := deploymentStore.CreateDeployment(deployment); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}

	duplicate := deployment
	duplicate.ID = "deployment-2"
	if err := deploymentStore.CreateDeployment(duplicate); err != ErrDeploymentAlreadyExists {
		t.Fatalf("duplicate error = %v, want %v", err, ErrDeploymentAlreadyExists)
	}

	deployment.Status = models.DeploymentStatusSucceeded
	deployment.StartedAt = queuedAt.Add(time.Second)
	deployment.FinishedAt = queuedAt.Add(2 * time.Second)
	if err := deploymentStore.UpdateDeployment(deployment); err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}

	got, err := deploymentStore.GetDeploymentByDeliveryID("delivery-1")
	if err != nil {
		t.Fatalf("GetDeploymentByDeliveryID: %v", err)
	}
	if got.ID != "deployment-1" || got.Status != models.DeploymentStatusSucceeded || !got.FinishedAt.Equal(deployment.FinishedAt) {
		t.Fatalf("stored deployment = %#v", got)
	}

	list, err := deploymentStore.ListProjectDeployments("project-1", 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListProjectDeployments = %#v, err=%v", list, err)
	}
}

func TestDeploymentStoreFailInterrupted(t *testing.T) {
	dir := t.TempDir()
	projectStore, err := NewProjectStore(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatalf("NewProjectStore: %v", err)
	}
	if err := projectStore.AddProject(models.Project{ID: "project-1", ServerID: "server-1", Name: "web"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	deploymentStore, err := NewDeploymentStore(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatalf("NewDeploymentStore: %v", err)
	}
	for i, status := range []models.DeploymentStatus{models.DeploymentStatusQueued, models.DeploymentStatusRunning, models.DeploymentStatusSucceeded} {
		if err := deploymentStore.CreateDeployment(models.Deployment{
			ID:         "deployment-" + string(rune('1'+i)),
			ProjectID:  "project-1",
			DeliveryID: "delivery-" + string(rune('1'+i)),
			Status:     status,
			QueuedAt:   time.Now().UTC(),
		}); err != nil {
			t.Fatalf("CreateDeployment(%s): %v", status, err)
		}
	}
	if err := deploymentStore.FailInterrupted("restart"); err != nil {
		t.Fatalf("FailInterrupted: %v", err)
	}
	queued, _ := deploymentStore.GetDeployment("deployment-1")
	running, _ := deploymentStore.GetDeployment("deployment-2")
	succeeded, _ := deploymentStore.GetDeployment("deployment-3")
	if queued.Status != models.DeploymentStatusFailed || running.Status != models.DeploymentStatusFailed {
		t.Fatalf("interrupted statuses = %q, %q", queued.Status, running.Status)
	}
	if succeeded.Status != models.DeploymentStatusSucceeded {
		t.Fatalf("succeeded deployment was changed to %q", succeeded.Status)
	}
}

func TestDeploymentStoreRecoverInterruptedResumesQueuedWork(t *testing.T) {
	dir := t.TempDir()
	projectStore, err := NewProjectStore(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatalf("NewProjectStore: %v", err)
	}
	if err := projectStore.AddProject(models.Project{ID: "project-1", ServerID: "server-1", Name: "web"}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	deploymentStore, err := NewDeploymentStore(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatalf("NewDeploymentStore: %v", err)
	}
	for i, status := range []models.DeploymentStatus{models.DeploymentStatusQueued, models.DeploymentStatusRunning} {
		id := "deployment-recover-" + string(rune('1'+i))
		if err := deploymentStore.CreateDeployment(models.Deployment{
			ID:         id,
			ProjectID:  "project-1",
			DeliveryID: "delivery-recover-" + string(rune('1'+i)),
			Status:     status,
			QueuedAt:   time.Now().UTC(),
		}); err != nil {
			t.Fatalf("CreateDeployment(%s): %v", status, err)
		}
	}
	queued, err := deploymentStore.RecoverInterrupted("restart while running")
	if err != nil {
		t.Fatalf("RecoverInterrupted: %v", err)
	}
	if len(queued) != 1 || queued[0].ID != "deployment-recover-1" {
		t.Fatalf("queued recovery = %#v", queued)
	}
	running, err := deploymentStore.GetDeployment("deployment-recover-2")
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if running.Status != models.DeploymentStatusFailed || running.Error != "restart while running" {
		t.Fatalf("running recovery = %#v", running)
	}
	queuedAgain, err := deploymentStore.GetDeployment("deployment-recover-1")
	if err != nil {
		t.Fatalf("GetDeployment queued: %v", err)
	}
	if queuedAgain.Status != models.DeploymentStatusQueued {
		t.Fatalf("queued deployment status = %q", queuedAgain.Status)
	}
}
