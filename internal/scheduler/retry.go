package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"time"

	"reni-social/internal/utils"
	"reni-social/pkg/config"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// StartRetryPoller monitors the retry sorted set and moves ready items
// to the main publishing queue. Designed to run under the supervisor.
func StartRetryPoller(db *sql.DB) {
	log.Println("[Retry Poller] Started — checking for delayed retries")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if err := processReadyRetries(); err != nil {
			log.Printf("[Retry Poller] error: %v", err)
		}
	}
}

func processReadyRetries() error {
	ctx := utils.Ctx
	now := float64(time.Now().Unix())

	// Get up to 100 items that are ready (score <= now)
	items, err := utils.RDB.ZRangeByScore(ctx, config.C.RetryPostsQueue, &redis.ZRangeBy{
		Min:   "-inf",
		Max:   fmt.Sprintf("%f", now),
		Count: 100,
	}).Result()
	if err != nil {
		return fmt.Errorf("fetching ready retries: %w", err)
	}
	if len(items) == 0 {
		return nil
	}

	// Atomically move them to the main queue and remove from retry set
	pipe := utils.RDB.Pipeline()
	for _, postID := range items {
		pipe.LPush(ctx, config.C.ScheduledPostsQueue, postID)
		pipe.ZRem(ctx, config.C.RetryPostsQueue, postID)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("moving retries to main queue: %w", err)
	}

	log.Printf("[Retry Poller] Moved %d delayed retries to main queue", len(items))
	return nil
}

// scheduleRetry attempts to schedule a retry for a failed post.
// Only schedules retries if the post was originally scheduled.
// Non-scheduled posts (instant publishes) do not get automatic retries.
// Returns false immediately for instant publishes without touching DB unnecessarily.
func scheduleRetry(ctx context.Context, db *sql.DB, postID, reason string) (bool, error) {
	var retryCount, maxRetries int
	var scheduledAt *time.Time

	// Single query: fetch retry counts AND scheduled_at to determine post type
	err := db.QueryRowContext(ctx,
		`SELECT retry_count, max_retries, scheduled_at FROM posts WHERE id = $1`,
		postID).Scan(&retryCount, &maxRetries, &scheduledAt)
	if err != nil {
		return false, fmt.Errorf("fetch post: %w", err)
	}

	// 🔒 EARLY EXIT: If it's an instant publish (no scheduled_at), don't retry
	// This avoids any further DB writes or Redis operations
	if scheduledAt == nil {
		log.Printf("⏭️  [Scheduler] Post %s was instant publish (not scheduled) — no automatic retries", postID)
		return false,nil // Return false, caller will mark as permanently failed
	}

	// Only for SCHEDULED posts: check if retries are exhausted
	if retryCount >= maxRetries {
		return false, nil // Max retries exceeded
	}

	newAttemptID := uuid.New()
	newRetryCount := retryCount + 1

	// Exponential backoff: 10s, 20s, 40s... capped at 5 minutes
	delay := time.Duration(math.Pow(2, float64(newRetryCount-1))) * 10 * time.Second
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	score := float64(time.Now().Add(delay).Unix())

	log.Printf("🔄 [Scheduler] Scheduling retry for SCHEDULED post %s (attempt %d/%d) in %v due to error: %s", postID, newRetryCount, maxRetries, delay, reason)

	_, updateErr := db.ExecContext(ctx, `
		UPDATE posts
		SET retry_count = $1, status = 'publishing', current_publish_attempt_id = $2, fail_reason = $3, updated_at = NOW()
		WHERE id = $4
	`, newRetryCount, newAttemptID, reason, postID)
	if updateErr != nil {
		// Redis now holds a retry entry pointing at bookkeeping we failed to
		// commit. Best-effort remove it so it doesn't fire against a stale
		// attempt_id/retry_count; the caller's exhausted-retries fallback
		// (mark permanently failed) still applies on the next pass.
		if remErr := utils.RDB.ZRem(utils.Ctx, config.C.RetryPostsQueue, postID).Err(); remErr != nil {
			log.Printf("⚠️  [Scheduler] Failed to roll back queued retry for post %s after DB update failure: %v", postID, remErr)
		}
		return false, fmt.Errorf("update post for retry: %w", updateErr)
	}

	if zaddErr := utils.RDB.ZAdd(utils.Ctx, config.C.RetryPostsQueue, redis.Z{
		Score:  score,
		Member: postID,
	}).Err(); zaddErr != nil {
		log.Printf("⚠️  [Scheduler] Failed to schedule retry for post %s: %v", postID, zaddErr)
	}

	return true, nil
}