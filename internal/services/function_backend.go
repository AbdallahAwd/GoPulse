package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/remote"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

const FunctionLogLimit = 64 * 1024

// The allowlist is deliberate: no user-controlled images, entrypoints or flags.
var functionImages = map[string]string{"python": "python:3.13-alpine", "javascript": "node:24-alpine"}

type FunctionBackend interface {
	Prepare(context.Context, models.Function, models.FunctionRun, io.Writer) (string, error)
	Execute(context.Context, models.FunctionVersion, models.FunctionRun, json.RawMessage, io.Writer) (int, error)
	Cleanup(context.Context, models.FunctionRun) error
}
type FunctionLogs struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
	result    json.RawMessage
}

func (l *FunctionLogs) SetResult(value json.RawMessage) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.result = append(json.RawMessage(nil), value...)
}
func (l *FunctionLogs) Result() json.RawMessage {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append(json.RawMessage(nil), l.result...)
}

func (l *FunctionLogs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(p)
	space := FunctionLogLimit - len(l.data)
	if len(p) > space {
		l.truncated = true
		p = p[:space]
	}
	l.data = append(l.data, p...)
	return n, nil
}
func (l *FunctionLogs) Snapshot() (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	text := strings.ToValidUTF8(string(l.data), "�")
	truncated := l.truncated
	if len(text) > FunctionLogLimit {
		text = text[:FunctionLogLimit]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
		truncated = true
	}
	return text, truncated
}

// Fixed wrappers read source and event only through stdin. They never become
// host commands or files. User code runs inside the resource-limited container.
const functionPythonWrapper = `import sys,json,ast,asyncio,inspect
p=json.load(sys.stdin)
code=compile(p['code'],'handler.py','exec')
if p['validate']:
    tree=ast.parse(p['code'])
    if not any(isinstance(n,(ast.FunctionDef,ast.AsyncFunctionDef)) and n.name=='handler' for n in tree.body):
        raise ValueError('Define handler(event, context)')
    print('Python syntax and handler validated')
else:
    scope={'__name__':'gopulse_function'}
    exec(code,scope)
    result=scope['handler'](p['event'],p['context'])
    if inspect.isawaitable(result): result=asyncio.run(result)
    encoded=json.dumps({'result':result},allow_nan=False)
    if len(encoded.encode('utf-8'))>32768: raise ValueError('Function result exceeds 32 KiB')
    print('\n\x1e'+p['result_marker']+':'+encoded)
`
const functionJSWrapper = `const fs=require('node:fs');
(async()=>{
 const p=JSON.parse(fs.readFileSync(0,'utf8'));
 if(p.validate){new (require('node:vm').Script)(p.code,{filename:'handler.cjs'}); console.log('JavaScript syntax validated; export module.exports.handler');return;}
 fs.writeFileSync('/tmp/handler.cjs',p.code,{mode:0o600});
 const {handler}=require('/tmp/handler.cjs');
 if(typeof handler!=='function')throw new Error('Export module.exports.handler = async (event, context) => ...');
 const result=await handler(p.event,p.context);
 const encoded=JSON.stringify({result:result??null});
 if(Buffer.byteLength(encoded)>32768)throw new Error('Function result exceeds 32 KiB');
 console.log('\n\x1e'+p.result_marker+':'+encoded);
})().catch(e=>{console.error(e.stack||String(e));process.exitCode=1;});
`

type SSHFunctionBackend struct {
	Servers   *store.ServerStore
	newClient func(models.Server) (*remote.SSHClient, error)
}

