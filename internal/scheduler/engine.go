// Package scheduler implements the two-stage post dispatch engine.
//
// Stage 1 — DB Poller (this file):
//
//	Every N seconds, query for posts where status='scheduled' AND scheduled_at <= NOW().
//	For each due post, atomically transition it to status='publishing' and push
//	its ID onto the Redis list queue. This decouples discovery from execution
//	and prevents double-publish on worker restart.
//
//	Also handles fast-fail detection for instant publishes (no scheduled_at):
//	If an instant publish is stuck in 'publishing' for > InstantPublishTimeoutMinutes,
//	mark it as 'failed' immediately. Scheduled posts use the longer ZombiePostTimeoutMinutes.
//
// Stage 2 — Queue Consumer (consumer.go):
//
//	Blocks on BRPOP from the Redis queue, fetches the full post, fans out to
//	platform adapters concurrently, and updates the post status.
package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"reni-social/internal/utils"
	"reni-social/pkg/config"
)

// StartPoller is the Stage 1 entry point. It runs forever and is designed
// to be wrapped by the supervisor goroutine in cmd/worker/main.go.
// StartPoller godoc
// @Summary StartPoller
// @Description Auto-generated thorough documentation for StartPoller
// @Tags scheduler
// @Produce json
// @Param db body object true "db"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func StartPoller(db *sql.DB) {
	interval := time.Duration(config.C.SchedulerPollIntervalSecs) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf(" [Poller] Started — polling every %s", interval)

	for range ticker.C {
		if err := pollAndEnqueue(db); err != nil {
			log.Printf("  [Poller] poll error: %v", err)
		}
		if err := failInstantPublishTimeouts(db); err != nil {
			log.Printf("  [Poller] fast-fail error: %v", err)
		}
	}
}

// pollAndEnqueue finds all scheduled posts due for publishing, marks them as 'publishing'
// and pushes their IDs onto the Redis queue. It also handles zombie recovery for scheduled posts.
// pollAndEnqueue godoc
// @Summary pollAndEnqueue
// @Description Auto-generated thorough documentation for pollAndEnqueue
// @Tags scheduler
// @Produce json
// @Param db body object true "db"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func pollAndEnqueue(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	zombieTimeoutMins := config.C.ZombiePostTimeoutMinutes
	if zombieTimeoutMins <= 0 {
		zombieTimeoutMins = 15 // Fallback
	}

	// UPDATED QUERY: Handles scheduled posts and zombie recovery (but NOT instant publishes).
	// Instant publishes with timeout are handled separately by failInstantPublishTimeouts().
	// This query excludes instant publishes by checking scheduled_at IS NOT NULL.
	// It also excludes posts that are legitimately waiting on async platform processing (e.g. IG video containers).
	query := `
WITH due AS (
    SELECT id, status FROM posts
    WHERE (
        -- 1. Normal scheduled posts (scheduled_at IS SET)
        (status = 'scheduled' AND scheduled_at <= NOW())
        OR
        -- 2. Zombie recovery: SCHEDULED posts stuck in 'publishing' for too long
        -- ONLY for posts with scheduled_at (instant publishes handled separately)
        (status = 'publishing' 
         AND scheduled_at IS NOT NULL
         AND updated_at < NOW() - ($1 || ' minutes')::INTERVAL
         AND NOT EXISTS (
             SELECT 1 FROM publish_results pr 
             WHERE pr.post_id = posts.id AND pr.status = 'processing'
         )
        )
    )
    FOR UPDATE SKIP LOCKED
)
UPDATE posts
SET status = 'publishing',
    updated_at = NOW(),
    current_publish_attempt_id = gen_random_uuid(),
    -- Increment retry_count ONLY for zombies to prevent infinite crash loops
    retry_count = CASE
        WHEN due.status = 'publishing' THEN posts.retry_count + 1
        ELSE posts.retry_count
    END
FROM due
WHERE posts.id = due.id
RETURNING posts.id::text
`

	rows, err := db.QueryContext(ctx, query, zombieTimeoutMins)
	if err != nil {
		return fmt.Errorf("poll query: %w", err)
	}
	defer rows.Close()

	var enqueued int
	for rows.Next() {
		var postID string
		if err := rows.Scan(&postID); err != nil {
			log.Printf("  [Poller] scan row: %v", err)
			continue
		}

		// Push to Redis queue — consumer will BRPOP this
		if err := utils.RDB.LPush(utils.Ctx, config.C.ScheduledPostsQueue, postID).Err(); err != nil {
			log.Printf("  [Poller] enqueue post %s: %v", postID, err)
			// Revert status so it will be picked up on the next tick
			_, _ = db.ExecContext(ctx,
				`UPDATE posts SET status='scheduled', updated_at=NOW() WHERE id=$1`, postID)
			continue
		}
		enqueued++
		log.Printf(" [Poller] Enqueued post %s", postID)
	}

	if enqueued > 0 {
		log.Printf(" [Poller] Enqueued %d post(s)", enqueued)
	}

	return rows.Err()
}

// failInstantPublishTimeouts marks instant publishes (scheduled_at IS NULL) as 'failed'
// if they've been stuck in 'publishing' for longer than InstantPublishTimeoutMinutes.
// This is a FAST-FAIL mechanism: instant publishes don't retry automatically,
// so we fail them quickly to notify the user, separate from zombie recovery for scheduled posts.
// failInstantPublishTimeouts godoc
// @Summary failInstantPublishTimeouts
// @Description Auto-generated thorough documentation for failInstantPublishTimeouts
// @Tags scheduler
// @Produce json
// @Param db body object true "db"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func failInstantPublishTimeouts(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	instantTimeoutMins := config.C.InstantPublishTimeoutMinutes
	if instantTimeoutMins <= 0 {
		instantTimeoutMins = 5 // Default: 5 minutes for instant publishes
	}

	// Find instant publishes (scheduled_at IS NULL) stuck in 'publishing' for too long
	// Exclude posts with active 'processing' status (e.g., IG video containers)
	query := `
UPDATE posts
SET status = 'failed',
    fail_reason = 'Instant publish timeout: stuck in publishing for >' || $1::text || ' minutes',
    updated_at = NOW()
WHERE status = 'publishing'
  AND scheduled_at IS NULL
  AND updated_at < NOW() - ($1 || ' minutes')::INTERVAL
  AND NOT EXISTS (
      SELECT 1 FROM publish_results pr
      WHERE pr.post_id = posts.id AND pr.status = 'processing'
  )
RETURNING id::text
`

	rows, err := db.QueryContext(ctx, query, instantTimeoutMins)
	if err != nil {
		return fmt.Errorf("instant publish timeout query: %w", err)
	}
	defer rows.Close()

	var failed int
	for rows.Next() {
		var postID string
		if err := rows.Scan(&postID); err != nil {
			log.Printf("  [Poller] scan failed instant publish: %v", err)
			continue
		}
		failed++
		log.Printf("⏱️  [Poller] Instant publish %s timed out after %d minutes — marked as failed", postID, instantTimeoutMins)
	}

	if failed > 0 {
		log.Printf("⏱️  [Poller] Failed %d instant publish(es) due to timeout", failed)
	}

	return rows.Err()
}
