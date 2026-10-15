package services

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/store"
)

// jsonUnmarshal is an alias to avoid import confusion
var jsonUnmarshal = json.Unmarshal

const (
	mysqlInternalAdminUser   = "root"
	mysqlDefaultCharacterSet = "utf8mb4"
	mysqlDefaultCollation    = "utf8mb4_unicode_ci"
)

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func sqlStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func mysqlIdentifier(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

func mysqlExecCommand(containerName string, db models.Database, database string, options string, query string, stderrRedirect string) string {
	parts := []string{
		"docker exec",
		fmt.Sprintf("-e MYSQL_PWD=%s", shellQuote(db.Password)),
		shellQuote(containerName),
		"mysql",
		fmt.Sprintf("--default-character-set=%s", mysqlDefaultCharacterSet),
		fmt.Sprintf("--user=%s", shellQuote(mysqlInternalAdminUser)),
	}
	if database != "" {
		parts = append(parts, fmt.Sprintf("--database=%s", shellQuote(database)))
	}
	if options != "" {
		parts = append(parts, options)
	}
	if query != "" {
		parts = append(parts, "-e", shellQuote(query))
	}
	if stderrRedirect != "" {
		parts = append(parts, stderrRedirect)
	}
	return strings.Join(parts, " ")
}

func mysqlAdminCommand(containerName string, db models.Database, args string, stderrRedirect string) string {
	parts := []string{
		"docker exec",
		fmt.Sprintf("-e MYSQL_PWD=%s", shellQuote(db.Password)),
		shellQuote(containerName),
		"mysqladmin",
		fmt.Sprintf("--user=%s", shellQuote(mysqlInternalAdminUser)),
	}
	if args != "" {
		parts = append(parts, args)
	}
	if stderrRedirect != "" {
		parts = append(parts, stderrRedirect)
	}
	return strings.Join(parts, " ")
}

// DatabaseService handles containerized database operations on remote servers via SSH
type DatabaseService struct {
	locks   sync.Map
	store   *store.DatabaseStore
	manager *ServiceManager
}

// NewDatabaseService creates a new DatabaseService
func NewDatabaseService(store *store.DatabaseStore, manager *ServiceManager) *DatabaseService {
	return &DatabaseService{
		store:   store,
		manager: manager,
	}
}

// ListDatabases returns all databases for a specific server
func (s *DatabaseService) ListDatabases(serverID string) ([]models.Database, error) {
	allDbs := s.store.ListDatabases()
	var serverDbs []models.Database
	for _, db := range allDbs {
		if db.ServerID == serverID {
			serverDbs = append(serverDbs, db)
		}
	}
	return serverDbs, nil
}

// GetDatabase returns a database by ID
func (s *DatabaseService) GetDatabase(id string) (models.Database, error) {
	return s.store.GetDatabase(id)
}

// ProvisionDatabase creates and runs a containerized database on the remote server
func (s *DatabaseService) ProvisionDatabase(ctx context.Context, serverID string, name string, dbType string, dbName string, username string, password string, hostPort int) (*models.Database, error) {
	if name == "" || (dbType != "sqlite" && (hostPort < 1 || hostPort > 65535)) {
		return nil, fmt.Errorf("invalid database name or port")
	}
	switch dbType {
	case "mysql", "postgres", "mongodb", "redis", "sqlite", "pocketbase", "appwrite":
	default:
		return nil, fmt.Errorf("unsupported database engine")
	}
	client, err := s.manager.GetSSHClient(serverID)
	if err != nil {
		return nil, fmt.Errorf("failed to get SSH client: %w", err)
	}

	if dbType != "sqlite" {
		// 1. Check if the host port is already listening
		portCheckCmd := fmt.Sprintf("netstat -tuln | grep -q ':%d ' && echo 'busy' || echo 'free'", hostPort)
		portCheckOut, _ := client.RunCommandContext(ctx, portCheckCmd)
		if strings.TrimSpace(portCheckOut) == "busy" {
			return nil, fmt.Errorf("port %d is already in use on the host", hostPort)
		}
	}

	// Generate a unique database ID
	dbID := fmt.Sprintf("db-%d", time.Now().UnixNano()/1e6)
	containerName := fmt.Sprintf("gopulse-%s", dbID)
	volumePath := fmt.Sprintf("/var/lib/gopulse/databases/%s", dbID)

	// Create directories on host with sudo and assign ownership to the SSH user
	_, err = client.RunCommandContext(ctx, fmt.Sprintf("sudo mkdir -p %[1]s && sudo chown -R $(whoami):$(whoami) %[1]s", volumePath))
	if err != nil {
		return nil, fmt.Errorf("failed to create persistent database directory: %w", err)
	}

	// Compose the run command based on database type
	var runCmd string
	switch dbType {
	case "mysql":
		mysqlEnv := []string{
			fmt.Sprintf("-e MYSQL_ROOT_PASSWORD=%s", shellQuote(password)),
			fmt.Sprintf("-e MYSQL_DATABASE=%s", shellQuote(dbName)),
		}
		if username != "" && username != mysqlInternalAdminUser {
			mysqlEnv = append(mysqlEnv,
				fmt.Sprintf("-e MYSQL_USER=%s", shellQuote(username)),
				fmt.Sprintf("-e MYSQL_PASSWORD=%s", shellQuote(password)),
			)
		}
		runCmd = fmt.Sprintf(
			"docker run -d --name %s -p %d:3306 -v %s:/var/lib/mysql %s --restart unless-stopped mysql:8.0 --character-set-server=%s --collation-server=%s",
			shellQuote(containerName), hostPort, shellQuote(volumePath), strings.Join(mysqlEnv, " "),
			mysqlDefaultCharacterSet, mysqlDefaultCollation,
		)
	case "postgres":
		runCmd = fmt.Sprintf(
			"docker run -d --name %s -p %d:5432 -v %s:/var/lib/postgresql/data -e POSTGRES_PASSWORD=%s -e POSTGRES_USER=%s -e POSTGRES_DB=%s --restart unless-stopped postgres:15",
			shellQuote(containerName), hostPort, shellQuote(volumePath), shellQuote(password), shellQuote(username), shellQuote(dbName),
		)
	case "mongodb":
		runCmd = fmt.Sprintf(
			"docker run -d --name %s -p %d:27017 -v %s:/data/db -e MONGO_INITDB_ROOT_USERNAME=%s -e MONGO_INITDB_ROOT_PASSWORD=%s --restart unless-stopped mongo:6.0",
			shellQuote(containerName), hostPort, shellQuote(volumePath), shellQuote(username), shellQuote(password),
		)
	case "redis":
		runCmd = fmt.Sprintf(
			"docker run -d --name %s -p %d:6379 -v %s:/data --restart unless-stopped redis:7.0-alpine redis-server --requirepass %s --appendonly yes",
			shellQuote(containerName), hostPort, shellQuote(volumePath), shellQuote(password),
		)
	case "sqlite":
		runCmd = fmt.Sprintf("sqlite3 %s/sqlite.db 'VACUUM;' 2>/dev/null || touch %s/sqlite.db", volumePath, volumePath)
	case "pocketbase":
		runCmd = fmt.Sprintf(
			"docker run -d --name %s --entrypoint /usr/local/bin/pocketbase -p %d:8090 -v %s/pb_data:/pb_data --restart unless-stopped spectado/pocketbase:latest serve --http=0.0.0.0:8090 --dir=/pb_data",
			containerName, hostPort, volumePath,
		)
	case "appwrite":
		// Database user & root passwords
		dbPass := password
		dbRootPass := fmt.Sprintf("root-%s-%s", dbID, password)
		if len(dbRootPass) > 32 {
			dbRootPass = dbRootPass[:32]
		}

		// Generate an encryption key for Appwrite (needs to be exactly or at least 32-64 chars)
		opensslKey := fmt.Sprintf("gopulse-appw-%s-%s", dbID, password)
		if len(opensslKey) > 64 {
			opensslKey = opensslKey[:64]
		} else {
			opensslKey = opensslKey + strings.Repeat("x", 64-len(opensslKey))
		}

		// Resolve the server's public host for Appwrite domain routing checks
		serverHost := "localhost"
		if srv, srvErr := s.manager.store.GetServer(serverID); srvErr == nil {
			serverHost = srv.Host
		}

		composeContent := fmt.Sprintf(`services:
  mariadb:
    image: mariadb:10.11
    container_name: %s-mariadb
    environment:
      MYSQL_ROOT_PASSWORD: "%s"
      MYSQL_DATABASE: appwrite
      MYSQL_USER: appwrite
      MYSQL_PASSWORD: "%s"
    volumes:
      - %s/mariadb:/var/lib/mysql
    restart: unless-stopped
    healthcheck:
      test: ["CMD-SHELL", "mysqladmin ping -h localhost -u root -p%s >/dev/null 2>&1"]
      interval: 5s
      timeout: 3s
      retries: 5

  redis:
    image: redis:7.0-alpine
    container_name: %s-redis
    volumes:
      - %s/redis:/data
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 5

  appwrite:
    image: appwrite/appwrite:1.5.4
    container_name: %s-appwrite
    ports:
      - "%d:80"
    environment:
      _APP_ENV: production
      _APP_OPENSSL_KEY_V1: "%s"
      _APP_DOMAIN: "%s"
      _APP_DOMAIN_FUNCTIONS: "%s"
      _APP_DOMAIN_TARGET: "%s"
      _APP_CONSOLE_HOSTNAMES: "%s,localhost"
      _APP_DB_ADAPTER: mariadb
      _APP_DB_HOST: mariadb
      _APP_DB_PORT: 3306
      _APP_DB_SCHEMA: appwrite
      _APP_DB_USER: appwrite
      _APP_DB_PASS: "%s"
      _APP_REDIS_HOST: redis
      _APP_REDIS_PORT: 6379
      _APP_OPTIONS_ABUSE: disabled
      _APP_SYSTEM_RESPONSE_FORMAT: ""
      _APP_STORAGE_LIMIT: 5000000000
      _APP_STORAGE_PREVIEW_LIMIT: 20000000
      _APP_STORAGE_DEVICE: local
    depends_on:
      mariadb:
        condition: service_healthy
      redis:
        condition: service_healthy
    volumes:
      - %s/uploads:/storage/uploads:rw
      - %s/cache:/storage/cache:rw
      - %s/config:/storage/config:rw
      - %s/certificates:/storage/certificates:rw
    restart: unless-stopped

  worker-databases:
    image: appwrite/appwrite:1.5.4
    container_name: %s-worker-databases
    entrypoint: worker-databases
    environment:
      _APP_ENV: production
      _APP_OPENSSL_KEY_V1: "%s"
      _APP_DOMAIN: "%s"
      _APP_DOMAIN_FUNCTIONS: "%s"
      _APP_DOMAIN_TARGET: "%s"
      _APP_CONSOLE_HOSTNAMES: "%s,localhost"
      _APP_DB_ADAPTER: mariadb
      _APP_DB_HOST: mariadb
      _APP_DB_PORT: 3306
      _APP_DB_SCHEMA: appwrite
      _APP_DB_USER: appwrite
      _APP_DB_PASS: "%s"
      _APP_REDIS_HOST: redis
      _APP_REDIS_PORT: 6379
      _APP_SYSTEM_RESPONSE_FORMAT: ""
    depends_on:
      mariadb:
        condition: service_healthy
      redis:
        condition: service_healthy
    volumes:
      - %s/uploads:/storage/uploads:rw
      - %s/cache:/storage/cache:rw
      - %s/config:/storage/config:rw
    restart: unless-stopped
`,
			containerName, dbRootPass, dbPass, volumePath, dbRootPass,
			containerName, volumePath,
			containerName, hostPort, opensslKey, serverHost, serverHost, serverHost, serverHost, dbPass, volumePath, volumePath, volumePath, volumePath,
			containerName, opensslKey, serverHost, serverHost, serverHost, serverHost, dbPass, volumePath, volumePath, volumePath,
		)

		composeB64 := base64.StdEncoding.EncodeToString([]byte(composeContent))
		writeComposeCmd := fmt.Sprintf(
			`echo '%s' | base64 -d > %s/docker-compose.yml`,
			composeB64, volumePath,
		)
		_, err = client.RunCommandContext(ctx, writeComposeCmd)
		if err != nil {
			return nil, fmt.Errorf("failed to write docker-compose.yml on host: %w", err)
		}

		runCmd = fmt.Sprintf("docker compose -f %s/docker-compose.yml up -d 2>&1", volumePath)
	default:
		return nil, fmt.Errorf("unsupported database engine type: %s", dbType)
	}

	var containerID string
	if dbType == "sqlite" {
		_, err = client.RunCommandContext(ctx, runCmd)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize sqlite database file: %w", err)
		}
		containerID = "sqlite-local"
	} else if dbType == "appwrite" {
		composeOut, runErr := client.RunCommandContext(ctx, runCmd)
		if runErr != nil {
			return nil, fmt.Errorf("failed to start appwrite stack: %s", strings.TrimSpace(composeOut))
		}
		containerID = "appwrite-compose"
	} else {
		containerIDOut, err := client.RunCommandContext(ctx, runCmd)
		if err != nil {
			return nil, fmt.Errorf("failed to execute docker run command: %w", err)
		}
		containerID = strings.TrimSpace(containerIDOut)
		if len(containerID) > 12 {
			containerID = containerID[:12]
		}

		// Bootstrap PocketBase admin superuser if provided
		if dbType == "pocketbase" && username != "" && password != "" && password != "none" {
			time.Sleep(1500 * time.Millisecond)

			// Try new version "superuser upsert" first (PocketBase 0.22+)
			adminCmd := fmt.Sprintf("docker exec %s /usr/local/bin/pocketbase superuser upsert %s %s --dir=/pb_data", shellQuote(containerName), shellQuote(username), shellQuote(password))
			_, err = client.RunCommandContext(ctx, adminCmd)
			if err != nil {
				// Fallback to older version "admin create" (PocketBase <0.22)
				fallbackCmd := fmt.Sprintf("docker exec %s /usr/local/bin/pocketbase admin create %s %s --dir=/pb_data", shellQuote(containerName), shellQuote(username), shellQuote(password))
				_, errFallback := client.RunCommandContext(ctx, fallbackCmd)
				if errFallback != nil {
					// Print warning but do not fail provisioning since the service is already running
					fmt.Printf("Warning: failed to bootstrap pocketbase superuser: %v (fallback error: %v)\n", err, errFallback)
				}
			}
		}
	}

	// Create database record
	db := models.Database{
		ID:           dbID,
		ServerID:     serverID,
		Name:         name,
		Type:         dbType,
		DatabaseName: dbName,
		Username:     username,
		Password:     password,
		HostPort:     hostPort,
		ContainerID:  containerID,
		Status:       models.DatabaseStatusStarting,
		CreatedAt:    time.Now(),
	}

	err = s.store.AddDatabase(db)
	if err != nil {
		// Attempt cleanup
		client.RunCommandContext(ctx, fmt.Sprintf("docker rm -f %s", containerName))
		return nil, fmt.Errorf("failed to persist database metadata: %w", err)
	}

	// Update status asynchronously
	go func() {
		time.Sleep(5 * time.Second)
		s.UpdateStatus(context.Background(), dbID)
	}()

	return &db, nil
}

