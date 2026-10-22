package services

import (
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"testing"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"golang.org/x/crypto/ssh"
)

func TestManagerRefreshesCachedSSHTrustAndCredentials(t *testing.T) {
	servers, err := store.NewServerStore(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	server := models.Server{ID: "one", Host: "host.invalid", Port: 22, TrustedHostKey: string(ssh.MarshalAuthorizedKey(key))}
	if err := servers.AddServer(server); err != nil {
		t.Fatal(err)
	}
	manager := NewServiceManager(servers)
	defer manager.CloseAll()
	first, err := manager.GetSSHClient("one")
	if err != nil {
		t.Fatal(err)
	}
	cached, err := manager.GetSSHClient("one")
	if err != nil || cached != first {
		t.Fatal("unchanged client not reused")
	}
	public, _, err = ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err = ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	if err := servers.TrustHostKey("one", server.Host, server.Port, string(ssh.MarshalAuthorizedKey(key))); err != nil {
		t.Fatal(err)
	}
	replacement, err := manager.GetSSHClient("one")
	if err != nil || replacement == first {
		t.Fatal("old trust reused")
	}
	server, err = servers.GetServer("one")
	if err != nil {
		t.Fatal(err)
	}
	server.Password = "new-password"
	if err := servers.UpdateServer(server); err != nil {
		t.Fatal(err)
	}
	updated, err := manager.GetSSHClient("one")
	if err != nil || updated == replacement {
		t.Fatal("old credentials reused")
	}
}
