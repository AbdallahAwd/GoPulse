package remote

import (
	"context"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/ssh"
)

// SSH copies stdout and stderr concurrently. Expose only Write so io.Copy cannot
// bypass synchronization through a sink's optional ReaderFrom implementation.
type cronSSHOutput struct {
	mu   sync.Mutex
	sink io.Writer
}

func (w *cronSSHOutput) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sink.Write(data)
}

// RunCommandOutputContext writes combined stdout/stderr to a bounded caller-owned
// sink. Use a dedicated client; cancellation closes only that client's session.
func (c *SSHClient) RunCommandOutputContext(ctx context.Context, command string, output io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.Connect(); err != nil {
		return err
	}
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client == nil {
		return fmt.Errorf("SSH disconnected")
	}
	type result struct {
		session *ssh.Session
		err     error
	}
	sessions := make(chan result)
	go func() {
		session, err := client.NewSession()
		select {
		case sessions <- result{session, err}:
		case <-ctx.Done():
			if session != nil {
				session.Close()
			}
		}
	}()
	var session *ssh.Session
	select {
	case res := <-sessions:
		if res.err != nil {
			return res.err
		}
		session = res.session
	case <-ctx.Done():
		c.Close()
		return ctx.Err()
	}
	defer session.Close()
	sink := &cronSSHOutput{sink: output}
	session.Stdout, session.Stderr = sink, sink
	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		session.Signal(ssh.SIGTERM)
		session.Close()
		c.Close()
		<-done
		return ctx.Err()
	}
}