// UpdateStatus verifies remote runtime health without treating a failed SSH or
// Docker command as evidence that the database stopped. Reads are bounded.
func (s *DatabaseService) UpdateStatus(parent context.Context, id string) (models.DatabaseStatus, error) {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return models.DatabaseStatusError, err
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	persist := func(status models.DatabaseStatus) (models.DatabaseStatus, error) {
		db.Status = status
		return status, s.store.UpdateDatabase(db)
	}
	if !safeResourceID.MatchString(id) {
		return db.Status, fmt.Errorf("invalid database identity")
	}
	containerName := "gopulse-" + id
	if db.Type == "appwrite" {
		containerName += "-appwrite"
	}
	if db.Type == "sqlite" {
		path := shellQuote("/var/lib/gopulse/databases/" + id + "/sqlite.db")
		out, err := s.manager.Run(ctx, db.ServerID, "if [ -f "+path+" ]; then echo exists; else echo missing; fi")
		if err != nil {
			return db.Status, err
		}
		switch strings.TrimSpace(out) {
		case "exists":
			return persist(models.DatabaseStatusRunning)
		case "missing":
			return persist(models.DatabaseStatusError)
		default:
			return db.Status, fmt.Errorf("unexpected SQLite health response")
		}
	}
	out, err := s.manager.Run(ctx, db.ServerID, "docker inspect -f '{{.State.Running}}' "+shellQuote(containerName))
	if err != nil {
		return db.Status, err
	}
	switch strings.TrimSpace(out) {
	case "false":
		return persist(models.DatabaseStatusStopped)
	case "true":
	default:
		return db.Status, fmt.Errorf("unexpected container health response")
	}
	if db.Type == "qdrant" {
		if _, err := s.QdrantCollections(ctx, id); err != nil {
			return db.Status, err
		}
		return persist(models.DatabaseStatusRunning)
	}
	var healthCommand string
	switch db.Type {
	case "mysql":
		healthCommand = mysqlExecCommand(containerName, db, "", "--batch --skip-column-names", "SELECT 1;", "")
	case "postgres":
		healthCommand = "docker exec -e PGPASSWORD=" + shellQuote(db.Password) + " " + shellQuote(containerName) + " psql -U " + shellQuote(db.Username) + " -d " + shellQuote(db.DatabaseName) + " -tAqc 'SELECT 1'"
	case "mongodb":
		healthCommand = "docker exec " + shellQuote(containerName) + " mongosh --quiet --username " + shellQuote(db.Username) + " --password " + shellQuote(db.Password) + " --authenticationDatabase admin --eval 'db.adminCommand({ping:1})'"
	case "redis":
		healthCommand = "docker exec -e REDISCLI_AUTH=" + shellQuote(db.Password) + " " + shellQuote(containerName) + " redis-cli ping 2>/dev/null | grep -qx PONG"
	case "pocketbase":
		healthCommand = "docker exec " + shellQuote(containerName) + " nc -z 127.0.0.1 8090"
	case "appwrite":
		healthCommand = "docker exec " + shellQuote(containerName) + " nc -z 127.0.0.1 80"
	default:
		return db.Status, fmt.Errorf("unsupported database health check")
	}
	// Credentials are not echoed to the browser. A command failure is reported as
	// a verification failure rather than a fabricated persisted lifecycle state.
	if _, err := s.manager.Run(ctx, db.ServerID, healthCommand+" >/dev/null 2>&1"); err != nil {
		return db.Status, fmt.Errorf("database connection check failed: %s", redactSecret(err.Error(), db.Password))
	}
	return persist(models.DatabaseStatusRunning)
}

// StartDatabase boots up a database container
func (s *DatabaseService) StartDatabase(ctx context.Context, id string) error {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}

	if db.Type == "sqlite" {
		return nil
	}

	if db.Type == "qdrant" {
		return s.QdrantLifecycle(ctx, id, "start")
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return err
	}

	if db.Type == "appwrite" {
		volumePath := fmt.Sprintf("/var/lib/gopulse/databases/%s", db.ID)
		_, err = client.RunCommandContext(ctx, fmt.Sprintf("docker compose -f %s/docker-compose.yml start", volumePath))
		if err != nil {
			return err
		}
		db.Status = models.DatabaseStatusStarting
		s.store.UpdateDatabase(db)
		go func() {
			time.Sleep(3 * time.Second)
			s.UpdateStatus(context.Background(), id)
		}()
		return nil
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)
	_, err = client.RunCommandContext(ctx, fmt.Sprintf("docker start %s", containerName))
	if err != nil {
		return err
	}

	db.Status = models.DatabaseStatusStarting
	s.store.UpdateDatabase(db)

	go func() {
		time.Sleep(3 * time.Second)
		s.UpdateStatus(context.Background(), id)
	}()

	return nil
}

// StopDatabase halts a database container
func (s *DatabaseService) StopDatabase(ctx context.Context, id string) error {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}

	if db.Type == "sqlite" {
		return nil
	}

	if db.Type == "qdrant" {
		return s.QdrantLifecycle(ctx, id, "stop")
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return err
	}

	if db.Type == "appwrite" {
		volumePath := fmt.Sprintf("/var/lib/gopulse/databases/%s", db.ID)
		_, err = client.RunCommandContext(ctx, fmt.Sprintf("docker compose -f %s/docker-compose.yml stop", volumePath))
		if err != nil {
			return err
		}
		db.Status = models.DatabaseStatusStopped
		s.store.UpdateDatabase(db)
		return nil
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)
	_, err = client.RunCommandContext(ctx, fmt.Sprintf("docker stop %s", containerName))
	if err != nil {
		return err
	}

	db.Status = models.DatabaseStatusStopped
	s.store.UpdateDatabase(db)
	return nil
}

// RestartDatabase reboots a database container
func (s *DatabaseService) RestartDatabase(ctx context.Context, id string) error {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}

	if db.Type == "sqlite" {
		return nil
	}

	if db.Type == "qdrant" {
		return s.QdrantLifecycle(ctx, id, "restart")
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return err
	}

	if db.Type == "appwrite" {
		volumePath := fmt.Sprintf("/var/lib/gopulse/databases/%s", db.ID)
		_, err = client.RunCommandContext(ctx, fmt.Sprintf("docker compose -f %s/docker-compose.yml restart", volumePath))
		if err != nil {
			return err
		}
		db.Status = models.DatabaseStatusStarting
		s.store.UpdateDatabase(db)
		go func() {
			time.Sleep(8 * time.Second)
			s.UpdateStatus(context.Background(), id)
		}()
		return nil
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)
	_, err = client.RunCommandContext(ctx, fmt.Sprintf("docker restart %s", containerName))
	if err != nil {
		return err
	}

	db.Status = models.DatabaseStatusStarting
	s.store.UpdateDatabase(db)

	go func() {
		time.Sleep(4 * time.Second)
		s.UpdateStatus(context.Background(), id)
	}()

	return nil
}

// DeleteDatabase removes a database container and metadata, optionally purging volumes
func (s *DatabaseService) DeleteDatabase(ctx context.Context, id string, deleteVolume bool) error {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}
	if !safeResourceID.MatchString(db.ID) {
		return fmt.Errorf("invalid database ID")
	}
	if db.Type == "qdrant" {
		return s.DeleteQdrant(ctx, id, deleteVolume)
	}
	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return err
	}
	dir := "/var/lib/gopulse/databases/" + db.ID
	cmd := "docker rm -f " + shellQuote("gopulse-"+db.ID)
	if db.Type == "sqlite" {
		cmd = "test -d " + shellQuote(dir)
	}
	if db.Type == "appwrite" {
		cmd = "docker compose -f " + shellQuote(dir+"/docker-compose.yml") + " down --remove-orphans"
	}
	if _, err := client.RunCommandContext(ctx, cmd); err != nil {
		return fmt.Errorf("remote removal failed; metadata retained: %w", err)
	}
	if deleteVolume {
		if _, err := client.RunCommandContext(ctx, "sudo -n rm -rf -- "+shellQuote(dir)+" "+shellQuote("/var/lib/gopulse/backups/"+db.ID)); err != nil {
			return fmt.Errorf("data removal failed; metadata retained: %w", err)
		}
	}
	return s.store.RemoveDatabase(id)
}

