package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AbdallahAwd/GoPulse/internal/middleware"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/services"
	"github.com/go-chi/chi/v5"
)

// DatabaseHandler handles database management endpoints
type DatabaseHandler struct {
	service *services.DatabaseService
	manager *services.ServiceManager
}

// NewDatabaseHandler creates a new DatabaseHandler
func NewDatabaseHandler(service *services.DatabaseService, manager *services.ServiceManager) *DatabaseHandler {
	return &DatabaseHandler{
		service: service,
		manager: manager,
	}
}

// ListDatabases handles GET /api/servers/{serverID}/databases
func (h *DatabaseHandler) ListDatabases(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	dbs, err := h.service.ListDatabases(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list databases", err)
		return
	}

	// Inventory reads must stay local and work even when a remote host is down.
	// Live verification is an explicit per-database action.
	for i := range dbs {
		dbs[i].Password = ""
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    dbs,
	})
}

// ProvisionDatabase handles POST /api/servers/{serverID}/databases
func (h *DatabaseHandler) ProvisionDatabase(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	var req struct {
		Name         string `json:"name"`
		Type         string `json:"type"`          // "mysql", "postgres", "mongodb"
		DatabaseName string `json:"database_name"` // Initial default DB name
		Username     string `json:"username"`
		Password     string `json:"password"`
		HostPort     int    `json:"host_port"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	// SQLite and PocketBase have relaxed credentials requirements
	if req.Type == "sqlite" || req.Type == "pocketbase" {
		if req.Name == "" {
			respondError(w, http.StatusBadRequest, "Database display name is required", nil)
			return
		}
		if req.Type == "pocketbase" && req.HostPort <= 0 {
			respondError(w, http.StatusBadRequest, "Host port is required for PocketBase", nil)
			return
		}
		if req.DatabaseName == "" {
			req.DatabaseName = "pocketbase"
		}
		if req.Username == "" {
			req.Username = "admin"
		}
		if req.Password == "" {
			req.Password = "none"
		}
		if req.Type == "sqlite" {
			req.HostPort = 0
		}
	} else {
		if req.Name == "" || req.Type == "" || req.DatabaseName == "" || req.Username == "" || req.Password == "" || req.HostPort <= 0 {
			respondError(w, http.StatusBadRequest, "All provisioning parameters are required", nil)
			return
		}
	}

	db, err := h.service.ProvisionDatabase(r.Context(), serverID, req.Name, req.Type, req.DatabaseName, req.Username, req.Password, req.HostPort)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to provision database container", err)
		return
	}

	db.Password = ""
	respondJSON(w, http.StatusCreated, models.APIResponse{
		Success: true,
		Message: "Database container provisioning successfully started",
		Data:    db,
	})
}

// GetDatabase handles GET /api/servers/{serverID}/databases/{dbID}
func (h *DatabaseHandler) GetDatabase(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")

	db, err := h.service.GetDatabase(dbID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Database not found", err)
		return
	}

	if r.URL.Query().Get("refresh") == "true" {
		status, err := h.service.UpdateStatus(r.Context(), db.ID)
		if err != nil {
			respondError(w, http.StatusBadGateway, "Unable to verify database health", sanitizeDatabaseError(err, db.Password))
			return
		}
		db.Status = status
	}
	db.Password = ""

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    db,
	})
}

// StartDatabase handles POST /api/servers/{serverID}/databases/{dbID}/start
func (h *DatabaseHandler) StartDatabase(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")

	if err := h.service.StartDatabase(r.Context(), dbID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to start database container", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Database boot command successfully executed",
	})
}

// StopDatabase handles POST /api/servers/{serverID}/databases/{dbID}/stop
func (h *DatabaseHandler) StopDatabase(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")

	if err := h.service.StopDatabase(r.Context(), dbID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to stop database container", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Database halt command successfully executed",
	})
}

// RestartDatabase handles POST /api/servers/{serverID}/databases/{dbID}/restart
func (h *DatabaseHandler) RestartDatabase(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")

	if err := h.service.RestartDatabase(r.Context(), dbID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to restart database container", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Database restart command successfully executed",
	})
}

// DeleteDatabase handles DELETE /api/servers/{serverID}/databases/{dbID}
func (h *DatabaseHandler) DeleteDatabase(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")
	if r.URL.Query().Get("confirm") != dbID {
		respondError(w, http.StatusBadRequest, "Confirm removal by supplying confirm=<database ID>", nil)
		return
	}
	deleteVolumeStr := r.URL.Query().Get("delete_volume")
	deleteVolume, _ := strconv.ParseBool(deleteVolumeStr)

	if err := h.service.DeleteDatabase(r.Context(), dbID, deleteVolume); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to delete database record and container", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Database container and metadata successfully removed",
	})
}

// ExecuteQuery handles POST /api/servers/{serverID}/databases/{dbID}/query
func (h *DatabaseHandler) ExecuteQuery(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")

	var req struct {
		Query  string `json:"query"`
		Schema string `json:"schema"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if req.Query == "" {
		respondError(w, http.StatusBadRequest, "Query is required", nil)
		return
	}

	result, err := h.service.ExecuteQuery(r.Context(), dbID, req.Schema, req.Query)
	if err != nil {
		respondJSON(w, http.StatusOK, models.APIResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    result,
	})
}

// CreateBackup handles POST /api/servers/{serverID}/databases/{dbID}/backup
func (h *DatabaseHandler) CreateBackup(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")

	backup, err := h.service.CreateBackup(r.Context(), dbID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to create database backup", err)
		return
	}

	respondJSON(w, http.StatusCreated, models.APIResponse{
		Success: true,
		Message: "Database backup archive created successfully",
		Data:    backup,
	})
}

// ListBackups handles GET /api/servers/{serverID}/databases/{dbID}/backups
func (h *DatabaseHandler) ListBackups(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")

	backups, err := h.service.ListBackups(r.Context(), dbID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list database backups", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    backups,
	})
}

