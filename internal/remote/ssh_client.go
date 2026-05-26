package remote

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/amaa/gopulse/internal/models"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
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

	config := &ssh.ClientConfig{
		User: server.User,
		Auth: []ssh.AuthMethod{
			authMethod,
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // Note: In production, use KnownHosts
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

	addr := fmt.Sprintf("%s:%d", c.server.Host, c.server.Port)

	// Use net.DialTimeout to enable TCP KeepAlive
	conn, err := net.DialTimeout("tcp", addr, c.config.Timeout)
	if err != nil {
		return err
	}

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(15 * time.Second)
	}

	cConn, chans, reqs, err := ssh.NewClientConn(conn, addr, c.config)
	if err != nil {
		conn.Close()
		return err
	}

	c.client = ssh.NewClient(cConn, chans, reqs)
	return nil
}

// RunCommand executes a command on the remote server and returns the output
func (c *SSHClient) RunCommand(cmd string) (string, error) {
	// Acquire semaphore to limit concurrent sessions
	c.sem <- struct{}{}
	defer func() {
		<-c.sem
		// Throttling: Wait a bit after releasing session to allow server to cleanup
		time.Sleep(100 * time.Millisecond)
	}()

	const maxRetries = 3
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		if err := c.Connect(); err != nil {
			lastErr = err
			log.Printf("Connect failed (attempt %d): %v", i+1, err)

			// Specific handling for Auth failures (MaxStartups/RateLimit)
			if strings.Contains(err.Error(), "unable to authenticate") {
				log.Println("Auth failed (likely server rate limit), waiting 5s...")
				time.Sleep(5 * time.Second)
			} else if i < maxRetries-1 {
				// Standard backoff
				time.Sleep(time.Duration(i+1) * 2000 * time.Millisecond)
			}
			continue
		}

		c.mu.Lock()
		client := c.client
		c.mu.Unlock()

		if client == nil {
			lastErr = fmt.Errorf("ssh client is nil after connect")
			continue
		}

		session, err := client.NewSession()
		if err != nil {
			lastErr = err
			// Check if connection is actually dead or just session limit?
			if _, _, reqErr := client.SendRequest("keepalive@openssh.com", true, nil); reqErr != nil {
				log.Printf("Session creation failed and connection seems dead: %v. Reconnecting...", reqErr)
				c.Close()
				continue
			}

			// Connection is alive, likely session limit reached
			log.Printf("Session creation failed but connection alive (MaxSessions?): %v. Retrying...", err)
			time.Sleep(1000 * time.Millisecond) // Wait longer for sessions to clear
			continue
		}

		// Use a closure to handle session execution to ensure Close is called via defer
		output, err := func() (string, error) {
			defer session.Close() // Ensure session is closed

			var stdout, stderr bytes.Buffer
			session.Stdout = &stdout
			session.Stderr = &stderr

			if err := session.Run(cmd); err != nil {
				// Return combined output on error for context
				return fmt.Sprintf("%s\n%s", stdout.String(), stderr.String()), err
			}
			// Return only stdout on success
			return stdout.String(), nil
		}()

		if err != nil {
			if _, ok := err.(*ssh.ExitError); ok {
				// Command ran but returned non-zero exit code
				// This is a valid execution, not a network error
				return output, err
			}

			// Network or transport error
			log.Printf("SSH command failed with transport error: %v. Retrying...", err)
			lastErr = err
			c.Close() // Force reconnection
			time.Sleep(200 * time.Millisecond)
			continue
		}

		// Success
		return output, nil
	}

	return "", fmt.Errorf("command failed after %d attempts: %w", maxRetries, lastErr)
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