// ExecuteQuery runs a SQL/NoSQL instruction in the container and returns clean formatted rows
func (s *DatabaseService) ExecuteQuery(ctx context.Context, id string, schema string, query string) (interface{}, error) {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return nil, err
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return nil, err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)

	// Escape double-quotes inside the query for shell safety
	safeQuery := strings.ReplaceAll(query, `"`, `\"`)
	safeQuery = strings.ReplaceAll(safeQuery, `$`, `\$`)

	targetSchema := schema
	if targetSchema == "" {
		targetSchema = db.DatabaseName
	}
	if targetSchema == "" && db.Type == "mongodb" {
		targetSchema = "test"
	}

	var execCmd string
	switch db.Type {
	case "mysql":
		// MySQL tabular output with -B (batch/tab-separated)
		execCmd = mysqlExecCommand(containerName, db, targetSchema, "-B", query, "2>&1")
	case "postgres":
		// For SELECT queries, use --csv for clean parseable output
		// For non-SELECT (INSERT/UPDATE/CREATE/DROP), use plain -c
		trimmedUpper := strings.ToUpper(strings.TrimSpace(query))
		if strings.HasPrefix(trimmedUpper, "SELECT") || strings.HasPrefix(trimmedUpper, "WITH") || strings.HasPrefix(trimmedUpper, "TABLE") || strings.HasPrefix(trimmedUpper, "VALUES") {
			execCmd = fmt.Sprintf(
				`docker exec %s psql -U %s -d %s --csv -c "%s" 2>&1`,
				containerName, db.Username, targetSchema, safeQuery,
			)
		} else {
			execCmd = fmt.Sprintf(
				`docker exec %s psql -U %s -d %s -c "%s" 2>&1`,
				containerName, db.Username, targetSchema, safeQuery,
			)
		}
	case "sqlite":
		trimmedUpper := strings.ToUpper(strings.TrimSpace(query))
		if strings.HasPrefix(trimmedUpper, "SELECT") || strings.HasPrefix(trimmedUpper, "WITH") || strings.HasPrefix(trimmedUpper, "PRAGMA") {
			execCmd = fmt.Sprintf(
				`sqlite3 -header -csv /var/lib/gopulse/databases/%s/sqlite.db "%s" 2>&1`,
				db.ID, safeQuery,
			)
		} else {
			execCmd = fmt.Sprintf(
				`sqlite3 /var/lib/gopulse/databases/%s/sqlite.db "%s" 2>&1`,
				db.ID, safeQuery,
			)
		}
	case "pocketbase":
		trimmedUpper := strings.ToUpper(strings.TrimSpace(query))
		if strings.HasPrefix(trimmedUpper, "SELECT") || strings.HasPrefix(trimmedUpper, "WITH") || strings.HasPrefix(trimmedUpper, "PRAGMA") {
			execCmd = fmt.Sprintf(
				`sqlite3 -header -csv /var/lib/gopulse/databases/%s/pb_data/data.db "%s" 2>&1`,
				db.ID, safeQuery,
			)
		} else {
			execCmd = fmt.Sprintf(
				`sqlite3 /var/lib/gopulse/databases/%s/pb_data/data.db "%s" 2>&1`,
				db.ID, safeQuery,
			)
		}
	case "mongodb":
		escapedEval := "'" + strings.ReplaceAll(query, "'", "'\\''") + "'"
		execCmd = fmt.Sprintf(
			`docker exec %s mongosh --quiet -u %s -p %s --authenticationDatabase admin %s --eval %s 2>&1`,
			containerName, db.Username, db.Password, targetSchema, escapedEval,
		)
	case "redis":
		dbNum := "0"
		if strings.HasPrefix(targetSchema, "db") {
			dbNum = strings.TrimPrefix(targetSchema, "db")
		}
		// Run command using redis-cli
		escapedPassword := "'" + strings.ReplaceAll(db.Password, "'", "'\\''") + "'"
		execCmd = fmt.Sprintf(
			`docker exec -e REDISCLI_AUTH=%s %s redis-cli -n %s %s 2>&1`,
			escapedPassword, containerName, dbNum, query,
		)
	default:
		return nil, fmt.Errorf("unsupported database engine: %s", db.Type)
	}

	output, err := client.RunCommandContext(ctx, execCmd)
	if err != nil {
		return nil, fmt.Errorf("execution error: %s", output)
	}

	// Parse results into clean standard format
	return parseQueryResult(db.Type, output)
}

// parseQueryResult parses different CLI database logs into uniform JSON structures
func parseQueryResult(dbType string, output string) (interface{}, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return map[string]interface{}{"message": "Query completed successfully. Empty result set."}, nil
	}

	// Check if output is a raw error message
	if strings.Contains(output, "ERROR:") || strings.Contains(output, "Error:") || strings.Contains(output, "Access denied") {
		return nil, fmt.Errorf("%s", output)
	}

	switch dbType {
	case "mysql":
		// Parse MySQL Tab-separated values
		lines := strings.Split(output, "\n")
		if len(lines) == 0 {
			return map[string]interface{}{"message": "Empty result set."}, nil
		}

		headers := strings.Split(lines[0], "\t")
		var rows [][]string
		for _, line := range lines[1:] {
			if line == "" {
				continue
			}
			rows = append(rows, strings.Split(line, "\t"))
		}

		return map[string]interface{}{
			"type":    "table",
			"columns": headers,
			"rows":    rows,
		}, nil

	case "postgres", "sqlite", "pocketbase":
		// Parse PostgreSQL / SQLite CSV values
		r := csv.NewReader(strings.NewReader(output))
		records, err := r.ReadAll()
		if err != nil {
			// Fallback to raw output if CSV parsing fails
			return map[string]interface{}{
				"type": "raw",
				"data": output,
			}, nil
		}

		if len(records) == 0 {
			return map[string]interface{}{"message": "Empty result set."}, nil
		}

		headers := records[0]
		var rows [][]string
		if len(records) > 1 {
			rows = records[1:]
		}

		return map[string]interface{}{
			"type":    "table",
			"columns": headers,
			"rows":    rows,
		}, nil

	case "mongodb":
		// MongoDB output is raw BSON/JSON records
		return map[string]interface{}{
			"type": "raw",
			"data": output,
		}, nil
	case "redis":
		// Redis output is raw lines
		return map[string]interface{}{
			"type": "raw",
			"data": output,
		}, nil
	}

	return output, nil
}

// CreateBackup takes a live dump of the remote database and saves it to a persistent archive folder
func (s *DatabaseService) CreateBackup(ctx context.Context, id string) (*models.DatabaseBackup, error) {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return nil, err
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return nil, err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)
	backupDir := fmt.Sprintf("/var/lib/gopulse/backups/%s", db.ID)

	// Create backup folder on host with sudo and assign ownership to the SSH user
	_, err = client.RunCommandContext(ctx, fmt.Sprintf("sudo mkdir -p %[1]s && sudo chown -R $(whoami):$(whoami) %[1]s", backupDir))
	if err != nil {
		return nil, fmt.Errorf("failed to create host backup folder: %w", err)
	}

	backupName := fmt.Sprintf("backup-%s-%s.sql.gz", db.ID, time.Now().Format("20060102-150405"))
	if db.Type == "mongodb" {
		backupName = fmt.Sprintf("backup-%s-%s.archive.gz", db.ID, time.Now().Format("20060102-150405"))
	} else if db.Type == "redis" {
		backupName = fmt.Sprintf("backup-%s-%s.rdb.gz", db.ID, time.Now().Format("20060102-150405"))
	} else if db.Type == "sqlite" {
		backupName = fmt.Sprintf("backup-%s-%s.db.gz", db.ID, time.Now().Format("20060102-150405"))
	} else if db.Type == "pocketbase" {
		backupName = fmt.Sprintf("backup-%s-%s.tar.gz", db.ID, time.Now().Format("20060102-150405"))
	}

	var dumpCmd string
	switch db.Type {
	case "mysql":
		dumpCmd = fmt.Sprintf(
			"docker exec -e MYSQL_PWD=%s %s mysqldump --default-character-set=%s --user=%s --single-transaction --quick %s | gzip > %s",
			shellQuote(db.Password), shellQuote(containerName), mysqlDefaultCharacterSet, shellQuote(mysqlInternalAdminUser), shellQuote(db.DatabaseName), shellQuote(backupDir+"/"+backupName),
		)
	case "postgres":
		dumpCmd = fmt.Sprintf(
			"docker exec %s pg_dump -U %s -d %s --clean --if-exists | gzip > %s/%s",
			containerName, db.Username, db.DatabaseName, backupDir, backupName,
		)
	case "mongodb":
		dumpCmd = fmt.Sprintf(
			"docker exec %s mongodump --username %s --password %s --authenticationDatabase admin --archive | gzip > %s/%s",
			containerName, db.Username, db.Password, backupDir, backupName,
		)
	case "redis":
		escapedPassword := "'" + strings.ReplaceAll(db.Password, "'", "'\\''") + "'"
		dumpCmd = fmt.Sprintf(
			"docker exec -e REDISCLI_AUTH=%s %s redis-cli SAVE && gzip -c /var/lib/gopulse/databases/%s/dump.rdb > %s/%s",
			escapedPassword, containerName, db.ID, backupDir, backupName,
		)
	case "sqlite":
		volumePath := fmt.Sprintf("/var/lib/gopulse/databases/%s", db.ID)
		dumpCmd = fmt.Sprintf(
			"gzip -c %s/sqlite.db > %s/%s",
			volumePath, backupDir, backupName,
		)
	case "pocketbase":
		volumePath := fmt.Sprintf("/var/lib/gopulse/databases/%s", db.ID)
		dumpCmd = fmt.Sprintf(
			"tar -czf %s/%s -C %s pb_data",
			backupDir, backupName, volumePath,
		)
	case "appwrite":
		dumpCmd = fmt.Sprintf(
			"docker exec %s-mariadb mysqldump -uappwrite -p%s appwrite | gzip > %s/%s",
			containerName, db.Password, backupDir, backupName,
		)
	}

	output, err := client.RunCommandContext(ctx, dumpCmd)
	if err != nil {
		return nil, fmt.Errorf("database dump failed: %s (err: %w)", output, err)
	}

	// Retrieve file size from remote host
	sizeCmd := fmt.Sprintf("stat -c%%s %s/%s", backupDir, backupName)
	sizeOut, err := client.RunCommandContext(ctx, sizeCmd)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve remote backup size: %w", err)
	}

	size, _ := strconv.ParseInt(strings.TrimSpace(sizeOut), 10, 64)

	return &models.DatabaseBackup{
		Filename:  backupName,
		Size:      size,
		CreatedAt: time.Now(),
	}, nil
}

// ListBackups scans the backups directory on the remote host and compiles an active list of archives
func (s *DatabaseService) ListBackups(ctx context.Context, id string) ([]models.DatabaseBackup, error) {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return nil, err
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return nil, err
	}

	backupDir := fmt.Sprintf("/var/lib/gopulse/backups/%s", db.ID)

	// Ensure folder exists, created with sudo and assigned ownership
	client.RunCommandContext(ctx, fmt.Sprintf("sudo mkdir -p %[1]s && sudo chown -R $(whoami):$(whoami) %[1]s", backupDir))

	// Search file listing using tabular output format (Filename, Size, Timestamp)
	findCmd := fmt.Sprintf("find %s -type f -name 'backup-*' -printf '%%f\\t%%s\\t%%T@\\n' 2>/dev/null", backupDir)
	output, err := client.RunCommandContext(ctx, findCmd)
	if err != nil {
		return nil, err
	}

	var backups []models.DatabaseBackup
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}

		filename := parts[0]
		size, _ := strconv.ParseInt(parts[1], 10, 64)
		tsFloat, _ := strconv.ParseFloat(parts[2], 64)
		createdAt := time.Unix(int64(tsFloat), 0)

		backups = append(backups, models.DatabaseBackup{
			Filename:  filename,
			Size:      size,
			CreatedAt: createdAt,
		})
	}

	// Sort backups with newest first
	for i := 0; i < len(backups)/2; i++ {
		j := len(backups) - i - 1
		backups[i], backups[j] = backups[j], backups[i]
	}

	return backups, nil
}

