package models

import "time"

// AlertRule includes durable evaluator state. Handlers redact webhook credentials
// for viewers. Configuration changes reset evaluation without creating a recovery.
type AlertRule struct {
	ID              string    `json:"id"`
	ServerID        string    `json:"server_id"`
	Name            string    `json:"name"`
	Metric          string    `json:"metric"`
	Threshold       float64   `json:"threshold"`
	DurationSeconds int       `json:"duration_seconds"`
	Enabled         bool      `json:"enabled"`
	WebhookURL      string    `json:"webhook_url,omitempty"`
	WebhookSecret   string    `json:"webhook_secret,omitempty"`
	HasWebhook      bool      `json:"has_webhook"`
	SilencedUntil   time.Time `json:"silenced_until"`
	State           string    `json:"state"`
	IncidentOpen    bool      `json:"incident_open"`
	BreachSince     time.Time `json:"breach_since"`
	LastSampleAt    time.Time `json:"last_sample_at"`
	LastObservedAt  time.Time `json:"last_observed_at"`
	LastValue       *float64  `json:"last_value"`
	NotifiedFiring  bool      `json:"notified_firing"`
}

type AlertEvent struct {
	ID             string    `json:"id"`
	RuleID         string    `json:"rule_id"`
	ServerID       string    `json:"server_id"`
	RuleName       string    `json:"rule_name"`
	Metric         string    `json:"metric"`
	State          string    `json:"state"`
	Value          *float64  `json:"value"`
	Threshold      float64   `json:"threshold"`
	OccurredAt     time.Time `json:"occurred_at"`
	DeliveryStatus string    `json:"delivery_status"`
	Attempts       int       `json:"attempts"`
	LastError      string    `json:"last_error,omitempty"`
}

// WebhookDelivery is internal; its destination and signing key never appear in
// the incident API. Retries retain the same event ID and immutable payload.
type WebhookDelivery struct {
	Event   AlertEvent
	URL     string
	Secret  string
	Payload []byte
}
