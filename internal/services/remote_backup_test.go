package services

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/google/uuid"
)

type backupFixture struct {
	mu                    sync.Mutex
	archives              map[string][]byte
	failTest, failCleanup bool
	block                 chan struct{}
	captures              int
}

func (b *backupFixture) Capture(ctx context.Context, p models.RemoteBackupPlan, d models.Database, id string) (RemoteBackupDump, error) {
	b.mu.Lock()
	b.captures++
	b.mu.Unlock()
	if b.block != nil {
		select {
		case <-b.block:
		case <-ctx.Done():
			return RemoteBackupDump{}, ctx.Err()
		}
	}
	return RemoteBackupDump{Reader: io.NopCloser(strings.NewReader("private database payload"))}, nil
}
func (b *backupFixture) Upload(ctx context.Context, p models.RemoteBackupPlan, id string, r io.Reader) (RemoteBackupReceipt, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return RemoteBackupReceipt{}, err
	}
	h := sha256.Sum256(data)
	b.mu.Lock()
	b.archives[id] = data
	b.mu.Unlock()
	return RemoteBackupReceipt{Path: p.Directory + "/" + p.ID + "/" + id + ".zip.age", Size: int64(len(data)), SHA256: hex.EncodeToString(h[:])}, nil
}
func (b *backupFixture) Open(ctx context.Context, p models.RemoteBackupPlan, a models.RemoteBackupArtifact) (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return io.NopCloser(bytes.NewReader(b.archives[a.ID])), nil
}
func (b *backupFixture) RestoreTest(ctx context.Context, p models.RemoteBackupPlan, a models.RemoteBackupArtifact, id string, r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(z.File) != 2 {
		return "", errors.New("invalid bundle")
	}
	if b.failTest {
		return "", errors.New("native restore failed")
	}
	return "Verified fixture restore", nil
}
func (b *backupFixture) Remove(ctx context.Context, p models.RemoteBackupPlan, a models.RemoteBackupArtifact) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.archives, a.ID)
	return nil
}
func (b *backupFixture) Cleanup(context.Context, models.RemoteBackupPlan, string) error {
	if b.failCleanup {
		return errors.New("offline")
	}
	return nil
}