// RestoreBackup restores a database snapshot from a remote file into the live container
func (s *DatabaseService) RestoreBackup(ctx context.Context, id string, filename string) error {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)
	backupDir := fmt.Sprintf("/var/lib/gopulse/backups/%s", db.ID)
	backupPath := fmt.Sprintf("%s/%s", backupDir, filename)

	// Confirm that the file exists on host
	checkCmd := fmt.Sprintf("[ -f %s ] && echo 'exists' || echo 'missing'", shellQuote(backupPath))
	checkOut, _ := client.RunCommandContext(ctx, checkCmd)
	if strings.TrimSpace(checkOut) != "exists" {
		return fmt.Errorf("backup file %s does not exist on remote host", filename)
	}

	isGzipped := strings.HasSuffix(filename, ".gz")

	var restoreCmd string
	switch db.Type {
	case "mysql":
		if isGzipped {
			restoreCmd = fmt.Sprintf(
				"gzip -d -c %s | docker exec -i -e MYSQL_PWD=%s %s mysql --default-character-set=%s --user=%s --database=%s 2>&1",
				shellQuote(backupPath), shellQuote(db.Password), shellQuote(containerName), mysqlDefaultCharacterSet, shellQuote(mysqlInternalAdminUser), shellQuote(db.DatabaseName),
			)
		} else {
			restoreCmd = fmt.Sprintf(
				"cat %s | docker exec -i -e MYSQL_PWD=%s %s mysql --default-character-set=%s --user=%s --database=%s 2>&1",
				shellQuote(backupPath), shellQuote(db.Password), shellQuote(containerName), mysqlDefaultCharacterSet, shellQuote(mysqlInternalAdminUser), shellQuote(db.DatabaseName),
			)
		}
	case "postgres":
		if isGzipped {
			restoreCmd = fmt.Sprintf(
				"gzip -d -c %s | docker exec -i %s psql -U %s -d %s 2>&1",
				backupPath, containerName, db.Username, db.DatabaseName,
			)
		} else {
			restoreCmd = fmt.Sprintf(
				"cat %s | docker exec -i %s psql -U %s -d %s 2>&1",
				backupPath, containerName, db.Username, db.DatabaseName,
			)
		}
	case "mongodb":
		if isGzipped {
			restoreCmd = fmt.Sprintf(
				"gzip -d -c %s | docker exec -i %s mongorestore --username %s --password %s --authenticationDatabase admin --archive --nsExclude=\"admin.system.*\" --drop 2>&1",
				backupPath, containerName, db.Username, db.Password,
			)
		} else {
			restoreCmd = fmt.Sprintf(
				"cat %s | docker exec -i %s mongorestore --username %s --password %s --authenticationDatabase admin --archive --nsExclude=\"admin.system.*\" --drop 2>&1",
				backupPath, containerName, db.Username, db.Password,
			)
		}
	case "redis":
		if isGzipped {
			restoreCmd = fmt.Sprintf(
				"gzip -d -c %s > /var/lib/gopulse/databases/%s/dump.rdb && docker restart %s 2>&1",
				backupPath, db.ID, containerName,
			)
		} else {
			restoreCmd = fmt.Sprintf(
				"cat %s > /var/lib/gopulse/databases/%s/dump.rdb && docker restart %s 2>&1",
				backupPath, db.ID, containerName,
			)
		}
	case "sqlite":
		volumePath := fmt.Sprintf("/var/lib/gopulse/databases/%s", db.ID)
		if isGzipped {
			restoreCmd = fmt.Sprintf(
				"gzip -d -c %s > %s/sqlite.db 2>&1",
				backupPath, volumePath,
			)
		} else {
			restoreCmd = fmt.Sprintf(
				"cat %s > %s/sqlite.db 2>&1",
				backupPath, volumePath,
			)
		}
	case "pocketbase":
		volumePath := fmt.Sprintf("/var/lib/gopulse/databases/%s", db.ID)
		if isGzipped {
			restoreCmd = fmt.Sprintf(
				"docker stop %s && rm -rf %s/pb_data && tar -xzf %s -C %s && docker start %s 2>&1",
				containerName, volumePath, backupPath, volumePath, containerName,
			)
		} else {
			restoreCmd = fmt.Sprintf(
				"docker stop %s && rm -rf %s/pb_data && tar -xf %s -C %s && docker start %s 2>&1",
				containerName, volumePath, backupPath, volumePath, containerName,
			)
		}
	case "appwrite":
		if isGzipped {
			restoreCmd = fmt.Sprintf(
				"gzip -d -c %s | docker exec -i %s-mariadb mysql -uappwrite -p%s appwrite 2>&1",
				backupPath, containerName, db.Password,
			)
		} else {
			restoreCmd = fmt.Sprintf(
				"cat %s | docker exec -i %s-mariadb mysql -uappwrite -p%s appwrite 2>&1",
				backupPath, containerName, db.Password,
			)
		}
	}

	output, err := client.RunCommandContext(ctx, restoreCmd)
	if err != nil {
		return fmt.Errorf("database restore failed: %s (err: %w)", output, err)
	}

	return nil
}

// SetAutoBackup updates the automatic backup status and retention period for a database
func (s *DatabaseService) SetAutoBackup(id string, enabled bool, retentionDays int) error {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}

	db.AutoBackup = enabled
	db.RetentionDays = retentionDays

	return s.store.UpdateDatabase(db)
}

// PurgeOldBackups scans the remote backups directory and deletes any backups older than the retention limit
func (s *DatabaseService) PurgeOldBackups(ctx context.Context, id string, retentionDays int) error {
	if retentionDays <= 0 {
		return nil // keep forever
	}

	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return err
	}

	backupDir := fmt.Sprintf("/var/lib/gopulse/backups/%s", db.ID)
	purgeCmd := fmt.Sprintf("find %s -type f -name 'backup-*' -mtime +%d -delete", backupDir, retentionDays)
	_, err = client.RunCommandContext(ctx, purgeCmd)
	if err != nil {
		return fmt.Errorf("failed to purge old backups: %w", err)
	}

	return nil
}

// =============================================================================
// Explorer — Schema / Table / Row browsing
// =============================================================================

// ListSchemas returns all databases/schemas inside the container
func (s *DatabaseService) ListSchemas(ctx context.Context, id string) ([]string, error) {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return nil, err
	}

	if db.Type == "redis" {
		var schemas []string
		for i := 0; i < 16; i++ {
			schemas = append(schemas, fmt.Sprintf("db%d", i))
		}
		return schemas, nil
	}

	if db.Type == "sqlite" || db.Type == "pocketbase" {
		return []string{"main"}, nil
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return nil, err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)

	var cmd string
	switch db.Type {
	case "mysql":
		cmd = mysqlExecCommand(containerName, db, "", "-B -N", "SHOW DATABASES;", "2>/dev/null")
	case "postgres":
		cmd = fmt.Sprintf(
			`docker exec %s psql -U %s -d %s -t -A -c "SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname;" 2>/dev/null`,
			containerName, db.Username, db.DatabaseName,
		)
	case "mongodb":
		cmd = fmt.Sprintf(
			`docker exec %s mongosh --quiet -u %s -p %s --authenticationDatabase admin --eval "db.adminCommand('listDatabases').databases.map(d => d.name).join('\n')" 2>/dev/null`,
			containerName, db.Username, db.Password,
		)
	default:
		return nil, fmt.Errorf("unsupported engine: %s", db.Type)
	}

	output, err := client.RunCommandContext(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to list schemas: %w", err)
	}

	var schemas []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			schemas = append(schemas, line)
		}
	}

	return schemas, nil
}

// ListTables returns all tables/collections inside a specific schema/database
func (s *DatabaseService) ListTables(ctx context.Context, id string, schema string) ([]string, error) {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return nil, err
	}

	if db.Type == "redis" {
		return []string{"Keys"}, nil
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return nil, err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)

	var cmd string
	switch db.Type {
	case "mysql":
		cmd = mysqlExecCommand(containerName, db, schema, "-B -N", "SHOW TABLES;", "2>/dev/null")
	case "postgres":
		cmd = fmt.Sprintf(
			`docker exec %s psql -U %s -d %s -t -A -c "SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename;" 2>/dev/null`,
			containerName, db.Username, schema,
		)
	case "sqlite":
		cmd = fmt.Sprintf(
			`sqlite3 /var/lib/gopulse/databases/%s/sqlite.db "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%%' ORDER BY name;" 2>/dev/null`,
			db.ID,
		)
	case "pocketbase":
		cmd = fmt.Sprintf(
			`sqlite3 /var/lib/gopulse/databases/%s/pb_data/data.db "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%%' AND name NOT LIKE '_%%' ORDER BY name;" 2>/dev/null`,
			db.ID,
		)
	case "mongodb":
		cmd = fmt.Sprintf(
			`docker exec %s mongosh --quiet -u %s -p %s --authenticationDatabase admin %s --eval "db.getCollectionNames().join('\n')" 2>/dev/null`,
			containerName, db.Username, db.Password, schema,
		)
	default:
		return nil, fmt.Errorf("unsupported engine: %s", db.Type)
	}

	output, err := client.RunCommandContext(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}

	var tables []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			tables = append(tables, line)
		}
	}

	return tables, nil
}

// ColumnInfo represents a table column descriptor
type ColumnInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// DescribeTable returns column metadata for a specific table
func (s *DatabaseService) DescribeTable(ctx context.Context, id string, schema string, table string) ([]ColumnInfo, error) {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return nil, err
	}

	if db.Type == "redis" {
		return []ColumnInfo{
			{Name: "key", Type: "string"},
			{Name: "type", Type: "string"},
			{Name: "value", Type: "string"},
			{Name: "ttl", Type: "integer"},
		}, nil
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return nil, err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)

	var cmd string
	switch db.Type {
	case "mysql":
		query := fmt.Sprintf(
			"SELECT COLUMN_NAME, COLUMN_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=%s AND TABLE_NAME=%s ORDER BY ORDINAL_POSITION;",
			sqlStringLiteral(schema), sqlStringLiteral(table),
		)
		cmd = mysqlExecCommand(containerName, db, schema, "-B -N", query, "2>/dev/null")
	case "postgres":
		cmd = fmt.Sprintf(
			`docker exec %s psql -U %s -d %s -t -A -F '	' -c "SELECT column_name, data_type FROM information_schema.columns WHERE table_schema = 'public' AND table_name = '%s' ORDER BY ordinal_position;" 2>/dev/null`,
			containerName, db.Username, schema, table,
		)
	case "sqlite":
		cmd = fmt.Sprintf(
			`sqlite3 -tabs /var/lib/gopulse/databases/%s/sqlite.db "PRAGMA table_info('%s');" 2>/dev/null`,
			db.ID, table,
		)
	case "pocketbase":
		cmd = fmt.Sprintf(
			`sqlite3 -tabs /var/lib/gopulse/databases/%s/pb_data/data.db "PRAGMA table_info('%s');" 2>/dev/null`,
			db.ID, table,
		)
	case "mongodb":
		// MongoDB is schema-less; sample one document to get field names
		cmd = fmt.Sprintf(
			`docker exec %s mongosh --quiet -u %s -p %s --authenticationDatabase admin %s --eval "const doc = db.getCollection('%s').findOne(); if(doc) { Object.keys(doc).forEach(k => print(k + '\t' + typeof doc[k])) } else { print('_id\tobjectId') }" 2>/dev/null`,
			containerName, db.Username, db.Password, schema, table,
		)
	default:
		return nil, fmt.Errorf("unsupported engine: %s", db.Type)
	}

	output, err := client.RunCommandContext(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to describe table: %w", err)
	}

	var columns []ColumnInfo
	if db.Type == "sqlite" || db.Type == "pocketbase" {
		for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Split(line, "\t")
			if len(parts) > 2 {
				columns = append(columns, ColumnInfo{
					Name: parts[1],
					Type: parts[2],
				})
			}
		}
		return columns, nil
	}

	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		col := ColumnInfo{Name: parts[0]}
		if len(parts) > 1 {
			col.Type = parts[1]
		}
		columns = append(columns, col)
	}

	return columns, nil
}

