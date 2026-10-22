package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
)

func TestQdrantExplicitLocalBindingAndLegacyConfig(t *testing.T) {
	s, f := qdrantFixture(t)
	db, err := s.InstallQdrant(context.Background(), "srv", "local", "", models.QdrantConfig{BindAddress: "127.0.0.1", HostPort: 16333, GRPCPort: 16334}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(f.commands, "\n"), "127.0.0.1:16334:6334") {
		t.Fatal("explicit local binding ignored")
	}
	cfg, _ := s.QdrantConfiguration(db.ID)
	cfg.GRPCPort = 0
	if err := s.store.SetQdrantConfig(db.ID, cfg); err != nil {
		t.Fatal(err)
	}
	legacy, _ := s.QdrantConfiguration(db.ID)
	if err := ValidateQdrantConfig(&legacy); err != nil || legacy.GRPCPort != 0 || legacy.BindAddress != "127.0.0.1" {
		t.Fatal("legacy settings changed implicitly")
	}
}

func TestQdrantAPIKeyExplicitRead(t *testing.T) {
	s, f := qdrantFixture(t)
	db, err := s.InstallQdrant(context.Background(), "srv", "vectors", "", models.QdrantConfig{}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("k", 40)
	f.run = func(_, cmd string) (string, error) {
		if strings.Contains(cmd, key) {
			t.Fatal("secret interpolated into command")
		}
		return key + "\n", nil
	}
	actual, err := s.QdrantAPIKey(context.Background(), db.ID)
	if err != nil || actual != key {
		t.Fatal("API key read failed", err)
	}
	saved, _ := s.GetDatabase(db.ID)
	if saved.Password != "" {
		t.Fatal("secret persisted in inventory")
	}
	f.run = func(_, _ string) (string, error) { return "short", nil }
	if _, err := s.QdrantAPIKey(context.Background(), db.ID); err == nil {
		t.Fatal("accepted invalid credentials")
	}
}

func TestQdrantNetworkReplacementAndRollback(t *testing.T) {
	for _, scenario := range []string{"success", "start-failure", "wrong-owner", "missing-key", "pending-backup"} {
		t.Run(scenario, func(t *testing.T) {
			s, f := qdrantFixture(t)
			db, err := s.InstallQdrant(context.Background(), "srv", "vectors", "", models.QdrantConfig{BindAddress: "127.0.0.1"}, func(string) {})
			if err != nil {
				t.Fatal(err)
			}
			previous, _ := s.QdrantConfiguration(db.ID)
			key := strings.Repeat("x", 40)
			health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("api-key") != key {
					w.WriteHeader(401)
					return
				}
				_, _ = w.Write([]byte(`{"status":"ok","result":{"collections":[]}}`))
			}))
			defer health.Close()
			_, portText, _ := net.SplitHostPort(strings.TrimPrefix(health.URL, "http://"))
			port, _ := strconv.Atoi(portText)
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(qdrantNetworkDockerFixture), 0700); err != nil {
				t.Fatal(err)
			}
			credential := key
			if scenario == "missing-key" {
				credential = ""
			}
			raw, _ := json.Marshal(map[string]interface{}{"service": map[string]string{"api_key": credential}})
			if err := os.WriteFile(filepath.Join(dir, "config.yaml"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			name := "gopulse-" + db.ID
			owner := db.ID
			if scenario == "wrong-owner" {
				owner = "other"
			}
			state := map[string]interface{}{name: map[string]interface{}{"Image": "sha256:original-image", "Config": map[string]interface{}{"Labels": map[string]string{"gopulse.database": owner}}, "State": map[string]bool{"Running": true}}}
			if scenario == "pending-backup" {
				state[name+"-network-backup"] = state[name]
			}
			raw, _ = json.Marshal(state)
			statePath := filepath.Join(dir, "state.json")
			if err := os.WriteFile(statePath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			f.run = func(_, command string) (string, error) {
				command = strings.TrimPrefix(command, "sudo -n ")
				command = strings.ReplaceAll(command, previous.StoragePath, dir)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "sh", "-c", command)
				cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "QDRANT_TEST_STATE="+statePath, "QDRANT_TEST_SCENARIO="+scenario)
				out, err := cmd.CombinedOutput()
				if strings.Contains(string(out), key) {
					t.Fatal("secret leaked in command output")
				}
				return string(out), err
			}
			cfg, err := s.ConfigureQdrantNetwork(context.Background(), db.ID, "0.0.0.0", port, 16334, func(string) {})
			if (err == nil) != (scenario == "success") {
				t.Fatalf("unexpected result: %v", err)
			}
			saved, _ := s.QdrantConfiguration(db.ID)
			raw, _ = os.ReadFile(statePath)
			var containers map[string]struct {
				Config struct{ Labels map[string]string }
				State  struct{ Running bool }
				Ports  []string
			}
			if err := json.Unmarshal(raw, &containers); err != nil {
				t.Fatal(err)
			}
			if scenario == "success" {
				if cfg.BindAddress != "0.0.0.0" || saved != cfg || len(containers) != 1 {
					t.Fatal("replacement not committed")
				}
				if !strings.Contains(strings.Join(containers[name].Ports, " "), fmt.Sprintf("0.0.0.0:%d:6333", port)) {
					t.Fatal("HTTP port not published")
				}
				if !strings.Contains(strings.Join(containers[name].Ports, " "), "0.0.0.0:16334:6334") {
					t.Fatal("gRPC port not published")
				}
				database, _ := s.GetDatabase(db.ID)
				if database.HostPort != port {
					t.Fatal("inventory port not updated")
				}
			} else if saved != previous || !containers[name].State.Running || containers[name].Config.Labels["gopulse.database"] != owner {
				t.Fatal("previous deployment changed after failure")
			}
			if scenario == "start-failure" && len(containers) != 1 {
				t.Fatal("failed candidate or backup retained after rollback")
			}
			credentialFile, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
			if !strings.Contains(string(credentialFile), credential) {
				t.Fatal("credential file changed")
			}
		})
	}
}

const qdrantNetworkDockerFixture = `#!/usr/bin/env python3
import json,os,sys
p=os.environ['QDRANT_TEST_STATE']
s=json.load(open(p)); a=sys.argv[1:]
def save():
 with open(p,'w') as f: json.dump(s,f)
if a[0]=='inspect':
 if a[1] not in s: sys.exit(1)
 print(json.dumps([s[a[1]]]))
elif a[0]=='ps':
 name=a[a.index('--filter')+1].removeprefix('name=^').removesuffix('$')
 if name in s: print(name)
elif a[0]=='stop': s[a[1]]['State']['Running']=False; save()
elif a[0]=='rename': s[a[2]]=s.pop(a[1]); save()
elif a[0]=='run':
 name=a[a.index('--name')+1]; owner=a[a.index('--label')+1].split('=',1)[1]
 s[name]={'Config':{'Labels':{'gopulse.database':owner}},'State':{'Running':True},'Ports':[a[i+1] for i,v in enumerate(a) if v=='-p']}; save()
 if os.environ['QDRANT_TEST_SCENARIO']=='start-failure': sys.exit(1)
 print('new-container')
elif a[0]=='rm': s.pop(a[-1]); save()
elif a[0]=='start': s[a[1]]['State']['Running']=True; save()
else: sys.exit(1)
`
