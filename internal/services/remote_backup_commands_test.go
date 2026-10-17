package services

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/google/uuid"
)

func TestRemoteBackupContainerScriptsWithCommandFixture(t *testing.T) {
	python, e := exec.LookPath("python3")
	if e != nil {
		t.Skip("python3 required for command fixture")
	}
	for _, engine := range []string{"postgres", "mysql", "mongodb", "redis"} {
		for _, fail := range []bool{false, true} {
			t.Run(engine+map[bool]string{false: "/success", true: "/native-failure"}[fail], func(t *testing.T) {
				root := t.TempDir()
				work := filepath.Join(root, "work's files")
				os.Mkdir(work, 0700)
				os.WriteFile(filepath.Join(work, "database.dump"), []byte("fixture payload"), 0600)
				log := filepath.Join(root, "commands.jsonl")
				program := `import os,sys,json
a=sys.argv[1:]
with open(os.environ['BACKUP_COMMAND_LOG'],'a') as f:f.write(json.dumps(a)+'\n')
if a[0]=='run': print('fixture-container');sys.exit(0)
if a[0] in ('rm','restart'): sys.exit(0)
if a[0]!='exec': sys.exit(2)
i=1
while a[i].startswith('-'):
 if a[i]=='-e': i+=2
 else:i+=1
container=a[i];cmd=a[i+1:];text=' '.join(cmd)
assert container.startswith('gopulse-restore-test-'),container
restore=('pg_restore' in text or 'mongorestore' in text or '--binary-mode' in text or cmd[0]=='sh')
if restore:
 assert sys.stdin.buffer.read()==b'fixture payload'
 if os.environ.get('BACKUP_NATIVE_FAIL')=='1':sys.exit(9)
if cmd[0]=='psql':
 if '-c' in cmd:print('SELECT count(*) FROM "table";')
 else:sys.stdin.read();print('2')
elif cmd[0]=='mysql':
 if any('information_schema.TABLES' in x for x in cmd):print('SELECT COUNT(*) FROM app.table;')
 elif not restore and not any(x.startswith('--execute') for x in cmd):sys.stdin.read();print('2')
elif cmd[0]=='mongosh' and 'getCollectionInfos' in text:print('Verified MongoDB restore: 1 collections read, 2 documents.')
elif cmd[0]=='redis-cli' and 'EVAL' in cmd:print('2')
`
				if e = os.WriteFile(filepath.Join(root, "docker"), []byte("#!"+python+"\n"+program), 0700); e != nil {
					t.Fatal(e)
				}
				id := uuid.NewString()
				a := models.RemoteBackupArtifact{Engine: engine, DatabaseName: "app", ImageID: "sha256:" + strings.Repeat("a", 64)}
				script, e := remoteBackupRestoreScript(models.RemoteBackupPlan{TestMemoryMB: 512}, a, id, work)
				if e != nil {
					t.Fatal(e)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "/bin/bash", "-euo", "pipefail", "-c", script)
				cmd.Env = append(os.Environ(), "PATH="+root+":"+os.Getenv("PATH"), "BACKUP_COMMAND_LOG="+log)
				if fail {
					cmd.Env = append(cmd.Env, "BACKUP_NATIVE_FAIL=1")
				}
				out, e := cmd.CombinedOutput()
				if fail {
					if e == nil {
						t.Fatal("native failure accepted", string(out))
					}
				} else if e != nil || !strings.HasPrefix(string(out), "Verified ") {
					t.Fatal(e, string(out))
				}
				data, _ := os.ReadFile(log)
				lines := strings.Split(strings.TrimSpace(string(data)), "\n")
				var commands [][]string
				for _, line := range lines {
					var args []string
					if e = json.Unmarshal([]byte(line), &args); e != nil {
						t.Fatal(e)
					}
					commands = append(commands, args)
				}
				run := strings.Join(commands[0], " ")
				for _, flag := range []string{"--pull=never", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--memory=512m", "--cpus=1", "--pids-limit=256", "--user="} {
					if !strings.Contains(run, flag) {
						t.Fatal("missing isolation", flag, run)
					}
				}
				if strings.Contains(run, "--publish") || strings.Contains(run, "--volume") || strings.Contains(run, "--privileged") {
					t.Fatal("unsafe container", run)
				}
				last := commands[len(commands)-1]
				if strings.Join(last, " ") != "rm --force --volumes gopulse-restore-test-"+id {
					t.Fatal("test container not cleaned", last)
				}
			})
		}
	}
}
func TestRemoteBackupDumpCredentialQuoting(t *testing.T) {
	for _, engine := range []string{"postgres", "mysql", "mongodb", "redis"} {
		t.Run(engine, func(t *testing.T) {
			root := t.TempDir()
			sentinel := filepath.Join(root, "injected")
			password := "p'$(touch " + sentinel + ")`false` \" value"
			script, e := remoteBackupDumpCommand(models.Database{ID: "safe", Type: engine, Username: "app", DatabaseName: "app", Password: password}, uuid.NewString())
			if e != nil {
				t.Fatal(e)
			}
			// A shell fixture consumes the generated argv. Shell metacharacters inside
			// credential arguments must never execute on the source host.
			fixture := "#!/bin/bash\nprintf '%s\\n' \"$@\" >> \"$BACKUP_COMMAND_LOG\"\nif [[ \" $* \" != *' rm -f '* ]]; then printf dump; fi\n"
			os.WriteFile(filepath.Join(root, "docker"), []byte(fixture), 0700)
			log := filepath.Join(root, "argv")
			cmd := exec.Command("/bin/bash", "-euo", "pipefail", "-c", script)
			cmd.Env = append(os.Environ(), "PATH="+root+":"+os.Getenv("PATH"), "BACKUP_COMMAND_LOG="+log)
			if out, e := cmd.CombinedOutput(); e != nil {
				t.Fatal(e, string(out))
			}
			if _, e = os.Stat(sentinel); !os.IsNotExist(e) {
				t.Fatal("credential command injection")
			}
			args, _ := os.ReadFile(log)
			if !strings.Contains(string(args), password) {
				t.Fatal("credential was altered", string(args))
			}
		})
	}
}
