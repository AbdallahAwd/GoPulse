package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/google/uuid"
)

// ExportStore combines all entity stores to support full dashboard export/import.
type ExportStore struct {
	CronJobs      *CronStore
	ImportCronJob func(models.CronJob, string) (string, error)
	Servers       *ServerStore
	Projects      *ProjectStore
	Databases     *DatabaseStore
	Domains       *DomainStore
	Users         *UserStore
	LoadBalancers *LoadBalancerStore
}

// NewExportStore creates an ExportStore from the individual stores.
func NewExportStore(
	servers *ServerStore,
	projects *ProjectStore,
	databases *DatabaseStore,
	domains *DomainStore,
	users *UserStore,
	loadBalancers *LoadBalancerStore,
) *ExportStore {
	return &ExportStore{
		Servers:       servers,
		Projects:      projects,
		Databases:     databases,
		Domains:       domains,
		Users:         users,
		LoadBalancers: loadBalancers,
	}
}

// ExportAll reads every entity from the stores and bundles them into a
// GoPulseExport envelope ready for JSON marshalling.
func (s *ExportStore) ExportAll() (*models.GoPulseExport, error) {
	servers := s.Servers.ListServers()
	for i := range servers {
		if servers[i].AuthType == "key" && servers[i].KeyPath != "" {
			keyBytes, err := os.ReadFile(servers[i].KeyPath)
			if err == nil {
				servers[i].SSHKeyContent = string(keyBytes)
			}
		}
	}

	lbs, err := s.LoadBalancers.ListAllLoadBalancers()
	if err != nil {
		lbs = []models.LoadBalancer{}
	}

	export := &models.GoPulseExport{
		Version:       "1.0",
		ExportedAt:    time.Now().UTC(),
		Servers:       servers,
		Projects:      s.Projects.ListProjects(),
		Databases:     s.Databases.ListDatabases(),
		Domains:       s.Domains.ListDomains(),
		Users:         s.Users.ListUsers(),
		LoadBalancers: lbs,
	}

	export.QdrantConfigs = make(map[string]models.QdrantConfig)
	if s.CronJobs != nil {
		export.CronJobs, err = s.CronJobs.List("")
		if err != nil {
			return nil, err
		}
		for i := range export.CronJobs {
			export.CronJobs[i].LastRun = nil
		}
	}
	for _, db := range export.Databases {
		if db.Type == "qdrant" {
			cfg, err := s.Databases.GetQdrantConfig(db.ID)
			if err != nil {
				return nil, fmt.Errorf("Qdrant configuration missing for %s", db.ID)
			}
			export.QdrantConfigs[db.ID] = cfg
		}
	}
	// Ensure nil slices become empty arrays in JSON.
	if export.Servers == nil {
		export.Servers = []models.Server{}
	}
	if export.Projects == nil {
		export.Projects = []models.Project{}
	}
	if export.Databases == nil {
		export.Databases = []models.Database{}
	}
	if export.Domains == nil {
		export.Domains = []models.Domain{}
	}
	if export.Users == nil {
		export.Users = []models.User{}
	}
	if export.LoadBalancers == nil {
		export.LoadBalancers = []models.LoadBalancer{}
	}

	return export, nil
}

