package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"golang.org/x/crypto/ssh"
)

func hostKeySigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func hostKeyFixture(t *testing.T, signers ...ssh.Signer) (models.Server, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	authCalls := &atomic.Int32{}
	config := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, _ []byte) (*ssh.Permissions, error) { authCalls.Add(1); return nil, nil }}
	for _, signer := range signers {
		config.AddHostKey(signer)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				server, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				for channel := range channels {
					channel.Reject(ssh.Prohibited, "fixture")
				}
			}()
		}
	}()
	return models.Server{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, User: "fixture", AuthType: "password", Password: "fixture-password"}, authCalls
}

func TestHostKeyDiscoveryNeverAuthenticatesAndPinSurvivesMissingKnownHosts(t *testing.T) {
	signer := hostKeySigner(t)
	server, authCalls := hostKeyFixture(t, signer)
	t.Setenv("GOPULSE_SSH_KNOWN_HOSTS", filepath.Join(t.TempDir(), "missing"))
	observed, err := ProbeSSHHostKey(context.Background(), server.Host, server.Port)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Fingerprint != ssh.FingerprintSHA256(signer.PublicKey()) || observed.Algorithm != ssh.KeyAlgoED25519 || observed.Host != server.Host || observed.Port != server.Port {
		t.Fatalf("wrong key: %+v", observed)
	}
	if authCalls.Load() != 0 {
		t.Fatal("probe attempted authentication")
	}
	server.TrustedHostKey = observed.PublicKey
	client, err := NewSSHClient(server)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	if authCalls.Load() != 1 {
		t.Fatal("pinned connection did not authenticate")
	}
}

func TestChangedHostKeyIsRejectedBeforeCredentials(t *testing.T) {
	server, authCalls := hostKeyFixture(t, hostKeySigner(t))
	server.TrustedHostKey = string(ssh.MarshalAuthorizedKey(hostKeySigner(t).PublicKey()))
	client, err := NewSSHClient(server)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Connect(); err == nil || !strings.Contains(err.Error(), "host key mismatch") {
		t.Fatalf("changed key accepted: %v", err)
	}
	if authCalls.Load() != 0 {
		t.Fatal("credentials sent to changed host key")
	}
}

func TestPinnedKeyControlsNegotiationIncludingRSA(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaSigner, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	server, _ := hostKeyFixture(t, hostKeySigner(t), rsaSigner)
	server.TrustedHostKey = string(ssh.MarshalAuthorizedKey(rsaSigner.PublicKey()))
	client, err := NewSSHClient(server)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Connect(); err != nil {
		t.Fatalf("RSA pin not negotiated: %v", err)
	}
}

func TestInvalidPinsAndEndpointsFailClosed(t *testing.T) {
	valid := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(hostKeySigner(t).PublicKey())))
	for _, raw := range []string{"", "not-a-key", valid + "\n" + valid, "command=\"unsafe\" " + valid, "junk\n" + valid} {
		if _, err := ParseHostKey(raw); err == nil {
			t.Errorf("invalid key accepted: %q", raw)
		}
	}
	if _, err := NewSSHClient(models.Server{TrustedHostKey: "bad key"}); err == nil {
		t.Fatal("malformed pin fell back to known_hosts")
	}
	for _, host := range []string{"", "host:22", "ssh://host", "a\nhost", "user@host", "[host]"} {
		if _, _, err := NormalizeSSHEndpoint(host, 22); err == nil {
			t.Errorf("invalid host accepted: %q", host)
		}
	}
	host, port, err := NormalizeSSHEndpoint(" [::1] ", 0)
	if err != nil || host != "::1" || port != 22 {
		t.Fatalf("IPv6 normalization: %q %d %v", host, port, err)
	}
	for _, port := range []int{-1, 65536} {
		if _, _, err := NormalizeSSHEndpoint("localhost", port); err == nil {
			t.Fatal("invalid port accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ProbeSSHHostKey(ctx, "127.0.0.1", 22); err == nil {
		t.Fatal("cancelled probe succeeded")
	}
}

func TestProbeCancellationInterruptsHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := ProbeSSHHostKey(ctx, "127.0.0.1", listener.Addr().(*net.TCPAddr).Port); done <- err }()
	select {
	case conn := <-accepted:
		defer conn.Close()
	case <-time.After(time.Second):
		t.Fatal("probe did not connect")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled handshake succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not interrupt handshake")
	}
}