// TableDataResult holds paginated row data
type TableDataResult struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
	Total   int64      `json:"total"`
	Page    int        `json:"page"`
	Limit   int        `json:"limit"`
}

// BrowseTableData returns paginated rows/documents from a table/collection
func (s *DatabaseService) BrowseTableData(ctx context.Context, id string, schema string, table string, page int, limit int) (*TableDataResult, error) {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return nil, err
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return nil, err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)

	if db.Type == "redis" {
		dbNum := "0"
		if strings.HasPrefix(schema, "db") {
			dbNum = strings.TrimPrefix(schema, "db")
		}

		if page < 1 {
			page = 1
		}
		if limit <= 0 || limit > 200 {
			limit = 50
		}
		offset := (page - 1) * limit

		// 1. Get total list of keys
		escapedPassword := "'" + strings.ReplaceAll(db.Password, "'", "'\\''") + "'"
		keysListCmd := fmt.Sprintf(`docker exec -e REDISCLI_AUTH=%s %s redis-cli -n %s KEYS "*" 2>/dev/null`, escapedPassword, containerName, dbNum)
		keysListOut, err := client.RunCommandContext(ctx, keysListCmd)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch keys: %w", err)
		}

		var allKeys []string
		for _, line := range strings.Split(strings.TrimSpace(keysListOut), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.Contains(line, "Warning:") {
				allKeys = append(allKeys, line)
			}
		}

		total := int64(len(allKeys))

		start := offset
		if start > len(allKeys) {
			start = len(allKeys)
		}
		end := offset + limit
		if end > len(allKeys) {
			end = len(allKeys)
		}
		pageKeys := allKeys[start:end]

		result := &TableDataResult{
			Columns: []string{"key", "type", "value", "ttl"},
			Rows:    [][]string{},
			Total:   total,
			Page:    page,
			Limit:   limit,
		}

		if len(pageKeys) > 0 {
			var keyCmds []string
			for _, k := range pageKeys {
				// Construct a shell-safe command slice for this key
				cmdPart := fmt.Sprintf(
					`t=$(redis-cli -n %[1]s TYPE "%[2]s" 2>/dev/null); `+
						`ttl=$(redis-cli -n %[1]s TTL "%[2]s" 2>/dev/null); `+
						`if [ "$t" = "string" ]; then v=$(redis-cli -n %[1]s GET "%[2]s" 2>/dev/null); `+
						`elif [ "$t" = "hash" ]; then v=$(redis-cli -n %[1]s HGETALL "%[2]s" 2>/dev/null | paste -sd " " -); `+
						`else v=$(redis-cli -n %[1]s LRANGE "%[2]s" 0 -1 2>/dev/null | paste -sd "," -); fi; `+
						`printf "%%s\t%%s\t%%s\t%%s\n" "%[2]s" "$t" "$v" "$ttl"`,
					dbNum, k,
				)
				keyCmds = append(keyCmds, cmdPart)
			}

			dataCmd := fmt.Sprintf(`docker exec -e REDISCLI_AUTH=%s %s sh -c '%s'`, escapedPassword, containerName, strings.Join(keyCmds, "; "))
			dataOut, err := client.RunCommandContext(ctx, dataCmd)
			if err == nil {
				for _, line := range strings.Split(strings.TrimSpace(dataOut), "\n") {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					parts := strings.Split(line, "\t")
					if len(parts) >= 4 {
						result.Rows = append(result.Rows, []string{parts[0], parts[1], parts[2], parts[3]})
					} else if len(parts) == 3 {
						result.Rows = append(result.Rows, []string{parts[0], parts[1], parts[2], "-1"})
					} else if len(parts) == 2 {
						result.Rows = append(result.Rows, []string{parts[0], parts[1], "", "-1"})
					}
				}
			}
		}

		return result, nil
	}

	if page < 1 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := (page - 1) * limit

	// First, get total row count
	var countCmd string
	switch db.Type {
	case "mysql":
		countCmd = mysqlExecCommand(containerName, db, schema, "-B -N", fmt.Sprintf("SELECT COUNT(*) FROM %s;", mysqlIdentifier(table)), "2>/dev/null")
	case "postgres":
		countCmd = fmt.Sprintf(
			`docker exec %s psql -U %s -d %s -t -A -c "SELECT COUNT(*) FROM \"%s\";" 2>/dev/null`,
			containerName, db.Username, schema, table,
		)
	case "sqlite":
		countCmd = fmt.Sprintf(
			`sqlite3 /var/lib/gopulse/databases/%s/sqlite.db "SELECT COUNT(*) FROM \"%s\";" 2>/dev/null`,
			db.ID, table,
		)
	case "pocketbase":
		countCmd = fmt.Sprintf(
			`sqlite3 /var/lib/gopulse/databases/%s/pb_data/data.db "SELECT COUNT(*) FROM \"%s\";" 2>/dev/null`,
			db.ID, table,
		)
	case "mongodb":
		countCmd = fmt.Sprintf(
			`docker exec %s mongosh --quiet -u %s -p %s --authenticationDatabase admin %s --eval "db.getCollection('%s').countDocuments()" 2>/dev/null`,
			containerName, db.Username, db.Password, schema, table,
		)
	}

	countOut, _ := client.RunCommandContext(ctx, countCmd)
	total, _ := strconv.ParseInt(strings.TrimSpace(countOut), 10, 64)

	// Then, fetch paginated rows
	var dataCmd string
	switch db.Type {
	case "mysql":
		dataCmd = mysqlExecCommand(containerName, db, schema, "-B", fmt.Sprintf("SELECT * FROM %s LIMIT %d OFFSET %d;", mysqlIdentifier(table), limit, offset), "2>/dev/null")
	case "postgres":
		dataCmd = fmt.Sprintf(
			`docker exec %s psql -U %s -d %s --csv -c "SELECT * FROM \"%s\" LIMIT %d OFFSET %d;" 2>/dev/null`,
			containerName, db.Username, schema, table, limit, offset,
		)
	case "sqlite":
		dataCmd = fmt.Sprintf(
			`sqlite3 -header -csv /var/lib/gopulse/databases/%s/sqlite.db "SELECT * FROM \"%s\" LIMIT %d OFFSET %d;" 2>/dev/null`,
			db.ID, table, limit, offset,
		)
	case "pocketbase":
		dataCmd = fmt.Sprintf(
			`sqlite3 -header -csv /var/lib/gopulse/databases/%s/pb_data/data.db "SELECT * FROM \"%s\" LIMIT %d OFFSET %d;" 2>/dev/null`,
			db.ID, table, limit, offset,
		)
	case "mongodb":
		dataCmd = fmt.Sprintf(
			`docker exec %s mongosh --quiet -u %s -p %s --authenticationDatabase admin %s --eval "JSON.stringify(db.getCollection('%s').find().skip(%d).limit(%d).toArray())" 2>/dev/null`,
			containerName, db.Username, db.Password, schema, table, offset, limit,
		)
	default:
		return nil, fmt.Errorf("unsupported engine: %s", db.Type)
	}

	output, err := client.RunCommandContext(ctx, dataCmd)
	if err != nil {
		return nil, fmt.Errorf("failed to browse table data: %w", err)
	}

	result := &TableDataResult{
		Total: total,
		Page:  page,
		Limit: limit,
	}

	output = strings.TrimSpace(output)
	if output == "" {
		return result, nil
	}

	// Parse output based on engine type
	switch db.Type {
	case "mysql":
		// Tab-separated with header row
		lines := strings.Split(output, "\n")
		if len(lines) > 0 {
			result.Columns = strings.Split(lines[0], "\t")
			for _, line := range lines[1:] {
				if line == "" {
					continue
				}
				result.Rows = append(result.Rows, strings.Split(line, "\t"))
			}
		}

	case "postgres", "sqlite", "pocketbase":
		// CSV with header row
		r := csv.NewReader(strings.NewReader(output))
		records, csvErr := r.ReadAll()
		if csvErr != nil {
			// Fallback: treat as raw
			result.Columns = []string{"output"}
			result.Rows = [][]string{{output}}
			return result, nil
		}
		if len(records) > 0 {
			result.Columns = records[0]
			if len(records) > 1 {
				result.Rows = records[1:]
			}
		}

	case "mongodb":
		// JSON array — parse and flatten to columns + rows
		result.Columns, result.Rows = parseMongoJSONArray(output)
	}

	return result, nil
}

// parseMongoJSONArray takes a JSON array string and converts it to columns + rows
func parseMongoJSONArray(jsonStr string) ([]string, [][]string) {
	jsonStr = strings.TrimSpace(jsonStr)
	if jsonStr == "" || jsonStr == "[]" {
		return nil, nil
	}

	// Simple JSON array parsing using encoding/json
	var docs []map[string]interface{}
	if err := jsonUnmarshal([]byte(jsonStr), &docs); err != nil || len(docs) == 0 {
		// Fallback: return raw output
		return []string{"document"}, [][]string{{jsonStr}}
	}

	// Collect all unique keys across documents to build columns
	keyOrder := make([]string, 0)
	keySet := make(map[string]bool)
	for _, doc := range docs {
		for k := range doc {
			if !keySet[k] {
				keySet[k] = true
				keyOrder = append(keyOrder, k)
			}
		}
	}

	// Build rows
	var rows [][]string
	for _, doc := range docs {
		row := make([]string, len(keyOrder))
		for i, key := range keyOrder {
			if val, ok := doc[key]; ok {
				row[i] = fmt.Sprintf("%v", val)
			} else {
				row[i] = ""
			}
		}
		rows = append(rows, row)
	}

	return keyOrder, rows
}

// CreateSchema creates a new schema/database
func (s *DatabaseService) CreateSchema(ctx context.Context, id string, name string) error {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)

	var cmd string
	switch db.Type {
	case "mysql":
		cmd = mysqlExecCommand(containerName, db, "", "", fmt.Sprintf("CREATE DATABASE %s CHARACTER SET %s COLLATE %s;", mysqlIdentifier(name), mysqlDefaultCharacterSet, mysqlDefaultCollation), "2>&1")
	case "postgres":
		cmd = fmt.Sprintf("docker exec %s psql -U %s -d postgres -c \"CREATE DATABASE \\\"%s\\\";\" 2>&1", containerName, db.Username, name)
	case "mongodb":
		cmd = fmt.Sprintf("docker exec %s mongosh --quiet -u %s -p %s --authenticationDatabase admin %s --eval \"db.createCollection('init')\" 2>&1", containerName, db.Username, db.Password, name)
	case "redis", "sqlite", "pocketbase":
		// Static schemas, no active dynamic creation
		return nil
	default:
		return fmt.Errorf("unsupported database type: %s", db.Type)
	}

	output, err := client.RunCommandContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to create schema: %s (err: %w)", output, err)
	}

	return nil
}

