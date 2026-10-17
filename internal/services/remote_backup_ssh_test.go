package services

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/remote"
	"github.com/google/uuid"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// A real SSH and SFTP endpoint using disposable local files. Only the outer
// GNU timeout wrapper is replaced on macOS; all backup/restore shell runs intact.
func backupSSHFixture(t *testing.T) models.Server {
	t.Helper()
	_, private, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	signer, e := ssh.NewSignerFromKey(private)
	if e != nil {
		t.Fatal(e)
	}
	config := &ssh.ServerConfig{PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil }}
	config.AddHostKey(signer)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	connections := map[net.Conn]bool{}
	ctx, cancel := context.WithCancel(context.Background())
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			mu.Lock()
			connections[conn] = true
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				defer func() { mu.Lock(); delete(connections, conn); mu.Unlock() }()
				_, channels, requests, e := ssh.NewServerConn(conn, config)
				if e != nil {
					return
				}
				go ssh.DiscardRequests(requests)
				for ch := range channels {
					if ch.ChannelType() != "session" {
						ch.Reject(ssh.UnknownChannelType, "session required")
						continue
					}
					channel, reqs, e := ch.Accept()
					if e != nil {
						continue
					}
					wg.Add(1)
					go func() {
						defer wg.Done()
						defer channel.Close()
						for req := range reqs {
							var payload struct{ Value string }
							if ssh.Unmarshal(req.Payload, &payload) != nil {
								req.Reply(false, nil)
								continue
							}
							switch req.Type {
							case "subsystem":
								if payload.Value != "sftp" {
									req.Reply(false, nil)
									continue
								}
								req.Reply(true, nil)
								server, e := sftp.NewServer(channel)
								if e == nil {
									server.Serve()
									server.Close()
								}
								return
							case "exec":
								req.Reply(true, nil)
								command := payload.Value
								if strings.HasPrefix(command, "command -v timeout ") {
									i := strings.Index(command, "/bin/bash -o pipefail -c ")
									if i < 0 {
										return
									}
									command = command[i:]
								}
								cmd := exec.CommandContext(ctx, "/bin/bash", "-c", command)
								cmd.Stdout = channel
								cmd.Stderr = channel.Stderr()
								cmd.Stdin = channel
								err := cmd.Run()
								status := uint32(0)
								if err != nil {
									status = 1
									if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() >= 0 {
										status = uint32(exit.ExitCode())
									}
								}
								buf := make([]byte, 4)
								binary.BigEndian.PutUint32(buf, status)
								channel.SendRequest("exit-status", false, buf)
								return
							default:
								req.Reply(false, nil)
							}
						}
					}()
				}
			}()
		}
	}()
	t.Cleanup(func() {
		cancel()
		listener.Close()
		mu.Lock()
		for conn := range connections {
			conn.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	var port int
	fmt.Sscan(portText, &port)
	hosts := filepath.Join(t.TempDir(), "known_hosts")
	if e = os.WriteFile(hosts, []byte(knownhosts.Line([]string{listener.Addr().String()}, signer.PublicKey())+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("GOPULSE_SSH_KNOWN_HOSTS", hosts)
	return models.Server{Host: host, Port: port, User: "fixture", AuthType: "password", Password: "fixture"}
}

func TestRemoteBackupRealSSHSQLite(t *testing.T) {
	if _, e := exec.LookPath("sqlite3"); e != nil {
		t.Skip("sqlite3 required")
	}
	s, _, in := newBackupTest(t)
	server := backupSSHFixture(t)
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	backend := NewSSHBackupBackend(s.Servers, s.Databases)
	backend.newClient = func(models.Server) (*remote.SSHClient, error) { return remote.NewSSHClient(server) }
	backend.dataRoot = filepath.Join(root, "data")
	backend.workRoot = filepath.Join(root, "work")
	os.MkdirAll(filepath.Join(backend.dataRoot, "database"), 0700)
	os.MkdirAll(backend.workRoot, 0700)
	source := filepath.Join(backend.dataRoot, "database", "sqlite.db")
	if out, e := exec.Command("sqlite3", source, `PRAGMA journal_mode=WAL; CREATE TABLE "unusual table"(id INTEGER PRIMARY KEY,value TEXT); INSERT INTO "unusual table" VALUES(1,'secret'),(2,'second');`).CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	original, e := os.ReadFile(source)
	if e != nil {
		t.Fatal(e)
	}
	s.backend = backend
	in.Directory = filepath.Join(root, "destination")
	p := saveBackupTest(t, s, in)
	run, e := s.RunNow("source", p.ID, "owner", "")
	if e != nil {
		t.Fatal(e)
	}
	result := awaitBackup(t, s, run)
	if result.Status != "succeeded" {
		t.Fatal(result)
	}
	a, e := s.Store.Artifact(p.ID, run.ID)
	if e != nil || a.TestStatus != "passed" || !strings.Contains(a.TestSummary, "1 tables read, 2 rows") {
		t.Fatal(a, e)
	}
	cipher, e := os.ReadFile(a.Path)
	if e != nil || bytes.Contains(cipher, []byte("secret")) {
		t.Fatal("unencrypted/missing archive", e)
	}
	for _, file := range []string{a.Path, backupKeyPath(s.keyDirectory, p.ID)} {
		info, e := os.Stat(file)
		if e != nil || info.Mode().Perm() != 0600 {
			t.Fatal(file, info, e)
		}
	}
	if _, e = os.Stat(filepath.Join(backend.workRoot, "gopulse-remote-backup-"+run.ID)); !os.IsNotExist(e) {
		t.Fatal("restore workspace retained", e)
	}
	after, _ := os.ReadFile(source)
	if !bytes.Equal(original, after) {
		t.Fatal("restore changed production database")
	}
	// Retention deletes an eligible owned archive over SFTP and persists pruning.
	expired := a
	expired.ID = uuid.NewString()
	expired.Path, _ = backupArchivePath(p, expired.ID)
	expired.CreatedAt = time.Now().Add(-40 * 24 * time.Hour)
	expired.TestStatus = "not_tested"
	if e = os.WriteFile(expired.Path, cipher, 0600); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.SaveArtifact(expired, true); e != nil {
		t.Fatal(e)
	}
	retentionPlan := p
	retentionPlan.KeepLast = 1
	retentionCtx, retentionCancel := context.WithTimeout(context.Background(), 5*time.Second)
	e = s.prune(retentionCtx, retentionPlan, time.Now())
	retentionCancel()
	if e != nil {
		t.Fatal(e)
	}
	expired, _ = s.Store.Artifact(p.ID, expired.ID)
	if !expired.Pruned {
		t.Fatal("pruning was not recorded")
	}
	if _, e = os.Stat(expired.Path); !os.IsNotExist(e) {
		t.Fatal("expired archive retained", e)
	}
	if _, e = os.Stat(a.Path); e != nil {
		t.Fatal("current passed backup removed", e)
	}
	// Manual retesting reads the persisted destination file over SSH.
	run, e = s.RunNow("source", p.ID, "owner", a.ID)
	if e != nil {
		t.Fatal(e)
	}
	if r := awaitBackup(t, s, run); r.Status != "succeeded" {
		t.Fatal(r)
	}
	// A ciphertext mutation must fail before an isolated restore is executed.
	cipher[len(cipher)/2] ^= 1
	os.WriteFile(a.Path, cipher, 0600)
	run, e = s.RunNow("source", p.ID, "owner", a.ID)
	if e != nil {
		t.Fatal(e)
	}
	if r := awaitBackup(t, s, run); r.Status != "failed" || r.NeedsCleanup {
		t.Fatal(r)
	}
	after, _ = os.ReadFile(source)
	if !bytes.Equal(original, after) {
		t.Fatal("corruption test changed production database")
	}
	// Symlink destinations and out-of-plan retention paths are refused.
	link := filepath.Join(root, "link")
	os.Symlink(in.Directory, link)
	bad := p
	bad.Directory = link
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, e = backend.Upload(ctx, bad, uuid.NewString(), strings.NewReader("test")); e == nil {
		t.Fatal("symlink accepted")
	}
	a.Path = source
	if e = backend.Remove(ctx, p, a); e == nil {
		t.Fatal("arbitrary retention path accepted")
	}
	client, e := remote.NewSSHClient(server)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	stream, e := client.StreamBackupContext(ctx, "printf partial; exit 7")
	if e != nil {
		t.Fatal(e)
	}
	data, e := io.ReadAll(stream)
	stream.Close()
	if string(data) != "partial" || e == nil {
		t.Fatal("nonzero remote exit lost", string(data), e)
	}
	// Missing source files must fail rather than implicitly creating an empty DB.
	os.Remove(source)
	run, e = s.RunNow("source", p.ID, "owner", "")
	if e != nil {
		t.Fatal(e)
	}
	if r := awaitBackup(t, s, run); r.Status != "failed" || r.NeedsCleanup {
		t.Fatal(r)
	}
	if _, e = os.Stat(source); !os.IsNotExist(e) {
		t.Fatal("missing source was created", e)
	}
	// A failed streaming producer cannot publish even a partial ciphertext file.
	id := uuid.NewString()
	if _, e = backend.Upload(ctx, p, id, failingBackupReader{}); e == nil {
		t.Fatal("failed producer accepted")
	}
	final, _ := backupArchivePath(p, id)
	for _, file := range []string{final, final + ".partial"} {
		if _, e = os.Stat(file); !os.IsNotExist(e) {
			t.Fatal("failed upload left a file", file, e)
		}
	}
}

func TestRemoteBackupSSHContainerCleanupOwnership(t *testing.T) {
	s, _, input := newBackupTest(t)
	db, _ := s.Databases.GetDatabase(input.DatabaseID)
	db.Type = "postgres"
	if err := s.Databases.UpdateDatabase(db); err != nil {
		t.Fatal(err)
	}
	plan := saveBackupTest(t, s, input)
	server := backupSSHFixture(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	plan.Directory = filepath.Join(root, "destination")
	backend := NewSSHBackupBackend(s.Servers, s.Databases)
	backend.newClient = func(models.Server) (*remote.SSHClient, error) { return remote.NewSSHClient(server) }
	backend.workRoot = root
	log := filepath.Join(root, "removed")
	program := `#!/bin/bash
set -euo pipefail
case "$1" in
info) test "$BACKUP_DOCKER_AVAILABLE" = 1 ;;
inspect) if [[ " $* " == *' --format '* ]]; then printf '%s\n' "$BACKUP_CONTAINER_OWNER"; fi ;;
rm) printf '%s\n' "$*" >> "$BACKUP_REMOVE_LOG" ;;
*) exit 2 ;;
esac
`
	if err = os.WriteFile(filepath.Join(root, "docker"), []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+":"+os.Getenv("PATH"))
	t.Setenv("BACKUP_REMOVE_LOG", log)
	id := uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, tc := range []struct {
		name, available, owner string
		wantError              bool
	}{
		{"unavailable", "0", id, true},
		{"foreign-container", "1", "another-run", true},
		{"owned-container", "1", id, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BACKUP_DOCKER_AVAILABLE", tc.available)
			t.Setenv("BACKUP_CONTAINER_OWNER", tc.owner)
			err := backend.Cleanup(ctx, plan, id)
			if (err != nil) != tc.wantError {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(log)
			if tc.wantError && len(data) > 0 {
				t.Fatal("removed a container without verifying ownership/availability", string(data))
			}
			if !tc.wantError && !strings.Contains(string(data), "rm --force --volumes gopulse-restore-test-"+id) {
				t.Fatal("owned test container not removed", string(data))
			}
		})
	}
}
