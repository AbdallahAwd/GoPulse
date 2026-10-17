package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
)

func TestAgentCredentialHashExpiryRevocationAndOwnerIsolation(t *testing.T) {
	st, err := NewAgentStore(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	credential, secret, err := st.Create(models.AgentCredential{Owner: "alice", OwnerID: "alice-id", Name: "fixture", Mode: "read_only", ServerIDs: []string{"a"}, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	var hash, raw string
	if err := st.db.QueryRow(`SELECT token_hash,data FROM agent_credentials WHERE id=?`, credential.ID).Scan(&hash, &raw); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, "gp_agent_") || len(hash) != 64 || strings.Contains(raw, secret) || hash == secret {
		t.Fatal("reusable secret persisted")
	}
	if _, err := st.Authenticate(secret); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Authenticate(secret + "wrong"); err == nil {
		t.Fatal("wrong secret accepted")
	}
	if err := st.Revoke("bob", credential.ID); err == nil {
		t.Fatal("another owner revoked credential")
	}
	if err := st.Revoke("alice", credential.ID, "recreated-alice"); err == nil {
		t.Fatal("recreated identity revoked old credential")
	}
	if err := st.Revoke("alice", credential.ID, "alice-id"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Authenticate(secret); err == nil {
		t.Fatal("revoked secret accepted")
	}
	items, err := st.List("alice", "alice-id")
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	encoded, _ := json.Marshal(items)
	if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), hash) {
		t.Fatal("secret/hash leaked in metadata")
	}
	items, err = st.List("alice", "recreated-alice")
	if err != nil || len(items) != 0 {
		t.Fatal("identity metadata reused")
	}
	_, expired, err := st.Create(models.AgentCredential{Owner: "alice", ExpiresAt: time.Now().Add(-time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Authenticate(expired); err == nil {
		t.Fatal("expired credential accepted")
	}
}
func TestAgentAuditOwnerNamespaceCannotBeSpoofedByUsername(t *testing.T) {
	dir := t.TempDir()
	agents, err := NewAgentStore(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	ops, err := NewOperationStore(filepath.Join(dir, "operations.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ops.AuditIdentity("alice", "credential-a", "list_servers", "a", "accepted", ""); err != nil {
		t.Fatal(err)
	}
	if err := ops.AuditIdentity("alice|agent:spoof", "credential-b", "secret-other-tool", "b", "accepted", ""); err != nil {
		t.Fatal(err)
	}
	items, err := agents.Audit("alice", 100)
	if err != nil || len(items) != 1 || items[0]["tool"] != "list_servers" {
		t.Fatal("audit owner leak", items, err)
	}
}
