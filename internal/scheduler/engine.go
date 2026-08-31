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
	"reni-social/internal/notify"
	"reni-social/internal/utils"
	"reni-social/pkg/config"
	"time"
)

// StartPoller runs the Postgres polling loop to find scheduled posts and detect zombies.
func StartPoller(db *sql.DB) {
	log.Println("[Scheduler] Started Post Poller — polling Postgres every 5s")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if err := pollAndEnqueue(db); err != nil {
			log.Printf("[Scheduler] Poll error: %v", err)
		}
		if err := failInstantPublishTimeouts(db); err != nil {
			log.Printf("  [Poller] fast-fail error: %v", err)
		}
	}
}

// pollAndEnqueue finds all posts due for publishing and detects zombie posts.
// Zombies are transitioned directly to 'failed' and the user is notified.
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

	// UPDATED QUERY: Detects zombies and transitions them directly to 'failed'
	// No more retry loops - just fail fast and notify the user
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
SET status = CASE
        WHEN due.status = 'publishing' THEN 'failed'
        ELSE 'publishing'
    END,
    updated_at = NOW(),
    current_publish_attempt_id = CASE
        WHEN due.status = 'publishing' THEN posts.current_publish_attempt_id
        ELSE gen_random_uuid()
    END,
    fail_reason = CASE
        WHEN due.status = 'publishing' THEN 'Post exceeded maximum processing time (15 minutes) and was marked as failed'
        ELSE posts.fail_reason
    END
FROM due
WHERE posts.id = due.id
RETURNING posts.id::text, 
          (CASE WHEN due.status = 'publishing' THEN 'failed' ELSE 'publishing' END) as new_status,
          (SELECT workspace_id FROM posts WHERE posts.id = due.id)::text as workspace_id
`

	rows, err := db.QueryContext(ctx, query, zombieTimeoutMins)
	if err != nil {
		return fmt.Errorf("poll query: %w", err)
	}
	defer rows.Close()

	var enqueued int
	var zombies []string

	for rows.Next() {
		var postID, newStatus, workspaceID string
		if err := rows.Scan(&postID, &newStatus, &workspaceID); err != nil {
			log.Printf("  [Poller] scan row: %v", err)
			continue
		}

		if newStatus == "failed" {
			// This is a zombie - collect it for notification
			zombies = append(zombies, postID)
			log.Printf(" [Poller] Post %s marked as failed (zombie: exceeded 15 min timeout)", postID)
			continue
		}

		// Normal scheduled post - push to Redis queue
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

	// Notify users about failed zombies
	if len(zombies) > 0 {
		go notifyZombieFailures(db, zombies)
	}

	return rows.Err()
}

// notifyZombieFailures sends notifications to workspace owners about posts that failed
// due to exceeding the processing timeout.
func notifyZombieFailures(db *sql.DB, zombiePostIDs []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, postID := range zombiePostIDs {
		// Fetch post details and workspace owner email
		var workspaceID, content string
		var ownerEmail sql.NullString
		err := db.QueryRowContext(ctx, `
			SELECT p.workspace_id::text, p.content, u.email
			FROM posts p
			JOIN workspaces w ON w.id = p.workspace_id
			LEFT JOIN users u ON u.id = w.owner_id
			WHERE p.id = $1
		`, postID).Scan(&workspaceID, &content, &ownerEmail)

		if err != nil {
			log.Printf("  [Poller] failed to fetch zombie post details for %s: %v", postID, err)
			continue
		}

		if !ownerEmail.Valid || ownerEmail.String == "" {
			log.Printf("  [Poller] no owner email for zombie post %s - skipping notification", postID)
			continue
		}

		// Truncate content for the notification
		preview := content
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}

		subject := "Action required: Your scheduled post failed to publish"
		body := fmt.Sprintf(
			"Hi there,\n\n"+
				"Your scheduled post failed to publish after exceeding the maximum processing time (15 minutes).\n\n"+
				"Post preview: %s\n\n"+
				"This can happen due to:\n"+
				"- Temporary platform API issues\n"+
				"- Network connectivity problems\n"+
				"- Worker process crashes\n\n"+
				"You can retry publishing this post manually from your workspace dashboard.\n\n"+
				"— The Reni Social team",
			preview,
		)

		if err := notify.EnqueueEmail(ownerEmail.String, subject, body); err != nil {
			log.Printf("  [Poller] failed to enqueue zombie notification for post %s: %v", postID, err)
		} else {
			log.Printf(" [Poller] Sent zombie failure notification for post %s to %s", postID, ownerEmail.String)
		}
	}
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
