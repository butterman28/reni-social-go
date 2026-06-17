package main

import (
	"dogo/pkg/config"
	"dogo/pkg/db"
	"dogo/schema/lock"
	"dogo/schema/migrator"
	"log"
	"os"
)

func main() {
	// 1️⃣ Connect to PostgreSQL
	os.Setenv("DBURL", config.C.DBURL)
	db.Connect()
	defer db.DB.Close()

	// 2ï¸ âƒ£ Acquire migration lock (fail if another migration is running)
	if err := lock.AcquireMigrationLock(db.DB); err != nil {
		log.Fatalf("[LOCK] Could not acquire migration lock: %v", err)
	}
	// // Ensure the lock is released at the end
	// defer lock.ReleaseMigrationLock(db.DB)

	// Ensure the lock is released even if we crash or get interrupted
	defer func() {
		log.Println("[LOCK] Releasing migration lock...")
		lock.ReleaseMigrationLock(db.DB)
	}()

	log.Println("[MAIN] Starting schema synchronization...")

	// 3️⃣ Sync public tables first (includes organizations)
	if err := migrator.SyncPublicTables(db.DB); err != nil {
		log.Fatal("[SYNC] Failed to sync public tables:", err)
	}

	log.Println("✅ All schemas are up-to-date")
}
