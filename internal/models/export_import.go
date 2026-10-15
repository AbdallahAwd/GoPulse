package models

import "time"

// GoPulseExport is the top-level envelope for a full dashboard export.
// Every entity slice is optional — an empty slice means "nothing to import"
// for that category.
type GoPulseExport struct {
	CronJobs      []CronJob               `json:"cron_jobs,omitempty"`
	QdrantConfigs map[string]QdrantConfig `json:"qdrant_configs,omitempty"`
	Version       string                  `json:"version"`     // Schema version, currently "1.0"
	ExportedAt    time.Time               `json:"exported_at"` // Timestamp of the export
	Servers       []Server                `json:"servers"`
	Projects      []Project               `json:"projects"`
	Databases     []Database              `json:"databases"`
	Domains       []Domain                `json:"domains"`
	Users         []User                  `json:"users"`
	LoadBalancers []LoadBalancer          `json:"load_balancers,omitempty"`
}

// ImportReport summarises the result of an import operation.
type ImportReport struct {
	CronJobsCreated int `json:"cron_jobs_created"`
	CronJobsSkipped int `json:"cron_jobs_skipped"`
	CronJobsUpdated int `json:"cron_jobs_updated"`
	ServersCreated  int `json:"servers_created"`
	ServersSkipped  int `json:"servers_skipped"`
	ServersUpdated  int `json:"servers_updated"`

	ProjectsCreated int `json:"projects_created"`
	ProjectsSkipped int `json:"projects_skipped"`
	ProjectsUpdated int `json:"projects_updated"`

	DatabasesCreated int `json:"databases_created"`
	DatabasesSkipped int `json:"databases_skipped"`
	DatabasesUpdated int `json:"databases_updated"`

	DomainsCreated int `json:"domains_created"`
	DomainsSkipped int `json:"domains_skipped"`
	DomainsUpdated int `json:"domains_updated"`

	UsersCreated int `json:"users_created"`
	UsersSkipped int `json:"users_skipped"`
	UsersUpdated int `json:"users_updated"`

	LoadBalancersCreated int `json:"load_balancers_created"`
	LoadBalancersSkipped int `json:"load_balancers_skipped"`
	LoadBalancersUpdated int `json:"load_balancers_updated"`

	Errors []string `json:"errors,omitempty"`
}
