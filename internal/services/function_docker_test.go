package services

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/google/uuid"
)

// Explicit opt-in: normal test runs never start Docker or contact a user's
// Docker daemon. Run on a disposable Linux Docker host with GNU timeout.
func TestFunctionsLiveDockerIsolation(t *testing.T) {
	if os.Getenv("GOPULSE_TEST_FUNCTION_DOCKER") != "1" {
		t.Skip("opt-in live Docker test; set GOPULSE_TEST_FUNCTION_DOCKER=1 on a disposable Linux Docker host")
	}
	for _, program := range []string{"docker", "timeout"} {
		if _, e := exec.LookPath(program); e != nil {
			t.Fatal(e)
		}
	}
	s, _, spec := newFunctionTest(t)
	host := backupSSHFixture(t)
	host.ID = "a"
	if e := s.Servers.UpdateServer(host); e != nil {
		t.Fatal(e)
	}
	b := NewSSHFunctionBackend(s.Servers)
	s.backend = b
	spec.Code = "import os,socket\ndef handler(event, context):\n assert os.getuid()==10001\n try:\n  open('/etc/gopulse-write-test','w')\n  raise AssertionError('root filesystem writable')\n except OSError: pass\n sock=socket.socket();sock.settimeout(0.2)\n try:\n  sock.connect(('1.1.1.1',80))\n  raise AssertionError('network access allowed')\n except OSError: pass\n finally: sock.close()\n return {'isolation':'passed','value':event['value']}\n"
	f := deployedFunction(t, s, spec)
	r, e := s.Begin("a", f.ID, "owner", "invoke", "manual", json.RawMessage(`{"value":42}`))
	if e != nil {
		t.Fatal(e)
	}
	got := waitFunction(t, s, r)
	if got.Status != "succeeded" || !strings.Contains(got.Logs, "passed") {
		t.Fatal(got)
	}
	// Use the pinned image and production runner to test the remote watchdog.
	v := *f.Deployed
	v.Spec.Code = "import time\ndef handler(event, context):\n time.sleep(30)\n"
	v.Spec.TimeoutSeconds = 1
	r = models.FunctionRun{ID: uuid.NewString(), FunctionID: f.ID, ServerID: "a", Kind: "invoke"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var logs FunctionLogs
	code, e := b.Execute(ctx, v, r, nil, &logs)
	if e == nil || code != 124 {
		t.Fatal("remote timeout failed", code, e)
	}
	if e = b.Cleanup(ctx, r); e != nil {
		t.Fatal(e)
	}
	v.Spec.Code = "def handler(event, context):\n return len(bytearray(1024*1024*1024))\n"
	v.Spec.MemoryMB = 128
	v.Spec.TimeoutSeconds = 10
	memoryCtx, memoryCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer memoryCancel()
	r.ID = uuid.NewString()
	code, e = b.Execute(memoryCtx, v, r, nil, &logs)
	if e == nil || code == 0 || code == 124 || code < 0 {
		t.Fatal("memory limit failed", code, e)
	}
	if e = b.Cleanup(memoryCtx, r); e != nil {
		t.Fatal(e)
	}
}
