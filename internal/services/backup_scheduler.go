package services

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/AbdallahAwd/GoPulse/internal/store"
)

type BackupScheduler struct {
	dbService *DatabaseService
	store     *store.DatabaseStore
	lastRun   map[string]string // dbID -> date string "YYYY-MM-DD"
	mu        sync.Mutex
}

// NewBackupScheduler creates a new BackupScheduler
func NewBackupScheduler(dbService *DatabaseService, dbStore *store.DatabaseStore) *BackupScheduler {
	return &BackupScheduler{
		dbService: dbService,
		store:     dbStore,
		lastRun:   make(map[string]string),
	}
}

// Start begins the background backup scheduler loop
func (s *BackupScheduler) Start(ctx context.Context) {
	// Check every minute to see if we need to run backups.
	// This is very cheap and ensures we don't miss the 2:00 AM window.
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	log.Println("Starting automatic daily database backup scheduler (window: 02:00 AM)")

	// Run an initial check upon start
	s.checkAndRunBackups(ctx)

	for {
		select {
		case <-ticker.C:
			s.checkAndRunBackups(ctx)
		case <-ctx.Done():
			log.Println("Stopping database backup scheduler")
			return
		}
	}
}

func (s *BackupScheduler) checkAndRunBackups(ctx context.Context) {
	now := time.Now()
	// Check if we are in the 2:00 AM hour
	if now.Hour() != 2 {
		return
	}

	databases := s.store.ListDatabases()
	todayStr := now.Format("2006-01-02")

	for _, db := range databases {
		// Only backup active running databases that have auto_backup enabled
		if !db.AutoBackup {
			continue
		}

		// Avoid duplicate runs today
		s.mu.Lock()
		lastDate, exists := s.lastRun[db.ID]
		if exists && lastDate == todayStr {
			s.mu.Unlock()
			continue
		}
		s.lastRun[db.ID] = todayStr
		s.mu.Unlock()

		// Skip databases that are stopped/error
		if db.Status != models.DatabaseStatusRunning {
			log.Printf("[BackupScheduler] Skipping auto-backup for database %s (%s) because status is %s", db.Name, db.ID, db.Status)
			continue
		}

		// Run backup in a separate goroutine so one slow database doesn't block the rest
		go func(database models.Database) {
			log.Printf("[BackupScheduler] Triggering auto-backup for database %s (%s)", database.Name, database.ID)
			backup, err := s.dbService.CreateBackup(ctx, database.ID)
			if err != nil {
				log.Printf("[BackupScheduler] Auto-backup failed for database %s (%s): %v", database.Name, database.ID, err)
				return
			}
			log.Printf("[BackupScheduler] Auto-backup completed for database %s (%s): file=%s, size=%d bytes", database.Name, database.ID, backup.Filename, backup.Size)

			// Clean up old backups based on retention settings
			if database.RetentionDays > 0 {
				log.Printf("[BackupScheduler] Purging backups older than %d days for database %s (%s)", database.RetentionDays, database.Name, database.ID)
				if err := s.dbService.PurgeOldBackups(ctx, database.ID, database.RetentionDays); err != nil {
					log.Printf("[BackupScheduler] Failed to purge old backups for database %s (%s): %v", database.Name, database.ID, err)
				}
			}
		}(db)
	}
}
