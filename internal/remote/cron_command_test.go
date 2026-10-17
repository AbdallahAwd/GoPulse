package remote

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type cronSSHBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *cronSSHBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}
func (b *cronSSHBuffer) Text() string { b.mu.Lock(); defer b.mu.Unlock(); return b.Buffer.String() }

// SSH fixture never executes the supplied command; it only simulates protocol
// output, nonzero exit status, and cancellation on a loopback listener.
func TestCronSSHCombinedOutputExitAndCancellation(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil }}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(knownhosts.Line([]string{listener.Addr().String()}, signer.PublicKey())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPULSE_SSH_KNOWN_HOSTS", path)
	var wg sync.WaitGroup
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				server, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				for channel := range channels {
					if channel.ChannelType() != "session" {
						channel.Reject(ssh.UnknownChannelType, "session only")
						continue
					}
					session, requests, err := channel.Accept()
					if err != nil {
						continue
					}
					go func() {
						defer session.Close()
						for request := range requests {
							if request.Type != "exec" {
								request.Reply(true, nil)
								continue
							}
							var payload struct{ Command string }
							ssh.Unmarshal(request.Payload, &payload)
							request.Reply(true, nil)
							if payload.Command == "hang" {
								for request := range requests {
									request.Reply(true, nil)
								}
								return
							}
							io.WriteString(session, "stdout\n")
							io.WriteString(session.Stderr(), "stderr\n")
							session.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{7}))
							return
						}
					}()
				}
			}()
		}
	}()
	client, err := NewSSHClient(models.Server{Host: "127.0.0.1", Port: port, User: "fixture", AuthType: "password", Password: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	output := &cronSSHBuffer{}
	err = client.RunCommandOutputContext(context.Background(), "fixture-only", output)
	var exit *ssh.ExitError
	if !errors.As(err, &exit) || exit.ExitStatus() != 7 || !strings.Contains(output.Text(), "stdout") || !strings.Contains(output.Text(), "stderr") {
		t.Fatal("SSH output/exit lost", err, output.Text())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = client.RunCommandOutputContext(ctx, "hang", output)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatal("SSH cancellation failed", err)
	}
	client.Close()
	listener.Close()
	wg.Wait()
}
