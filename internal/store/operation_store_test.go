package store

import (
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"path/filepath"
	"testing"
)

func TestOperationAuditAndUniqueActiveResource(t *testing.T) {
	st, err := NewOperationStore(filepath.Join(t.TempDir(), "ops.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Audit("alice", "restart_service", "server-a", "requested", "op-1"); err != nil {
		t.Fatal(err)
	}
	var actor, tool, outcome string
	if err := st.db.QueryRow(`SELECT actor,tool,outcome FROM tool_audit`).Scan(&actor, &tool, &outcome); err != nil {
		t.Fatal(err)
	}
	if actor != "alice" || tool != "restart_service" || outcome != "requested" {
		t.Fatal("wrong audit entry")
	}
	op := models.Operation{ID: "op-1", Owner: "alice", Resource: "server", Status: "running"}
	if err := st.Create(op); err != nil {
		t.Fatal(err)
	}
	op.ID = "op-2"
	if st.Create(op) == nil {
		t.Fatal("duplicate active resource")
	}
}
