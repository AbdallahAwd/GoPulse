package services

import (
	"bytes"
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

// This executable command fixture checks the actual Docker argv and stdin,
// then runs the real language wrapper using local interpreters. It does not
// emulate namespaces, cgroups, seccomp or a Docker daemon.
func functionCommandFixture(t *testing.T) string {
	t.Helper()
	python, e := exec.LookPath("python3")
	if e != nil {
		t.Skip("python3 required for function command fixture")
	}
	node, e := exec.LookPath("node")
	if e != nil {
		t.Skip("node required for function command fixture")
	}
	root := t.TempDir()
	log := filepath.Join(root, "commands.jsonl")
	program := `import os,sys,json,subprocess
args=sys.argv[1:]
with open(os.environ['FN_COMMAND_LOG'],'a') as f:f.write(json.dumps(args)+'\n')
if args[0]=='info':
 if os.environ.get('FN_OFFLINE')=='1':sys.exit(1)
 print(json.dumps({'MemoryLimit':True,'SwapLimit':True,'CpuCfsQuota':os.environ.get('FN_UNSUPPORTED')!='1','PidsLimit':True,'SecurityOptions':['name=seccomp,profile='+os.environ.get('FN_SECCOMP','builtin')]}));sys.exit(0)
if args[0]=='pull':print('Runtime downloaded');sys.exit(0)
if args[:2]==['image','inspect']:print('sha256:'+'a'*64);sys.exit(0)
if args[:2]==['container','ls']:
 if os.environ.get('FN_LIST_FAILED')=='1':sys.exit(1)
 if os.environ.get('FN_CONTAINER')=='1':print(args[args.index('--filter')+1][7:-1])
 sys.exit(0)
if args[0]=='inspect':
 print('foreign-owner' if os.environ.get('FN_FOREIGN')=='1' else os.environ.get('FN_RUN_ID',''));sys.exit(0)
if args[0]=='rm':sys.exit(0)
assert args[0]=='run',args
for flag in ['--rm','--init','--interactive','--pull=never','--network=none','--read-only','--user=10001:10001','--cap-drop=ALL','--security-opt=no-new-privileges','--pids-limit=64','--ulimit','--log-driver=none']:
 assert flag in args,flag
assert any(x.startswith('--memory=') for x in args)
assert any(x.startswith('--memory-swap=') for x in args)
assert any(x.startswith('--cpus=') for x in args)
assert not any(x in args for x in ['--volume','--mount','-v','--privileged','--publish'])
i=next(i for i,x in enumerate(args) if x.startswith('sha256:'))
assert args[i]=='sha256:'+'a'*64
cmd=args[i+1:]
if cmd[0]=='python':cmd[0]=os.environ['FN_PYTHON']
else:
 assert cmd[0]=='node'
 cmd[0]=os.environ['FN_NODE']
 cmd[-1]=cmd[-1].replace('/tmp/handler.cjs',os.environ['FN_HANDLER_PATH'])
if os.environ.get('FN_TIMEOUT')=='1':sys.exit(124)
sys.exit(subprocess.run(cmd,stdin=sys.stdin.buffer).returncode)
`
	if e = os.WriteFile(filepath.Join(root, "docker"), []byte("#!"+python+"\n"+program), 0700); e != nil {
		t.Fatal(e)
	}
	// macOS has no GNU timeout. Verify its argv, then run its child in this
	// fixture; actual watchdog/cgroup enforcement needs the optional Docker test.
	if e = os.WriteFile(filepath.Join(root, "timeout"), []byte("#!"+python+"\nimport sys,os\na=sys.argv[1:]\nassert a[0]=='--signal=TERM' and a[1]=='--kill-after=5s' and a[2].endswith('s')\nos.execvp(a[3],a[3:])\n"), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", root+":"+os.Getenv("PATH"))
	t.Setenv("FN_COMMAND_LOG", log)
	t.Setenv("FN_PYTHON", python)
	t.Setenv("FN_NODE", node)
	t.Setenv("FN_HANDLER_PATH", filepath.Join(root, "handler.cjs"))
	return log
}
func TestFunctionsRealSSHWithLanguageAndDockerCommandFixtures(t *testing.T) {
	log := functionCommandFixture(t)
	host := backupSSHFixture(t)
	s, _, spec := newFunctionTest(t)
	host.ID = "a"
	if e := s.Servers.UpdateServer(host); e != nil {
		t.Fatal(e)
	}
	s.backend = NewSSHFunctionBackend(s.Servers)
	for _, runtime := range []string{"python", "javascript"} {
		t.Run(runtime, func(t *testing.T) {
			spec.Runtime = runtime
			spec.HTTPEnabled = true
			if runtime == "python" {
				spec.Code = "import sys\nasync def handler(event, context):\n print('stdout λ')\n print('stderr line',file=sys.stderr)\n return {'answer':event['value']+1}\n"
			} else {
				spec.Code = "module.exports.handler = async (event, context) => { console.log('stdout λ'); console.error('stderr line'); return {answer:event.value+1}; };"
			}
			f := deployedFunction(t, s, spec)
			r, e := s.Begin("a", f.ID, "owner", "invoke", "manual", json.RawMessage(`{"value":41}`))
			if e != nil {
				t.Fatal(e)
			}
			r = waitFunction(t, s, r)
			for _, fragment := range []string{"stdout λ", "stderr line", "42"} {
				if r.Status != "succeeded" || !strings.Contains(r.Logs, fragment) || r.NeedsCleanup {
					t.Fatal(r)
				}
			}
			spec.Code = "not valid code !!!"
			if _, e = s.Save("a", f.ID, "owner", spec); e != nil {
				t.Fatal(e)
			}
			r, e = s.Begin("a", f.ID, "owner", "deploy", "manual", nil)
			if e != nil {
				t.Fatal(e)
			}
			r = waitFunction(t, s, r)
			if r.Status != "failed" || !strings.Contains(r.Logs, "SyntaxError") {
				t.Fatal(r)
			}
			current, _ := s.Store.Get("a", f.ID)
			if current.Deployed.ID != f.Deployed.ID {
				t.Fatal("invalid syntax activated")
			}
		})
	}
	data, e := os.ReadFile(log)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(data), "value\":41") || strings.Contains(string(data), "return {answer") {
		t.Fatal("source/event became host command arguments")
	}
}
func TestFunctionCleanupFailsClosed(t *testing.T) {
	functionCommandFixture(t)
	host := backupSSHFixture(t)
	s, _, _ := newFunctionTest(t)
	host.ID = "a"
	if e := s.Servers.UpdateServer(host); e != nil {
		t.Fatal(e)
	}
	b := NewSSHFunctionBackend(s.Servers)
	r := models.FunctionRun{ID: uuid.NewString(), ServerID: "a"}
	t.Setenv("FN_RUN_ID", r.ID)
	for _, flag := range []string{"FN_OFFLINE", "FN_LIST_FAILED", "FN_FOREIGN"} {
		t.Run(flag, func(t *testing.T) {
			t.Setenv("FN_CONTAINER", "1")
			t.Setenv(flag, "1")
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if e := b.Cleanup(ctx, r); e == nil {
				t.Fatal("uncertain cleanup accepted")
			}
		})
	}
	t.Setenv("FN_CONTAINER", "1")
	if e := b.Cleanup(context.Background(), r); e != nil {
		t.Fatal(e)
	}
}
func TestFunctionContainerCommandRejectsInjection(t *testing.T) {
	_, _, spec := newFunctionTest(t)
	r := models.FunctionRun{ID: uuid.NewString()}
	valid := "sha256:" + strings.Repeat("a", 64)
	for _, image := range []string{"node:latest", valid + "; touch /tmp/escape", "sha256:" + strings.Repeat("z", 64)} {
		if _, e := functionContainerScript(spec, r, image); e == nil {
			t.Fatal("invalid image accepted")
		}
	}
	r.ID = "../../escape"
	if _, e := functionContainerScript(spec, r, valid); e == nil {
		t.Fatal("invalid ID accepted")
	}
}
func TestFunctionNativeWrappersErrorAndValidation(t *testing.T) {
	for _, runtime := range []string{"python", "javascript"} {
		command, wrapper := "python3", functionPythonWrapper
		args := []string{"-I", "-u", "-c"}
		source := "def handler(event, context):\n raise RuntimeError('fixture failure')\n"
		if runtime == "javascript" {
			command, wrapper = "node", strings.ReplaceAll(functionJSWrapper, "/tmp/handler.cjs", filepath.Join(t.TempDir(), "handler.cjs"))
			args = []string{"-e"}
			source = "module.exports.handler = async () => { throw new Error('fixture failure'); };"
		}
		if _, e := exec.LookPath(command); e != nil {
			t.Skip(e)
		}
		for _, validate := range []bool{true, false} {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			cmd := exec.CommandContext(ctx, command, append(args, wrapper)...)
			spec := models.FunctionSpec{Code: source}
			cmd.Stdin = bytes.NewReader(functionPayload(spec, models.FunctionRun{}, nil, validate))
			out, e := cmd.CombinedOutput()
			cancel()
			if validate && e != nil {
				t.Fatal(e, string(out))
			}
			if !validate && (e == nil || !strings.Contains(string(out), "fixture failure")) {
				t.Fatal("native error accepted", e, string(out))
			}
		}
	}
}

func TestFunctionsDeploymentRequiresHostProtection(t *testing.T) {
	functionCommandFixture(t)
	host := backupSSHFixture(t)
	s, _, spec := newFunctionTest(t)
	host.ID = "a"
	if e := s.Servers.UpdateServer(host); e != nil {
		t.Fatal(e)
	}
	backend := NewSSHFunctionBackend(s.Servers)
	for _, flag := range []string{"FN_UNSUPPORTED", "FN_SECCOMP"} {
		t.Run(flag, func(t *testing.T) {
			value := "1"
			if flag == "FN_SECCOMP" {
				value = "unconfined"
			}
			t.Setenv(flag, value)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var logs FunctionLogs
			_, e := backend.Prepare(ctx, models.Function{ServerID: "a", Draft: spec}, models.FunctionRun{ID: uuid.NewString()}, &logs)
			if e == nil {
				t.Fatal("unsafe host accepted")
			}
		})
	}
}

func TestFunctionRemoteWatchdogExitStatus(t *testing.T) {
	functionCommandFixture(t)
	t.Setenv("FN_TIMEOUT", "1")
	host := backupSSHFixture(t)
	s, _, spec := newFunctionTest(t)
	host.ID = "a"
	if e := s.Servers.UpdateServer(host); e != nil {
		t.Fatal(e)
	}
	b := NewSSHFunctionBackend(s.Servers)
	v := models.FunctionVersion{Spec: spec, ImageID: "sha256:" + strings.Repeat("a", 64)}
	r := models.FunctionRun{ID: uuid.NewString(), ServerID: "a"}
	var logs FunctionLogs
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	code, e := b.Execute(ctx, v, r, nil, &logs)
	if e == nil || code != 124 {
		t.Fatal(code, e)
	}
	t.Setenv("FN_SECCOMP", "unconfined")
	if _, e = b.Execute(ctx, v, r, nil, &logs); e == nil || !strings.Contains(e.Error(), "seccomp") {
		t.Fatal("host protections were not rechecked", e)
	}
}
