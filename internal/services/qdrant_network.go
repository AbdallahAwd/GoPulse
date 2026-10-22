package services

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
)

func qdrantLocalURL(cfg models.QdrantConfig) string {
	host := cfg.BindAddress
	if host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, fmt.Sprint(cfg.HostPort))
}

func qdrantRunCommand(id string, cfg models.QdrantConfig) string {
	ports := fmt.Sprintf("-p %s:%d:6333", cfg.BindAddress, cfg.HostPort)
	if cfg.GRPCPort != 0 {
		ports += fmt.Sprintf(" -p %s:%d:6334", cfg.BindAddress, cfg.GRPCPort)
	}
	return fmt.Sprintf("docker run -d --name %s --label gopulse.database=%s --restart unless-stopped %s -v %s:/qdrant/storage -v %s:/qdrant/snapshots -v %s:/qdrant/config/production.yaml:ro qdrant/qdrant:%s", shellQuote("gopulse-"+id), shellQuote(id), ports, shellQuote(cfg.StoragePath+"/storage"), shellQuote(cfg.StoragePath+"/snapshots"), shellQuote(cfg.StoragePath+"/config.yaml"), cfg.Version)
}

// QdrantAPIKey is used only by the explicit, permission-guarded credentials endpoint.
// The key remains on the VPS and is never written into dashboard inventory or operations.
func (s *DatabaseService) QdrantAPIKey(ctx context.Context, id string) (string, error) {
	cfg, err := s.QdrantConfiguration(id)
	if err != nil {
		return "", err
	}
	if err := ValidateQdrantConfig(&cfg); err != nil {
		return "", err
	}
	if cfg.StoragePath == "" {
		return "", fmt.Errorf("Qdrant storage path unavailable")
	}
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return "", err
	}
	script := "import json; print(json.load(open(" + pythonString(cfg.StoragePath+"/config.yaml") + "))['service']['api_key'])"
	out, err := s.manager.Run(ctx, db.ServerID, "sudo -n python3 -c "+shellQuote(script))
	if err != nil {
		return "", fmt.Errorf("Qdrant API key unavailable; check server connectivity and configuration")
	}
	key := strings.TrimSuffix(out, "\n")
	if len(key) < 32 || len(key) > 256 || strings.ContainsAny(key, "\r\n") {
		return "", fmt.Errorf("invalid Qdrant API key in server configuration")
	}
	return key, nil
}

// ConfigureQdrantNetwork replaces Docker port bindings without replacing data or credentials.
// The previous container is kept until authenticated health and metadata persistence succeed.
func (s *DatabaseService) ConfigureQdrantNetwork(ctx context.Context, id, bind string, httpPort, grpcPort int, progress func(string)) (models.QdrantConfig, error) {
	unlock, err := s.qdrantLock(id)
	if err != nil {
		return models.QdrantConfig{}, err
	}
	defer unlock()
	previous, err := s.QdrantConfiguration(id)
	if err != nil {
		return previous, err
	}
	if !safeResourceID.MatchString(id) || previous.StoragePath == "" {
		return previous, fmt.Errorf("invalid Qdrant deployment")
	}
	cfg := previous
	cfg.BindAddress, cfg.HostPort, cfg.GRPCPort = bind, httpPort, grpcPort
	if err := ValidateQdrantConfig(&cfg); err != nil {
		return previous, err
	}
	db, err := s.store.GetDatabase(id)
	if err != nil {
		return previous, err
	}
	container := "gopulse-" + id
	backup := container + "-network-backup"
	// Inspect ownership and credentials before publishing. A failed replacement restores
	// the previous container; an interrupted operation leaves the backup for recovery.
	script := fmt.Sprintf(`import json,subprocess,urllib.request,time,sys,shlex
name,backup,owner=%s,%s,%s
def docker(*args):
 return subprocess.check_output(['docker',*args],stderr=subprocess.DEVNULL,text=True).strip()
renamed=False
stopped=False
running=False
try:
 original=json.loads(docker('inspect',name))[0]
 if original.get('Config',{}).get('Labels',{}).get('gopulse.database')!=owner: raise ValueError('ownership')
 if docker('ps','-a','--filter','name=^'+backup+'$','--format','{{.Names}}'): raise ValueError('recovery pending')
 secret=json.load(open(%s))['service']['api_key']
 if not isinstance(secret,str) or len(secret)<32 or len(secret)>256: raise ValueError('authentication required')
 running=original['State']['Running']
 docker('stop',name); stopped=True
 docker('rename',name,backup); renamed=True
 args=shlex.split(%s)[1:]
 args[-1]=original['Image']
 docker(*args)
 opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
 for attempt in range(30):
  try:
   req=urllib.request.Request(%s,headers={'api-key':secret})
   with opener.open(req,timeout=2) as response:
    if json.load(response).get('status')!='ok': raise ValueError('health')
   break
  except Exception:
   if attempt==29: raise
   time.sleep(1)
 print('verified')
except Exception:
 try:
  if renamed:
   # docker run may create a container even when startup fails.
   candidate=subprocess.run(['docker','inspect',name],capture_output=True,text=True)
   if candidate.returncode==0:
    if json.loads(candidate.stdout)[0]['Config']['Labels'].get('gopulse.database')!=owner: raise ValueError('ownership')
    docker('rm','-f',name)
   docker('rename',backup,name)
  if stopped and running: docker('start',name)
 except Exception:
  print('Qdrant network change failed; previous container retained as '+backup+'; recovery required'); sys.exit(1)
 print('Qdrant network change failed; previous deployment retained'); sys.exit(1)
`, pythonString(container), pythonString(backup), pythonString(id), pythonString(cfg.StoragePath+"/config.yaml"), pythonString(qdrantRunCommand(id, cfg)), pythonString(qdrantLocalURL(cfg)+"/collections"))
	progress("Apply Qdrant network bindings and verify authenticated health")
	if _, err := s.manager.Run(ctx, db.ServerID, "sudo -n python3 -c "+shellQuote(script)); err != nil {
		return previous, fmt.Errorf("Qdrant network change failed; inspect the existing container and its network-backup before retrying")
	}
	// Use a bounded independent context for rollback/cleanup even if the request was canceled.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := s.store.CommitQdrantNetwork(id, cfg); err != nil {
		return previous, s.rollbackQdrantNetwork(cleanupCtx, db.ServerID, container, backup, id)
	}
	progress("Network settings saved; persistent data and API key preserved")
	if _, err := s.manager.Run(cleanupCtx, db.ServerID, "docker rm "+shellQuote(backup)); err != nil {
		return cfg, fmt.Errorf("network settings applied; stopped backup container retained as %s", backup)
	}
	return cfg, nil
}

func (s *DatabaseService) rollbackQdrantNetwork(ctx context.Context, server, container, backup, id string) error {
	cmd := "set -eu\ntest \"$(docker inspect -f '{{index .Config.Labels \"gopulse.database\"}}' " + shellQuote(backup) + ")\" = " + shellQuote(id) + "\ntest \"$(docker inspect -f '{{index .Config.Labels \"gopulse.database\"}}' " + shellQuote(container) + ")\" = " + shellQuote(id) + "\ndocker rm -f " + shellQuote(container) + " >/dev/null\ndocker rename " + shellQuote(backup) + " " + shellQuote(container) + "\ndocker start " + shellQuote(container) + " >/dev/null"
	if _, err := s.manager.Run(ctx, server, cmd); err != nil {
		return fmt.Errorf("network metadata save failed; previous container retained as %s; recovery required", backup)
	}
	return fmt.Errorf("network metadata save failed; previous deployment restored")
}