// DeleteSchema deletes an existing schema/database
func (s *DatabaseService) DeleteSchema(ctx context.Context, id string, name string) error {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)

	var cmd string
	switch db.Type {
	case "mysql":
		cmd = mysqlExecCommand(containerName, db, "", "", fmt.Sprintf("DROP DATABASE %s;", mysqlIdentifier(name)), "2>&1")
	case "postgres":
		cmd = fmt.Sprintf("docker exec %s psql -U %s -d postgres -c \"DROP DATABASE \\\"%s\\\";\" 2>&1", containerName, db.Username, name)
	case "mongodb":
		cmd = fmt.Sprintf("docker exec %s mongosh --quiet -u %s -p %s --authenticationDatabase admin %s --eval \"db.dropDatabase()\" 2>&1", containerName, db.Username, db.Password, name)
	case "redis":
		dbNum := "0"
		if strings.HasPrefix(name, "db") {
			dbNum = strings.TrimPrefix(name, "db")
		}
		escapedPassword := "'" + strings.ReplaceAll(db.Password, "'", "'\\''") + "'"
		cmd = fmt.Sprintf("docker exec -e REDISCLI_AUTH=%s %s redis-cli -n %s FLUSHDB 2>&1", escapedPassword, containerName, dbNum)
	case "sqlite", "pocketbase":
		// Static schemas, no active dynamic dropping
		return nil
	default:
		return fmt.Errorf("unsupported database type: %s", db.Type)
	}

	output, err := client.RunCommandContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to delete schema: %s (err: %w)", output, err)
	}

	return nil
}

// CreateTable creates a new table or collection with a custom schema
func (s *DatabaseService) CreateTable(ctx context.Context, id string, schema string, name string, columns []models.TableColumn) error {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)

	var cmd string
	switch db.Type {
	case "mysql":
		var colDefs []string
		colDefs = append(colDefs, mysqlIdentifier("id")+" INT AUTO_INCREMENT PRIMARY KEY")
		for _, col := range columns {
			nullableStr := "NOT NULL"
			if col.Nullable {
				nullableStr = "NULL"
			}
			uniqueStr := ""
			if col.Unique {
				uniqueStr = "UNIQUE"
			}
			colDefs = append(colDefs, fmt.Sprintf("%s %s %s %s", mysqlIdentifier(col.Name), col.Type, nullableStr, uniqueStr))
		}
		query := fmt.Sprintf("CREATE TABLE %s (%s) DEFAULT CHARSET=%s COLLATE=%s;", mysqlIdentifier(name), strings.Join(colDefs, ", "), mysqlDefaultCharacterSet, mysqlDefaultCollation)
		cmd = mysqlExecCommand(containerName, db, schema, "", query, "2>&1")
	case "postgres":
		var colDefs []string
		colDefs = append(colDefs, "\"id\" SERIAL PRIMARY KEY")
		for _, col := range columns {
			nullableStr := "NOT NULL"
			if col.Nullable {
				nullableStr = "NULL"
			}
			uniqueStr := ""
			if col.Unique {
				uniqueStr = "UNIQUE"
			}
			colDefs = append(colDefs, fmt.Sprintf("\"%s\" %s %s %s", col.Name, col.Type, nullableStr, uniqueStr))
		}
		query := fmt.Sprintf("CREATE TABLE \"%s\" (%s);", name, strings.Join(colDefs, ", "))
		cmd = fmt.Sprintf("docker exec %s psql -U %s -d %s -c \"%s\" 2>&1", containerName, db.Username, schema, query)
	case "sqlite":
		var colDefs []string
		colDefs = append(colDefs, "\"id\" INTEGER PRIMARY KEY AUTOINCREMENT")
		for _, col := range columns {
			nullableStr := "NOT NULL"
			if col.Nullable {
				nullableStr = "NULL"
			}
			uniqueStr := ""
			if col.Unique {
				uniqueStr = "UNIQUE"
			}
			colDefs = append(colDefs, fmt.Sprintf("\"%s\" %s %s %s", col.Name, col.Type, nullableStr, uniqueStr))
		}
		query := fmt.Sprintf("CREATE TABLE \"%s\" (%s);", name, strings.Join(colDefs, ", "))
		cmd = fmt.Sprintf(`sqlite3 /var/lib/gopulse/databases/%s/sqlite.db "%s" 2>&1`, db.ID, query)
	case "pocketbase":
		var colDefs []string
		colDefs = append(colDefs, "\"id\" TEXT PRIMARY KEY")
		for _, col := range columns {
			nullableStr := "NOT NULL"
			if col.Nullable {
				nullableStr = "NULL"
			}
			uniqueStr := ""
			if col.Unique {
				uniqueStr = "UNIQUE"
			}
			colDefs = append(colDefs, fmt.Sprintf("\"%s\" %s %s %s", col.Name, col.Type, nullableStr, uniqueStr))
		}
		query := fmt.Sprintf("CREATE TABLE \"%s\" (%s);", name, strings.Join(colDefs, ", "))
		cmd = fmt.Sprintf(`sqlite3 /var/lib/gopulse/databases/%s/pb_data/data.db "%s" 2>&1`, db.ID, query)
	case "mongodb":
		cmd = fmt.Sprintf("docker exec %s mongosh --quiet -u %s -p %s --authenticationDatabase admin %s --eval \"db.createCollection('%s')\" 2>&1", containerName, db.Username, db.Password, schema, name)
	case "redis":
		// Only virtual table Keyspace is allowed, no custom dynamic tables
		return nil
	default:
		return fmt.Errorf("unsupported database type: %s", db.Type)
	}

	output, err := client.RunCommandContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to create table: %s (err: %w)", output, err)
	}

	return nil
}

// DeleteTable deletes an existing table or collection
func (s *DatabaseService) DeleteTable(ctx context.Context, id string, schema string, name string) error {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)

	var cmd string
	switch db.Type {
	case "mysql":
		cmd = mysqlExecCommand(containerName, db, schema, "", fmt.Sprintf("DROP TABLE %s;", mysqlIdentifier(name)), "2>&1")
	case "postgres":
		cmd = fmt.Sprintf("docker exec %s psql -U %s -d %s -c \"DROP TABLE \\\"%s\\\";\" 2>&1", containerName, db.Username, schema, name)
	case "sqlite":
		cmd = fmt.Sprintf(`sqlite3 /var/lib/gopulse/databases/%s/sqlite.db "DROP TABLE \"%s\";" 2>&1`, db.ID, name)
	case "pocketbase":
		cmd = fmt.Sprintf(`sqlite3 /var/lib/gopulse/databases/%s/pb_data/data.db "DROP TABLE \"%s\";" 2>&1`, db.ID, name)
	case "mongodb":
		cmd = fmt.Sprintf("docker exec %s mongosh --quiet -u %s -p %s --authenticationDatabase admin %s --eval \"db.getCollection('%s').drop()\" 2>&1", containerName, db.Username, db.Password, schema, name)
	case "redis":
		dbNum := "0"
		if strings.HasPrefix(schema, "db") {
			dbNum = strings.TrimPrefix(schema, "db")
		}
		escapedPassword := "'" + strings.ReplaceAll(db.Password, "'", "'\\''") + "'"
		cmd = fmt.Sprintf("docker exec -e REDISCLI_AUTH=%s %s redis-cli -n %s FLUSHDB 2>&1", escapedPassword, containerName, dbNum)
	default:
		return fmt.Errorf("unsupported database type: %s", db.Type)
	}

	output, err := client.RunCommandContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("failed to delete table: %s (err: %w)", output, err)
	}

	return nil
}

// InsertRow inserts a new row or document into a table or collection
func (s *DatabaseService) InsertRow(ctx context.Context, id string, schema string, table string, data map[string]interface{}) error {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)

	var cmd string
	switch db.Type {
	case "mysql", "postgres", "sqlite", "pocketbase":
		var cols []string
		var vals []string
		if db.Type == "pocketbase" {
			if _, hasID := data["id"]; !hasID {
				const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
				b := make([]byte, 15)
				for i := range b {
					b[i] = chars[time.Now().UnixNano()%int64(len(chars))]
					time.Sleep(1 * time.Nanosecond)
				}
				data["id"] = string(b)
			}
		}

		for k, v := range data {
			colName := mysqlIdentifier(k)
			if db.Type == "postgres" || db.Type == "sqlite" || db.Type == "pocketbase" {
				colName = fmt.Sprintf("\"%s\"", k)
			}
			cols = append(cols, colName)

			valStr := fmt.Sprintf("%v", v)
			escapedVal := strings.ReplaceAll(valStr, "'", "''")
			vals = append(vals, fmt.Sprintf("'%s'", escapedVal))
		}

		if len(cols) == 0 {
			return fmt.Errorf("no columns provided for insert")
		}

		if db.Type == "mysql" {
			query := fmt.Sprintf("INSERT INTO %s.%s (%s) VALUES (%s);", mysqlIdentifier(schema), mysqlIdentifier(table), strings.Join(cols, ", "), strings.Join(vals, ", "))
			cmd = mysqlExecCommand(containerName, db, "", "", query, "2>&1")
		} else if db.Type == "postgres" {
			query := fmt.Sprintf("INSERT INTO \"%s\" (%s) VALUES (%s);", table, strings.Join(cols, ", "), strings.Join(vals, ", "))
			cmd = fmt.Sprintf(`docker exec %s psql -U %s -d %s -c "%s" 2>&1`, containerName, db.Username, schema, query)
		} else if db.Type == "sqlite" { // sqlite
			query := fmt.Sprintf("INSERT INTO \"%s\" (%s) VALUES (%s);", table, strings.Join(cols, ", "), strings.Join(vals, ", "))
			cmd = fmt.Sprintf(`sqlite3 /var/lib/gopulse/databases/%s/sqlite.db "%s" 2>&1`, db.ID, query)
		} else { // pocketbase
			query := fmt.Sprintf("INSERT INTO \"%s\" (%s) VALUES (%s);", table, strings.Join(cols, ", "), strings.Join(vals, ", "))
			cmd = fmt.Sprintf(`sqlite3 /var/lib/gopulse/databases/%s/pb_data/data.db "%s" 2>&1`, db.ID, query)
		}

	case "mongodb":
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to marshal document data: %w", err)
		}
		jsonStr := string(jsonBytes)
		evalQuery := fmt.Sprintf(`db.getCollection('%s').insertOne(%s)`, table, jsonStr)
		escapedEval := "'" + strings.ReplaceAll(evalQuery, "'", "'\\''") + "'"
		cmd = fmt.Sprintf(`docker exec %s mongosh --quiet -u %s -p %s --authenticationDatabase admin %s --eval %s 2>&1`,
			containerName, db.Username, db.Password, schema, escapedEval,
		)
	case "redis":
		dbNum := "0"
		if strings.HasPrefix(schema, "db") {
			dbNum = strings.TrimPrefix(schema, "db")
		}
		keyVal := fmt.Sprintf("%v", data["key"])
		typeVal := fmt.Sprintf("%v", data["type"])
		valueVal := fmt.Sprintf("%v", data["value"])
		ttlVal := 0
		if t, ok := data["ttl"]; ok && t != nil && fmt.Sprintf("%v", t) != "" {
			if tInt, err := strconv.Atoi(fmt.Sprintf("%v", t)); err == nil {
				ttlVal = tInt
			}
		}

		var redisCmd string
		switch strings.ToLower(typeVal) {
		case "hash":
			redisCmd = fmt.Sprintf(`HSET "%s" "data" "%s"`, keyVal, valueVal)
		case "list":
			redisCmd = fmt.Sprintf(`RPUSH "%s" "%s"`, keyVal, valueVal)
		case "set":
			redisCmd = fmt.Sprintf(`SADD "%s" "%s"`, keyVal, valueVal)
		case "zset":
			redisCmd = fmt.Sprintf(`ZADD "%s" 0 "%s"`, keyVal, valueVal)
		default: // string
			redisCmd = fmt.Sprintf(`SET "%s" "%s"`, keyVal, valueVal)
		}

		escapedPassword := "'" + strings.ReplaceAll(db.Password, "'", "'\\''") + "'"
		if ttlVal > 0 {
			cmd = fmt.Sprintf(`docker exec -e REDISCLI_AUTH=%s %s redis-cli -n %s %s && docker exec -e REDISCLI_AUTH=%s %s redis-cli -n %s EXPIRE "%s" %d 2>&1`,
				escapedPassword, containerName, dbNum, redisCmd,
				escapedPassword, containerName, dbNum, keyVal, ttlVal,
			)
		} else {
			cmd = fmt.Sprintf(`docker exec -e REDISCLI_AUTH=%s %s redis-cli -n %s %s 2>&1`,
				escapedPassword, containerName, dbNum, redisCmd,
			)
		}
	default:
		return fmt.Errorf("unsupported database type: %s", db.Type)
	}

	output, err := client.RunCommandContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("insert failed: %s (err: %w)", output, err)
	}

	if db.Type == "redis" {
		trimmedOut := strings.TrimSpace(output)
		if strings.Contains(trimmedOut, "ERR ") || strings.Contains(trimmedOut, "NOAUTH") || strings.Contains(trimmedOut, "WRONGPASS") {
			return fmt.Errorf("redis execution error: %s", trimmedOut)
		}
	}

	return nil
}