func NewSSHFunctionBackend(servers *store.ServerStore) *SSHFunctionBackend {
	return &SSHFunctionBackend{servers, remote.NewSSHClient}
}
func (b *SSHFunctionBackend) run(ctx context.Context, server, command string, input io.Reader, logs io.Writer) error {
	host, err := b.Servers.GetServer(server)
	if err != nil {
		return err
	}
	client, err := b.newClient(host)
	if err != nil {
		return err
	}
	defer client.Close()
	return client.RunFunctionContext(ctx, command, input, logs)
}
func functionRunName(r models.FunctionRun) (string, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil || id.String() != r.ID {
		return "", fmt.Errorf("invalid invocation ID")
	}
	return "gopulse-function-" + r.ID, nil
}
func functionContainerScript(spec models.FunctionSpec, r models.FunctionRun, image string) (string, error) {
	name, err := functionRunName(r)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(image, "sha256:") || len(image) != 71 {
		return "", fmt.Errorf("invalid pinned image")
	}
	for _, c := range image[7:] {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return "", fmt.Errorf("invalid pinned image")
		}
	}
	executable, wrapper := "python", functionPythonWrapper
	args := "-I -u -c"
	if spec.Runtime == "javascript" {
		executable, wrapper, args = "node", functionJSWrapper, "--disable-proto=throw -e"
	} else if spec.Runtime != "python" {
		return "", fmt.Errorf("invalid runtime")
	}
	// A shell trap and a separate durable cleanup job cover both normal exit and
	// SSH loss. The watchdog runs on the managed host, not just the controller.
	script := fmt.Sprintf(`set -eu
cleanup() { if [ "$(docker inspect --format '{{index .Config.Labels "gopulse.function.run"}}' %s 2>/dev/null)" = %s ]; then docker rm --force %s >/dev/null 2>&1 || true; fi; }
trap cleanup EXIT
trap 'exit 143' HUP INT TERM
set +e
timeout --signal=TERM --kill-after=5s %ds docker run --rm --init --interactive --pull=never --name %s --label %s --network=none --read-only --user=10001:10001 --cap-drop=ALL --security-opt=no-new-privileges --memory=%dm --memory-swap=%dm --cpus=%.3f --pids-limit=64 --ulimit nofile=128:128 --ulimit core=0 --log-driver=none --tmpfs /tmp:rw,noexec,nosuid,nodev,size=64m,mode=1777 --workdir=/tmp --env HOME=/tmp --env PYTHONDONTWRITEBYTECODE=1 %s %s %s %s
rc=$?
exit "$rc"`, shellQuote(name), shellQuote(r.ID), shellQuote(name), spec.TimeoutSeconds, shellQuote(name), shellQuote("gopulse.function.run="+r.ID), spec.MemoryMB, spec.MemoryMB, float64(spec.CPUMillis)/1000, shellQuote(image), executable, args, shellQuote(wrapper))
	return "exec /bin/bash -c " + shellQuote(script), nil
}
func functionPayload(spec models.FunctionSpec, r models.FunctionRun, event json.RawMessage, validate bool) []byte {
	if len(event) == 0 {
		event = json.RawMessage(`{}`)
	}
	data, _ := json.Marshal(map[string]interface{}{"code": spec.Code, "event": event, "validate": validate, "result_marker": r.ID, "context": map[string]interface{}{"invocation_id": r.ID, "function_id": r.FunctionID, "version_id": r.VersionID, "trigger": r.Trigger}})
	return data
}
func (b *SSHFunctionBackend) checkHost(ctx context.Context, server string) error {
	var info FunctionLogs
	if err := b.run(ctx, server, "docker info --format '{{json .}}'", nil, &info); err != nil {
		return fmt.Errorf("Docker is unavailable; verify SSH and Docker access")
	}
	raw, _ := info.Snapshot()
	var capabilities struct {
		MemoryLimit, SwapLimit, CPUCfsQuota, PidsLimit bool
		SecurityOptions                                []string
	}
	if err := json.Unmarshal([]byte(raw), &capabilities); err != nil {
		return fmt.Errorf("cannot read Docker resource capabilities")
	}
	if !capabilities.MemoryLimit || !capabilities.SwapLimit || !capabilities.CPUCfsQuota || !capabilities.PidsLimit {
		return fmt.Errorf("host must support memory, swap, CPU quota and PID limits")
	}
	seccomp := false
	for _, option := range capabilities.SecurityOptions {
		if option == "name=seccomp,profile=builtin" || option == "name=seccomp,profile=default" {
			seccomp = true
		}
	}
	if !seccomp {
		return fmt.Errorf("Docker default seccomp protection is required")
	}
	return nil
}
func (b *SSHFunctionBackend) Prepare(ctx context.Context, f models.Function, r models.FunctionRun, logs io.Writer) (string, error) {
	image, ok := functionImages[f.Draft.Runtime]
	if !ok {
		return "", fmt.Errorf("unsupported runtime")
	}
	if err := b.checkHost(ctx, f.ServerID); err != nil {
		return "", err
	}
	fmt.Fprintln(logs, "Resolving runtime image "+image)
	if err := b.run(ctx, f.ServerID, "timeout --signal=TERM --kill-after=5s 180s docker pull "+shellQuote(image), nil, logs); err != nil {
		return "", fmt.Errorf("runtime image pull failed")
	}
	var pinned FunctionLogs
	if err := b.run(ctx, f.ServerID, "docker image inspect --format '{{.Id}}' "+shellQuote(image), nil, &pinned); err != nil {
		return "", fmt.Errorf("runtime image resolution failed")
	}
	id, _ := pinned.Snapshot()
	id = strings.TrimSpace(id)
	spec := f.Draft
	spec.TimeoutSeconds = 30
	command, err := functionContainerScript(spec, r, id)
	if err != nil {
		return "", err
	}
	err = b.run(ctx, f.ServerID, command, bytes.NewReader(functionPayload(spec, r, nil, true)), logs)
	if err != nil {
		return "", fmt.Errorf("function validation failed; inspect deployment logs")
	}
	return id, nil
}
func (b *SSHFunctionBackend) Execute(ctx context.Context, v models.FunctionVersion, r models.FunctionRun, event json.RawMessage, logs io.Writer) (int, error) {
	if err := b.checkHost(ctx, r.ServerID); err != nil {
		fmt.Fprintln(logs, err.Error())
		return -1, err
	}
	command, err := functionContainerScript(v.Spec, r, v.ImageID)
	if err != nil {
		return -1, err
	}
	output := &functionOutput{logs: logs, marker: []byte("\x1e" + r.ID + ":")}
	err = b.run(ctx, r.ServerID, command, bytes.NewReader(functionPayload(v.Spec, r, event, false)), output)
	resultErr := output.finish()
	if err == nil {
		if resultErr != nil {
			fmt.Fprintln(logs, resultErr)
			return -1, resultErr
		}
		return 0, nil
	}
	var exit *ssh.ExitError
	if errors.As(err, &exit) {
		return exit.ExitStatus(), err
	}
	return -1, err
}
func (b *SSHFunctionBackend) Cleanup(ctx context.Context, r models.FunctionRun) error {
	name, err := functionRunName(r)
	if err != nil {
		return err
	}
	script := fmt.Sprintf(`set -eu
docker info >/dev/null 2>&1
found=$(docker container ls -a --filter "name=^/%s$" --format '{{.Names}}')
if [ "$found" = %s ]; then
 [ "$(docker inspect --format '{{index .Config.Labels "gopulse.function.run"}}' %s)" = %s ] || exit 1
 docker rm --force %s >/dev/null
fi`, name, shellQuote(name), shellQuote(name), shellQuote(r.ID), shellQuote(name))
	var logs FunctionLogs
	return b.run(ctx, r.ServerID, "timeout --signal=TERM --kill-after=5s 20s /bin/bash -o pipefail -c "+shellQuote(script), nil, &logs)
}

