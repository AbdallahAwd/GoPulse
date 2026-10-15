package models

import "time"

type DashboardDomainInput struct {
	Domain     string `json:"domain"`
	HTTPS      bool   `json:"https"`
	Email      string `json:"email"`
	AgreeTerms bool   `json:"agree_terms"`
	ExpectedIP string `json:"expected_ip"`
}

type DashboardDomainCheck struct {
	Addresses      []string  `json:"addresses"`
	DNSResolved    bool      `json:"dns_resolved"`
	ExpectedMatch  bool      `json:"expected_match"`
	DashboardMatch bool      `json:"dashboard_match"`
	Message        string    `json:"message"`
	CheckedAt      time.Time `json:"checked_at"`
	CertificateEnd time.Time `json:"certificate_end,omitempty"`
}

type DashboardDomainSettings struct {
	DashboardDomainInput
	Revision        string               `json:"revision"`
	UpdatedAt       time.Time            `json:"updated_at"`
	AppliedDomain   string               `json:"applied_domain"`
	AppliedRevision string               `json:"applied_revision"`
	AppliedHTTPS    bool                 `json:"applied_https"`
	AppliedAt       time.Time            `json:"applied_at"`
	LastError       string               `json:"last_error"`
	OperationID     string               `json:"operation_id"`
	Check           DashboardDomainCheck `json:"check"`
}
