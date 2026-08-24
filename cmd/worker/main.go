package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"reni-social/internal/media"
	"reni-social/internal/notify"
	"reni-social/internal/scheduler"
	"reni-social/internal/utils"
	"reni-social/pkg/config"
	"reni-social/pkg/db"
	"reni-social/pkg/linkedin"
	"reni-social/pkg/meta" // <-- NEW IMPORT
	"time"
)

func main() {
	os.Setenv("DBURL", config.C.DBURL)
	db.Connect()
	utils.InitRedis()

	go supervise(scheduler.StartPoller, "Post Scheduler Poller")
	go supervise(scheduler.StartConsumer, "Post Queue Consumer")
	go supervise(scheduler.StartIGContainerFinalizer, "IG Container Finalizer")
	go supervise(media.StartMediaWorker, "Media Processing Worker")
	go supervise(scheduler.StartRetryPoller, "Post Retry Poller") 
	go supervise(func(*sql.DB) { notify.StartNotifyWorker() }, "Notification Worker")

	// ═══════════════════════════════════════════════════════
	// NEW: Meta Token Refresher (Async & Fault Tolerant)
	// ═══════════════════════════════════════════════════════
	go supervise(func(db *sql.DB) {
		repo := meta.NewPostgresRepo(db)
		cfg := meta.Config{
			CheckInterval:     time.Duration(config.C.MetaRefreshCheckIntervalHours) * time.Hour,
			RefreshBeforeDays: config.C.MetaRefreshBeforeDays,
			BatchSize:         config.C.MetaRefreshBatchSize,
			MaxConcurrency:    config.C.MetaRefreshMaxConcurrency,
			RequestTimeout:    30 * time.Second,
			BackoffBase:       1 * time.Second,
		}
		refresher := meta.NewRefresher(cfg, repo, notify.MetaReauthNotifier{})
		ctx := context.Background()
		refresher.Start(ctx)
	}, "Meta Token Refresher")

	// ═══════════════════════════════════════════════════════
	// LinkedIn Expiry Monitor (no refresh token → re-auth flow)
	// ═══════════════════════════════════════════════════════
	go supervise(func(db *sql.DB) {
		repo := linkedin.NewPostgresRepo(db)
		cfg := linkedin.Config{
			CheckInterval:  time.Duration(config.C.LinkedInExpiryCheckIntervalHours) * time.Hour,
			WarnBeforeDays: config.C.LinkedInExpiryWarnBeforeDays,
			BatchSize:      config.C.LinkedInExpiryBatchSize,
		}
		monitor := linkedin.NewMonitor(cfg, repo, notify.LinkedInExpiryNotifier{})
		ctx := context.Background()
		monitor.Start(ctx)
	}, "LinkedIn Expiry Monitor")

	fmt.Println("✅ Reni Social Worker is running...")
	select {} // Block main forever
}

// supervise wraps a worker function in a restart loop with panic recovery.
// If the worker panics or returns, it is restarted after a 5-second backoff.
func supervise(workerFunc func(*sql.DB), workerName string) {
	for {
		log.Printf(" [Supervisor] Starting %s...", workerName)

		err := runWithRecovery(workerFunc)
		if err != nil {
			log.Printf("  [Supervisor] %s stopped with error: %v — restarting in 5s...", workerName, err)
		} else {
			log.Printf("  [Supervisor] %s exited cleanly — restarting in 5s...", workerName)
		}

		time.Sleep(5 * time.Second)
	}
}

// runWithRecovery executes the worker and catches any panics, converting them to errors.
func runWithRecovery(workerFunc func(*sql.DB)) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
			log.Printf(" [Supervisor] Recovered from panic: %v", r)
		}
	}()

	workerFunc(db.DB)
	return nil
}
