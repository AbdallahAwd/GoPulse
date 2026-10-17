package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/netip"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
	"github.com/google/uuid"
)

type RemoteBackupValidationError struct{ Message string }

func (e *RemoteBackupValidationError) Error() string { return e.Message }
func invalidBackup(message string) error             { return &RemoteBackupValidationError{message} }

var ErrRemoteBackupCapacity = errors.New("both remote backup workers are busy; try again shortly")
var ErrRemoteBackupAccess = errors.New("database:write and server:write plus access to both servers are required")

func uuidBackupID(id string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return uuid.Nil, invalidBackup("Invalid backup identifier.")
	}
	return parsed, nil
}

var backupDBName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]{0,62}$`)

func RemoteBackupEngineSupported(engine string) bool {
	switch engine {
	case "postgres", "mysql", "mongodb", "redis", "sqlite":
		return true
	}
	return false
}

type RemoteBackupInput struct {
	Name           string `json:"name"`
	DatabaseID     string `json:"database_id"`
	DestinationID  string `json:"destination_id"`
	Directory      string `json:"directory"`
	Schedule       string `json:"schedule"`
	Timezone       string `json:"timezone"`
	RetentionDays  int    `json:"retention_days"`
	KeepLast       int    `json:"keep_last"`
	Enabled        bool   `json:"enabled"`
	AutoTest       bool   `json:"auto_test"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	TestMemoryMB   int    `json:"test_memory_mb"`
	MaxSizeGB      int    `json:"max_size_gb"`
}
type RemoteBackupDump struct {
	Reader  io.ReadCloser
	ImageID string
}
type RemoteBackupReceipt struct {
	Path   string
	Size   int64
	SHA256 string
}
type RemoteBackupBackend interface {
	Capture(context.Context, models.RemoteBackupPlan, models.Database, string) (RemoteBackupDump, error)
	Upload(context.Context, models.RemoteBackupPlan, string, io.Reader) (RemoteBackupReceipt, error)
	Open(context.Context, models.RemoteBackupPlan, models.RemoteBackupArtifact) (io.ReadCloser, error)
	RestoreTest(context.Context, models.RemoteBackupPlan, models.RemoteBackupArtifact, string, io.Reader) (string, error)
	Remove(context.Context, models.RemoteBackupPlan, models.RemoteBackupArtifact) error
	Cleanup(context.Context, models.RemoteBackupPlan, string) error
}

type RemoteBackupService struct {
	Store        *store.RemoteBackupStore
	Servers      *store.ServerStore
	Databases    *store.DatabaseStore
	Users        *store.UserStore
	backend      RemoteBackupBackend
	keyDirectory string
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	active       map[string]context.CancelFunc
	workers      chan struct{}
	wg           sync.WaitGroup
	closed       bool
}

