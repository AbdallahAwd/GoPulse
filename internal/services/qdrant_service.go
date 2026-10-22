package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/google/uuid"
)

var qdrantVersion = regexp.MustCompile(`^v1\.[0-9]{1,2}\.[0-9]{1,2}$`)
var collectionName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// ValidateQdrantConfig validates literal IPv4 bindings. New installs allow external clients by default.
func ValidateQdrantConfig(cfg *models.QdrantConfig) error {
	if cfg.Version == "" {
		cfg.Version = "v1.19.0"
	}
	if !qdrantVersion.MatchString(cfg.Version) {
		return fmt.Errorf("use an explicit Qdrant v1.x.y version")
	}
	if cfg.BindAddress == "" {
		cfg.BindAddress = "0.0.0.0"
	}
	ip := net.ParseIP(cfg.BindAddress)
	if ip == nil || ip.IsMulticast() || cfg.BindAddress == "255.255.255.255" {
		return fmt.Errorf("Qdrant bind address must be a unicast IPv4 address or 0.0.0.0")
	}
	if ip.To4() == nil {
		return fmt.Errorf("IPv4 bind address required")
	}
	cfg.BindAddress = ip.To4().String()
	if cfg.HostPort == 0 {
		cfg.HostPort = 6333
	}
	if cfg.HostPort < 1024 || cfg.HostPort > 65535 {
		return fmt.Errorf("Qdrant port must be 1024–65535")
	}
	if cfg.GRPCPort != 0 && (cfg.GRPCPort < 1024 || cfg.GRPCPort > 65535 || cfg.GRPCPort == cfg.HostPort) {
		return fmt.Errorf("Qdrant gRPC port must be 1024–65535 and different from the HTTP port, or 0 to disable")
	}
	if cfg.StoragePath != "" {
		clean := filepath.Clean(cfg.StoragePath)
		if clean != cfg.StoragePath || !strings.HasPrefix(clean, "/var/lib/gopulse/databases/") || !safeResourceID.MatchString(filepath.Base(clean)) {
			return fmt.Errorf("storage must be a dedicated directory under /var/lib/gopulse/databases")
		}
	}
	return nil
}

func (s *DatabaseService) InstallQdrant(ctx context.Context, serverID, name, apiKey string, cfg models.QdrantConfig, progress func(string)) (result *models.Database, installErr error) {
	if cfg.GRPCPort == 0 {
		cfg.GRPCPort = 6334
	}
	if err := ValidateQdrantConfig(&cfg); err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" || len(name) > 100 {
		return nil, fmt.Errorf("display name is required (maximum 100 characters)")
	}
	if apiKey == "" {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, err
		}
		apiKey = base64.RawURLEncoding.EncodeToString(secret)
	}
	if len(apiKey) < 32 || len(apiKey) > 256 || strings.ContainsAny(apiKey, "\r\n") {
		return nil, fmt.Errorf("API key must contain 32–256 characters without line breaks")
	}
	id := "db-" + uuid.NewString()
	if cfg.StoragePath == "" {
		cfg.StoragePath = "/var/lib/gopulse/databases/" + id
	}
	// The generated secret stays in the protected VPS configuration, never in a response, SQL row or logs.
	configuration, _ := json.Marshal(map[string]interface{}{"service": map[string]interface{}{"api_key": apiKey, "host": "0.0.0.0", "http_port": 6333}, "storage": map[string]interface{}{"storage_path": "/qdrant/storage", "snapshots_path": "/qdrant/snapshots"}})
	container := "gopulse-" + id
	run := func(step, command string) error {
		progress(step)
		_, err := s.manager.Run(ctx, serverID, command)
		if err != nil {
			return fmt.Errorf("%s failed; check VPS prerequisites or connectivity", step)
		}
		return nil
	}
	if err := run("Check Docker, Python and permissions", "command -v python3 >/dev/null && docker info >/dev/null && sudo -n true"); err != nil {
		return nil, err
	}
	// Refuse reuse: never take over a directory with existing data or a credential file.
	if err := run("Create private persistent storage", "sudo -n test ! -e "+shellQuote(cfg.StoragePath)+" && sudo -n install -d -m 700 "+shellQuote(cfg.StoragePath)+" "+shellQuote(cfg.StoragePath+"/storage")+" "+shellQuote(cfg.StoragePath+"/snapshots")); err != nil {
		return nil, err
	}
	db := models.Database{ID: id, ServerID: serverID, Name: name, Type: "qdrant", Username: "api-key", HostPort: cfg.HostPort, ContainerID: container, DatabaseName: "collections", Status: models.DatabaseStatusStarting, CreatedAt: time.Now()}
	if err := s.store.AddDatabase(db); err != nil {
		return nil, err
	}
	if err := s.store.SetQdrantConfig(id, cfg); err != nil {
		return &db, err
	}
	result = &db
	defer func() {
		if installErr != nil {
			db.Status = models.DatabaseStatusError
			s.store.UpdateDatabase(db)
			result = &db
		}
	}()
	encoded := base64.StdEncoding.EncodeToString(configuration)
	script := "import base64,os; p=" + pythonString(cfg.StoragePath+"/config.yaml") + "; fd=os.open(p,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600); os.write(fd,base64.b64decode(" + pythonString(encoded) + ")); os.close(fd)"
	if err := run("Save protected API credentials", "sudo -n python3 -c "+shellQuote(script)); err != nil {
		return nil, err
	}
	cmd := qdrantRunCommand(id, cfg)
	if err := run("Install Qdrant container", cmd); err != nil {
		return nil, err
	}
	progress("Verify authenticated Qdrant connectivity")
	var err error
	for attempt := 0; attempt < 30; attempt++ {
		_, err = s.QdrantRequest(ctx, id, "GET", "/collections", nil)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return &db, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if err != nil {
		db.Status = models.DatabaseStatusError
		s.store.UpdateDatabase(db)
		return &db, fmt.Errorf("authenticated health verification failed; container and persistent data retained for diagnostics")
	}
	db.Status = models.DatabaseStatusRunning
	if err := s.store.UpdateDatabase(db); err != nil {
		return &db, err
	}
	progress("Qdrant verified; port bound to " + cfg.BindAddress)
	return &db, nil
}