// RestoreBackup handles POST /api/servers/{serverID}/databases/{dbID}/backups/restore
func (h *DatabaseHandler) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")

	var req struct {
		Filename string `json:"filename"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if req.Filename == "" {
		respondError(w, http.StatusBadRequest, "Filename is required", nil)
		return
	}

	if err := h.service.RestoreBackup(r.Context(), dbID, req.Filename); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to restore database from backup", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Database snapshot successfully restored",
	})
}

// ImportBackup handles POST /api/servers/{serverID}/databases/{dbID}/backups/import
func (h *DatabaseHandler) ImportBackup(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	dbID := chi.URLParam(r, "dbID")

	fileService, err := h.manager.GetFileProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get file service", err)
		return
	}
	defer fileService.Close()

	db, err := h.service.GetDatabase(dbID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Database not found", err)
		return
	}

	backupDir := fmt.Sprintf("/var/lib/gopulse/backups/%s", db.ID)
	if err := fileService.Mkdir(r.Context(), backupDir); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to create backups directory", err)
		return
	}

	reader, err := r.MultipartReader()
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid multipart request", err)
		return
	}

	var filename string
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			respondError(w, http.StatusBadRequest, "Failed to read multipart", err)
			return
		}

		if part.FormName() == "file" {
			filename = part.FileName()
			if filename == "" {
				continue
			}

			destPath := filepath.Join(backupDir, filename)
			if err := fileService.WriteFile(r.Context(), destPath, part, 0644); err != nil {
				respondError(w, http.StatusInternalServerError, "Failed to upload backup file", err)
				return
			}
			break
		}
	}

	if filename == "" {
		respondError(w, http.StatusBadRequest, "No file found in request", nil)
		return
	}

	// Trigger restore/import of the newly uploaded file!
	if err := h.service.RestoreBackup(r.Context(), dbID, filename); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to import database from file", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Database backup imported and successfully restored",
	})
}

// DownloadBackup handles GET /api/servers/{serverID}/databases/{dbID}/backups/download
func (h *DatabaseHandler) DownloadBackup(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	dbID := chi.URLParam(r, "dbID")
	filename := r.URL.Query().Get("filename")

	if filename == "" {
		respondError(w, http.StatusBadRequest, "Filename is required", nil)
		return
	}

	// Security: Sanitize filename to prevent directory traversal
	filename = filepath.Base(filename)

	backupPath := fmt.Sprintf("/var/lib/gopulse/backups/%s/%s", dbID, filename)

	service, err := h.manager.GetFileProvider(serverID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to connect to host file provider", err)
		return
	}
	defer service.Close()

	reader, err := service.ReadFile(r.Context(), backupPath)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to read remote backup file", err)
		return
	}
	defer reader.Close()

	// Stream downloaded compressed dump file directly to browser client
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Type", "application/octet-stream")

	if _, err := io.Copy(w, reader); err != nil {
		return
	}
}

// GetAutoBackupConfig handles GET /api/servers/{serverID}/databases/{dbID}/auto-backup
func (h *DatabaseHandler) GetAutoBackupConfig(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")

	db, err := h.service.GetDatabase(dbID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Database not found", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"auto_backup":    db.AutoBackup,
			"retention_days": db.RetentionDays,
		},
	})
}

// SetAutoBackupConfig handles PUT /api/servers/{serverID}/databases/{dbID}/auto-backup
func (h *DatabaseHandler) SetAutoBackupConfig(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")

	var req struct {
		AutoBackup    bool `json:"auto_backup"`
		RetentionDays int  `json:"retention_days"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if err := h.service.SetAutoBackup(dbID, req.AutoBackup, req.RetentionDays); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update auto backup configuration", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Automatic backup configuration updated successfully",
	})
}