// ImportAll imports the given export data into all stores.
// mode must be "skip" (default) or "overwrite".
//   - skip:      existing records (matched by ID) are left untouched.
//   - overwrite: existing records are updated with the imported data.
//
// The function uses best-effort semantics: individual record failures are
// collected in the report but do not abort the remaining records.
func (s *ExportStore) ImportAll(data *models.GoPulseExport, mode string) (*models.ImportReport, error) {
	if mode == "" {
		mode = "skip"
	}
	if mode != "skip" && mode != "overwrite" {
		return nil, fmt.Errorf("invalid import mode %q, must be \"skip\" or \"overwrite\"", mode)
	}

	report := &models.ImportReport{}

	// Helper function to write key file locally
	writeKeyFile := func(srv *models.Server) error {
		if srv.AuthType != "key" || srv.SSHKeyContent == "" {
			return nil
		}
		keysDir := filepath.Join(DataDirectory(), "keys")
		if err := os.MkdirAll(keysDir, 0700); err != nil {
			return fmt.Errorf("create keys directory: %w", err)
		}
		baseName := filepath.Base(srv.KeyPath)
		if baseName == "." || baseName == "/" || baseName == "" {
			baseName = "imported.key"
		}
		// Ensure unique filename to prevent collision
		uniqueID := uuid.New().String()[:8]
		filename := fmt.Sprintf("imp_%s_%s", uniqueID, baseName)
		newKeyPath := filepath.Join(keysDir, filename)

		if err := os.WriteFile(newKeyPath, []byte(srv.SSHKeyContent), 0600); err != nil {
			return fmt.Errorf("write key file: %w", err)
		}
		srv.KeyPath = newKeyPath
		return nil
	}

	// --- Servers ---
	for _, srv := range data.Servers {
		_, err := s.Servers.GetServer(srv.ID)
		if err != nil {
			// Record doesn't exist — create it.
			if keyErr := writeKeyFile(&srv); keyErr != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("server %s (%s) key: %v", srv.Name, srv.ID, keyErr))
				continue
			}
			if addErr := s.Servers.AddServer(srv); addErr != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("server %s (%s): %v", srv.Name, srv.ID, addErr))
			} else {
				report.ServersCreated++
			}
		} else {
			if mode == "skip" {
				report.ServersSkipped++
			} else {
				if keyErr := writeKeyFile(&srv); keyErr != nil {
					report.Errors = append(report.Errors, fmt.Sprintf("server %s (%s) key: %v", srv.Name, srv.ID, keyErr))
					continue
				}
				if updErr := s.Servers.UpdateServer(srv); updErr != nil {
					report.Errors = append(report.Errors, fmt.Sprintf("server %s (%s): %v", srv.Name, srv.ID, updErr))
				} else {
					report.ServersUpdated++
				}
			}
		}
	}

	// --- Projects ---
	for _, proj := range data.Projects {
		_, err := s.Projects.GetProject(proj.ID)
		if err != nil {
			if addErr := s.Projects.AddProject(proj); addErr != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("project %s (%s): %v", proj.Name, proj.ID, addErr))
			} else {
				report.ProjectsCreated++
			}
		} else {
			if mode == "skip" {
				report.ProjectsSkipped++
			} else {
				if updErr := s.Projects.UpdateProject(proj); updErr != nil {
					report.Errors = append(report.Errors, fmt.Sprintf("project %s (%s): %v", proj.Name, proj.ID, updErr))
				} else {
					report.ProjectsUpdated++
				}
			}
		}
	}

	// --- Databases ---
	for _, db := range data.Databases {
		_, err := s.Databases.GetDatabase(db.ID)
		if err != nil {
			if addErr := s.Databases.AddDatabase(db); addErr != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("database %s (%s): %v", db.Name, db.ID, addErr))
			} else {
				report.DatabasesCreated++
			}
		} else {
			if mode == "skip" {
				report.DatabasesSkipped++
			} else {
				if updErr := s.Databases.UpdateDatabase(db); updErr != nil {
					report.Errors = append(report.Errors, fmt.Sprintf("database %s (%s): %v", db.Name, db.ID, updErr))
				} else {
					report.DatabasesUpdated++
				}
			}
		}
	}
	for id, cfg := range data.QdrantConfigs {
		db, err := s.Databases.GetDatabase(id)
		if err != nil || db.Type != "qdrant" {
			continue
		}
		if mode == "skip" {
			if _, err := s.Databases.GetQdrantConfig(id); err == nil {
				continue
			}
		}
		if err := s.Databases.SetQdrantConfig(id, cfg); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("Qdrant settings %s: %v", id, err))
		}
	}

	// --- Domains ---
	for _, dom := range data.Domains {
		_, err := s.Domains.GetDomain(dom.ID)
		if err != nil {
			if addErr := s.Domains.AddDomain(dom); addErr != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("domain %s (%s): %v", dom.Domain, dom.ID, addErr))
			} else {
				report.DomainsCreated++
			}
		} else {
			if mode == "skip" {
				report.DomainsSkipped++
			} else {
				// DomainStore doesn't have UpdateDomain, so we delete+add for overwrite.
				if delErr := s.Domains.RemoveDomain(dom.ID); delErr != nil {
					report.Errors = append(report.Errors, fmt.Sprintf("domain %s (%s) remove: %v", dom.Domain, dom.ID, delErr))
					continue
				}
				if addErr := s.Domains.AddDomain(dom); addErr != nil {
					report.Errors = append(report.Errors, fmt.Sprintf("domain %s (%s) re-add: %v", dom.Domain, dom.ID, addErr))
				} else {
					report.DomainsUpdated++
				}
			}
		}
	}

	// --- Users ---
	for _, usr := range data.Users {
		existing, err := s.Users.GetUserByID(usr.ID)
		if err != nil {
			// New user — AddUser would normally hash the password, but
			// the exported password is already bcrypt-hashed, so we use
			// the raw insert that preserves the hash.
			if addErr := s.Users.AddUserRaw(usr); addErr != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("user %s (%s): %v", usr.Username, usr.ID, addErr))
			} else {
				report.UsersCreated++
			}
		} else {
			_ = existing
			if mode == "skip" {
				report.UsersSkipped++
			} else {
				if updErr := s.Users.UpdateUser(usr); updErr != nil {
					report.Errors = append(report.Errors, fmt.Sprintf("user %s (%s): %v", usr.Username, usr.ID, updErr))
				} else {
					report.UsersUpdated++
				}
			}
		}
	}

	// --- Load Balancers ---
	for _, lb := range data.LoadBalancers {
		_, err := s.LoadBalancers.GetLoadBalancer(lb.ID)
		if err != nil {
			// Record doesn't exist — create it.
			if addErr := s.LoadBalancers.AddLoadBalancer(lb); addErr != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("load balancer %s (%s): %v", lb.Name, lb.ID, addErr))
			} else {
				report.LoadBalancersCreated++
			}
		} else {
			if mode == "skip" {
				report.LoadBalancersSkipped++
			} else {
				// Overwrite. Remove first, then add.
				if delErr := s.LoadBalancers.RemoveLoadBalancer(lb.ID); delErr != nil {
					report.Errors = append(report.Errors, fmt.Sprintf("load balancer %s (%s) remove: %v", lb.Name, lb.ID, delErr))
					continue
				}
				if addErr := s.LoadBalancers.AddLoadBalancer(lb); addErr != nil {
					report.Errors = append(report.Errors, fmt.Sprintf("load balancer %s (%s) re-add: %v", lb.Name, lb.ID, addErr))
				} else {
					report.LoadBalancersUpdated++
				}
			}
		}
	}

	// Cron definitions are validated by the scheduler and restored paused.
	for _, job := range data.CronJobs {
		if s.ImportCronJob == nil {
			report.Errors = append(report.Errors, "cron job "+job.ID+": cron import is unavailable")
			continue
		}
		outcome, err := s.ImportCronJob(job, mode)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("cron job %s (%s): %v", job.Name, job.ID, err))
			continue
		}
		switch outcome {
		case "created":
			report.CronJobsCreated++
		case "updated":
			report.CronJobsUpdated++
		case "skipped":
			report.CronJobsSkipped++
		}
	}
	return report, nil
}
