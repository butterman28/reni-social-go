package lock

import (
	"database/sql"
	"fmt"
	"log"
)

const MigrationLockKey int64 = 777777

// AcquireMigrationLock blocks until it gets the advisory lock
func AcquireMigrationLock(db *sql.DB) error {
	log.Println("[LOCK] Waiting for migration lock...")
	_, err := db.Exec(`SELECT pg_advisory_lock($1)`, MigrationLockKey)
	if err != nil {
		return fmt.Errorf("failed to acquire migration lock: %w", err)
	}

	log.Println("[LOCK] Migration lock acquired")
	return nil
}

// ReleaseMigrationLock releases the advisory lock
func ReleaseMigrationLock(db *sql.DB) {
	_, err := db.Exec(`SELECT pg_advisory_unlock($1)`, MigrationLockKey)
	if err != nil {
		log.Println("[LOCK] Failed to release migration lock:", err)
		return
	}

	log.Println("[LOCK] Migration lock released")
}