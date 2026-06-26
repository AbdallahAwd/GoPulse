package store

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/amaa/gopulse/internal/models"
)

// LoadBalancerStore handles storage and retrieval of load balancer configurations.
type LoadBalancerStore struct {
	legacyPath string
	db         *sql.DB
	mu         sync.RWMutex
}

// NewLoadBalancerStore creates a SQLite-backed LoadBalancerStore.
func NewLoadBalancerStore(legacyPath string) (*LoadBalancerStore, error) {
	db, err := openSQLiteStore(legacyPath)
	if err != nil {
		return nil, err
	}

	return &LoadBalancerStore{
		legacyPath: legacyPath,
		db:         db,
	}, nil
}

func scanLoadBalancer(scanner interface {
	Scan(dest ...interface{}) error
}) (models.LoadBalancer, error) {
	var lb models.LoadBalancer
	var ssl int
	var errorMessageNull sql.NullString
	var createdAtRaw string

	err := scanner.Scan(
		&lb.ID,
		&lb.ServerID,
		&lb.Name,
		&lb.Domain,
		&lb.Port,
		&lb.Algorithm,
		&ssl,
		&lb.HealthCheckPath,
		&lb.HealthCheckInterval,
		&lb.Status,
		&errorMessageNull,
		&createdAtRaw,
	)
	if err != nil {
		return models.LoadBalancer{}, err
	}

	if errorMessageNull.Valid {
		lb.ErrorMessage = errorMessageNull.String
	}
	lb.SSL = intToBool(ssl)
	lb.CreatedAt = timeFromText(createdAtRaw)
	return lb, nil
}

func scanBackend(scanner interface {
	Scan(dest ...interface{}) error
}) (models.LoadBalancerBackend, error) {
	var b models.LoadBalancerBackend
	var backup int
	var targetServerIDNull sql.NullString
	var targetProjectIDNull sql.NullString
	var createdAtRaw string

	err := scanner.Scan(
		&b.ID,
		&b.LoadBalancerID,
		&targetServerIDNull,
		&targetProjectIDNull,
		&b.Host,
		&b.Port,
		&b.Weight,
		&backup,
		&b.Status,
		&createdAtRaw,
	)
	if err != nil {
		return models.LoadBalancerBackend{}, err
	}

	if targetServerIDNull.Valid {
		b.TargetServerID = targetServerIDNull.String
	}
	if targetProjectIDNull.Valid {
		b.TargetProjectID = targetProjectIDNull.String
	}
	b.Backup = intToBool(backup)
	b.CreatedAt = timeFromText(createdAtRaw)
	return b, nil
}

func (s *LoadBalancerStore) getBackendsForLoadBalancer(lbID string) []models.LoadBalancerBackend {
	rows, err := s.db.Query(
		`SELECT id, load_balancer_id, target_server_id, target_project_id, host, port, weight, backup, status, created_at
		FROM load_balancer_backends WHERE load_balancer_id = ? ORDER BY rowid`,
		lbID,
	)
	if err != nil {
		return []models.LoadBalancerBackend{}
	}
	defer rows.Close()

	var backends []models.LoadBalancerBackend
	for rows.Next() {
		b, err := scanBackend(rows)
		if err == nil {
			backends = append(backends, b)
		}
	}
	return backends
}