func newBackupTest(t *testing.T) (*RemoteBackupService, *backupFixture, RemoteBackupInput) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "state.json")
	servers, err := store.NewServerStore(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"source", "destination"} {
		if err = servers.AddServer(models.Server{ID: id, Host: id + ".invalid"}); err != nil {
			t.Fatal(err)
		}
	}
	dbs, err := store.NewDatabaseStore(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = dbs.AddDatabase(models.Database{ID: "database", ServerID: "source", Name: "Database", Type: "sqlite", DatabaseName: "app"}); err != nil {
		t.Fatal(err)
	}
	users, err := store.NewUserStore(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = users.AddUserRaw(models.User{ID: "owner", Username: "owner", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	st, err := store.NewRemoteBackupStore(file)
	if err != nil {
		t.Fatal(err)
	}
	backend := &backupFixture{archives: map[string][]byte{}}
	s, err := NewRemoteBackupService(st, servers, dbs, users, backend, filepath.Join(t.TempDir(), "keys"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, backend, RemoteBackupInput{Name: "Daily", DatabaseID: "database", DestinationID: "destination", Directory: "/srv/backups", Schedule: "0 2 * * *", Timezone: "UTC", RetentionDays: 7, KeepLast: 2, AutoTest: true, TimeoutSeconds: 120, MaxSizeGB: 1}
}
func saveBackupTest(t *testing.T, s *RemoteBackupService, in RemoteBackupInput) models.RemoteBackupPlan {
	t.Helper()
	p, e := s.Save("source", "", "owner", in)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func awaitBackup(t *testing.T, s *RemoteBackupService, run models.RemoteBackupRun) models.RemoteBackupRun {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r, e := s.Store.Run(run.PlanID, run.ID)
		if e != nil {
			t.Fatal(e)
		}
		s.mu.Lock()
		_, active := s.active[run.PlanID]
		s.mu.Unlock()
		if r.Status != "running" && !active {
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("backup did not finish")
	return run
}
func TestRemoteBackupEncryptedRoundTripAndFailureRetention(t *testing.T) {
	s, b, in := newBackupTest(t)
	p := saveBackupTest(t, s, in)
	key, err := s.RecoveryKey(p)
	if err != nil || !strings.HasPrefix(key, "AGE-SECRET-KEY-") {
		t.Fatal(key, err)
	}
	info, _ := os.Stat(backupKeyPath(s.keyDirectory, p.ID))
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	run, err := s.RunNow("source", p.ID, "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	if r := awaitBackup(t, s, run); r.Status != "succeeded" || r.NeedsCleanup {
		t.Fatal(r)
	}
	a, err := s.Store.Artifact(p.ID, run.ID)
	if err != nil || a.TestStatus != "passed" {
		t.Fatal(a, err)
	}
	data := b.archives[a.ID]
	if bytes.Contains(data, []byte("private database payload")) || !bytes.HasPrefix(data, []byte("age-encryption.org/v1")) {
		t.Fatal("archive not encrypted")
	}
	b.failTest = true
	run, err = s.RunNow("source", p.ID, "owner", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r := awaitBackup(t, s, run); r.Status != "failed" {
		t.Fatal(r)
	}
	a, _ = s.Store.Artifact(p.ID, a.ID)
	if a.TestStatus != "failed" || len(b.archives[a.ID]) == 0 {
		t.Fatal("failed test lost archive", a)
	}
	if err = s.Databases.CheckRemoteBackupDeletion("database"); !errors.Is(err, store.ErrRemoteBackupDependency) {
		t.Fatal(err)
	}
	if err = s.Servers.CheckRemoteBackupDeletion("destination"); !errors.Is(err, store.ErrRemoteBackupDependency) {
		t.Fatal(err)
	}
	if err = s.DeletePlan("source", p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(backupKeyPath(s.keyDirectory, p.ID)); err != nil {
		t.Fatal("deletion removed recovery key", err)
	}
	if len(b.archives[a.ID]) == 0 {
		t.Fatal("deletion removed remote archive")
	}
}
func TestRemoteBackupOverlapCancellationAndRevocation(t *testing.T) {
	s, b, in := newBackupTest(t)
	b.block = make(chan struct{})
	p := saveBackupTest(t, s, in)
	run, e := s.RunNow("source", p.ID, "owner", "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RunNow("source", p.ID, "owner", ""); !errors.Is(e, store.ErrRemoteBackupBusy) {
		t.Fatal(e)
	}
	if _, e = s.Save("source", p.ID, "owner", in); !errors.Is(e, store.ErrRemoteBackupBusy) {
		t.Fatal(e)
	}
	if e = s.Cancel("source", p.ID); e != nil {
		t.Fatal(e)
	}
	if r := awaitBackup(t, s, run); r.Status != "cancelled" {
		t.Fatal(r)
	}
	user, _ := s.Users.GetUser("owner")
	user.Role = "viewer"
	user.Permissions = nil
	if e = s.Users.UpdateUser(user); e != nil {
		t.Fatal(e)
	}
	if _, e = s.RunNow("source", p.ID, "owner", ""); !errors.Is(e, ErrRemoteBackupAccess) {
		t.Fatal(e)
	}
	if e = s.authorizeID(p.OwnerID, p); !errors.Is(e, ErrRemoteBackupAccess) {
		t.Fatal(e)
	}
}
func TestRemoteBackupValidationAndMissingKey(t *testing.T) {
	s, _, in := newBackupTest(t)
	for _, dir := range []string{"/", "/tmp", "../backups", "/etc/backups", "/srv/../root", "/srv/a\n"} {
		bad := in
		bad.Directory = dir
		if _, e := s.Save("source", "", "owner", bad); e == nil {
			t.Fatal(dir)
		}
	}
	bad := in
	bad.DestinationID = "source"
	if _, e := s.Save("source", "", "owner", bad); e == nil {
		t.Fatal("same host accepted")
	}
	p := saveBackupTest(t, s, in)
	os.Remove(backupKeyPath(s.keyDirectory, p.ID))
	run, e := s.RunNow("source", p.ID, "owner", "")
	if e != nil {
		t.Fatal(e)
	}
	if r := awaitBackup(t, s, run); r.Status != "failed" || !strings.Contains(r.Error, "recovery key unavailable") {
		t.Fatal(r)
	}
	if _, e := os.Stat(backupKeyPath(s.keyDirectory, p.ID)); !os.IsNotExist(e) {
		t.Fatal("missing key regenerated")
	}
}

func TestRemoteBackupCleanupFailureRetry(t *testing.T) {
	s, b, in := newBackupTest(t)
	b.failCleanup = true
	p := saveBackupTest(t, s, in)
	run, err := s.RunNow("source", p.ID, "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	r := awaitBackup(t, s, run)
	if r.Status != "cleanup_failed" || !r.NeedsCleanup {
		t.Fatal(r)
	}
	if err = s.DeletePlan("source", p.ID); !errors.Is(err, store.ErrRemoteBackupBusy) {
		t.Fatal("removed plan awaiting cleanup", err)
	}
	b.failCleanup = false
	s.retryCleanup()
	r, _ = s.Store.Run(p.ID, run.ID)
	if r.Status != "succeeded" || r.NeedsCleanup || r.Error != "" {
		t.Fatal("cleanup retry not reflected", r)
	}
}
func TestRemoteBackupScheduleRetentionAndRestart(t *testing.T) {
	s, b, in := newBackupTest(t)
	in.Enabled = true
	p := saveBackupTest(t, s, in)
	p.NextRunAt = time.Now().Add(-time.Minute).UTC()
	if e := s.Store.Save(p, false); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	if e := s.Tick(now); e != nil {
		t.Fatal(e)
	}
	runs, _ := s.Store.Runs(p.ID)
	if len(runs) != 1 {
		t.Fatal(runs)
	}
	awaitBackup(t, s, runs[0])
	if e := s.Tick(now); e != nil {
		t.Fatal(e)
	}
	runs, _ = s.Store.Runs(p.ID)
	if len(runs) != 1 {
		t.Fatal("duplicate schedule", runs)
	}
	for i, status := range []string{"passed", "not_tested", "not_tested", "not_tested"} {
		a := models.RemoteBackupArtifact{ID: uuid.NewString(), PlanID: p.ID, ServerID: p.ServerID, DestinationID: p.DestinationID, CreatedAt: now.Add(-time.Duration(30-i) * 24 * time.Hour), TestStatus: status}
		if e := s.Store.SaveArtifact(a, true); e != nil {
			t.Fatal(e)
		}
	}
	expired, e := s.Store.ExpiredArtifacts(p, now)
	if e != nil || len(expired) != 3 {
		t.Fatal("protect newest two including new run, latest passed is new run", len(expired), e)
	}
	// Restart recovery must never replay an abandoned invocation.
	p, _ = s.Store.Get("source", p.ID)
	r := newRemoteBackupRun(p, "backup", "manual", p.OwnerID, "", now)
	if e = s.Store.Claim(p, r, nil); e != nil {
		t.Fatal(e)
	}
	restored, e := NewRemoteBackupService(s.Store, s.Servers, s.Databases, s.Users, b, s.keyDirectory)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	r, _ = s.Store.Run(p.ID, r.ID)
	if r.Status != "interrupted" || !r.NeedsCleanup {
		t.Fatal(r)
	}
	restored.retryCleanup()
	r, _ = s.Store.Run(p.ID, r.ID)
	if r.NeedsCleanup {
		t.Fatal(r)
	}
}

type failingBackupReader struct{}

func (failingBackupReader) Read(p []byte) (int, error) {
	copy(p, "partial")
	return min(len(p), 7), errors.New("remote nonzero exit")
}
func (failingBackupReader) Close() error { return nil }
func TestRemoteBackupCryptoRejectsTamperingAndPartialDumps(t *testing.T) {
	id, _ := age.GenerateX25519Identity()
	payload := bytes.Repeat([]byte("private"), 20000)
	read := encryptBackupBundle(io.NopCloser(bytes.NewReader(payload)), models.BackupManifest{Version: 1}, id.Recipient().String(), int64(len(payload)))
	encrypted, e := io.ReadAll(read)
	read.Close()
	if e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256(encrypted)
	a := models.RemoteBackupArtifact{Size: int64(len(encrypted)), SHA256: hex.EncodeToString(hash[:])}
	plain, e := decryptVerifiedBackup(bytes.NewReader(encrypted), id, a)
	if e != nil {
		t.Fatal(e)
	}
	data, e := io.ReadAll(plain)
	if e != nil {
		t.Fatal(e)
	}
	z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if e != nil || len(z.File) != 2 {
		t.Fatal(e)
	}
	for _, kind := range []string{"corrupt", "truncated", "trailing", "checksum", "wrong-key"} {
		t.Run(kind, func(t *testing.T) {
			buf := bytes.Clone(encrypted)
			meta := a
			key := id
			switch kind {
			case "corrupt":
				buf[len(buf)/2] ^= 1
			case "truncated":
				buf = buf[:len(buf)-1]
			case "trailing":
				buf = append(buf, 1)
			case "checksum":
				meta.SHA256 = strings.Repeat("0", 64)
			case "wrong-key":
				key, _ = age.GenerateX25519Identity()
			}
			r, e := decryptVerifiedBackup(bytes.NewReader(buf), key, meta)
			if e == nil {
				_, e = io.ReadAll(r)
			}
			if e == nil {
				t.Fatal("invalid archive accepted")
			}
		})
	}
	for _, source := range []io.ReadCloser{failingBackupReader{}, io.NopCloser(strings.NewReader("")), io.NopCloser(strings.NewReader("oversized"))} {
		r := encryptBackupBundle(source, models.BackupManifest{}, id.Recipient().String(), 4)
		if _, e := io.ReadAll(r); e == nil {
			t.Fatal("bad source accepted")
		}
		r.Close()
	}
}
