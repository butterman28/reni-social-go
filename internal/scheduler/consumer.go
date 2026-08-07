package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"reni-social/internal/domain"
	"reni-social/internal/platform"
	"reni-social/internal/platform/linkedin"
	"reni-social/internal/platform/meta"
	xplatform "reni-social/internal/platform/x"
	"reni-social/internal/utils"
	"reni-social/pkg/config"
)

const maxConcurrentPublishes = 10

// StartConsumer is the Stage 2 entry point. It blocks on BRPOP from the Redis
// queue and dispatches each post ID to publishPost in a goroutine pool.
// Designed to run under the supervisor in cmd/worker/main.go.
func StartConsumer(db *sql.DB) {
	sem := make(chan struct{}, maxConcurrentPublishes)

	log.Printf("🚀 [Consumer] Started — listening on queue: %s", config.C.ScheduledPostsQueue)

	for {
		// BRPOP blocks until a message arrives (5s timeout so we can check for shutdown)
		result, err := utils.RDB.BRPop(utils.Ctx, 5*time.Second, config.C.ScheduledPostsQueue).Result()
		if err != nil {
			// Timeout is normal — loop again
			continue
		}

		// result[0] = key, result[1] = value (postID)
		postID := result[1]

		sem <- struct{}{} // acquire slot
		go func(id string) {
			defer func() { <-sem }() // release slot
			publishPost(db, id)
		}(postID)
	}
}

// publishPost fetches a post from DB, fans out to each platform adapter concurrently,
// and marks the post as published or failed.
func publishPost(db *sql.DB, postID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	post, err := fetchPost(ctx, db, postID)
	if err != nil {
		log.Printf("❌ [Consumer] fetch post %s: %v", postID, err)
		_ = markPostFailed(ctx, db, postID, err.Error())
		return
	}

	connections, err := fetchConnections(ctx, db, post.WorkspaceID.String(), post.Platforms)
	if err != nil {
		log.Printf("❌ [Consumer] fetch connections for post %s: %v", postID, err)
		_ = markPostFailed(ctx, db, postID, err.Error())
		return
	}
	
	// Safely extract the attempt ID (guaranteed to exist because the poller/API set it)
	attemptID := *post.CurrentPublishAttemptID 
	var wg sync.WaitGroup

	// Check platforms that have already succeeded across previous attempts to avoid duplicate posts
	publishedPlatforms := make(map[string]bool)
	pRows, pErr := db.QueryContext(ctx, `
		SELECT DISTINCT platform FROM publish_results
		WHERE post_id = $1 AND status = 'published'
	`, postID)
	if pErr == nil {
		defer pRows.Close()
		for pRows.Next() {
			var pStr string
			if err := pRows.Scan(&pStr); err == nil {
				publishedPlatforms[pStr] = true
			}
		}
	}

	for _, conn := range connections {
		if publishedPlatforms[string(conn.Platform)] {
			log.Printf("ℹ️  [Consumer] Platform %s already published for post %s — skipping retry", conn.Platform, postID)
			continue
		}

		conn := conn
		adapter := adapterForPlatform(conn.Platform)
		if adapter == nil {
			log.Printf("⚠️  [Consumer] no adapter for platform %s", conn.Platform)
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			result, pubErr := adapter.Publish(ctx, post, conn)

			if pubErr != nil {
				log.Printf("❌ [Consumer] %s publish error for post %s: %v", conn.Platform, postID, pubErr)
				// 👇 Fixed: Added attemptID, removed "..."
				_ = InsertPublishResult(ctx, db, postID, attemptID, string(conn.Platform), conn.ID,
					ResultFailed, "", "", "", pubErr.Error())
				return
			}

			// Async / pending (Instagram video)
			if result != nil && result.Pending {
				log.Printf("⏳ [Consumer] %s container %s pending for post %s",
					conn.Platform, result.ContainerID, postID)
				// 👇 Fixed: Added attemptID
				_ = InsertPublishResult(ctx, db, postID, attemptID, string(conn.Platform), conn.ID,
					ResultProcessing, "", "", result.ContainerID, "")
				return
			}

			// Synchronous success
			log.Printf("✅ [Consumer] %s published post %s", conn.Platform, postID)
			// 👇 Fixed: Added attemptID
			_ = InsertPublishResult(ctx, db, postID, attemptID, string(conn.Platform), conn.ID,
				ResultPublished, result.PlatformPostID, result.URL, "", "")
		}()
	}

	wg.Wait()

	// Derive the post-level status from per-platform results
	// 👇 Fixed: Removed the broken "{ ... }" duplicate line and passed attemptID
	if err := FinalizePostStatus(ctx, db, postID, attemptID); err != nil {
		log.Printf("❌ [Consumer] finalize post %s: %v", postID, err)
	}
}


