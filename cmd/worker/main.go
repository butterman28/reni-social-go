package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"reni-social/internal/media"
	"reni-social/internal/scheduler"
	"reni-social/internal/utils"
	"reni-social/pkg/config"
	"reni-social/pkg/db"
)

func main() {
	// 1. Initialize global connections
	os.Setenv("DBURL", config.C.DBURL)
	db.Connect()
	utils.InitRedis()

	// 2. Start workers — each runs under a supervisor that auto-restarts on panic/exit
	go supervise(scheduler.StartPoller, "Post Scheduler Poller")
	go supervise(scheduler.StartConsumer, "Post Queue Consumer")
	go supervise(media.StartMediaWorker, "Media Processing Worker")

	fmt.Println("🚀 Reni Social Worker is running...")

	// 3. Block forever — workers run in goroutines above
	select {}
}

// supervise wraps a worker function in a restart loop with panic recovery.
// If the worker panics or returns, it is restarted after a 5-second backoff.
func supervise(workerFunc func(*sql.DB), workerName string) {
	for {
		log.Printf("🔄 [Supervisor] Starting %s...", workerName)

		err := runWithRecovery(workerFunc)
		if err != nil {
			log.Printf("⚠️  [Supervisor] %s stopped with error: %v — restarting in 5s...", workerName, err)
		} else {
			log.Printf("ℹ️  [Supervisor] %s exited cleanly — restarting in 5s...", workerName)
		}

		time.Sleep(5 * time.Second)
	}
}

// runWithRecovery executes the worker and catches any panics, converting them to errors.
func runWithRecovery(workerFunc func(*sql.DB)) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
			log.Printf("🔥 [Supervisor] Recovered from panic: %v", r)
		}
	}()

	workerFunc(db.DB)
	return nil
}
