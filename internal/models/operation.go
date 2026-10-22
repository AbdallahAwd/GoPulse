package models

import "time"

type Operation struct {
	AgentCredentialID  string      `json:"agent_credential_id,omitempty"`
	AgentName          string      `json:"agent_name,omitempty"`
	RequestKey         string      `json:"request_key,omitempty"`
	RequestFingerprint string      `json:"request_fingerprint,omitempty"`
	Preview            interface{} `json:"preview,omitempty"`
	TargetServers      []string    `json:"target_servers,omitempty"`
	ID                 string      `json:"operation_id"`
	Owner              string      `json:"owner"`
	ServerID           string      `json:"server_id"`
	Resource           string      `json:"resource"`
	Kind               string      `json:"operation"`
	Status             string      `json:"status"`
	Steps              []string    `json:"steps"`
	Error              string      `json:"error,omitempty"`
	Result             interface{} `json:"result,omitempty"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
}

type QdrantConfig struct {
	Version     string `json:"version"`
	BindAddress string `json:"bind_address"`
	StoragePath string `json:"storage_path"`
	HostPort    int    `json:"host_port"`
	GRPCPort    int    `json:"grpc_port,omitempty"` // Zero preserves HTTP-only legacy deployments.
}
