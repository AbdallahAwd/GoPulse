package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amaa/gopulse/internal/models"
)

func writeLegacyJSON(t *testing.T, path string, value interface{}) {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal legacy JSON: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write legacy JSON: %v", err)
	}
}

func TestSQLiteStoresImportLegacyJSONOnce(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 24, 10, 30, 0, 0, time.UTC)

	serversPath := filepath.Join(dir, "servers.json")
	usersPath := filepath.Join(dir, "users.json")
	projectsPath := filepath.Join(dir, "projects.json")
	domainsPath := filepath.Join(dir, "domains.json")
	databasesPath := filepath.Join(dir, "databases.json")

	writeLegacyJSON(t, serversPath, []models.Server{{
		ID:       "srv-1",
		Name:     "Production",
		Host:     "192.0.2.10",
		Port:     22,
		User:     "root",
		AuthType: "key",
		KeyPath:  "/keys/prod.pem",
		Tags:     []string{"web", "prod"},
		IsDbOnly: true,
	}})
	writeLegacyJSON(t, usersPath, []models.User{{
		ID:             "user-1",
		Username:       "admin",
		Password:       "hashed",
		Role:           "admin",
		Permissions:    []string{"*"},
		AllowedServers: []string{"srv-1"},
		CreatedAt:      now,
	}})
	writeLegacyJSON(t, projectsPath, []models.Project{{
		ID:             "project-1",
		ServerID:       "srv-1",
		Name:           "api",
		Type:           "laravel",
		SourceType:     "git",
		SourceURL:      "https://example.com/api.git",
		BuildType:      "process",
		Port:           8080,
		HostPort:       8080,
		Status:         models.ProjectStatusStopped,
		Path:           "/home/root/projects/laravel/api",
		CreatedAt:      now,
		StartCommand:   "php artisan serve --port=8080 --host=0.0.0.0",
		BuildCommand:   "composer install",
		LastBuild:      now,
		WebhookSecret:  "secret",
		WebhookEnabled: true,
		LastDeploy:     now,
		EnvVars:        map[string]string{"APP_ENV": "production"},
		RestartOnBoot:  true,
	}})
	writeLegacyJSON(t, domainsPath, []models.Domain{{
		ID:        "domain-1",
		ServerID:  "srv-1",
		Domain:    "api.example.com",
		Port:      8080,
		SSL:       true,
		CreatedAt: now,
	}})
	writeLegacyJSON(t, databasesPath, []models.Database{{
		ID:            "db-1",
		ServerID:      "srv-1",
		Name:          "main",
		Type:          "mysql",
		DatabaseName:  "app",
		Username:      "app",
		Password:      "secret",
		HostPort:      3306,
		ContainerID:   "container-1",
		Status:        models.DatabaseStatusRunning,
		CreatedAt:     now,
		AutoBackup:    true,
		RetentionDays: 14,
	}})

	serverStore, err := NewServerStore(serversPath)
	if err != nil {
		t.Fatalf("NewServerStore: %v", err)
	}
	userStore, err := NewUserStore(usersPath)
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}
	projectStore, err := NewProjectStore(projectsPath)
	if err != nil {
		t.Fatalf("NewProjectStore: %v", err)
	}
	domainStore, err := NewDomainStore(domainsPath)
	if err != nil {
		t.Fatalf("NewDomainStore: %v", err)
	}
	databaseStore, err := NewDatabaseStore(databasesPath)
	if err != nil {
		t.Fatalf("NewDatabaseStore: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "gopulse.db")); err != nil {
		t.Fatalf("SQLite database was not created: %v", err)
	}

	if got := serverStore.ListServers(); len(got) != 1 || got[0].Tags[1] != "prod" || !got[0].IsDbOnly {
		t.Fatalf("legacy server import mismatch: %#v", got)
	}
	if got := userStore.ListUsers(); len(got) != 1 || got[0].AllowedServers[0] != "srv-1" {
		t.Fatalf("legacy user import mismatch: %#v", got)
	}
	if got := projectStore.ListProjects(); len(got) != 1 || got[0].EnvVars["APP_ENV"] != "production" || !got[0].WebhookEnabled {
		t.Fatalf("legacy project import mismatch: %#v", got)
	}
	if got := domainStore.ListDomainsByServer("srv-1"); len(got) != 1 || !got[0].SSL {
		t.Fatalf("legacy domain import mismatch: %#v", got)
	}
	if got := databaseStore.ListDatabases(); len(got) != 1 || !got[0].AutoBackup || got[0].RetentionDays != 14 {
		t.Fatalf("legacy database import mismatch: %#v", got)
	}

	writeLegacyJSON(t, serversPath, []models.Server{{ID: "srv-late", Name: "late"}})
	serverStoreAgain, err := NewServerStore(serversPath)
	if err != nil {
		t.Fatalf("NewServerStore again: %v", err)
	}
	if got := serverStoreAgain.ListServers(); len(got) != 1 || got[0].ID != "srv-1" {
		t.Fatalf("legacy import should be one-time only, got %#v", got)
	}
}

func TestSQLiteUserStoreBootstrapsFirstUser(t *testing.T) {
	userStore, err := NewUserStore(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}

	err = userStore.AddUser(models.User{
		Username: "owner",
		Password: "hashed",
	})
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	user, err := userStore.GetUser("owner")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.Role != "admin" || len(user.Permissions) != 1 || user.Permissions[0] != "*" {
		t.Fatalf("first user was not bootstrapped as admin: %#v", user)
	}
}