// DeleteRow deletes an existing row or document from a table or collection
func (s *DatabaseService) DeleteRow(ctx context.Context, id string, schema string, table string, pkCol string, pkVal string) error {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)

	var cmd string
	escapedPkVal := strings.ReplaceAll(pkVal, "'", "''")

	switch db.Type {
	case "mysql":
		query := fmt.Sprintf("DELETE FROM %s.%s WHERE %s = %s;", mysqlIdentifier(schema), mysqlIdentifier(table), mysqlIdentifier(pkCol), sqlStringLiteral(pkVal))
		cmd = mysqlExecCommand(containerName, db, "", "", query, "2>&1")
	case "postgres":
		query := fmt.Sprintf("DELETE FROM \"%s\" WHERE \"%s\" = '%s';", table, pkCol, escapedPkVal)
		cmd = fmt.Sprintf(`docker exec %s psql -U %s -d %s -c "%s" 2>&1`, containerName, db.Username, schema, query)
	case "sqlite":
		query := fmt.Sprintf("DELETE FROM \"%s\" WHERE \"%s\" = '%s';", table, pkCol, escapedPkVal)
		cmd = fmt.Sprintf(`sqlite3 /var/lib/gopulse/databases/%s/sqlite.db "%s" 2>&1`, db.ID, query)
	case "pocketbase":
		query := fmt.Sprintf("DELETE FROM \"%s\" WHERE \"%s\" = '%s';", table, pkCol, escapedPkVal)
		cmd = fmt.Sprintf(`sqlite3 /var/lib/gopulse/databases/%s/pb_data/data.db "%s" 2>&1`, db.ID, query)
	case "mongodb":
		evalQuery := fmt.Sprintf(`
			let idVal = '%s';
			try {
				if (idVal.length === 24) {
					db.getCollection('%s').deleteOne({_id: ObjectId(idVal)});
				} else {
					db.getCollection('%s').deleteOne({_id: idVal});
				}
			} catch(e) {
				db.getCollection('%s').deleteOne({_id: idVal});
			}
		`, escapedPkVal, table, table, table)
		escapedEval := "'" + strings.ReplaceAll(evalQuery, "'", "'\\''") + "'"
		cmd = fmt.Sprintf(`docker exec %s mongosh --quiet -u %s -p %s --authenticationDatabase admin %s --eval %s 2>&1`,
			containerName, db.Username, db.Password, schema, escapedEval,
		)
	case "redis":
		dbNum := "0"
		if strings.HasPrefix(schema, "db") {
			dbNum = strings.TrimPrefix(schema, "db")
		}
		escapedPassword := "'" + strings.ReplaceAll(db.Password, "'", "'\\''") + "'"
		cmd = fmt.Sprintf(`docker exec -e REDISCLI_AUTH=%s %s redis-cli -n %s DEL "%s" 2>&1`, escapedPassword, containerName, dbNum, pkVal)
	default:
		return fmt.Errorf("unsupported database type: %s", db.Type)
	}

	output, err := client.RunCommandContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("delete failed: %s (err: %w)", output, err)
	}

	if db.Type == "redis" {
		trimmedOut := strings.TrimSpace(output)
		if strings.Contains(trimmedOut, "ERR ") || strings.Contains(trimmedOut, "NOAUTH") || strings.Contains(trimmedOut, "WRONGPASS") {
			return fmt.Errorf("redis execution error: %s", trimmedOut)
		}
	}

	return nil
}

// UpdateRow updates an existing row or document in a table or collection
func (s *DatabaseService) UpdateRow(ctx context.Context, id string, schema string, table string, pkCol string, pkVal string, data map[string]interface{}) error {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)

	var cmd string
	switch db.Type {
	case "mysql", "postgres", "sqlite", "pocketbase":
		var sets []string
		for k, v := range data {
			if k == pkCol {
				continue // Skip primary key column updates
			}
			colName := fmt.Sprintf("`%s`", k)
			if db.Type == "mysql" {
				colName = mysqlIdentifier(k)
			} else if db.Type == "postgres" || db.Type == "sqlite" || db.Type == "pocketbase" {
				colName = fmt.Sprintf("\"%s\"", k)
			}

			valStr := fmt.Sprintf("%v", v)
			escapedVal := strings.ReplaceAll(valStr, "'", "''")
			sets = append(sets, fmt.Sprintf("%s = '%s'", colName, escapedVal))
		}

		if len(sets) == 0 {
			return fmt.Errorf("no columns provided for update")
		}

		if db.Type == "mysql" {
			query := fmt.Sprintf("UPDATE %s.%s SET %s WHERE %s = %s;", mysqlIdentifier(schema), mysqlIdentifier(table), strings.Join(sets, ", "), mysqlIdentifier(pkCol), sqlStringLiteral(pkVal))
			cmd = mysqlExecCommand(containerName, db, "", "", query, "2>&1")
		} else if db.Type == "postgres" {
			query := fmt.Sprintf("UPDATE \"%s\" SET %s WHERE \"%s\" = '%s';", table, strings.Join(sets, ", "), pkCol, strings.ReplaceAll(pkVal, "'", "''"))
			cmd = fmt.Sprintf(`docker exec %s psql -U %s -d %s -c "%s" 2>&1`, containerName, db.Username, schema, query)
		} else if db.Type == "sqlite" {
			query := fmt.Sprintf("UPDATE \"%s\" SET %s WHERE \"%s\" = '%s';", table, strings.Join(sets, ", "), pkCol, strings.ReplaceAll(pkVal, "'", "''"))
			cmd = fmt.Sprintf(`sqlite3 /var/lib/gopulse/databases/%s/sqlite.db "%s" 2>&1`, db.ID, query)
		} else { // pocketbase
			query := fmt.Sprintf("UPDATE \"%s\" SET %s WHERE \"%s\" = '%s';", table, strings.Join(sets, ", "), pkCol, strings.ReplaceAll(pkVal, "'", "''"))
			cmd = fmt.Sprintf(`sqlite3 /var/lib/gopulse/databases/%s/pb_data/data.db "%s" 2>&1`, db.ID, query)
		}

	case "mongodb":
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to marshal document data: %w", err)
		}
		jsonStr := string(jsonBytes)
		evalQuery := fmt.Sprintf(`
			let idVal = '%s';
			let setObj = %s;
			try {
				if (idVal.length === 24) {
					db.getCollection('%s').updateOne({_id: new ObjectId(idVal)}, {$set: setObj});
				} else {
					db.getCollection('%s').updateOne({_id: idVal}, {$set: setObj});
				}
			} catch(e) {
				db.getCollection('%s').updateOne({_id: idVal}, {$set: setObj});
			}
		`, strings.ReplaceAll(pkVal, "'", "\\'"), jsonStr, table, table, table)
		escapedEval := "'" + strings.ReplaceAll(evalQuery, "'", "'\\''") + "'"
		cmd = fmt.Sprintf(`docker exec %s mongosh --quiet -u %s -p %s --authenticationDatabase admin %s --eval %s 2>&1`,
			containerName, db.Username, db.Password, schema, escapedEval,
		)

	case "redis":
		dbNum := "0"
		if strings.HasPrefix(schema, "db") {
			dbNum = strings.TrimPrefix(schema, "db")
		}
		typeVal := fmt.Sprintf("%v", data["type"])
		valueVal := fmt.Sprintf("%v", data["value"])
		ttlVal := 0
		if t, ok := data["ttl"]; ok && t != nil && fmt.Sprintf("%v", t) != "" {
			if tInt, err := strconv.Atoi(fmt.Sprintf("%v", t)); err == nil {
				ttlVal = tInt
			}
		}

		var redisCmd string
		switch strings.ToLower(typeVal) {
		case "hash":
			redisCmd = fmt.Sprintf(`HSET "%s" "data" "%s"`, pkVal, valueVal)
		case "list":
			redisCmd = fmt.Sprintf(`RPUSH "%s" "%s"`, pkVal, valueVal)
		case "set":
			redisCmd = fmt.Sprintf(`SADD "%s" "%s"`, pkVal, valueVal)
		case "zset":
			redisCmd = fmt.Sprintf(`ZADD "%s" 0 "%s"`, pkVal, valueVal)
		default: // string
			redisCmd = fmt.Sprintf(`SET "%s" "%s"`, pkVal, valueVal)
		}

		escapedPassword := "'" + strings.ReplaceAll(db.Password, "'", "'\\''") + "'"
		if ttlVal > 0 {
			cmd = fmt.Sprintf(`docker exec -e REDISCLI_AUTH=%s %s redis-cli -n %s %s && docker exec -e REDISCLI_AUTH=%s %s redis-cli -n %s EXPIRE "%s" %d 2>&1`,
				escapedPassword, containerName, dbNum, redisCmd,
				escapedPassword, containerName, dbNum, pkVal, ttlVal,
			)
		} else {
			cmd = fmt.Sprintf(`docker exec -e REDISCLI_AUTH=%s %s redis-cli -n %s %s 2>&1`,
				escapedPassword, containerName, dbNum, redisCmd,
			)
		}

	default:
		return fmt.Errorf("unsupported database type: %s", db.Type)
	}

	output, err := client.RunCommandContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("update failed: %s (err: %w)", output, err)
	}

	if db.Type == "redis" {
		trimmedOut := strings.TrimSpace(output)
		if strings.Contains(trimmedOut, "ERR ") || strings.Contains(trimmedOut, "NOAUTH") || strings.Contains(trimmedOut, "WRONGPASS") {
			return fmt.Errorf("redis execution error: %s", trimmedOut)
		}
	}

	return nil
}