// AddLoadBalancer adds a new load balancer.
func (s *LoadBalancerStore) AddLoadBalancer(lb models.LoadBalancer) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var idExists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM load_balancers WHERE id = ?`, lb.ID).Scan(&idExists); err != nil {
		return err
	}
	if idExists > 0 {
		return fmt.Errorf("load balancer with ID %s already exists", lb.ID)
	}

	var domainExists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM load_balancers WHERE server_id = ? AND domain = ?`, lb.ServerID, lb.Domain).Scan(&domainExists); err != nil {
		return err
	}
	if domainExists > 0 {
		return fmt.Errorf("load balancer domain %s already configured for this server", lb.Domain)
	}

	if lb.CreatedAt.IsZero() {
		lb.CreatedAt = time.Now()
	}
	var errMsg sql.NullString
	if lb.ErrorMessage != "" {
		errMsg = sql.NullString{String: lb.ErrorMessage, Valid: true}
	}

	_, err := s.db.Exec(
		`INSERT INTO load_balancers (id, server_id, name, domain, port, algorithm, ssl, health_check_path, health_check_interval, status, error_message, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		lb.ID,
		lb.ServerID,
		lb.Name,
		lb.Domain,
		lb.Port,
		lb.Algorithm,
		boolToInt(lb.SSL),
		lb.HealthCheckPath,
		lb.HealthCheckInterval,
		lb.Status,
		errMsg,
		timeToText(lb.CreatedAt),
	)
	return err
}

// GetLoadBalancer returns a load balancer by ID.
func (s *LoadBalancerStore) GetLoadBalancer(id string) (models.LoadBalancer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(
		`SELECT id, server_id, name, domain, port, algorithm, ssl, health_check_path, health_check_interval, status, error_message, created_at
		FROM load_balancers WHERE id = ?`,
		id,
	)
	lb, err := scanLoadBalancer(row)
	if err == sql.ErrNoRows {
		return models.LoadBalancer{}, fmt.Errorf("load balancer not found")
	}
	if err != nil {
		return models.LoadBalancer{}, err
	}

	lb.Backends = s.getBackendsForLoadBalancer(lb.ID)
	return lb, nil
}

// UpdateLoadBalancer updates a load balancer configuration.
func (s *LoadBalancerStore) UpdateLoadBalancer(lb models.LoadBalancer) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errMsg sql.NullString
	if lb.ErrorMessage != "" {
		errMsg = sql.NullString{String: lb.ErrorMessage, Valid: true}
	}

	result, err := s.db.Exec(
		`UPDATE load_balancers SET name = ?, domain = ?, port = ?, algorithm = ?, ssl = ?, health_check_path = ?, health_check_interval = ?, status = ?, error_message = ?
		WHERE id = ?`,
		lb.Name,
		lb.Domain,
		lb.Port,
		lb.Algorithm,
		boolToInt(lb.SSL),
		lb.HealthCheckPath,
		lb.HealthCheckInterval,
		lb.Status,
		errMsg,
		lb.ID,
	)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("load balancer not found")
	}
	return nil
}

// RemoveLoadBalancer removes a load balancer by ID.
func (s *LoadBalancerStore) RemoveLoadBalancer(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM load_balancers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("load balancer not found")
	}
	return nil
}

// ListLoadBalancersByServer returns all load balancers for a server.
func (s *LoadBalancerStore) ListLoadBalancersByServer(serverID string) []models.LoadBalancer {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, server_id, name, domain, port, algorithm, ssl, health_check_path, health_check_interval, status, error_message, created_at
		FROM load_balancers WHERE server_id = ? ORDER BY rowid`,
		serverID,
	)
	if err != nil {
		return []models.LoadBalancer{}
	}

	var lbs []models.LoadBalancer
	for rows.Next() {
		lb, err := scanLoadBalancer(rows)
		if err == nil {
			lbs = append(lbs, lb)
		}
	}
	rows.Close()

	for i := range lbs {
		lbs[i].Backends = s.getBackendsForLoadBalancer(lbs[i].ID)
	}

	return lbs
}

// ListAllLoadBalancers returns all load balancers in the database.
func (s *LoadBalancerStore) ListAllLoadBalancers() ([]models.LoadBalancer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, server_id, name, domain, port, algorithm, ssl, health_check_path, health_check_interval, status, error_message, created_at
		FROM load_balancers ORDER BY rowid`,
	)
	if err != nil {
		return nil, err
	}

	var lbs []models.LoadBalancer
	for rows.Next() {
		lb, err := scanLoadBalancer(rows)
		if err == nil {
			lbs = append(lbs, lb)
		}
	}
	rows.Close()

	for i := range lbs {
		lbs[i].Backends = s.getBackendsForLoadBalancer(lbs[i].ID)
	}

	return lbs, nil
}

// AddBackend adds a backend to a load balancer.
func (s *LoadBalancerStore) AddBackend(backend models.LoadBalancerBackend) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var idExists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM load_balancer_backends WHERE id = ?`, backend.ID).Scan(&idExists); err != nil {
		return err
	}
	if idExists > 0 {
		return fmt.Errorf("backend with ID %s already exists", backend.ID)
	}

	var targetServerID sql.NullString
	if backend.TargetServerID != "" {
		targetServerID = sql.NullString{String: backend.TargetServerID, Valid: true}
	}
	var targetProjectID sql.NullString
	if backend.TargetProjectID != "" {
		targetProjectID = sql.NullString{String: backend.TargetProjectID, Valid: true}
	}

	if backend.CreatedAt.IsZero() {
		backend.CreatedAt = time.Now()
	}

	_, err := s.db.Exec(
		`INSERT INTO load_balancer_backends (id, load_balancer_id, target_server_id, target_project_id, host, port, weight, backup, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		backend.ID,
		backend.LoadBalancerID,
		targetServerID,
		targetProjectID,
		backend.Host,
		backend.Port,
		backend.Weight,
		boolToInt(backend.Backup),
		backend.Status,
		timeToText(backend.CreatedAt),
	)
	return err
}

// RemoveBackend removes a backend from a load balancer.
func (s *LoadBalancerStore) RemoveBackend(loadBalancerID, backendID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		`DELETE FROM load_balancer_backends WHERE load_balancer_id = ? AND id = ?`,
		loadBalancerID,
		backendID,
	)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("backend not found")
	}
	return nil
}

// UpdateBackendStatus updates the status of a backend.
func (s *LoadBalancerStore) UpdateBackendStatus(backendID string, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		`UPDATE load_balancer_backends SET status = ? WHERE id = ?`,
		status,
		backendID,
	)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("backend not found")
	}
	return nil
}