func validateFunctionSpec(spec models.FunctionSpec) error {
	if strings.TrimSpace(spec.Name) == "" || len(spec.Name) > 100 || strings.ContainsAny(spec.Name, "\x00\r\n") {
		return &FunctionValidationError{"Name must contain 1–100 bytes without control characters."}
	}
	if _, ok := functionImages[spec.Runtime]; !ok {
		return &FunctionValidationError{"Choose Python or JavaScript."}
	}
	if !utf8.ValidString(spec.Code) || len(spec.Code) == 0 || len(spec.Code) > 32768 || strings.ContainsRune(spec.Code, 0) {
		return &FunctionValidationError{"Function source must contain 1–32768 UTF-8 bytes."}
	}
	if spec.TimeoutSeconds < 1 || spec.TimeoutSeconds > 900 || spec.MemoryMB < 128 || spec.MemoryMB > 2048 || spec.CPUMillis < 100 || spec.CPUMillis > 2000 || spec.Concurrency < 1 || spec.Concurrency > 4 {
		return &FunctionValidationError{"Limits: timeout 1–900 seconds, memory 128–2048 MiB, CPU 100–2000 millicores, concurrency 1–4."}
	}
	if spec.Schedule != "" || spec.ScheduleEnabled {
		if _, err := ParseCronSchedule(spec.Schedule, spec.Timezone); err != nil {
			return &FunctionValidationError{err.Error()}
		}
	}
	return nil
}
