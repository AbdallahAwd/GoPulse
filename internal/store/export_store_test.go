package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amaa/gopulse/internal/models"
)

func TestExportAndImport(t *testing.T) {
	// Setup Source Stores in a temp directory
	srcDir := t.TempDir()
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	srcServers, err := NewServerStore(filepath.Join(srcDir, "servers.json"))
	if err != nil {
		t.Fatalf("Failed to create srcServerStore: %v", err)
	}
	srcUsers, err := NewUserStore(filepath.Join(srcDir, "users.json"))
	if err != nil {
		t.Fatalf("Failed to create srcUserStore: %v", err)
	}
	srcProjects, err := NewProjectStore(filepath.Join(srcDir, "projects.json"))
	if err != nil {
		t.Fatalf("Failed to create srcProjectStore: %v", err)
	}
	srcDomains, err := NewDomainStore(filepath.Join(srcDir, "domains.json"))
	if err != nil {
		t.Fatalf("Failed to create srcDomainStore: %v", err)
	}
	srcDatabases, err := NewDatabaseStore(filepath.Join(srcDir, "databases.json"))
	if err != nil {
		t.Fatalf("Failed to create srcDatabaseStore: %v", err)
	}
	srcLoadBalancers, err := NewLoadBalancerStore(filepath.Join(srcDir, "load_balancers.json"))
	if err != nil {
		t.Fatalf("Failed to create srcLoadBalancerStore: %v", err)
	}

	// 1. Setup a password-auth server
	server1 := models.Server{
		ID:       "srv-pass",
		Name:     "Pass Auth VPS",
		Host:     "1.2.3.4",
		Port:     22,
		User:     "root",
		AuthType: "password",
		Password: "supersecretpassword",
		Tags:     []string{"tag1"},
		IsDbOnly: false,
	}
	if err := srcServers.AddServer(server1); err != nil {
		t.Fatalf("AddServer server1 failed: %v", err)
	}

	// 2. Setup a key-auth server with a local key file
	mockKeyContent := "-----BEGIN OPENSSH PRIVATE KEY-----\ntest-key\n-----END OPENSSH PRIVATE KEY-----"
	mockKeyPath := filepath.Join(srcDir, "test_key.pem")
	if err := os.WriteFile(mockKeyPath, []byte(mockKeyContent), 0600); err != nil {
		t.Fatalf("Failed to write mock private key file: %v", err)
	}

	server2 := models.Server{
		ID:       "srv-key",
		Name:     "Key Auth VPS",
		Host:     "5.6.7.8",
		Port:     22,
		User:     "ubuntu",
		AuthType: "key",
		KeyPath:  mockKeyPath,
		Tags:     []string{"tag2"},
		IsDbOnly: false,
	}
	if err := srcServers.AddServer(server2); err != nil {
		t.Fatalf("AddServer server2 failed: %v", err)
	}

	user := models.User{
		ID:             "usr-test",
		Username:       "tester",
		Password:       "bcrypt-hash-goes-here",
		Role:           "viewer",
		Permissions:    []string{"read"},
		AllowedServers: []string{"srv-pass", "srv-key"},
		CreatedAt:      now,
	}
	if err := srcUsers.AddUserRaw(user); err != nil {
		t.Fatalf("AddUserRaw failed: %v", err)
	}

	project := models.Project{
		ID:        "proj-test",
		ServerID:  "srv-pass",
		Name:      "Test App",
		Type:      "go",
		CreatedAt: now,
	}
	if err := srcProjects.AddProject(project); err != nil {
		t.Fatalf("AddProject failed: %v", err)
	}

	domain := models.Domain{
		ID:        "dom-test",
		ServerID:  "srv-pass",
		Domain:    "app.test",
		Port:      80,
		SSL:       false,
		CreatedAt: now,
	}
	if err := srcDomains.AddDomain(domain); err != nil {
		t.Fatalf("AddDomain failed: %v", err)
	}

	database := models.Database{
		ID:            "db-test",
		ServerID:      "srv-pass",
		Name:          "Test DB",
		Type:          "postgres",
		DatabaseName:  "test_db",
		Username:      "postgres",
		Password:      "db-pass",
		CreatedAt:     now,
		AutoBackup:    true,
		RetentionDays: 7,
	}
	if err := srcDatabases.AddDatabase(database); err != nil {
		t.Fatalf("AddDatabase failed: %v", err)
	}

	lb := models.LoadBalancer{
		ID:                  "lb-test",
		ServerID:            "srv-pass",
		Name:                "Test LB",
		Domain:              "lb.test",
		Port:                80,
		Algorithm:           "round-robin",
		SSL:                 true,
		HealthCheckPath:     "/health",
		HealthCheckInterval: 10,
		Status:              "active",
		CreatedAt:           now,
	}
	if err := srcLoadBalancers.AddLoadBalancer(lb); err != nil {
		t.Fatalf("AddLoadBalancer failed: %v", err)
	}
	backend := models.LoadBalancerBackend{
		ID:             "b-test",
		LoadBalancerID: "lb-test",
		Host:           "127.0.0.1",
		Port:           8080,
		Weight:         1,
		Backup:         false,
		Status:         "healthy",
		CreatedAt:      now,
	}
	if err := srcLoadBalancers.AddBackend(backend); err != nil {
		t.Fatalf("AddBackend failed: %v", err)
	}

	// 1. Run Export
	srcExportStore := NewExportStore(srcServers, srcProjects, srcDatabases, srcDomains, srcUsers, srcLoadBalancers)
	exportData, err := srcExportStore.ExportAll()
	if err != nil {
		t.Fatalf("ExportAll failed: %v", err)
	}

	// Verify Export Package
	if exportData.Version != "1.0" {
		t.Errorf("Expected version 1.0, got %s", exportData.Version)
	}
	if len(exportData.Servers) != 2 {
		t.Errorf("Expected 2 exported servers, got %d", len(exportData.Servers))
	}

	// Find servers in export data
	var expSrvPass, expSrvKey models.Server
	for _, s := range exportData.Servers {
		if s.ID == "srv-pass" {
			expSrvPass = s
		} else if s.ID == "srv-key" {
			expSrvKey = s
		}
	}

	if expSrvPass.ID == "" || expSrvKey.ID == "" {
		t.Fatalf("Could not locate exported servers")
	}

	if expSrvPass.SSHKeyContent != "" {
		t.Errorf("Password-auth server should not have SSHKeyContent")
	}
	if expSrvKey.SSHKeyContent != mockKeyContent {
		t.Errorf("Key-auth server missing or mismatching SSHKeyContent: %q", expSrvKey.SSHKeyContent)
	}

	// Setup Destination Stores in a second temp directory
	destDir := t.TempDir()
	destServers, err := NewServerStore(filepath.Join(destDir, "servers.json"))
	if err != nil {
		t.Fatalf("Failed to create destServerStore: %v", err)
	}
	destUsers, err := NewUserStore(filepath.Join(destDir, "users.json"))
	if err != nil {
		t.Fatalf("Failed to create destUserStore: %v", err)
	}
	destProjects, err := NewProjectStore(filepath.Join(destDir, "projects.json"))
	if err != nil {
		t.Fatalf("Failed to create destProjectStore: %v", err)
	}
	destDomains, err := NewDomainStore(filepath.Join(destDir, "domains.json"))
	if err != nil {
		t.Fatalf("Failed to create destDomainStore: %v", err)
	}
	destDatabases, err := NewDatabaseStore(filepath.Join(destDir, "databases.json"))
	if err != nil {
		t.Fatalf("Failed to create destDatabaseStore: %v", err)
	}
	destLoadBalancers, err := NewLoadBalancerStore(filepath.Join(destDir, "load_balancers.json"))
	if err != nil {
		t.Fatalf("Failed to create destLoadBalancerStore: %v", err)
	}

	destExportStore := NewExportStore(destServers, destProjects, destDatabases, destDomains, destUsers, destLoadBalancers)

	// Set HOME environment variable to destDir during import, so local key directory is created there
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)
	os.Setenv("HOME", destDir)

	// 2. Import into empty dest stores (should perform "Create")
	report, err := destExportStore.ImportAll(exportData, "skip")
	if err != nil {
		t.Fatalf("ImportAll (skip/create) failed: %v", err)
	}

	if report.ServersCreated != 2 || report.UsersCreated != 1 || report.ProjectsCreated != 1 || report.DomainsCreated != 1 || report.DatabasesCreated != 1 || report.LoadBalancersCreated != 1 {
		t.Errorf("Expected 2 servers and all others 1 created, got report: %+v", report)
	}
	if len(report.Errors) > 0 {
		t.Errorf("Unexpected import errors: %v", report.Errors)
	}

	// Verify imported key-auth server key path and key content
	impSrvKey, err := destServers.GetServer("srv-key")
	if err != nil {
		t.Fatalf("Failed to retrieve imported key server: %v", err)
	}

	if impSrvKey.KeyPath == mockKeyPath {
		t.Errorf("Imported key path was not rewritten, still points to old path: %s", impSrvKey.KeyPath)
	}
	if !strings.Contains(impSrvKey.KeyPath, filepath.Join(destDir, ".gopulse", "keys")) {
		t.Errorf("Imported key path was not written to local home folder: %s", impSrvKey.KeyPath)
	}

	importedKeyBytes, err := os.ReadFile(impSrvKey.KeyPath)
	if err != nil {
		t.Fatalf("Failed to read newly written imported key file: %v", err)
	}
	if string(importedKeyBytes) != mockKeyContent {
		t.Errorf("Imported key content mismatch")
	}

	// 3. Import same data again with "skip" mode (should perform "Skip")
	report, err = destExportStore.ImportAll(exportData, "skip")
	if err != nil {
		t.Fatalf("ImportAll (skip/duplicate) failed: %v", err)
	}

	if report.ServersSkipped != 2 || report.UsersSkipped != 1 || report.ProjectsSkipped != 1 || report.DomainsSkipped != 1 || report.DatabasesSkipped != 1 || report.LoadBalancersSkipped != 1 {
		t.Errorf("Expected all skipped, got report: %+v", report)
	}

	// 4. Modify some data in export envelope and import with "overwrite" mode (should perform "Update")
	exportData.Servers[0].Name = "Updated Test VPS"
	exportData.Users[0].Role = "admin"

	// Also modify the private key content to check overwrite
	newKeyContent := "-----BEGIN OVERSSH PRIVATE KEY-----\nupdated-key\n-----END OPENSSH PRIVATE KEY-----"
	for i, s := range exportData.Servers {
		if s.ID == "srv-key" {
			exportData.Servers[i].SSHKeyContent = newKeyContent
		}
	}

	report, err = destExportStore.ImportAll(exportData, "overwrite")
	if err != nil {
		t.Fatalf("ImportAll (overwrite) failed: %v", err)
	}

	if report.ServersUpdated != 2 || report.UsersUpdated != 1 || report.ProjectsUpdated != 1 || report.DomainsUpdated != 1 || report.DatabasesUpdated != 1 || report.LoadBalancersUpdated != 1 {
		t.Errorf("Expected updated, got report: %+v", report)
	}

	// Verify imported key server updated its key file
	impSrvKey2, _ := destServers.GetServer("srv-key")
	importedKeyBytes2, _ := os.ReadFile(impSrvKey2.KeyPath)
	if string(importedKeyBytes2) != newKeyContent {
		t.Errorf("Overwritten key content mismatch: expected %q, got %q", newKeyContent, string(importedKeyBytes2))
	}
}
