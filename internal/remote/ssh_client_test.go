package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"github.com/amaa/gopulse/internal/models"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestSSHHostVerificationAndCancellation(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(knownhosts.Line([]string{"[127.0.0.1]:2222"}, key)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPULSE_SSH_KNOWN_HOSTS", path)
	client, err := NewSSHClient(models.Server{Host: "127.0.0.1", Port: 2222, User: "fixture", AuthType: "password"})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.config.HostKeyCallback("127.0.0.1:2222", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2222}, key); err != nil {
		t.Fatalf("trusted key rejected: %v", err)
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	wrong, _ := ssh.NewPublicKey(other)
	if client.config.HostKeyCallback("127.0.0.1:2222", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2222}, wrong) == nil {
		t.Fatal("mismatched host key accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.RunCommandContext(ctx, "touch /tmp/should-never-run"); err == nil {
		t.Fatal("cancelled execution accepted")
	}
}
func TestSSHMissingTrustedHostKeysFailsClosed(t *testing.T) {
	t.Setenv("GOPULSE_SSH_KNOWN_HOSTS", filepath.Join(t.TempDir(), "missing"))
	if _, err := NewSSHClient(models.Server{AuthType: "password"}); err == nil {
		t.Fatal("missing trust accepted")
	}
}
