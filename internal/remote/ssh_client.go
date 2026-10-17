package remote

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHClient handles SSH connections to a remote server
type SSHClient struct {
	server models.Server
	config *ssh.ClientConfig
	client *ssh.Client
	mu     sync.Mutex
	sem    chan struct{} // Semaphore to limit concurrent sessions
}

// NewSSHClient creates a new SSHClient
func NewSSHClient(server models.Server) (*SSHClient, error) {
	var authMethod ssh.AuthMethod

	if server.AuthType == "key" {
		key, err := os.ReadFile(server.KeyPath)
		if err != nil {
			return nil, fmt.Errorf("unable to read private key: %v", err)
		}

		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("unable to parse private key: %v", err)
		}
		authMethod = ssh.PublicKeys(signer)
	} else {
		// Default to password
		authMethod = ssh.Password(server.Password)
	}

	hostsPath := os.Getenv("GOPULSE_SSH_KNOWN_HOSTS")
	if hostsPath == "" {
		hostsPath = filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts")
	}
	verifyHost, err := knownhosts.New(hostsPath)
	if err != nil {
		return nil, fmt.Errorf("trusted SSH host keys required (%s): %w", hostsPath, err)
	}
	config := &ssh.ClientConfig{
		User: server.User,
		Auth: []ssh.AuthMethod{
			authMethod,
		},
		HostKeyCallback: verifyHost,
		Timeout:         5 * time.Second,
	}

	return &SSHClient{
		server: server,
		config: config,
		sem:    make(chan struct{}, 10), // Allow up to 10 concurrent sessions (SFTP + Exec + Stats)
	}, nil
}

// Connect establishes the SSH connection
func (c *SSHClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		return nil
	}

	addr := net.JoinHostPort(c.server.Host, fmt.Sprint(c.server.Port))

	// Use net.DialTimeout to enable TCP KeepAlive
	conn, err := net.DialTimeout("tcp", addr, c.config.Timeout)
	if err != nil {
		return err
	}

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(15 * time.Second)
	}

	conn.SetDeadline(time.Now().Add(c.config.Timeout))
	cConn, chans, reqs, err := ssh.NewClientConn(conn, addr, c.config)
	if err != nil {
		conn.Close()
		return err
	}

	conn.SetDeadline(time.Time{})
	c.client = ssh.NewClient(cConn, chans, reqs)
	return nil
}

// RunCommand executes a command on the remote server and returns the output
func (c *SSHClient) RunCommand(cmd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return c.RunCommandContext(ctx, cmd)
}

// RunCommandContext never retries an executed command: transport loss is an uncertain outcome.
func (c *SSHClient) RunCommandContext(parent context.Context, cmd string) (string, error) {
	if err := parent.Err(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Minute)
	defer cancel()
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-c.sem }()
	if err := c.Connect(); err != nil {
		return "", err
	}
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client == nil {
		return "", fmt.Errorf("SSH disconnected")
	}
	type sessionResult struct {
		session *ssh.Session
		err     error
	}
	sessions := make(chan sessionResult)
	go func() {
		session, err := client.NewSession()
		select {
		case sessions <- sessionResult{session, err}:
		case <-ctx.Done():
			if session != nil {
				session.Close()
			}
		}
	}()
	var session *ssh.Session
	select {
	case result := <-sessions:
		if result.err != nil {
			c.Close()
			return "", result.err
		}
		session = result.session
	case <-ctx.Done():
		c.Close()
		return "", fmt.Errorf("SSH session creation cancelled: %w", ctx.Err())
	}
	defer session.Close()
	var stdout, stderr bytes.Buffer
	session.Stdout, session.Stderr = &stdout, &stderr
	done := make(chan error, 1)
	go func() { done <- session.Run(cmd) }()
	select {
	case err := <-done:
		if err != nil {
			return stdout.String() + "\n" + stderr.String(), err
		}
		return stdout.String(), nil
	case <-ctx.Done():
		c.Close()
		session.Close()
		return "", fmt.Errorf("remote command timed out or cancelled; verify remote state before retrying: %w", ctx.Err())
	}
}

// StreamCommand executes a command and returns immediate output stream
// The caller is responsible for reading the stream and closing it (Wait() is handled by read completion or context?)
// Actually, for SSH session, we need to return the stdout pipe and we need to keep session open until read is done.
// We can return a struct that wraps io.Reader and has a Close method that waits/closes session.
type SSHStream struct {
	Session *ssh.Session
	Stdout  io.Reader
	Stderr  io.Reader
}

func (s *SSHStream) Read(p []byte) (n int, err error) {
	return s.Stdout.Read(p)
}

func (s *SSHStream) Close() error {
	return s.Session.Close()
}

func (c *SSHClient) StreamCommand(cmd string) (io.ReadCloser, error) {
	if err := c.Connect(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	client := c.client
	c.mu.Unlock()

	if client == nil {
		return nil, fmt.Errorf("ssh client is nil")
	}

	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, err
	}

	// We might want to combine stderr?
	// stderr, _ := session.StderrPipe()

	if err := session.Start(cmd); err != nil {
		session.Close()
		return nil, err
	}

	return &SSHStream{Session: session, Stdout: stdout}, nil
}

// WrappedSFTPClient wraps sftp.Client to handle semaphore release on close
type WrappedSFTPClient struct {
	*sftp.Client
	done func()
}

// Close closes the SFTP client and releases the semaphore
func (c *WrappedSFTPClient) Close() error {
	err := c.Client.Close()
	if c.done != nil {
		c.done()
	}
	return err
}

// NewSFTPClient creates a new SFTP client using the existing SSH connection
// It acquires a semaphore slot to limit concurrent sessions
func (c *SSHClient) NewSFTPClient() (*WrappedSFTPClient, error) {
	// Acquire semaphore
	c.sem <- struct{}{}

	// Ensure connected
	if err := c.Connect(); err != nil {
		<-c.sem // Release if connect fails
		return nil, err
	}

	c.mu.Lock()
	client := c.client
	c.mu.Unlock()

	if client == nil {
		<-c.sem
		return nil, fmt.Errorf("ssh client is nil")
	}

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		<-c.sem
		return nil, err
	}

	return &WrappedSFTPClient{
		Client: sftpClient,
		done: func() {
			<-c.sem
		},
	}, nil
}

// IsRoot returns true if the connected user is root
func (c *SSHClient) IsRoot() bool {
	return c.server.User == "root"
}

// GetClient returns the underlying SSH client connection (used by terminal handler for direct PTY access)
func (c *SSHClient) GetClient() *ssh.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.client
}

// Close closes the SSH connection
func (c *SSHClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		err := c.client.Close()
		c.client = nil
		return err
	}
	return nil
}