// GetDatabaseTelemetry fetches dynamic connection count and disk usage size on-demand
func (s *DatabaseService) GetDatabaseTelemetry(ctx context.Context, id string) (*models.DatabaseTelemetry, error) {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return nil, err
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return nil, err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)
	volumePath := fmt.Sprintf("/var/lib/gopulse/databases/%s", db.ID)

	telemetry := &models.DatabaseTelemetry{
		ActiveConnections: 0,
		DataSize:          0,
	}

	// 1. Get disk data directory size (in kilobytes, multiply by 1024)
	sizeCmd := fmt.Sprintf("du -sk %s 2>/dev/null", volumePath)
	sizeOut, err := client.RunCommandContext(ctx, sizeCmd)
	if err == nil {
		fields := strings.Fields(sizeOut)
		if len(fields) > 0 {
			if kb, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
				telemetry.DataSize = kb * 1024
			}
		}
	}

	// 2. Get active database connections (non-blocking query)
	var connCmd string
	switch db.Type {
	case "mysql":
		connCmd = mysqlExecCommand(containerName, db, "", "-B -N", "SHOW STATUS LIKE 'Threads_connected';", "2>/dev/null")
	case "postgres":
		connCmd = fmt.Sprintf(
			`docker exec %s psql -U %s -d %s -t -A -c "SELECT count(*) FROM pg_stat_activity;" 2>/dev/null`,
			containerName, db.Username, db.DatabaseName,
		)
	case "mongodb":
		connCmd = fmt.Sprintf(
			`docker exec %s mongosh --quiet -u %s -p %s --authenticationDatabase admin --eval "db.serverStatus().connections.current" 2>/dev/null`,
			containerName, db.Username, db.Password,
		)
	case "redis":
		escapedPassword := "'" + strings.ReplaceAll(db.Password, "'", "'\\''") + "'"
		connCmd = fmt.Sprintf(
			`docker exec -e REDISCLI_AUTH=%s %s redis-cli INFO clients 2>/dev/null | grep connected_clients | cut -d: -f2`,
			escapedPassword, containerName,
		)
	case "sqlite", "pocketbase":
		// SQLite/PocketBase is a file-based database with no direct TCP connections tracked on the CLI
		telemetry.ActiveConnections = 0
	}

	if connCmd != "" {
		connOut, err := client.RunCommandContext(ctx, connCmd)
		if err == nil {
			trimmed := strings.TrimSpace(connOut)
			if db.Type == "mysql" {
				// Parse "Threads_connected \t 3"
				lines := strings.Split(trimmed, "\n")
				for _, line := range lines {
					fields := strings.Fields(line)
					if len(fields) >= 2 && fields[0] == "Threads_connected" {
						if c, err := strconv.Atoi(fields[1]); err == nil {
							telemetry.ActiveConnections = c
						}
						break
					}
				}
			} else {
				if c, err := strconv.Atoi(trimmed); err == nil {
					telemetry.ActiveConnections = c
				}
			}
		}
	}

	return telemetry, nil
}

// ListDatabaseUsers returns all users in the target database cluster
func (s *DatabaseService) ListDatabaseUsers(ctx context.Context, id string) ([]models.DatabaseUser, error) {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return nil, err
	}

	if db.Type == "redis" || db.Type == "sqlite" || db.Type == "pocketbase" {
		return []models.DatabaseUser{}, nil
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return nil, err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)
	var cmd string
	switch db.Type {
	case "mysql":
		cmd = mysqlExecCommand(containerName, db, "", "-B -N", "SELECT User, Host FROM mysql.user WHERE User NOT IN ('mysql.infoschema', 'mysql.session', 'mysql.sys', 'root');", "2>/dev/null")
	case "postgres":
		cmd = fmt.Sprintf(
			`docker exec %s psql -U %s -d %s -t -A -F '	' -c "SELECT usename, usesuper FROM pg_catalog.pg_user WHERE usename NOT IN ('postgres');" 2>/dev/null`,
			containerName, db.Username, db.DatabaseName,
		)
	case "mongodb":
		cmd = fmt.Sprintf(
			`docker exec %s mongosh --quiet -u %s -p %s --authenticationDatabase admin admin --eval "JSON.stringify(db.getUsers())" 2>/dev/null`,
			containerName, db.Username, db.Password,
		)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", db.Type)
	}

	output, err := client.RunCommandContext(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to list database users: %w", err)
	}

	var users []models.DatabaseUser
	trimmedOut := strings.TrimSpace(output)
	if trimmedOut == "" {
		return users, nil
	}

	switch db.Type {
	case "mysql":
		lines := strings.Split(trimmedOut, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Split(line, "\t")
			if len(parts) >= 2 {
				username := parts[0]
				host := parts[1]
				users = append(users, models.DatabaseUser{
					Username: username,
					Host:     host,
					Scope:    "readWrite",
					Database: "* (All)",
				})
			}
		}
	case "postgres":
		lines := strings.Split(trimmedOut, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Split(line, "\t")
			if len(parts) >= 2 {
				username := parts[0]
				isSuper := parts[1] == "t"
				scope := "readWrite"
				dbScope := db.DatabaseName
				if isSuper {
					scope = "admin"
					dbScope = "* (All)"
				}
				users = append(users, models.DatabaseUser{
					Username: username,
					Scope:    scope,
					Database: dbScope,
				})
			}
		}
	case "mongodb":
		var mongoUsers []struct {
			User  string `json:"user"`
			Roles []struct {
				Role string `json:"role"`
				Db   string `json:"db"`
			} `json:"roles"`
		}
		if err := json.Unmarshal([]byte(trimmedOut), &mongoUsers); err == nil {
			for _, mu := range mongoUsers {
				scope := "read"
				dbScope := "admin"
				if len(mu.Roles) > 0 {
					r := mu.Roles[0].Role
					dbScope = mu.Roles[0].Db
					if dbScope == "admin" && (r == "root" || r == "readWriteAnyDatabase" || r == "readAnyDatabase") {
						dbScope = "* (All)"
					}
					if r == "dbOwner" || r == "root" || r == "userAdminAnyDatabase" {
						scope = "admin"
					} else if r == "readWrite" || r == "readWriteAnyDatabase" {
						scope = "readWrite"
					}
				}
				users = append(users, models.DatabaseUser{
					Username: mu.User,
					Scope:    scope,
					Database: dbScope,
				})
			}
		}
	}

	return users, nil
}

// CreateDatabaseUser provisions a new database user inside the container
func (s *DatabaseService) CreateDatabaseUser(ctx context.Context, id string, username string, password string, scope string, targetDb string) error {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}

	if db.Type == "redis" || db.Type == "sqlite" || db.Type == "pocketbase" {
		return nil
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)

	var cmds []string
	switch db.Type {
	case "mysql":
		mysqlUser := fmt.Sprintf("%s@'%%'", sqlStringLiteral(username))
		cmds = append(cmds, fmt.Sprintf(`CREATE USER %s IDENTIFIED BY %s;`, mysqlUser, sqlStringLiteral(password)))

		dbScope := "*.*"
		if targetDb != "" && targetDb != "*" && targetDb != "all" {
			dbScope = fmt.Sprintf("%s.*", mysqlIdentifier(targetDb))
		}

		switch scope {
		case "read":
			cmds = append(cmds, fmt.Sprintf(`GRANT SELECT ON %s TO %s;`, dbScope, mysqlUser))
		case "readWrite":
			cmds = append(cmds, fmt.Sprintf(`GRANT SELECT, INSERT, UPDATE, DELETE ON %s TO %s;`, dbScope, mysqlUser))
		case "admin":
			cmds = append(cmds, fmt.Sprintf(`GRANT ALL PRIVILEGES ON %s TO %s;`, dbScope, mysqlUser))
		}
		cmds = append(cmds, "FLUSH PRIVILEGES;")

		for _, sql := range cmds {
			execCmd := mysqlExecCommand(containerName, db, "", "", sql, "2>&1")
			out, err := client.RunCommandContext(ctx, execCmd)
			if err != nil {
				return fmt.Errorf("mysql error: %s (err: %w)", out, err)
			}
		}

	case "postgres":
		cmds = append(cmds, fmt.Sprintf(`CREATE USER "%s" WITH PASSWORD '%s';`, username, password))

		targetName := targetDb
		if targetName == "" || targetName == "*" || targetName == "all" {
			targetName = db.DatabaseName
		}

		switch scope {
		case "read":
			cmds = append(cmds, fmt.Sprintf(`GRANT CONNECT ON DATABASE "%s" TO "%s";`, targetName, username))
			cmds = append(cmds, fmt.Sprintf(`GRANT USAGE ON SCHEMA public TO "%s";`, username))
			cmds = append(cmds, fmt.Sprintf(`GRANT SELECT ON ALL TABLES IN SCHEMA public TO "%s";`, username))
		case "readWrite":
			cmds = append(cmds, fmt.Sprintf(`GRANT CONNECT ON DATABASE "%s" TO "%s";`, targetName, username))
			cmds = append(cmds, fmt.Sprintf(`GRANT USAGE, CREATE ON SCHEMA public TO "%s";`, username))
			cmds = append(cmds, fmt.Sprintf(`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO "%s";`, username))
			cmds = append(cmds, fmt.Sprintf(`GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO "%s";`, username))
		case "admin":
			cmds = append(cmds, fmt.Sprintf(`ALTER USER "%s" WITH SUPERUSER;`, username))
		}

		for _, sql := range cmds {
			execCmd := fmt.Sprintf(`docker exec %s psql -U %s -d %s -c "%s" 2>&1`, containerName, db.Username, targetName, sql)
			out, err := client.RunCommandContext(ctx, execCmd)
			if err != nil {
				return fmt.Errorf("postgres error: %s (err: %w)", out, err)
			}
		}

	case "mongodb":
		var evalJs string
		isClusterWide := targetDb == "" || targetDb == "*" || targetDb == "all"

		if isClusterWide {
			var role string
			switch scope {
			case "read":
				role = "readAnyDatabase"
			case "readWrite":
				role = "readWriteAnyDatabase"
			case "admin":
				role = "root"
			}
			evalJs = fmt.Sprintf(
				`db.getSiblingDB("admin").createUser({ user: "%s", pwd: "%s", roles: [{ role: "%s", db: "admin" }] })`,
				username, password, role,
			)
		} else {
			var role string
			switch scope {
			case "read":
				role = "read"
			case "readWrite":
				role = "readWrite"
			case "admin":
				role = "dbOwner"
			}
			evalJs = fmt.Sprintf(
				`db.getSiblingDB("admin").createUser({ user: "%s", pwd: "%s", roles: [{ role: "%s", db: "%s" }] })`,
				username, password, role, targetDb,
			)
		}

		execCmd := fmt.Sprintf(
			`docker exec %s mongosh --quiet -u %s -p %s --authenticationDatabase admin admin --eval '%s' 2>&1`,
			containerName, db.Username, db.Password, evalJs,
		)
		out, err := client.RunCommandContext(ctx, execCmd)
		if err != nil {
			return fmt.Errorf("mongodb error: %s (err: %w)", out, err)
		}
	default:
		return fmt.Errorf("unsupported database type: %s", db.Type)
	}

	return nil
}

// DeleteDatabaseUser removes a user from the target database cluster
func (s *DatabaseService) DeleteDatabaseUser(ctx context.Context, id string, username string) error {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}

	if db.Type == "redis" || db.Type == "sqlite" || db.Type == "pocketbase" {
		return nil
	}

	client, err := s.manager.GetSSHClient(db.ServerID)
	if err != nil {
		return err
	}

	containerName := fmt.Sprintf("gopulse-%s", db.ID)
	var execCmd string
	switch db.Type {
	case "mysql":
		execCmd = mysqlExecCommand(containerName, db, "", "", fmt.Sprintf("DROP USER %s@'%%';", sqlStringLiteral(username)), "2>&1")
	case "postgres":
		execCmd = fmt.Sprintf(`docker exec %s psql -U %s -d %s -c "DROP OWNED BY \"%s\"; DROP USER \"%s\";" 2>&1`, containerName, db.Username, db.DatabaseName, username, username)
	case "mongodb":
		evalJs := fmt.Sprintf(`db.dropUser("%s")`, username)
		execCmd = fmt.Sprintf(
			`docker exec %s mongosh --quiet -u %s -p %s --authenticationDatabase admin %s --eval '%s' 2>&1`,
			containerName, db.Username, db.Password, db.DatabaseName, evalJs,
		)
	default:
		return fmt.Errorf("unsupported database type: %s", db.Type)
	}

	out, err := client.RunCommandContext(ctx, execCmd)
	if err != nil {
		return fmt.Errorf("delete user error: %s (err: %w)", out, err)
	}

	return nil
}
