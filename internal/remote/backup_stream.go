package remote

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"golang.org/x/crypto/ssh"
)

// Backup streams preserve the remote exit status. EOF from stdout alone does
// not prove that a database dump completed successfully.
type backupStream struct {
	completed  atomic.Bool
	stdout     io.Reader
	session    *ssh.Session
	client     *SSHClient
	done       chan error
	closeOnce  sync.Once
	finishOnce sync.Once
	finishErr  error
}

func (s *backupStream) Read(p []byte) (int, error) {
	n, err := s.stdout.Read(p)
	if err == io.EOF {
		s.finishOnce.Do(func() { s.finishErr = <-s.done; s.completed.Store(true) })
		if s.finishErr != nil {
			return n, fmt.Errorf("remote dump command failed: %w", s.finishErr)
		}
	}
	return n, err
}
func (s *backupStream) Close() error {
	s.closeOnce.Do(func() {
		if !s.completed.Load() {
			s.session.Signal(ssh.SIGTERM)
			s.session.Close()
			s.client.Close()
		} else {
			s.session.Close()
		}
	})
	return nil
}
func (c *SSHClient) StreamBackupContext(ctx context.Context, command string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := c.Connect(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client == nil {
		return nil, fmt.Errorf("SSH disconnected")
	}
	type sessionResult struct {
		session *ssh.Session
		err     error
	}
	ready := make(chan sessionResult)
	go func() {
		session, err := client.NewSession()
		select {
		case ready <- sessionResult{session, err}:
		case <-ctx.Done():
			if session != nil {
				session.Close()
			}
		}
	}()
	var session *ssh.Session
	select {
	case result := <-ready:
		if result.err != nil {
			return nil, result.err
		}
		session = result.session
	case <-ctx.Done():
		c.Close()
		return nil, ctx.Err()
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	session.Stderr = io.Discard
	if err = session.Start(command); err != nil {
		session.Close()
		return nil, err
	}
	stream := &backupStream{stdout: stdout, session: session, client: c, done: make(chan error, 1)}
	go func() { stream.done <- session.Wait() }()
	return stream, nil
}
