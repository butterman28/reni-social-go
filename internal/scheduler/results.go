package scheduler

import (
	"context"
	"database/sql"
	"fmt"

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

// InsertPublishResult creates one row per platform attempt.
func InsertPublishResult(ctx context.Context, db *sql.DB, postID string, attemptID uuid.UUID, platform string, connID uuid.UUID, status PublishResultStatus, platformPostID, platformURL, containerID, failReason string) error {
	id := uuid.New()
	_, err := db.ExecContext(ctx, `
		INSERT INTO publish_results
			(id, post_id, publish_attempt_id, platform, connection_id, status,
			 platform_post_id, platform_post_url, container_id, fail_reason,
			 created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW(),NOW())
	`, id, postID, attemptID, platform, connID, string(status),
		platformPostID, platformURL, containerID, failReason)
	return err
}


// UpdatePublishResult flips a row to published or failed.
func UpdatePublishResult(ctx context.Context, db *sql.DB, resultID string, status PublishResultStatus, platformPostID, platformURL, failReason string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE publish_results
		SET status=$1, platform_post_id=$2, platform_post_url=$3,
		    fail_reason=$4, updated_at=NOW()
		WHERE id=$5
	`, string(status), platformPostID, platformURL, failReason, resultID)
	return err
}

// FinalizePostStatus checks publish_results and updates the parent post's
// status accordingly. Called after every publish_results mutation.
func FinalizePostStatus(ctx context.Context, db *sql.DB, postID string, attemptID uuid.UUID) error {
	var total, done, failed int
	err := db.QueryRowContext(ctx, `
		SELECT
			COUNT(*)                                           AS total,
			COUNT(*) FILTER (WHERE status IN ('published','failed')) AS done,
			COUNT(*) FILTER (WHERE status = 'failed')          AS failed
		FROM publish_results
		WHERE post_id = $1 AND publish_attempt_id = $2  -- 👈 SCOPED TO THIS ATTEMPT
	`, postID, attemptID).Scan(&total, &done, &failed)
	if err != nil {
		return fmt.Errorf("check results for post %s: %w", postID, err)
	}

	if total == 0 {
		return nil // No platforms were attempted
	}

	if total != done {
		// At least one platform is still processing — leave post as 'publishing'
		return nil
	}

	if failed > 0 {
		var reason string
		_ = db.QueryRowContext(ctx, `
			SELECT fail_reason FROM publish_results
			WHERE post_id=$1 AND status='failed' LIMIT 1
		`, postID).Scan(&reason)

		_, err = db.ExecContext(ctx, `
			UPDATE posts SET status='failed', fail_reason=$1, updated_at=NOW()
			WHERE id=$2
		`, reason, postID)
		return err
	}

	// All platforms published successfully
	_, err = db.ExecContext(ctx, `
		UPDATE posts SET status='published', published_at=NOW(), updated_at=NOW()
		WHERE id=$1
	`, postID)
	return err
}