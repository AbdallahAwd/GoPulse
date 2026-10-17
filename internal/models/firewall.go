package models

import "time"

// FirewallBackend represents the type of active VPS firewall engine
type FirewallBackend string

const (
	FirewallBackendUFW       FirewallBackend = "ufw"
	FirewallBackendFirewalld FirewallBackend = "firewalld"
	FirewallBackendNFTables  FirewallBackend = "nftables"
	FirewallBackendIPTables  FirewallBackend = "iptables"
	FirewallBackendNone      FirewallBackend = "none"
)

// FirewallState holds real-time overview info for a server's firewall
type FirewallState struct {
	Enabled               bool            `json:"enabled"`
	Backend               FirewallBackend `json:"backend"`
	DefaultIncomingPolicy string          `json:"default_incoming_policy"`
	DefaultOutgoingPolicy string          `json:"default_outgoing_policy"`
	RulesCount            int             `json:"rules_count"`
	LastUpdated           time.Time       `json:"last_updated"`
}

// FirewallRule represents a custom firewall rule
type FirewallRule struct {
	ID        string    `json:"id"`
	ServerID  string    `json:"server_id"`
	Action    string    `json:"action"`    // "allow", "deny", "reject", "limit"
	Direction string    `json:"direction"` // "ingress", "egress"
	Protocol  string    `json:"protocol"`  // "tcp", "udp", "icmp", "any"
	Port      string    `json:"port"`      // e.g. "80", "10000:20000", "any"
	Source    string    `json:"source"`    // "any", "1.2.3.4", "192.168.1.0/24", etc.
	Interface string    `json:"interface"` // "any", "eth0", "ens3", "docker0", etc.
	Enabled   bool      `json:"enabled"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}

// FirewallBackup stores a JSON snapshot of a server's configured rules
type FirewallBackup struct {
	ID        string    `json:"id"`
	ServerID  string    `json:"server_id"`
	Name      string    `json:"name"`
	RulesJSON string    `json:"rules_json"` // JSON string representation of []FirewallRule
	CreatedAt time.Time `json:"created_at"`
}

// FirewallSchedule queues/schedules a future rule status change
type FirewallSchedule struct {
	ID           string    `json:"id"`
	ServerID     string    `json:"server_id"`
	RuleData     string    `json:"rule_data"`   // JSON representation of FirewallRule to add/edit/delete
	ActionType   string    `json:"action_type"` // "add", "delete", "enable", "disable"
	ScheduledAt  time.Time `json:"scheduled_at"`
	Executed     bool      `json:"executed"`
	Status       string    `json:"status"` // "pending", "success", "failed"
	ErrorMessage string    `json:"error_message"`
	CreatedAt    time.Time `json:"created_at"`
}

// FirewallActivityLog tracks operations performed on a firewall
type FirewallActivityLog struct {
	ID        string    `json:"id"`
	ServerID  string    `json:"server_id"`
	Action    string    `json:"action"` // "enable", "disable", "add_rule", "delete_rule", etc.
	Details   string    `json:"details"`
	Timestamp time.Time `json:"timestamp"`
}
