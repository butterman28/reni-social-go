package scheduler

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"reni-social/internal/utils"
	"reni-social/pkg/config"

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