// ──────────────────────────────────────────────────────────────────────────────
// DB helpers
// ──────────────────────────────────────────────────────────────────────────────
func saveContainerID(ctx context.Context, db *sql.DB, postID, containerID string) error {
    _, err := db.ExecContext(ctx,
        `UPDATE posts SET ig_container_id=$1, ig_container_status='processing', updated_at=NOW() WHERE id=$2`,
        containerID, postID)
    return err
}
func fetchPost(ctx context.Context, db *sql.DB, postID string) (*domain.Post, error) {
	var p domain.Post
	var mediaJSON, platformJSON []byte
	var status string
	var attemptIDStr sql.NullString // <--- Added
	
	var articleURL, articleTitle, failReason sql.NullString

	err := db.QueryRowContext(ctx, `
		SELECT id, workspace_id, author_id, content, media_urls, platforms,
		       article_url, article_title, current_publish_attempt_id,
		       status, scheduled_at, published_at, fail_reason, created_at, updated_at,
		       retry_count, max_retries
		FROM posts WHERE id = $1
	`, postID).Scan(
		&p.ID, &p.WorkspaceID, &p.AuthorID, &p.Content,
		&mediaJSON, &platformJSON,
		&articleURL, &articleTitle, &attemptIDStr,
		&status,
		&p.ScheduledAt, &p.PublishedAt, 
		&failReason,              
		&p.CreatedAt, &p.UpdatedAt,
		&p.RetryCount, &p.MaxRetries,
	)
	if err != nil {
		return nil, fmt.Errorf("fetch post: %w", err)
	}
	
	if attemptIDStr.Valid {
		parsed, _ := uuid.Parse(attemptIDStr.String)
		p.CurrentPublishAttemptID = &parsed
	}

	p.Status = domain.PostStatus(status)
	_ = json.Unmarshal(mediaJSON, &p.MediaURLs)
	_ = json.Unmarshal(platformJSON, &p.Platforms)

	if articleURL.Valid {
		p.ArticleURL = articleURL.String
	}
	if articleTitle.Valid {
		p.ArticleTitle = articleTitle.String
	}
	if failReason.Valid {
		p.FailReason = failReason.String
	}

	return &p, nil
}

