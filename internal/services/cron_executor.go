package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/amaa/gopulse/internal/models"
	"github.com/amaa/gopulse/internal/remote"
	"github.com/amaa/gopulse/internal/store"
	"golang.org/x/crypto/ssh"
)

const cronOutputLimit = 64 * 1024

type cronOutput struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (b *cronOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := cronOutputLimit - len(b.data)
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	b.data = append(b.data, p...)
	return n, nil
}
func (b *cronOutput) result() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.ToValidUTF8(string(b.data), "�"), b.truncated
}

type ServerCronExecutor struct{ Servers *store.ServerStore }

func (e *ServerCronExecutor) Execute(ctx context.Context, job models.CronJob) CronResult {
	server, err := e.Servers.GetServer(job.ServerID)
	if err != nil {
		return CronResult{Err: err}
	}
	output := &cronOutput{}
	var exit *int
	var timedOut bool
	if server.Host == "localhost" || server.Host == "127.0.0.1" || server.Host == "::1" {
		exit, timedOut, err = executeLocalCron(ctx, job, output)
	} else {
		// Dedicated SSH connection: job timeout/cancellation cannot disconnect
		// another resource's terminal, database query, or monitor session.
		client, clientErr := remote.NewSSHClient(server)
		if clientErr != nil {
			return CronResult{Err: clientErr}
		}
		defer client.Close()
		command := job.Command
		if job.WorkingDir != "" {
			command = "cd -- " + cronShellQuote(job.WorkingDir) + " && exec /bin/sh -c " + cronShellQuote(command)
		}
		// The remote watchdog survives control-plane/SSH disconnects. Refuse to
		// execute without timeout rather than leaving an unbounded remote job.
		command = "command -v timeout >/dev/null 2>&1 || { printf '%s\\n' 'GNU timeout is required on this server'; exit 125; }; exec timeout --signal=TERM --kill-after=5s " + fmt.Sprint(job.TimeoutSeconds) + "s /bin/sh -c " + cronShellQuote(command)
		remoteCtx, cancel := context.WithTimeout(ctx, time.Duration(job.TimeoutSeconds+15)*time.Second)
		defer cancel()
		err = client.RunCommandOutputContext(remoteCtx, command, output)
		if err == nil {
			code := 0
			exit = &code
		} else {
			var status *ssh.ExitError
			if errors.As(err, &status) {
				code := status.ExitStatus()
				exit = &code
				timedOut = code == 124 || code == 137
			}
		}
		if errors.Is(remoteCtx.Err(), context.DeadlineExceeded) {
			timedOut = true
		}
	}
	text, truncated := output.result()
	// A truncated multi-byte rune can expand during UTF-8 repair; keep JSON bounded.
	if len(text) > cronOutputLimit {
		text = text[:cronOutputLimit]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
		truncated = true
	}
	return CronResult{Output: text, ExitCode: exit, Truncated: truncated, TimedOut: timedOut, Err: err}
}
func cronShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