func NewRemoteBackupService(st *store.RemoteBackupStore, servers *store.ServerStore, databases *store.DatabaseStore, users *store.UserStore, backend RemoteBackupBackend, keyDirectory string) (*RemoteBackupService, error) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &RemoteBackupService{Store: st, Servers: servers, Databases: databases, Users: users, backend: backend, keyDirectory: keyDirectory, ctx: ctx, cancel: cancel, active: map[string]context.CancelFunc{}, workers: make(chan struct{}, 2)}
	runs, err := st.ActiveRuns()
	if err != nil {
		cancel()
		return nil, err
	}
	for _, run := range runs {
		run.Status = "interrupted"
		run.Error = "GoPulse restarted; execution was not replayed. Check the destination for a finalized encrypted archive."
		run.FinishedAt = time.Now().UTC()
		run.NeedsCleanup = true
		if run.ArtifactID != "" {
			a, e := st.Artifact(run.PlanID, run.ArtifactID)
			if e == nil && a.TestStatus == "running" {
				a.TestStatus = "interrupted"
				a.TestError = "GoPulse restarted during the restore test."
				if e = st.SaveArtifact(a, false); e != nil {
					cancel()
					return nil, e
				}
			}
		}
		if err = st.UpdateRun(run); err != nil {
			cancel()
			return nil, err
		}
	}
	plans, err := st.Plans("")
	if err != nil {
		cancel()
		return nil, err
	}
	now := time.Now().UTC()
	for _, plan := range plans {
		if plan.Enabled && !plan.NextRunAt.After(now) {
			schedule, e := ParseCronSchedule(plan.Schedule, plan.Timezone)
			if e != nil {
				cancel()
				return nil, e
			}
			plan.NextRunAt = schedule.Next(now)
			if err = st.Save(plan, false); err != nil {
				cancel()
				return nil, err
			}
		}
	}
	return s, nil
}
func backupUserCanWrite(user models.User, source, destination string) bool {
	permission := func(want string) bool {
		if user.Role == "admin" {
			return true
		}
		for _, p := range user.Permissions {
			if p == "*" || p == want {
				return true
			}
		}
		return false
	}
	scope := func(id string) bool {
		if user.Role == "admin" {
			return true
		}
		for _, allowed := range user.AllowedServers {
			if allowed == id {
				return true
			}
		}
		return false
	}
	return permission("database:write") && permission("server:write") && scope(source) && scope(destination)
}
func (s *RemoteBackupService) Authorize(actor, source, destination string) (models.User, error) {
	user, err := s.Users.GetUser(actor)
	if err != nil || !backupUserCanWrite(user, source, destination) {
		return models.User{}, ErrRemoteBackupAccess
	}
	return user, nil
}
func (s *RemoteBackupService) authorizeID(id string, plan models.RemoteBackupPlan) error {
	user, err := s.Users.GetUserByID(id)
	if err != nil || !backupUserCanWrite(user, plan.ServerID, plan.DestinationID) {
		return ErrRemoteBackupAccess
	}
	return nil
}
func canonicalBackupHost(host string) string {
	host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	if host == "localhost" {
		return "loopback"
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.IsLoopback() {
			return "loopback"
		}
		return ip.Unmap().String()
	}
	return host
}
func validBackupDirectory(raw string) bool {
	if raw == "" || !strings.HasPrefix(raw, "/") || path.Clean(raw) != raw || len(raw) > 1024 || strings.ContainsAny(raw, "\x00\r\n\t") {
		return false
	}
	if raw == "/" || raw == "/tmp" {
		return false
	}
	for _, root := range []string{"/etc", "/bin", "/sbin", "/usr", "/proc", "/sys", "/dev", "/run"} {
		if raw == root || strings.HasPrefix(raw, root+"/") {
			return false
		}
	}
	return true
}
func (s *RemoteBackupService) Save(serverID, id, actor string, input RemoteBackupInput) (models.RemoteBackupPlan, error) {
	if input.TestMemoryMB == 0 {
		input.TestMemoryMB = 1024
	}
	if input.TestMemoryMB < 128 || input.TestMemoryMB > 16384 {
		return models.RemoteBackupPlan{}, invalidBackup("Restore-test memory must be 128–16384 MiB.")
	}
	unlockDatabase := store.LockDatabaseLifecycle(input.DatabaseID)
	defer unlockDatabase()
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 100 {
		return models.RemoteBackupPlan{}, invalidBackup("Plan name must contain 1–100 characters.")
	}
	if !validBackupDirectory(input.Directory) {
		return models.RemoteBackupPlan{}, invalidBackup("Use a clean absolute backup directory outside system directories, for example /srv/gopulse-backups.")
	}
	if input.RetentionDays < 1 || input.RetentionDays > 3650 || input.KeepLast < 1 || input.KeepLast > 100 {
		return models.RemoteBackupPlan{}, invalidBackup("Retention must be 1–3650 days and keep at least 1–100 recent backups.")
	}
	if input.TimeoutSeconds < 120 || input.TimeoutSeconds > 7200 || input.MaxSizeGB < 1 || input.MaxSizeGB > 1024 {
		return models.RemoteBackupPlan{}, invalidBackup("Timeout must be 120–7200 seconds and size limit 1–1024 GiB.")
	}
	schedule, err := ParseCronSchedule(input.Schedule, input.Timezone)
	if err != nil {
		return models.RemoteBackupPlan{}, invalidBackup(err.Error())
	}
	db, err := s.Databases.GetDatabase(input.DatabaseID)
	if err != nil || db.ServerID != serverID {
		return models.RemoteBackupPlan{}, invalidBackup("Choose a database on this source server.")
	}
	if !RemoteBackupEngineSupported(db.Type) {
		return models.RemoteBackupPlan{}, invalidBackup("Remote Backups supports PostgreSQL, MySQL, MongoDB, Redis and SQLite.")
	}
	if !safeResourceID.MatchString(db.ID) || (db.Type != "sqlite" && db.Type != "redis" && !backupDBName.MatchString(db.DatabaseName)) {
		return models.RemoteBackupPlan{}, invalidBackup("This database identifier or database name is not supported by Remote Backups.")
	}
	source, err := s.Servers.GetServer(serverID)
	if err != nil {
		return models.RemoteBackupPlan{}, invalidBackup("Source server not found.")
	}
	destination, err := s.Servers.GetServer(input.DestinationID)
	if err != nil {
		return models.RemoteBackupPlan{}, invalidBackup("Destination server not found.")
	}
	if source.ID == destination.ID || canonicalBackupHost(source.Host) == canonicalBackupHost(destination.Host) {
		return models.RemoteBackupPlan{}, invalidBackup("Choose a different destination host for an off-server backup.")
	}
	user, err := s.Authorize(actor, serverID, destination.ID)
	if err != nil {
		return models.RemoteBackupPlan{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return models.RemoteBackupPlan{}, ErrRemoteBackupCapacity
	}
	create := id == ""
	var plan models.RemoteBackupPlan
	if create {
		existing, e := s.Store.Plans(serverID)
		if e != nil {
			return plan, e
		}
		if len(existing) >= 100 {
			return plan, invalidBackup("Maximum 100 remote backup plans per source server.")
		}
		id = uuid.NewString()
		recipient, e := createBackupIdentity(s.keyDirectory, id)
		if e != nil {
			return plan, fmt.Errorf("could not securely create the recovery key")
		}
		plan = models.RemoteBackupPlan{ID: id, ServerID: serverID, DatabaseID: db.ID, DestinationID: destination.ID, Directory: input.Directory, Recipient: recipient, CreatedAt: time.Now().UTC()}
	} else {
		plan, err = s.Store.Get(serverID, id)
		if err != nil {
			return plan, err
		}
		if plan.DatabaseID != db.ID || plan.DestinationID != destination.ID || plan.Directory != input.Directory {
			return plan, invalidBackup("Source database and destination are fixed for a plan. Create another plan to change them.")
		}
		if _, busy := s.active[id]; busy {
			return plan, store.ErrRemoteBackupBusy
		}
	}
	plan.Name = input.Name
	plan.Schedule = strings.Join(strings.Fields(input.Schedule), " ")
	plan.Timezone = input.Timezone
	plan.RetentionDays = input.RetentionDays
	plan.KeepLast = input.KeepLast
	plan.Enabled = input.Enabled
	plan.AutoTest = input.AutoTest
	plan.TimeoutSeconds = input.TimeoutSeconds
	plan.MaxSizeGB = input.MaxSizeGB
	plan.TestMemoryMB = input.TestMemoryMB
	plan.OwnerID = user.ID
	plan.UpdatedAt = time.Now().UTC()
	plan.NextRunAt = time.Time{}
	if plan.Enabled {
		plan.NextRunAt = schedule.Next(plan.UpdatedAt)
	}
	err = s.Store.Save(plan, create)
	if err != nil && create {
		os.Remove(backupKeyPath(s.keyDirectory, id))
	}
	return plan, err
}
func (s *RemoteBackupService) RecoveryKey(plan models.RemoteBackupPlan) (string, error) {
	identity, err := readBackupIdentity(s.keyDirectory, plan)
	if err != nil {
		return "", err
	}
	return identity.String() + "\n", nil
}
func newRemoteBackupRun(plan models.RemoteBackupPlan, kind, trigger, actor, artifactID string, now time.Time) models.RemoteBackupRun {
	return models.RemoteBackupRun{ID: uuid.NewString(), PlanID: plan.ID, ServerID: plan.ServerID, Kind: kind, Trigger: trigger, Actor: actor, ArtifactID: artifactID, Status: "running", Phase: "starting", NeedsCleanup: true, StartedAt: now}
}
func (s *RemoteBackupService) RunNow(serverID, id, actor, artifactID string) (models.RemoteBackupRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.Store.Get(serverID, id)
	if err != nil {
		return models.RemoteBackupRun{}, err
	}
	user, err := s.Authorize(actor, plan.ServerID, plan.DestinationID)
	if err != nil {
		return models.RemoteBackupRun{}, err
	}
	if s.closed {
		return models.RemoteBackupRun{}, ErrRemoteBackupCapacity
	}
	if _, busy := s.active[id]; busy {
		return models.RemoteBackupRun{}, store.ErrRemoteBackupBusy
	}
	kind := "backup"
	if artifactID != "" {
		a, e := s.Store.Artifact(id, artifactID)
		if e != nil || a.Pruned {
			if e == nil {
				e = store.ErrRemoteBackupNotFound
			}
			return models.RemoteBackupRun{}, e
		}
		kind = "restore_test"
	}
	select {
	case s.workers <- struct{}{}:
	default:
		return models.RemoteBackupRun{}, ErrRemoteBackupCapacity
	}
	run := newRemoteBackupRun(plan, kind, "manual", user.ID, artifactID, time.Now().UTC())
	if err = s.Store.Claim(plan, run, nil); err != nil {
		<-s.workers
		return run, err
	}
	s.launch(plan, run)
	return run, nil
}
func (s *RemoteBackupService) Cancel(serverID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.Store.Get(serverID, id); err != nil {
		return err
	}
	cancel, ok := s.active[id]
	if !ok {
		return invalidBackup("No active execution to cancel.")
	}
	cancel()
	return nil
}
func (s *RemoteBackupService) Tick(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	plans, err := s.Store.Plans("")
	if err != nil {
		return err
	}
	for _, plan := range plans {
		if !plan.Enabled || plan.NextRunAt.IsZero() || plan.NextRunAt.After(now) {
			continue
		}
		expected := plan.NextRunAt
		schedule, e := ParseCronSchedule(plan.Schedule, plan.Timezone)
		if e != nil {
			return e
		}
		plan.NextRunAt = schedule.Next(now)
		run := newRemoteBackupRun(plan, "backup", "scheduled", plan.OwnerID, "", now.UTC())
		reserved := false
		if _, busy := s.active[plan.ID]; !busy {
			select {
			case s.workers <- struct{}{}:
				reserved = true
			default:
			}
		}
		if !reserved {
			run.Status = "skipped"
			run.Phase = "skipped"
			run.NeedsCleanup = false
			run.FinishedAt = now.UTC()
			run.Error = "Worker capacity reached or another execution is active; schedule skipped."
		}
		if err = s.Store.Claim(plan, run, &expected); err != nil {
			if reserved {
				<-s.workers
			}
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return err
		}
		saved, e := s.Store.Run(plan.ID, run.ID)
		if e != nil {
			if reserved {
				<-s.workers
			}
			return e
		}
		if reserved && saved.Status == "running" {
			s.launch(plan, run)
		} else if reserved {
			<-s.workers
		}
	}
	return nil
}
func (s *RemoteBackupService) phase(run *models.RemoteBackupRun, phase string) error {
	run.Phase = phase
	return s.Store.UpdateRun(*run)
}
func (s *RemoteBackupService) launch(plan models.RemoteBackupPlan, run models.RemoteBackupRun) {
	ctx, cancel := context.WithTimeout(s.ctx, time.Duration(plan.TimeoutSeconds)*time.Second)
	s.active[plan.ID] = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		err := func() (err error) {
			defer func() {
				if recover() != nil {
					err = fmt.Errorf("backup worker failed unexpectedly")
				}
			}()
			return s.execute(ctx, plan, &run)
		}()
		run.Status = "succeeded"
		if err != nil {
			run.Status = "failed"
			run.Error = err.Error()
		}
		if ctx.Err() != nil {
			run.Status = "cancelled"
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				run.Status = "timed_out"
			}
			run.Error = "Execution stopped. Any completed encrypted archive is retained."
		}
		if err != nil && run.ArtifactID != "" {
			artifact, e := s.Store.Artifact(plan.ID, run.ArtifactID)
			if e == nil && (artifact.TestStatus == "running" || artifact.TestStatus == "pending") {
				artifact.TestStatus = "interrupted"
				artifact.TestError = "Execution stopped before restore validation completed."
				if e = s.Store.SaveArtifact(artifact, false); e != nil {
					log.Printf("Remote backup interrupted test state: %v", e)
				}
			}
		}
		// Always use a fresh SSH connection/context for cleanup after cancellation.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		cleanupErr := s.backend.Cleanup(cleanupCtx, plan, run.ID)
		cleanupCancel()
		run.NeedsCleanup = cleanupErr != nil
		if cleanupErr != nil {
			run.Status = "cleanup_failed"
			run.Error += " Temporary restore data could not be removed. Automatic cleanup will retry; inspect the source host."
		}
		run.FinishedAt = time.Now().UTC()
		if e := s.Store.UpdateRun(run); e != nil {
			log.Printf("Remote backup run %s could not be recorded: %v", run.ID, e)
		}
		s.mu.Lock()
		delete(s.active, plan.ID)
		<-s.workers
		s.mu.Unlock()
	}()
}
func (s *RemoteBackupService) execute(ctx context.Context, plan models.RemoteBackupPlan, run *models.RemoteBackupRun) error {
	if err := s.authorizeID(run.Actor, plan); err != nil {
		return err
	}
	source, sourceErr := s.Servers.GetServer(plan.ServerID)
	destination, destinationErr := s.Servers.GetServer(plan.DestinationID)
	if sourceErr != nil || destinationErr != nil || canonicalBackupHost(source.Host) == canonicalBackupHost(destination.Host) {
		return fmt.Errorf("source and destination must remain available on different hosts")
	}
	if _, err := readBackupIdentity(s.keyDirectory, plan); err != nil {
		return err
	}
	db, err := s.Databases.GetDatabase(plan.DatabaseID)
	if err != nil || db.ServerID != plan.ServerID || !RemoteBackupEngineSupported(db.Type) {
		return fmt.Errorf("source database is no longer available for this plan")
	}
	var artifact models.RemoteBackupArtifact
	if run.Kind == "backup" {
		if err = s.phase(run, "dumping"); err != nil {
			return err
		}
		dump, err := s.backend.Capture(ctx, plan, db, run.ID)
		if err != nil {
			return fmt.Errorf("database dump failed; check source SSH access, native database tools and Docker permissions")
		}
		manifest := models.BackupManifest{Version: 1, PlanID: plan.ID, RunID: run.ID, SourceServerID: plan.ServerID, DatabaseID: db.ID, Engine: db.Type, ImageID: dump.ImageID, DatabaseName: db.DatabaseName, CreatedAt: time.Now().UTC()}
		encrypted := encryptBackupBundle(dump.Reader, manifest, plan.Recipient, int64(plan.MaxSizeGB)<<30)
		defer encrypted.Close()
		if err = s.authorizeID(run.Actor, plan); err != nil {
			return err
		}
		if err = s.phase(run, "encrypting_and_uploading"); err != nil {
			return err
		}
		receipt, err := s.backend.Upload(ctx, plan, run.ID, encrypted)
		if err != nil {
			return fmt.Errorf("encrypted transfer or checksum verification failed; check destination SSH access, writable directory and size limit")
		}
		artifact = models.RemoteBackupArtifact{ID: run.ID, PlanID: plan.ID, ServerID: plan.ServerID, DestinationID: plan.DestinationID, Path: receipt.Path, Size: receipt.Size, SHA256: receipt.SHA256, Engine: db.Type, ImageID: dump.ImageID, DatabaseName: db.DatabaseName, CreatedAt: manifest.CreatedAt, TestStatus: "not_tested"}
		if plan.AutoTest {
			artifact.TestStatus = "pending"
		}
		if err = s.Store.SaveArtifact(artifact, true); err != nil {
			return fmt.Errorf("archive uploaded but metadata could not be saved; preserve the destination file")
		}
		run.ArtifactID = artifact.ID
	} else {
		artifact, err = s.Store.Artifact(plan.ID, run.ArtifactID)
		if err != nil || artifact.Pruned {
			return store.ErrRemoteBackupNotFound
		}
	}
	if plan.AutoTest || run.Kind == "restore_test" {
		if err = s.phase(run, "restore_testing"); err != nil {
			return err
		}
		if err = s.authorizeID(run.Actor, plan); err != nil {
			return err
		}
		artifact.TestStatus = "running"
		artifact.TestError = ""
		if err = s.Store.SaveArtifact(artifact, false); err != nil {
			return err
		}
		summary, testErr := s.testArtifact(ctx, plan, artifact, run.ID)
		artifact.TestedAt = time.Now().UTC()
		artifact.TestSummary = summary
		artifact.TestStatus = "passed"
		if testErr != nil {
			artifact.TestStatus = "failed"
			artifact.TestError = "Download, decryption, isolated restore or data-read validation failed."
		}
		if err = s.Store.SaveArtifact(artifact, false); err != nil {
			return err
		}
		if testErr != nil {
			return fmt.Errorf("restore test failed; the encrypted backup is retained and production data was not restored")
		}
	}
	if run.Kind == "backup" {
		if err = s.phase(run, "retention"); err != nil {
			return err
		}
		if err = s.prune(ctx, plan, time.Now().UTC()); err != nil {
			return fmt.Errorf("backup stored, but destination retention cleanup failed; existing archives were retained where deletion failed")
		}
	}
	return s.phase(run, "complete")
}
func (s *RemoteBackupService) testArtifact(ctx context.Context, plan models.RemoteBackupPlan, a models.RemoteBackupArtifact, runID string) (string, error) {
	identity, err := readBackupIdentity(s.keyDirectory, plan)
	if err != nil {
		return "", err
	}
	source, err := s.backend.Open(ctx, plan, a)
	if err != nil {
		return "", err
	}
	defer source.Close()
	plain, err := decryptVerifiedBackup(source, identity, a)
	if err != nil {
		return "", err
	}
	return s.backend.RestoreTest(ctx, plan, a, runID, plain)
}
func (s *RemoteBackupService) prune(ctx context.Context, plan models.RemoteBackupPlan, now time.Time) error {
	artifacts, err := s.Store.ExpiredArtifacts(plan, now)
	if err != nil {
		return err
	}
	for _, a := range artifacts {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = s.backend.Remove(ctx, plan, a); err != nil {
			return err
		}
		a.Pruned = true
		if err = s.Store.SaveArtifact(a, false); err != nil {
			return err
		}
	}
	return nil
}
func (s *RemoteBackupService) retryCleanup() {
	runs, err := s.Store.CleanupPending()
	if err != nil {
		log.Printf("Remote backup cleanup history: %v", err)
		return
	}
	for _, run := range runs {
		if s.ctx.Err() != nil {
			return
		}
		plan, e := s.Store.Get(run.ServerID, run.PlanID)
		if e != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
		e = s.backend.Cleanup(ctx, plan, run.ID)
		cancel()
		if e == nil {
			run.NeedsCleanup = false
			if run.Status == "cleanup_failed" {
				run.Status = "failed"
				run.Error = strings.Replace(run.Error, "Automatic cleanup will retry; inspect the source host.", "Temporary cleanup completed on retry.", 1)
				if run.Phase == "complete" {
					run.Status = "succeeded"
					run.Error = ""
				}
			}
			if e = s.Store.UpdateRun(run); e != nil {
				log.Printf("Remote backup cleanup state: %v", e)
			}
		}
	}
}
func (s *RemoteBackupService) Start(ctx context.Context) {
	cleanupRunning := make(chan struct{}, 1)
	startCleanup := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.closed {
			return
		}
		select {
		case cleanupRunning <- struct{}{}:
		default:
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { <-cleanupRunning }()
			s.retryCleanup()
		}()
	}
	startCleanup()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	cleanupTicker := time.NewTicker(time.Minute)
	defer cleanupTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.ctx.Done():
			return
		case now := <-ticker.C:
			if err := s.Tick(now); err != nil {
				log.Printf("Remote backup scheduler: %v", err)
			}
		case <-cleanupTicker.C:
			startCleanup()
		}
	}
}
func (s *RemoteBackupService) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *RemoteBackupService) DeletePlan(serverID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := s.Store.Get(serverID, id)
	if err != nil {
		return err
	}
	if plan.Enabled {
		return invalidBackup("Pause this plan before removing it.")
	}
	if _, busy := s.active[id]; busy {
		return store.ErrRemoteBackupBusy
	}
	// Recovery keys and finalized remote files are intentionally retained.
	return s.Store.Delete(serverID, id)
}

func (s *RemoteBackupService) OpenArchive(ctx context.Context, plan models.RemoteBackupPlan, a models.RemoteBackupArtifact) (io.ReadCloser, error) {
	return s.backend.Open(ctx, plan, a)
}