func pythonString(value string) string { raw, _ := json.Marshal(value); return string(raw) }

// QdrantRequest runs on the target VPS. Credentials are read from its protected file, never interpolated into argv.
func (s *DatabaseService) QdrantRequest(ctx context.Context, id, method, path string, body interface{}) (interface{}, error) {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return nil, err
	}
	if db.Type != "qdrant" {
		return nil, fmt.Errorf("database is not Qdrant")
	}
	cfg, err := s.store.GetQdrantConfig(id)
	if err != nil {
		return nil, err
	}
	if err := ValidateQdrantConfig(&cfg); err != nil {
		return nil, err
	}
	if method != "GET" && method != "PUT" && method != "DELETE" && method != "POST" {
		return nil, fmt.Errorf("unsupported method")
	}
	raw, _ := json.Marshal(body)
	// Bounded response, timeout, and no server-provided errors/headers/credentials in command output.
	script := fmt.Sprintf(`import json,urllib.request,urllib.error,sys
cfg=json.load(open(%s))
data=%s
req=urllib.request.Request(%s,data=None if data=="null" else data.encode(),method=%s,headers={"api-key":cfg["service"]["api_key"],"Content-Type":"application/json"})
try:
 with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(req,timeout=10) as r:
  raw=r.read(1048577)
  if len(raw)>1048576: raise ValueError("response too large")
  value=json.loads(raw)
  if isinstance(value,dict) and value.get("status","ok")!="ok": raise ValueError("Qdrant rejected operation")
  print(json.dumps(value))
except urllib.error.HTTPError as e:
 print("Qdrant HTTP status "+str(e.code)); sys.exit(1)
except Exception:
 print("Qdrant request failed"); sys.exit(1)
	`, pythonString(cfg.StoragePath+"/config.yaml"), pythonString(string(raw)), pythonString(qdrantLocalURL(cfg)+path), pythonString(method))
	out, err := s.manager.Run(ctx, db.ServerID, "sudo -n python3 -c "+shellQuote(script))
	if err != nil {
		return nil, fmt.Errorf("Qdrant request failed (check health, credentials and connectivity)")
	}
	var result interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return nil, fmt.Errorf("invalid Qdrant response")
	}
	return result, nil
}
func (s *DatabaseService) QdrantCollections(ctx context.Context, id string) (interface{}, error) {
	return s.QdrantRequest(ctx, id, "GET", "/collections", nil)
}
func (s *DatabaseService) CreateQdrantCollection(ctx context.Context, id, name string, size int, distance string) (interface{}, error) {
	unlock, err := s.qdrantLock(id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if !collectionName.MatchString(name) || size < 1 || size > 65536 {
		return nil, fmt.Errorf("invalid collection name or vector size")
	}
	if distance != "Cosine" && distance != "Dot" && distance != "Euclid" && distance != "Manhattan" {
		return nil, fmt.Errorf("unsupported distance")
	}
	// PUT is idempotent at the tool level: verify an existing collection has the requested dimensions.
	existing, err := s.QdrantRequest(ctx, id, "GET", "/collections/"+url.PathEscape(name), nil)
	if err == nil {
		data, _ := existing.(map[string]interface{})
		result, _ := data["result"].(map[string]interface{})
		config, _ := result["config"].(map[string]interface{})
		params, _ := config["params"].(map[string]interface{})
		vectors, _ := params["vectors"].(map[string]interface{})
		if vectors["size"] == float64(size) && vectors["distance"] == distance {
			return existing, nil
		}
		return nil, fmt.Errorf("collection already exists with different settings")
	}
	if _, err := s.QdrantRequest(ctx, id, "PUT", "/collections/"+url.PathEscape(name)+"?timeout=10", map[string]interface{}{"vectors": map[string]interface{}{"size": size, "distance": distance}}); err != nil {
		return nil, err
	}
	verified, err := s.QdrantRequest(ctx, id, "GET", "/collections/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, fmt.Errorf("collection creation returned, but verification failed")
	}
	return verified, nil
}
func (s *DatabaseService) DeleteQdrantCollection(ctx context.Context, id, name string) (interface{}, error) {
	unlock, err := s.qdrantLock(id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if !collectionName.MatchString(name) {
		return nil, fmt.Errorf("invalid collection name")
	}
	return s.QdrantRequest(ctx, id, "DELETE", "/collections/"+url.PathEscape(name)+"?timeout=10", nil)
}
func (s *DatabaseService) QdrantLifecycle(ctx context.Context, id, action string) error {
	unlock, err := s.qdrantLock(id)
	if err != nil {
		return err
	}
	defer unlock()

	if action != "start" && action != "stop" && action != "restart" {
		return fmt.Errorf("unsupported action")
	}
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}
	if db.Type != "qdrant" || !safeResourceID.MatchString(id) {
		return fmt.Errorf("invalid Qdrant database")
	}
	if _, err := s.manager.Run(ctx, db.ServerID, "docker "+action+" "+shellQuote("gopulse-"+id)); err != nil {
		return fmt.Errorf("Qdrant %s failed", action)
	}
	if action == "stop" {
		out, err := s.manager.Run(ctx, db.ServerID, "docker inspect -f '{{.State.Running}}' "+shellQuote("gopulse-"+id))
		if err != nil || strings.TrimSpace(out) != "false" {
			return fmt.Errorf("Qdrant stop verification failed")
		}
		db.Status = models.DatabaseStatusStopped
	} else {
		if _, err := s.QdrantCollections(ctx, id); err != nil {
			return fmt.Errorf("Qdrant is starting or unavailable; inspect status and retry verification")
		}
		db.Status = models.DatabaseStatusRunning
	}
	return s.store.UpdateDatabase(db)
}
func (s *DatabaseService) DeleteQdrant(ctx context.Context, id string, purge bool) error {
	unlock, err := s.qdrantLock(id)
	if err != nil {
		return err
	}
	defer unlock()

	db, err := s.store.GetDatabase(id)
	if err != nil {
		return err
	}
	if db.Type != "qdrant" || !safeResourceID.MatchString(id) {
		return fmt.Errorf("invalid Qdrant database")
	}
	cfg, err := s.store.GetQdrantConfig(id)
	if err != nil {
		return err
	}
	if err := ValidateQdrantConfig(&cfg); err != nil {
		return err
	}
	// Listing must succeed before treating an absent container as a recoverable partial installation.
	// Refuse removal if another process replaced the managed name with an unrelated container.
	container := "gopulse-" + id
	remove := "set -eu\nfound=$(docker ps -a --filter " + shellQuote("name=^"+container+"$") + " --format " + shellQuote("{{.Names}}|{{.Label \"gopulse.database\"}}") + ")\ncase \"$found\" in\n" + shellQuote(container+"|"+id) + ") docker rm -f " + shellQuote(container) + " >/dev/null;;\n'') :;;\n*) exit 1;;\nesac"
	if _, err := s.manager.Run(ctx, db.ServerID, remove); err != nil {
		return fmt.Errorf("container removal failed; metadata retained")
	}
	if purge {
		if _, err := s.manager.Run(ctx, db.ServerID, "sudo -n rm -rf -- "+shellQuote(cfg.StoragePath)); err != nil {
			return fmt.Errorf("data removal failed; metadata retained")
		}
	}
	return s.store.RemoveDatabase(id)
}
func (s *DatabaseService) QdrantDiagnostics(ctx context.Context, id string) (map[string]interface{}, error) {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return nil, err
	}
	if db.Type != "qdrant" || !safeResourceID.MatchString(id) {
		return nil, fmt.Errorf("invalid Qdrant database")
	}
	cfg, err := s.store.GetQdrantConfig(id)
	if err != nil {
		return nil, err
	}
	health, healthErr := s.QdrantCollections(ctx, id)
	version, versionErr := s.QdrantRequest(ctx, id, "GET", "/", nil)
	if versionErr != nil {
		version = nil
	}
	stats, err := s.manager.Run(ctx, db.ServerID, "docker stats --no-stream --format '{{json .}}' "+shellQuote("gopulse-"+id))
	if err != nil {
		return nil, fmt.Errorf("resource stats unavailable")
	}
	redactScript := "import json,subprocess; c=json.load(open(" + pythonString(cfg.StoragePath+"/config.yaml") + ")); p=subprocess.run(['docker','logs','--tail','100'," + pythonString("gopulse-"+id) + "],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=True); print(p.stdout.decode(errors='replace').replace(c['service']['api_key'],'[REDACTED]'))"
	logs, err := s.manager.Run(ctx, db.ServerID, "sudo -n python3 -c "+shellQuote(redactScript))
	if err != nil {
		return nil, fmt.Errorf("logs unavailable or cannot safely redact logs")
	}

	return map[string]interface{}{"config": cfg, "instance": version, "healthy": healthErr == nil, "collections": health, "stats": stats, "logs": logs}, nil
}
func ValidCollectionName(name string) bool { return collectionName.MatchString(name) }

