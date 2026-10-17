package services

import (
	"encoding/json"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
)

// Exposure is an immediate control-plane switch, separate from source drafts.
func (s *FunctionService) SetMCP(server, id, actor string, enabled bool) (models.Function, error) {
	if _, err := s.Authorize(actor, server); err != nil {
		return models.Function{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.Store.Get(server, id)
	if err != nil {
		return f, err
	}
	if enabled && f.Deployed == nil {
		return f, &FunctionValidationError{"Deploy a version before exposing it through MCP."}
	}
	f.MCPEnabled = enabled
	f.UpdatedAt = time.Now().UTC()
	return f, s.Store.Save(f, false)
}

// Expected version pins reviewed calls so redeploying cannot change the code
// between submission, human approval, and execution.
func (s *FunctionService) BeginMCP(server, id, actor, version string, event json.RawMessage) (models.FunctionRun, error) {
	u, err := s.Authorize(actor, server)
	if err != nil {
		return models.FunctionRun{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.Store.Get(server, id)
	if err != nil {
		return models.FunctionRun{}, err
	}
	if !f.MCPEnabled || f.Paused || f.Deployed == nil || f.Deployed.ID != version {
		return models.FunctionRun{}, &FunctionValidationError{"Function MCP exposure, pause state, or deployed version changed. Discover tools again and submit a new request."}
	}
	return s.beginLocked(server, id, u.ID, "invoke", "mcp", event)
}
