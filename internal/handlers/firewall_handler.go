package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/services"
	"github.com/amaa/gopulse/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// FirewallHandler handles HTTP routes for VPS firewall management
type FirewallHandler struct {
	service        *services.FirewallService
	serverStore    *store.ServerStore
	serviceManager *services.ServiceManager
}

// NewFirewallHandler creates a new FirewallHandler instance
func NewFirewallHandler(
	service *services.FirewallService,
	serverStore *store.ServerStore,
	serviceManager *services.ServiceManager,
) *FirewallHandler {
	return &FirewallHandler{
		service:        service,
		serverStore:    serverStore,
		serviceManager: serviceManager,
	}
}

// GetFirewall returns status and rules, with optional sync from VPS
func (h *FirewallHandler) GetFirewall(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	refresh := r.URL.Query().Get("refresh") == "true"

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		http.Error(w, "SSH connection error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if refresh {
		if _, err := h.service.SyncFromVPS(serverID, client); err != nil {
			http.Error(w, "Failed to sync rules from VPS: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	state, err := h.service.GetState(serverID, client)
	if err != nil {
		http.Error(w, "Failed to query firewall state: "+err.Error(), http.StatusInternalServerError)
		return
	}

	rules, err := h.service.GetStore().ListRules(serverID)
	if err != nil {
		http.Error(w, "Failed to load rules: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"state": state,
			"rules": rules,
		},
	})
}

// EnableFirewall activates the firewall
func (h *FirewallHandler) EnableFirewall(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	server, err := h.serverStore.GetServer(serverID)
	if err != nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		http.Error(w, "SSH connection error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.service.EnableFirewall(serverID, client, server); err != nil {
		http.Error(w, "Failed to enable firewall: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// DisableFirewall deactivates the firewall
func (h *FirewallHandler) DisableFirewall(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		http.Error(w, "SSH connection error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.service.DisableFirewall(serverID, client); err != nil {
		http.Error(w, "Failed to disable firewall: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// ReloadFirewall reloads firewall rules
func (h *FirewallHandler) ReloadFirewall(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		http.Error(w, "SSH connection error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.service.ReloadFirewall(serverID, client); err != nil {
		http.Error(w, "Failed to reload firewall: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// ResetFirewall resets the firewall rules list
func (h *FirewallHandler) ResetFirewall(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	server, err := h.serverStore.GetServer(serverID)
	if err != nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		http.Error(w, "SSH connection error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.service.ResetFirewall(serverID, client, server); err != nil {
		http.Error(w, "Failed to reset firewall: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// ChangePolicies updates incoming and outgoing policies
func (h *FirewallHandler) ChangePolicies(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	var req struct {
		Incoming string `json:"incoming"`
		Outgoing string `json:"outgoing"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		http.Error(w, "SSH connection error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.service.ChangeDefaultPolicies(serverID, client, req.Incoming, req.Outgoing); err != nil {
		http.Error(w, "Failed to change policies: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// InstallUFW installs UFW firewall
func (h *FirewallHandler) InstallUFW(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	server, err := h.serverStore.GetServer(serverID)
	if err != nil {
		http.Error(w, "Server not found", http.StatusNotFound)
		return
	}

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		http.Error(w, "SSH connection error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.service.InstallUFW(serverID, client, server); err != nil {
		http.Error(w, "Failed to install UFW: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// AddRule inserts a new firewall rule
func (h *FirewallHandler) AddRule(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	var rule models.FirewallRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	rule.ID = uuid.New().String()
	rule.ServerID = serverID
	rule.CreatedAt = time.Now()

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		http.Error(w, "SSH connection error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.service.AddRule(serverID, rule, client); err != nil {
		http.Error(w, "Failed to add rule: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": rule})
}

// UpdateRule edits rule parameters or toggles enabled state
func (h *FirewallHandler) UpdateRule(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	ruleID := chi.URLParam(r, "ruleId")

	oldRule, err := h.service.GetStore().GetRule(ruleID)
	if err != nil {
		http.Error(w, "Rule not found", http.StatusNotFound)
		return
	}

	var newRule models.FirewallRule
	if err := json.NewDecoder(r.Body).Decode(&newRule); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	newRule.ID = oldRule.ID
	newRule.ServerID = serverID

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		http.Error(w, "SSH connection error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// If only Toggle enabled state
	if oldRule.Action == newRule.Action && oldRule.Direction == newRule.Direction &&
		oldRule.Protocol == newRule.Protocol && oldRule.Port == newRule.Port &&
		oldRule.Source == newRule.Source && oldRule.Interface == newRule.Interface {
		if err := h.service.ToggleRule(serverID, ruleID, newRule.Enabled, client); err != nil {
			http.Error(w, "Failed to toggle rule state: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		// Full update
		if err := h.service.EditRule(serverID, oldRule, newRule, client); err != nil {
			http.Error(w, "Failed to update rule parameters: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// DeleteRule deletes a firewall rule
func (h *FirewallHandler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	ruleID := chi.URLParam(r, "ruleId")

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		http.Error(w, "SSH connection error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.service.DeleteRule(serverID, ruleID, client); err != nil {
		http.Error(w, "Failed to delete rule: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// BulkAction handles mass toggling/deletion of rules
func (h *FirewallHandler) BulkAction(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	var req struct {
		Action  string   `json:"action"` // "delete", "enable", "disable"
		RuleIDs []string `json:"rule_ids"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		http.Error(w, "SSH connection error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	for _, ruleID := range req.RuleIDs {
		var opErr error
		if req.Action == "delete" {
			opErr = h.service.DeleteRule(serverID, ruleID, client)
		} else if req.Action == "enable" {
			opErr = h.service.ToggleRule(serverID, ruleID, true, client)
		} else if req.Action == "disable" {
			opErr = h.service.ToggleRule(serverID, ruleID, false, client)
		}

		if opErr != nil {
			http.Error(w, fmt.Sprintf("Bulk action failed on rule %s: %v", ruleID, opErr), http.StatusInternalServerError)
			return
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// -----------------------------------------------------------------------------
// BACKUPS
// -----------------------------------------------------------------------------

// ListBackups lists backups for a server
func (h *FirewallHandler) ListBackups(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	backups, err := h.service.GetStore().ListBackups(serverID)
	if err != nil {
		http.Error(w, "Failed to load backups: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": backups})
}

// CreateBackup creates a backup from the current DB rules
func (h *FirewallHandler) CreateBackup(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	rules, err := h.service.GetStore().ListRules(serverID)
	if err != nil {
		http.Error(w, "Failed to load rules: "+err.Error(), http.StatusInternalServerError)
		return
	}

	rulesBytes, err := json.Marshal(rules)
	if err != nil {
		http.Error(w, "JSON marshal error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	backup := models.FirewallBackup{
		ID:        uuid.New().String(),
		ServerID:  serverID,
		Name:      req.Name,
		RulesJSON: string(rulesBytes),
		CreatedAt: time.Now(),
	}

	if err := h.service.GetStore().AddBackup(backup); err != nil {
		http.Error(w, "Failed to save backup: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": backup})
}

// RestoreBackup restores rules from a backup and applies to VPS
func (h *FirewallHandler) RestoreBackup(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	backupID := chi.URLParam(r, "backupId")

	client, err := h.serviceManager.GetSSHClient(serverID)
	if err != nil {
		http.Error(w, "SSH connection error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	backup, err := h.service.GetStore().GetBackup(backupID)
	if err != nil {
		http.Error(w, "Backup not found", http.StatusNotFound)
		return
	}

	var rules []models.FirewallRule
	if err := json.Unmarshal([]byte(backup.RulesJSON), &rules); err != nil {
		http.Error(w, "Corrupt backup rules format: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Delete existing rules in DB
	h.service.GetStore().DeleteAllRules(serverID)

	// Apply restored rules
	for _, rule := range rules {
		// Set new ID to prevent clashes or keep old one
		rule.ServerID = serverID
		h.service.GetStore().AddRule(rule)
		if rule.Enabled {
			h.service.ApplyRule(client, rule)
		}
	}

	h.service.GetStore().AddActivityLog(models.FirewallActivityLog{
		ID:        uuid.New().String(),
		ServerID:  serverID,
		Action:    "restore_backup",
		Details:   fmt.Sprintf("Restored rules from backup: %s", backup.Name),
		Timestamp: time.Now(),
	})

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// DeleteBackup deletes a backup
func (h *FirewallHandler) DeleteBackup(w http.ResponseWriter, r *http.Request) {
	backupID := chi.URLParam(r, "backupId")
	if err := h.service.GetStore().DeleteBackup(backupID); err != nil {
		http.Error(w, "Failed to delete backup: "+err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// -----------------------------------------------------------------------------
// SCHEDULES
// -----------------------------------------------------------------------------

// ListSchedules returns schedules
func (h *FirewallHandler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	scheds, err := h.service.GetStore().ListSchedules(serverID)
	if err != nil {
		http.Error(w, "Failed to load schedules: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": scheds})
}

// CreateSchedule adds a new schedule
func (h *FirewallHandler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")

	var req struct {
		Rule       models.FirewallRule `json:"rule"`
		ActionType string              `json:"action_type"`
		RunTime    string              `json:"run_time"` // RFC3339 timestamp
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	runTime, err := time.Parse(time.RFC3339, req.RunTime)
	if err != nil {
		http.Error(w, "Invalid RFC3339 runtime format", http.StatusBadRequest)
		return
	}

	// Validate rule params
	if err := h.service.ValidateRule(req.Rule); err != nil {
		http.Error(w, "Invalid rule params: "+err.Error(), http.StatusBadRequest)
		return
	}

	req.Rule.ID = uuid.New().String()
	req.Rule.ServerID = serverID
	req.Rule.CreatedAt = time.Now()

	ruleBytes, err := json.Marshal(req.Rule)
	if err != nil {
		http.Error(w, "Rule marshal error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	sched := models.FirewallSchedule{
		ID:           uuid.New().String(),
		ServerID:     serverID,
		RuleData:     string(ruleBytes),
		ActionType:   req.ActionType,
		ScheduledAt:  runTime,
		Executed:     false,
		Status:       "pending",
		ErrorMessage: "",
		CreatedAt:    time.Now(),
	}

	if err := h.service.GetStore().AddSchedule(sched); err != nil {
		http.Error(w, "Failed to save schedule: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": sched})
}

// CancelSchedule cancels/deletes a schedule
func (h *FirewallHandler) CancelSchedule(w http.ResponseWriter, r *http.Request) {
	schedID := chi.URLParam(r, "scheduleId")
	if err := h.service.GetStore().DeleteSchedule(schedID); err != nil {
		http.Error(w, "Failed to delete schedule: "+err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// -----------------------------------------------------------------------------
// LOGS
// -----------------------------------------------------------------------------

// GetLogs returns audit trails
func (h *FirewallHandler) GetLogs(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverID")
	logs, err := h.service.GetStore().ListActivityLogs(serverID)
	if err != nil {
		http.Error(w, "Failed to load activity logs: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": logs})
}