var qdrantSnapshotName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,199}\.snapshot$`)

func (s *DatabaseService) QdrantSnapshots(ctx context.Context, id, collection string, create bool) (interface{}, error) {
	if !collectionName.MatchString(collection) {
		return nil, fmt.Errorf("invalid collection")
	}
	method := "GET"
	if create {
		method = "POST"
	}
	return s.QdrantRequest(ctx, id, method, "/collections/"+collection+"/snapshots", nil)
}
func (s *DatabaseService) RestoreQdrantSnapshot(ctx context.Context, id, collection, filename string) (interface{}, error) {
	unlock, err := s.qdrantLock(id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if !collectionName.MatchString(collection) || !qdrantSnapshotName.MatchString(filename) || strings.Contains(filename, "..") {
		return nil, fmt.Errorf("invalid collection or snapshot name")
	}
	return s.QdrantRequest(ctx, id, "PUT", "/collections/"+collection+"/snapshots/recover?wait=true", map[string]interface{}{"location": "file:///qdrant/snapshots/" + collection + "/" + filename, "priority": "snapshot"})
}

func (s *DatabaseService) qdrantLock(id string) (func(), error) {
	value, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	if !lock.TryLock() {
		return nil, fmt.Errorf("another database operation is in progress")
	}
	return lock.Unlock, nil
}
func (s *DatabaseService) QdrantConfiguration(id string) (models.QdrantConfig, error) {
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return models.QdrantConfig{}, err
	}
	if db.Type != "qdrant" {
		return models.QdrantConfig{}, fmt.Errorf("not a Qdrant database")
	}
	return s.store.GetQdrantConfig(id)
}
