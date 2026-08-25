// Package scheduler implements the two-stage post dispatch engine.
//
// Stage 1 — DB Poller (this file):
//
//	Every N seconds, query for posts where status='scheduled' AND scheduled_at <= NOW().
//	For each due post, atomically transition it to status='publishing' and push
//	its ID onto the Redis list queue. This decouples discovery from execution
//	and prevents double-publish on worker restart.
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
	}
}

// pollAndEnqueue finds all posts due for publishing, marks them as 'publishing'
// and pushes their IDs onto the Redis queue.
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
// internal/scheduler/engine.go

// ...

func pollAndEnqueue(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	zombieTimeoutMins := config.C.ZombiePostTimeoutMinutes
	if zombieTimeoutMins <= 0 {
		zombieTimeoutMins = 15 // Fallback
	}

	// UPDATED QUERY: Now includes zombie detection, but EXCLUDES posts that are
	// legitimately waiting on async platform processing (e.g. IG video containers).
	// Without this exclusion, a long-running IG video (>15m) would be incorrectly
	// flagged as a zombie, re-enqueued, and result in duplicate IG posts.
	query := `
WITH due AS (
    SELECT id, status FROM posts
    WHERE (
        -- 1. Normal scheduled posts
        (status = 'scheduled' AND scheduled_at <= NOW())
        OR
        -- 2. Zombie recovery: posts stuck in 'publishing' for too long
        -- BUT exclude posts that have active 'processing' results (e.g. IG video containers)
        (status = 'publishing' 
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
    // ...
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
