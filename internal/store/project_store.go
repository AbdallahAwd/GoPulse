package store

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/amaa/gopulse/internal/models"
)

// ProjectStore handles storage and retrieval of project configurations
type ProjectStore struct {
	filePath string
	mu       sync.RWMutex
	projects []models.Project
}

// NewProjectStore creates a new ProjectStore
func NewProjectStore(filePath string) (*ProjectStore, error) {
	store := &ProjectStore{
		filePath: filePath,
		projects: []models.Project{},
	}

	// Load existing projects if file exists
	if _, err := os.Stat(filePath); err == nil {
		if err := store.load(); err != nil {
			return nil, fmt.Errorf("failed to load projects: %w", err)
		}
	} else {
		// Create file if it doesn't exist to ensure we have write permissions/path exists
		// Actually save() handles that, but good to check.
		// We'll rely on save() creating it.
	}

	return store, nil
}

// load reads projects from the JSON file
func (s *ProjectStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &s.projects)
}

// save writes projects to the JSON file
func (s *ProjectStore) save() error {
	// Note: mu should be locked by caller

	data, err := json.MarshalIndent(s.projects, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, data, 0644)
}

// AddProject adds a new project
func (s *ProjectStore) AddProject(project models.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if ID already exists
	for _, existing := range s.projects {
		if existing.ID == project.ID {
			return fmt.Errorf("project with ID %s already exists", project.ID)
		}
	}

	project.CreatedAt = time.Now()
	s.projects = append(s.projects, project)
	return s.save()
}

// RemoveProject removes a project by ID
func (s *ProjectStore) RemoveProject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, project := range s.projects {
		if project.ID == id {
			// Remove element at index i
			s.projects = append(s.projects[:i], s.projects[i+1:]...)
			return s.save()
		}
	}

	return fmt.Errorf("project not found")
}

// GetProject returns a project by ID
func (s *ProjectStore) GetProject(id string) (models.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, project := range s.projects {
		if project.ID == id {
			return project, nil
		}
	}

	return models.Project{}, fmt.Errorf("project not found")
}

// ListProjects returns all projects
func (s *ProjectStore) ListProjects() []models.Project {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy to avoid race conditions if caller modifies the slice
	result := make([]models.Project, len(s.projects))
	copy(result, s.projects)
	return result
}

// UpdateProject updates an existing project
func (s *ProjectStore) UpdateProject(project models.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, existing := range s.projects {
		if existing.ID == project.ID {
			// Preserve CreatedAt if not set in update (though usually update replaces all)
			if project.CreatedAt.IsZero() {
				project.CreatedAt = existing.CreatedAt
			}
			s.projects[i] = project
			return s.save()
		}
	}

	return fmt.Errorf("project not found")
}