func fetchConnections(ctx context.Context, db *sql.DB, workspaceID string, platforms []domain.Platform) ([]*domain.PlatformConnection, error) {
	if len(platforms) == 0 {
		return nil, nil
	}

	var conns []*domain.PlatformConnection
	hasLinkedIn := false
	hasMeta := false
	hasX := false

	for _, p := range platforms {
		switch p {
		case domain.PlatformLinkedIn:
			hasLinkedIn = true
		case domain.PlatformMetaFB, domain.PlatformMetaIG:
			hasMeta = true
		case domain.PlatformX:
			hasX = true
		}
	}

	if hasLinkedIn {
		rows, err := db.QueryContext(ctx, `
			SELECT id, workspace_id, account_id, account_name, access_token, refresh_token, expires_at, created_at
			FROM linkedin_connections WHERE workspace_id = $1
		`, workspaceID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			c := &domain.PlatformConnection{Platform: domain.PlatformLinkedIn}
			if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.AccountID, &c.AccountName, &c.AccessToken, &c.RefreshToken, &c.ExpiresAt, &c.CreatedAt); err != nil {
				return nil, err
			}
			conns = append(conns, c)
		}
	}

	if hasMeta {
		rows, err := db.QueryContext(ctx, `
			SELECT id, workspace_id, platform, account_id, account_name, access_token, refresh_token, expires_at, created_at
			FROM meta_connections WHERE workspace_id = $1
		`, workspaceID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			c := &domain.PlatformConnection{}
			var platformStr string
			if err := rows.Scan(&c.ID, &c.WorkspaceID, &platformStr, &c.AccountID, &c.AccountName, &c.AccessToken, &c.RefreshToken, &c.ExpiresAt, &c.CreatedAt); err != nil {
				return nil, err
			}
			c.Platform = domain.Platform(platformStr)
			// Only include if this platform is in the requested platforms list
			isRequested := false
			for _, reqP := range platforms {
				if reqP == c.Platform {
					isRequested = true
					break
				}
			}
			if isRequested {
				conns = append(conns, c)
			}
		}
	}

	if hasX {
		rows, err := db.QueryContext(ctx, `
			SELECT id, workspace_id, platform, account_id, account_name, access_token, refresh_token, expires_at, created_at
			FROM x_connections WHERE workspace_id = $1
		`, workspaceID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			c := &domain.PlatformConnection{}
			var platformStr string
			if err := rows.Scan(&c.ID, &c.WorkspaceID, &platformStr, &c.AccountID, &c.AccountName, &c.AccessToken, &c.RefreshToken, &c.ExpiresAt, &c.CreatedAt); err != nil {
				return nil, err
			}
			c.Platform = domain.Platform(platformStr)
			conns = append(conns, c)
		}
	}

	return conns, nil
}

func markPostFailed(ctx context.Context, db *sql.DB, postID, reason string) error {
	var retryCount, maxRetries int
	err := db.QueryRowContext(ctx, `SELECT retry_count, max_retries FROM posts WHERE id = $1`, postID).Scan(&retryCount, &maxRetries)
	if err == nil && retryCount < maxRetries {
		newAttemptID := uuid.New()
		newRetryCount := retryCount + 1
		log.Printf("🔄 [Consumer] Retrying post %s (attempt %d/%d) due to error: %s", postID, newRetryCount, maxRetries, reason)

		_, updateErr := db.ExecContext(ctx, `
			UPDATE posts
			SET retry_count = $1, status = 'publishing', current_publish_attempt_id = $2, fail_reason = $3, updated_at = NOW()
			WHERE id = $4
		`, newRetryCount, newAttemptID, reason, postID)
		if updateErr != nil {
			return updateErr
		}

		if enqueueErr := utils.RDB.LPush(utils.Ctx, config.C.ScheduledPostsQueue, postID).Err(); enqueueErr != nil {
			log.Printf("⚠️  [Consumer] Failed to re-enqueue post %s for retry: %v", postID, enqueueErr)
		} else {
			return nil
		}
	}

	_, err = db.ExecContext(ctx,
		`UPDATE posts SET status='failed', fail_reason=$1, updated_at=NOW() WHERE id=$2`,
		reason, postID)
	return err
}

// ──────────────────────────────────────────────────────────────────────────────
// Platform adapter factory
// ──────────────────────────────────────────────────────────────────────────────

func adapterForPlatform(p domain.Platform) platform.Publisher {
	switch p {
	case domain.PlatformLinkedIn:
		return linkedin.New()
	case domain.PlatformMetaFB, domain.PlatformMetaIG:
		return meta.New()
	case domain.PlatformX:
		return xplatform.New()
	}
	return nil
}

// Blank use of uuid to prevent import errors before full integration.
var _ = uuid.Nil
