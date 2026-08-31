package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reni-social/internal/utils"
	"time"

	"github.com/google/uuid"
)

// PublishResultStatus mirrors the lifecycle of a single platform publish.
type PublishResultStatus string

const (
	ResultPending    PublishResultStatus = "pending"
	ResultProcessing PublishResultStatus = "processing"
	ResultPublished  PublishResultStatus = "published"
	ResultFailed     PublishResultStatus = "failed"
)

// resultsCacheKey returns the Redis key for a given post+attempt.
// resultsCacheKey godoc
// @Summary resultsCacheKey
// @Description Auto-generated thorough documentation for resultsCacheKey
// @Tags scheduler
// @Produce json
// @Param postID body string true "postID"
// @Param attemptID body string true "attemptID"
// @Success 200 {string} string "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func resultsCacheKey(postID string, attemptID uuid.UUID) string {
	return fmt.Sprintf("results:%s:%s", postID, attemptID.String())
}

// resultsCacheTTL is how long results stay in Redis (2 hours).
const resultsCacheTTL = 2 * time.Hour

// CacheResultEntry is the JSON shape stored in Redis per platform result.
type CacheResultEntry struct {
	ID             string `json:"id"`
	Platform       string `json:"platform"`
	Status         string `json:"status"`
	PlatformPostID string `json:"platform_post_id"`
	PlatformURL    string `json:"platform_post_url"`
	ContainerID    string `json:"container_id"`
	FailReason     string `json:"fail_reason"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// cacheResult writes a single result entry into the Redis hash for this post+attempt.
// cacheResult godoc
// @Summary cacheResult
// @Description Auto-generated thorough documentation for cacheResult
// @Tags scheduler
// @Produce json
// @Param postID body string true "postID"
// @Param attemptID body string true "attemptID"
// @Param entry body string true "entry"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func cacheResult(postID string, attemptID uuid.UUID, entry CacheResultEntry) {
	key := resultsCacheKey(postID, attemptID)
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	pipe := utils.RDB.TxPipeline()
	pipe.HSet(utils.Ctx, key, entry.Platform, data)
	pipe.Expire(utils.Ctx, key, resultsCacheTTL)
	_, _ = pipe.Exec(utils.Ctx)
}

// ReadCachedResults reads all cached results for a post+attempt from Redis.
// Returns nil if nothing is cached (caller should fall back to Postgres).
// ReadCachedResults godoc
// @Summary ReadCachedResults
// @Description Auto-generated thorough documentation for ReadCachedResults
// @Tags scheduler
// @Produce json
// @Param postID body string true "postID"
// @Param attemptID body string true "attemptID"
// @Success 200 {array} CacheResultEntry "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func ReadCachedResults(postID string, attemptID uuid.UUID) []CacheResultEntry {
	key := resultsCacheKey(postID, attemptID)
	vals, err := utils.RDB.HVals(utils.Ctx, key).Result()
	if err != nil || len(vals) == 0 {
		return nil
	}
	results := make([]CacheResultEntry, 0, len(vals))
	for _, v := range vals {
		var entry CacheResultEntry
		if err := json.Unmarshal([]byte(v), &entry); err != nil {
			continue
		}
		results = append(results, entry)
	}
	return results
}

// InsertPublishResult creates one row per platform attempt and caches it.
// InsertPublishResult godoc
// @Summary InsertPublishResult
// @Description Auto-generated thorough documentation for InsertPublishResult
// @Tags scheduler
// @Produce json
// @Param db body object true "db"
// @Param postID body string true "postID"
// @Param attemptID body string true "attemptID"
// @Param platform body string true "platform"
// @Param connID body string true "connID"
// @Param status body string true "status"
// @Param failReason body string true "failReason"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func InsertPublishResult(ctx context.Context, db *sql.DB, postID string, attemptID uuid.UUID, platform string, connID uuid.UUID, status PublishResultStatus, platformPostID, platformURL, containerID, failReason string) error {
	id := uuid.New()
	now := time.Now()
	_, err := db.ExecContext(ctx, `
		INSERT INTO publish_results
			(id, post_id, publish_attempt_id, platform, connection_id, status,
			 platform_post_id, platform_post_url, container_id, fail_reason,
			 created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW(),NOW())
	`, id, postID, attemptID, platform, connID, string(status),
		platformPostID, platformURL, containerID, failReason)
	if err != nil {
		return err
	}

	// Write-through to Redis cache
	cacheResult(postID, attemptID, CacheResultEntry{
		ID:             id.String(),
		Platform:       platform,
		Status:         string(status),
		PlatformPostID: platformPostID,
		PlatformURL:    platformURL,
		ContainerID:    containerID,
		FailReason:     failReason,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	return nil
}

// UpdatePublishResult flips a row to published or failed and updates cache.
// UpdatePublishResult godoc
// @Summary UpdatePublishResult
// @Description Auto-generated thorough documentation for UpdatePublishResult
// @Tags scheduler
// @Produce json
// @Param db body object true "db"
// @Param resultID body string true "resultID"
// @Param postID body string true "postID"
// @Param attemptID body string true "attemptID"
// @Param platform body string true "platform"
// @Param status body string true "status"
// @Param failReason body string true "failReason"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func UpdatePublishResult(ctx context.Context, db *sql.DB, resultID string, postID string, attemptID uuid.UUID, platform string, status PublishResultStatus, platformPostID, platformURL, failReason string) error {
	now := time.Now()
	_, err := db.ExecContext(ctx, `
		UPDATE publish_results
		SET status=$1, platform_post_id=$2, platform_post_url=$3,
			fail_reason=$4, updated_at=NOW()
		WHERE id=$5
	`, string(status), platformPostID, platformURL, failReason, resultID)
	if err != nil {
		return err
	}

	// Write-through to Redis cache
	cacheResult(postID, attemptID, CacheResultEntry{
		ID:             resultID,
		Platform:       platform,
		Status:         string(status),
		PlatformPostID: platformPostID,
		PlatformURL:    platformURL,
		FailReason:     failReason,
		UpdatedAt:      now,
	})
	return nil
}

// FinalizePostStatus checks publish_results and updates the parent post's
// status accordingly. Called after every publish_results mutation.
// FinalizePostStatus godoc
// @Summary FinalizePostStatus
// @Description Auto-generated thorough documentation for FinalizePostStatus
// @Tags scheduler
// @Produce json
// @Param db body object true "db"
// @Param postID body string true "postID"
// @Param attemptID body string true "attemptID"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func FinalizePostStatus(ctx context.Context, db *sql.DB, postID string, attemptID uuid.UUID) error {
	var total, done, failed int
	err := db.QueryRowContext(ctx, `
SELECT
	COUNT(*)                                               AS total,
	COUNT(*) FILTER (WHERE status IN ('published','failed')) AS done,
	COUNT(*) FILTER (WHERE status = 'failed')              AS failed
FROM publish_results
WHERE post_id = $1 AND publish_attempt_id = $2
`, postID, attemptID).Scan(&total, &done, &failed)
	if err != nil {
		return fmt.Errorf("check results for post %s: %w", postID, err)
	}

	if total == 0 {
		// Edge case: All platforms were skipped because they succeeded in a previous attempt.
		var hasPublished bool
		_ = db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM publish_results WHERE post_id=$1 AND status='published')
		`, postID).Scan(&hasPublished)
		
		if hasPublished {
			_, _ = db.ExecContext(ctx, `UPDATE posts SET status='published', published_at=NOW(), updated_at=NOW() WHERE id=$1`, postID)
		}
		return nil
	}
	if total != done {
		return nil
	}

	if failed > 0 {
    var reason string
    _ = db.QueryRowContext(ctx, `
    SELECT fail_reason FROM publish_results
    WHERE post_id=$1 AND publish_attempt_id=$2 AND status='failed' LIMIT 1
    `, postID, attemptID).Scan(&reason)

		scheduled, schedErr := scheduleRetry(ctx, db, postID, reason)
		if schedErr != nil {
			return schedErr
		}
		if scheduled {
			return nil
		}

		// Max retries exceeded, mark as permanently failed
		_, err = db.ExecContext(ctx, `
UPDATE posts SET status='failed', fail_reason=$1, updated_at=NOW()
WHERE id=$2
`, reason, postID)
		return err
	}

	_, err = db.ExecContext(ctx, `
UPDATE posts SET status='published', published_at=NOW(), updated_at=NOW()
WHERE id=$1
`, postID)
	return err
}