// =============================================================================
// Explorer Handlers
// =============================================================================

// ListSchemas handles GET /api/servers/{serverID}/databases/{dbID}/schemas
func (h *DatabaseHandler) ListSchemas(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")

	schemas, err := h.service.ListSchemas(r.Context(), dbID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list database schemas", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    schemas,
	})
}

// ListTables handles GET /api/servers/{serverID}/databases/{dbID}/schemas/{schema}/tables
func (h *DatabaseHandler) ListTables(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")
	schema := chi.URLParam(r, "schema")

	tables, err := h.service.ListTables(r.Context(), dbID, schema)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list tables", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    tables,
	})
}

// DescribeTable handles GET /api/servers/{serverID}/databases/{dbID}/schemas/{schema}/tables/{table}/columns
func (h *DatabaseHandler) DescribeTable(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")
	schema := chi.URLParam(r, "schema")
	table := chi.URLParam(r, "table")

	columns, err := h.service.DescribeTable(r.Context(), dbID, schema, table)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to describe table", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    columns,
	})
}

// BrowseTableData handles GET /api/servers/{serverID}/databases/{dbID}/schemas/{schema}/tables/{table}/rows
func (h *DatabaseHandler) BrowseTableData(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")
	schema := chi.URLParam(r, "schema")
	table := chi.URLParam(r, "table")

	pageStr := r.URL.Query().Get("page")
	limitStr := r.URL.Query().Get("limit")

	page, _ := strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 50
	}

	result, err := h.service.BrowseTableData(r.Context(), dbID, schema, table, page, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to browse table data", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    result,
	})
}

// CreateSchema handles POST /api/servers/{serverID}/databases/{dbID}/schemas
func (h *DatabaseHandler) CreateSchema(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")

	var req struct {
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "Database/Schema name is required", nil)
		return
	}

	if err := h.service.CreateSchema(r.Context(), dbID, req.Name); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to create database/schema", err)
		return
	}

	respondJSON(w, http.StatusCreated, models.APIResponse{
		Success: true,
		Message: "Database/Schema created successfully",
	})
}

// DeleteSchema handles DELETE /api/servers/{serverID}/databases/{dbID}/schemas/{schema}
func (h *DatabaseHandler) DeleteSchema(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")
	schema := chi.URLParam(r, "schema")

	if schema == "" {
		respondError(w, http.StatusBadRequest, "Database/Schema name is required", nil)
		return
	}

	if err := h.service.DeleteSchema(r.Context(), dbID, schema); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to delete database/schema", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Database/Schema dropped successfully",
	})
}

// CreateTable handles POST /api/servers/{serverID}/databases/{dbID}/schemas/{schema}/tables
func (h *DatabaseHandler) CreateTable(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")
	schema := chi.URLParam(r, "schema")

	var req struct {
		Name    string               `json:"name"`
		Columns []models.TableColumn `json:"columns"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "Table/Collection name is required", nil)
		return
	}

	if err := h.service.CreateTable(r.Context(), dbID, schema, req.Name, req.Columns); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to create table/collection", err)
		return
	}

	respondJSON(w, http.StatusCreated, models.APIResponse{
		Success: true,
		Message: "Table/Collection created successfully",
	})
}

// DeleteTable handles DELETE /api/servers/{serverID}/databases/{dbID}/schemas/{schema}/tables/{table}
func (h *DatabaseHandler) DeleteTable(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")
	schema := chi.URLParam(r, "schema")
	table := chi.URLParam(r, "table")

	if table == "" {
		respondError(w, http.StatusBadRequest, "Table/Collection name is required", nil)
		return
	}

	if err := h.service.DeleteTable(r.Context(), dbID, schema, table); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to delete table/collection", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Table/Collection dropped successfully",
	})
}

// InsertRow handles POST /api/servers/{serverID}/databases/{dbID}/schemas/{schema}/tables/{table}/rows
func (h *DatabaseHandler) InsertRow(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")
	schema := chi.URLParam(r, "schema")
	table := chi.URLParam(r, "table")

	var req struct {
		Data map[string]interface{} `json:"data"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if err := h.service.InsertRow(r.Context(), dbID, schema, table, req.Data); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to insert row/document", err)
		return
	}

	respondJSON(w, http.StatusCreated, models.APIResponse{
		Success: true,
		Message: "Row/Document inserted successfully",
	})
}

