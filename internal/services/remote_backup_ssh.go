package services

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/remote"
	"github.com/AbdallahAwd/GoPulse/internal/store"
)

type SSHBackupBackend struct {
	Servers   *store.ServerStore
	Databases *store.DatabaseStore
	newClient func(models.Server) (*remote.SSHClient, error)
	dataRoot  string
	workRoot  string
}

func NewSSHBackupBackend(servers *store.ServerStore, databases *store.DatabaseStore) *SSHBackupBackend {
	return &SSHBackupBackend{Servers: servers, Databases: databases, newClient: remote.NewSSHClient, dataRoot: "/var/lib/gopulse/databases", workRoot: "/tmp"}
}

type backupHost struct {
	ssh   *remote.SSHClient
	files *remote.WrappedSFTPClient
	stop  func() bool
	once  sync.Once
}

func (h *backupHost) Close() error {
	var err error
	h.once.Do(func() { h.stop(); err = errors.Join(h.files.Close(), h.ssh.Close()) })
	return err
}
func (b *SSHBackupBackend) openHost(ctx context.Context, id string) (*backupHost, error) {
	server, err := b.Servers.GetServer(id)
	if err != nil {
		return nil, err
	}
	client, err := b.newClient(server)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { client.Close() })
	files, err := client.NewSFTPClient()
	if err != nil {
		stop()
		client.Close()
		return nil, err
	}
	return &backupHost{ssh: client, files: files, stop: stop}, nil
}
func backupShell(ctx context.Context, script string) string {
	seconds := 7200
	if deadline, ok := ctx.Deadline(); ok {
		seconds = max(1, int(time.Until(deadline).Seconds()))
	}
	return "command -v timeout >/dev/null 2>&1 && exec timeout --signal=TERM --kill-after=10s " + fmt.Sprint(seconds) + "s /bin/bash -o pipefail -c " + shellQuote("set -euo pipefail\numask 077\n"+script)
}
func (h *backupHost) run(ctx context.Context, script string) (string, error) {
	stream, err := h.ssh.StreamBackupContext(ctx, backupShell(ctx, script))
	if err != nil {
		return "", err
	}
	// Closing a command stream closes its dedicated client. Successful EOF has
	// already waited for command completion, so preserve the connection on success.
	defer stream.Close()
	data, err := io.ReadAll(io.LimitReader(stream, 32769))
	if err != nil || len(data) > 32768 {
		stream.Close()
		if err == nil {
			err = fmt.Errorf("remote command output too large")
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
func (b *SSHBackupBackend) work(runID string) (string, error) {
	if _, err := uuidBackupID(runID); err != nil {
		return "", err
	}
	return path.Join(b.workRoot, "gopulse-remote-backup-"+runID), nil
}
func ensureBackupDir(h *backupHost, directory string) error {
	current := "/"
	for _, part := range strings.Split(strings.TrimPrefix(directory, "/"), "/") {
		if part == "" {
			continue
		}
		current = path.Join(current, part)
		info, err := h.files.Lstat(current)
		if os.IsNotExist(err) {
			if err = h.files.Mkdir(current); err != nil {
				return err
			}
			if err = h.files.Chmod(current, 0700); err != nil {
				return err
			}
			info, err = h.files.Lstat(current)
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("backup directory components must be real directories")
		}
	}
	return nil
}
func createBackupWork(h *backupHost, work string) error {
	if err := h.files.Mkdir(work); err != nil {
		return fmt.Errorf("private backup workspace already exists or is unavailable")
	}
	return h.files.Chmod(work, 0700)
}

type backupReadCloser struct {
	io.Reader
	close func() error
}

func (r *backupReadCloser) Close() error { return r.close() }

var backupImageID = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func (b *SSHBackupBackend) Capture(ctx context.Context, plan models.RemoteBackupPlan, db models.Database, runID string) (RemoteBackupDump, error) {
	host, err := b.openHost(ctx, plan.ServerID)
	if err != nil {
		return RemoteBackupDump{}, err
	}
	failed := true
	defer func() {
		if failed {
			host.Close()
		}
	}()
	if !safeResourceID.MatchString(db.ID) || !RemoteBackupEngineSupported(db.Type) {
		return RemoteBackupDump{}, fmt.Errorf("unsupported source database")
	}
	container := "gopulse-" + db.ID
	if db.Type == "sqlite" {
		work, err := b.work(runID)
		if err != nil {
			return RemoteBackupDump{}, err
		}
		if err = createBackupWork(host, work); err != nil {
			return RemoteBackupDump{}, err
		}
		snapshot := path.Join(work, "sqlite.snapshot")
		// SQLite's online backup API includes committed WAL content consistently.
		source := shellQuote(path.Join(b.dataRoot, db.ID, "sqlite.db"))
		command := "test -f " + source + "\nsqlite3 -readonly -bail -cmd '.timeout 10000' " + source + " " + shellQuote(".backup '"+snapshot+"'")
		if _, err = host.run(ctx, command); err != nil {
			return RemoteBackupDump{}, err
		}
		file, err := host.files.Open(snapshot)
		if err != nil {
			return RemoteBackupDump{}, err
		}
		failed = false
		return RemoteBackupDump{Reader: &backupReadCloser{Reader: file, close: func() error { return errors.Join(file.Close(), host.Close()) }}}, nil
	}
	image, err := host.run(ctx, "docker inspect --format '{{.Image}}' "+shellQuote(container))
	if err != nil || !backupImageID.MatchString(image) {
		return RemoteBackupDump{}, fmt.Errorf("source container image is unavailable")
	}
	command, err := remoteBackupDumpCommand(db, runID)
	if err != nil {
		return RemoteBackupDump{}, err
	}
	stream, err := host.ssh.StreamBackupContext(ctx, backupShell(ctx, command))
	if err != nil {
		return RemoteBackupDump{}, err
	}
	failed = false
	return RemoteBackupDump{Reader: &backupReadCloser{Reader: stream, close: func() error { return errors.Join(stream.Close(), host.Close()) }}, ImageID: image}, nil
}
func backupArchivePath(plan models.RemoteBackupPlan, id string) (string, error) {
	if _, err := uuidBackupID(plan.ID); err != nil {
		return "", err
	}
	if _, err := uuidBackupID(id); err != nil {
		return "", err
	}
	if !validBackupDirectory(plan.Directory) {
		return "", fmt.Errorf("invalid backup directory")
	}
	return path.Join(plan.Directory, plan.ID, id+".zip.age"), nil
}
func (b *SSHBackupBackend) Upload(ctx context.Context, plan models.RemoteBackupPlan, runID string, reader io.Reader) (RemoteBackupReceipt, error) {
	final, err := backupArchivePath(plan, runID)
	if err != nil {
		return RemoteBackupReceipt{}, err
	}
	host, err := b.openHost(ctx, plan.DestinationID)
	if err != nil {
		return RemoteBackupReceipt{}, err
	}
	defer host.Close()
	if err = ensureBackupDir(host, path.Dir(final)); err != nil {
		return RemoteBackupReceipt{}, err
	}
	partial := final + ".partial"
	file, err := host.files.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return RemoteBackupReceipt{}, err
	}
	promoted := false
	defer func() {
		file.Close()
		if !promoted {
			host.files.Remove(partial)
		}
	}()
	if err = file.Chmod(0600); err != nil {
		return RemoteBackupReceipt{}, err
	}
	digest := sha256.New()
	maxBytes := int64(plan.MaxSizeGB) << 30
	limit := maxBytes + maxBytes/500 + (1 << 20)
	size, err := io.Copy(io.MultiWriter(file, digest), io.LimitReader(reader, limit+1))
	if err != nil {
		return RemoteBackupReceipt{}, err
	}
	if size > limit || size == 0 {
		return RemoteBackupReceipt{}, fmt.Errorf("encrypted backup exceeds size limit")
	}
	if _, ok := host.files.HasExtension("fsync@openssh.com"); ok {
		if err = file.Sync(); err != nil {
			return RemoteBackupReceipt{}, err
		}
	}
	if err = file.Close(); err != nil {
		return RemoteBackupReceipt{}, err
	}
	uploaded, err := host.files.Open(partial)
	if err != nil {
		return RemoteBackupReceipt{}, err
	}
	verification := sha256.New()
	count, readErr := io.Copy(verification, io.LimitReader(uploaded, size+1))
	closeErr := uploaded.Close()
	if readErr != nil || closeErr != nil || count != size || !strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), hex.EncodeToString(verification.Sum(nil))) {
		return RemoteBackupReceipt{}, fmt.Errorf("destination checksum mismatch")
	}
	if err = ctx.Err(); err != nil {
		return RemoteBackupReceipt{}, err
	}
	if _, err = host.files.Lstat(final); !os.IsNotExist(err) {
		return RemoteBackupReceipt{}, fmt.Errorf("destination archive already exists or cannot be checked")
	}
	if err = host.files.Rename(partial, final); err != nil {
		return RemoteBackupReceipt{}, err
	}
	promoted = true
	return RemoteBackupReceipt{Path: final, Size: size, SHA256: hex.EncodeToString(digest.Sum(nil))}, nil
}
func (b *SSHBackupBackend) Open(ctx context.Context, plan models.RemoteBackupPlan, a models.RemoteBackupArtifact) (io.ReadCloser, error) {
	expected, err := backupArchivePath(plan, a.ID)
	if err != nil || expected != a.Path || a.PlanID != plan.ID || a.DestinationID != plan.DestinationID {
		return nil, fmt.Errorf("archive does not belong to this plan")
	}
	host, err := b.openHost(ctx, plan.DestinationID)
	if err != nil {
		return nil, err
	}
	if err = ensureBackupDir(host, path.Dir(expected)); err != nil {
		host.Close()
		return nil, err
	}
	info, err := host.files.Lstat(expected)
	if err != nil || !info.Mode().IsRegular() || info.Size() != a.Size {
		host.Close()
		return nil, fmt.Errorf("archive missing, replaced or truncated")
	}
	file, err := host.files.Open(expected)
	if err != nil {
		host.Close()
		return nil, err
	}
	return &backupReadCloser{Reader: file, close: func() error { return errors.Join(file.Close(), host.Close()) }}, nil
}
func (b *SSHBackupBackend) RestoreTest(ctx context.Context, plan models.RemoteBackupPlan, a models.RemoteBackupArtifact, runID string, reader io.Reader) (string, error) {
	work, err := b.work(runID)
	if err != nil {
		return "", err
	}
	host, err := b.openHost(ctx, plan.ServerID)
	if err != nil {
		return "", err
	}
	defer host.Close()
	// A SQLite capture already owns this run's workspace. Other test runs create it.
	if info, e := host.files.Lstat(work); os.IsNotExist(e) {
		if err = createBackupWork(host, work); err != nil {
			return "", err
		}
	} else if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("invalid restore workspace")
	}
	bundlePath := path.Join(work, "bundle.zip")
	bundle, err := host.files.OpenFile(bundlePath, os.O_CREATE|os.O_EXCL|os.O_RDWR)
	if err != nil {
		return "", err
	}
	defer bundle.Close()
	if err = bundle.Chmod(0600); err != nil {
		return "", err
	}
	maxBytes := int64(plan.MaxSizeGB) << 30
	limit := maxBytes + maxBytes/500 + (1 << 20)
	// No database command runs until complete decryption, authentication and the
	// recorded ciphertext checksum have all succeeded.
	size, err := io.Copy(bundle, io.LimitReader(reader, limit+1))
	if err != nil || size > limit {
		return "", fmt.Errorf("decrypted bundle verification failed")
	}
	archive, err := zip.NewReader(bundle, size)
	if err != nil {
		return "", err
	}
	if len(archive.File) != 2 {
		return "", fmt.Errorf("unexpected backup bundle entries")
	}
	var manifest models.BackupManifest
	var payload *zip.File
	for _, entry := range archive.File {
		switch entry.Name {
		case "manifest.json":
			if manifest.Version != 0 || entry.UncompressedSize64 > 16384 {
				return "", fmt.Errorf("invalid backup manifest")
			}
			r, e := entry.Open()
			if e != nil {
				return "", e
			}
			data, e := io.ReadAll(io.LimitReader(r, 16385))
			r.Close()
			if e != nil {
				return "", e
			}
			if e = json.Unmarshal(data, &manifest); e != nil {
				return "", e
			}
		case "database.dump":
			if payload != nil || entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > uint64(maxBytes) {
				return "", fmt.Errorf("invalid database dump entry")
			}
			payload = entry
		default:
			return "", fmt.Errorf("unexpected backup bundle entry")
		}
	}
	if manifest.Version != 1 || manifest.PlanID != plan.ID || manifest.RunID != a.ID || manifest.DatabaseID != plan.DatabaseID || manifest.SourceServerID != plan.ServerID || manifest.Engine != a.Engine || manifest.ImageID != a.ImageID || manifest.DatabaseName != a.DatabaseName || payload == nil {
		return "", fmt.Errorf("backup manifest does not match the recorded archive")
	}
	dump, err := payload.Open()
	if err != nil {
		return "", err
	}
	defer dump.Close()
	file, err := host.files.OpenFile(path.Join(work, "database.dump"), os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return "", err
	}
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return "", err
	}
	written, copyErr := io.Copy(file, io.LimitReader(dump, maxBytes+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || uint64(written) != payload.UncompressedSize64 {
		return "", fmt.Errorf("database archive checksum or size check failed")
	}
	script, err := remoteBackupRestoreScript(plan, a, runID, work)
	if err != nil {
		return "", err
	}
	summary, err := host.run(ctx, script)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(summary, "Verified ") {
		return "", fmt.Errorf("restore validation did not complete")
	}
	return summary, nil
}
func (b *SSHBackupBackend) Remove(ctx context.Context, plan models.RemoteBackupPlan, a models.RemoteBackupArtifact) error {
	expected, err := backupArchivePath(plan, a.ID)
	if err != nil || expected != a.Path || a.PlanID != plan.ID {
		return fmt.Errorf("retention path does not belong to this plan")
	}
	host, err := b.openHost(ctx, plan.DestinationID)
	if err != nil {
		return err
	}
	defer host.Close()
	if err = ensureBackupDir(host, path.Dir(expected)); err != nil {
		return err
	}
	info, err := host.files.Lstat(expected)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to prune a non-regular archive")
	}
	return host.files.Remove(expected)
}
func (b *SSHBackupBackend) Cleanup(ctx context.Context, plan models.RemoteBackupPlan, runID string) error {
	work, err := b.work(runID)
	if err != nil {
		return err
	}
	host, err := b.openHost(ctx, plan.ServerID)
	if err != nil {
		return err
	}
	var cleanupErr error
	db, dbErr := b.Databases.GetDatabase(plan.DatabaseID)
	if dbErr == nil && db.Type != "sqlite" {
		name := "gopulse-restore-test-" + runID
		script := "docker info >/dev/null 2>&1\nif docker inspect --type=container " + shellQuote(name) + " >/dev/null 2>&1; then test \"$(docker inspect --type=container --format '{{index .Config.Labels \"gopulse.remote-backup-test\"}}' " + shellQuote(name) + ")\" = " + shellQuote(runID) + "; docker rm --force --volumes " + shellQuote(name) + " >/dev/null; fi"
		if _, err = host.run(ctx, script); err != nil {
			cleanupErr = fmt.Errorf("restore container cleanup failed")
		}
		if db.Type == "redis" {
			_, err = host.run(ctx, "docker exec "+shellQuote("gopulse-"+db.ID)+" rm -f "+shellQuote("/tmp/gopulse-rb-"+runID+".rdb"))
			if err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
			}
		}
	}
	for _, name := range []string{"sqlite.snapshot", "bundle.zip", "database.dump", "checks.sql", "counts.txt"} {
		err = host.files.Remove(path.Join(work, name))
		if err != nil && !os.IsNotExist(err) {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	if err = host.files.RemoveDirectory(work); err != nil && !os.IsNotExist(err) {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	host.Close()
	final, err := backupArchivePath(plan, runID)
	if err != nil {
		return errors.Join(cleanupErr, err)
	}
	destination, err := b.openHost(ctx, plan.DestinationID)
	if err != nil {
		return errors.Join(cleanupErr, err)
	}
	defer destination.Close()
	if err = destination.files.Remove(final + ".partial"); err != nil && !os.IsNotExist(err) {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	return cleanupErr
}
