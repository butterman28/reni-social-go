package main

import (
	"database/sql"
	"dogo/internal/utils"
	"dogo/internal/workers" // Adjust path to your worker package
	"dogo/pkg/config"
	"dogo/pkg/db"
	"fmt"
	"log"
	"os"
	"time"

	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

func main() {
	// 1. Initialize Global Connections
	os.Setenv("DBURL", config.C.DBURL)
	db.Connect()
	utils.InitRedis()

	// 2. Start Workers Independently
	// Each worker gets its own supervisor loop and goroutine
	go supervise(workers.StartEventMediaWorker, "Event Media Worker")

	fmt.Println("Dogo Worker Services are running...")

	// 3. Block forever
	select {}
}

// supervise handles the recovery and restart logic for a specific worker
func supervise(workerFunc func(*sql.DB), workerName string) {
	for {
		log.Printf("🔄 [Supervisor] Starting %s...", workerName)

		// Execute the worker and capture any panics/errors
		err := runWithRecovery(workerFunc)

		if err != nil {
			log.Printf("⚠️ [Supervisor] %s stopped: %v. Restarting in 5s...", workerName, err)
		} else {
			log.Printf("ℹ️ [Supervisor] %s exited cleanly. Restarting in 5s...", workerName)
		}

		// Avoid rapid-fire restart loops if the error is persistent (like missing FFmpeg)
		time.Sleep(5 * time.Second)
	}
}

// runWithRecovery wraps the worker call in a defer/recover block
func runWithRecovery(workerFunc func(*sql.DB)) (err error) {
	defer func() {
		if r := recover(); r != nil {
			// Capture the panic and turn it into an error
			err = fmt.Errorf("panic: %v", r)
			log.Printf("🔥 CRITICAL: Recovered from panic in worker: %v", r)
		}
	}()

	// Call the actual worker function
	workerFunc(db.DB)

	return nil
}