// DeleteRow handles DELETE /api/servers/{serverID}/databases/{dbID}/schemas/{schema}/tables/{table}/rows
func (h *DatabaseHandler) DeleteRow(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")
	schema := chi.URLParam(r, "schema")
	table := chi.URLParam(r, "table")

	pkCol := r.URL.Query().Get("pk_col")
	pkVal := r.URL.Query().Get("pk_val")

	if pkCol == "" || pkVal == "" {
		respondError(w, http.StatusBadRequest, "Primary key column (pk_col) and value (pk_val) are required", nil)
		return
	}

	if err := h.service.DeleteRow(r.Context(), dbID, schema, table, pkCol, pkVal); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to delete row/document", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Row/Document deleted successfully",
	})
}

// UpdateRow handles PUT /api/servers/{serverID}/databases/{dbID}/schemas/{schema}/tables/{table}/rows
func (h *DatabaseHandler) UpdateRow(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")
	schema := chi.URLParam(r, "schema")
	table := chi.URLParam(r, "table")

	pkCol := r.URL.Query().Get("pk_col")
	pkVal := r.URL.Query().Get("pk_val")

	if pkCol == "" || pkVal == "" {
		respondError(w, http.StatusBadRequest, "Primary key column (pk_col) and value (pk_val) are required", nil)
		return
	}

	var req struct {
		Data map[string]interface{} `json:"data"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if err := h.service.UpdateRow(r.Context(), dbID, schema, table, pkCol, pkVal, req.Data); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update row/document", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Row/Document updated successfully",
	})
}

// GetDatabaseTelemetry handles GET /api/servers/{serverID}/databases/{dbID}/telemetry
func (h *DatabaseHandler) GetDatabaseTelemetry(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")

	telemetry, err := h.service.GetDatabaseTelemetry(r.Context(), dbID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to retrieve database telemetry", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    telemetry,
	})
}

// ListDatabaseUsers handles GET /api/servers/{serverID}/databases/{dbID}/users
func (h *DatabaseHandler) ListDatabaseUsers(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")

	users, err := h.service.ListDatabaseUsers(r.Context(), dbID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list database users", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Data:    users,
	})
}

// CreateDatabaseUser handles POST /api/servers/{serverID}/databases/{dbID}/users
func (h *DatabaseHandler) CreateDatabaseUser(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Scope    string `json:"scope"` // "read", "readWrite", "admin"
		Database string `json:"database"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if req.Username == "" || req.Password == "" || req.Scope == "" {
		respondError(w, http.StatusBadRequest, "Username, password, and scope are required", nil)
		return
	}

	if err := h.service.CreateDatabaseUser(r.Context(), dbID, req.Username, req.Password, req.Scope, req.Database); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to create database user", err)
		return
	}

	respondJSON(w, http.StatusCreated, models.APIResponse{
		Success: true,
		Message: "Database user created successfully",
	})
}

// DeleteDatabaseUser handles DELETE /api/servers/{serverID}/databases/{dbID}/users/{username}
func (h *DatabaseHandler) DeleteDatabaseUser(w http.ResponseWriter, r *http.Request) {
	dbID := chi.URLParam(r, "dbID")
	username := chi.URLParam(r, "username")

	if username == "" {
		respondError(w, http.StatusBadRequest, "Username is required", nil)
		return
	}

	if err := h.service.DeleteDatabaseUser(r.Context(), dbID, username); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to delete database user", err)
		return
	}

	respondJSON(w, http.StatusOK, models.APIResponse{
		Success: true,
		Message: "Database user successfully dropped",
	})
}

func sanitizeDatabaseError(err error, password string) error {
	if password == "" {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), password, "[redacted]"))
}

// Credentials are requested explicitly and guarded by database management
// permission plus the parent server/ownership middleware.
func (h *DatabaseHandler) Credentials(w http.ResponseWriter, r *http.Request) {
	if !middleware.HasPermission(r, "database:write") {
		respondError(w, http.StatusForbidden, "Database management permission required to read credentials", nil)
		return
	}
	db, err := h.service.GetDatabase(chi.URLParam(r, "dbID"))
	if err != nil {
		respondError(w, http.StatusNotFound, "Database not found", nil)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if db.Type == "qdrant" {
		key, err := h.service.QdrantAPIKey(r.Context(), db.ID)
		if err != nil {
			respondError(w, http.StatusBadGateway, "Qdrant API key unavailable", err)
			return
		}
		respondJSON(w, http.StatusOK, models.APIResponse{Success: true, Data: map[string]string{"api_key": key}})
		return
	}
	respondJSON(w, http.StatusOK, models.APIResponse{Success: true, Data: map[string]string{"password": db.Password}})
}
