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

	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup

	for _, conn := range connections {
		conn := conn
		adapter := adapterForPlatform(conn.Platform)
		if adapter == nil {
			log.Printf("⚠️  [Consumer] no adapter for platform %s", conn.Platform)
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			_, pubErr := adapter.Publish(ctx, post, conn)
			if pubErr != nil {
				log.Printf("❌ [Consumer] %s publish error for post %s: %v", conn.Platform, postID, pubErr)
				mu.Lock()
				if firstErr == nil {
					firstErr = pubErr
				}
				mu.Unlock()
			} else {
				log.Printf("✅ [Consumer] %s published post %s", conn.Platform, postID)
			}
		}()
	}

	wg.Wait()

	if firstErr != nil {
		_ = markPostFailed(ctx, db, postID, firstErr.Error())
		return
	}

	_, err = db.ExecContext(ctx,
		`UPDATE posts SET status='published', published_at=NOW(), updated_at=NOW() WHERE id=$1`, postID)
	if err != nil {
		log.Printf("❌ [Consumer] mark published for post %s: %v", postID, err)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// DB helpers
// ──────────────────────────────────────────────────────────────────────────────

func fetchPost(ctx context.Context, db *sql.DB, postID string) (*domain.Post, error) {
	var p domain.Post
	var mediaJSON, platformJSON []byte
	var status string

	err := db.QueryRowContext(ctx, `
		SELECT id, workspace_id, author_id, content, media_urls, platforms,
		       status, scheduled_at, published_at, fail_reason, created_at, updated_at
		FROM posts WHERE id = $1
	`, postID).Scan(
		&p.ID, &p.WorkspaceID, &p.AuthorID, &p.Content,
		&mediaJSON, &platformJSON, &status,
		&p.ScheduledAt, &p.PublishedAt, &p.FailReason,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("fetch post: %w", err)
	}

	p.Status = domain.PostStatus(status)
	_ = json.Unmarshal(mediaJSON, &p.MediaURLs)
	_ = json.Unmarshal(platformJSON, &p.Platforms)
	return &p, nil
}

func fetchConnections(ctx context.Context, db *sql.DB, workspaceID string, platforms []domain.Platform) ([]*domain.PlatformConnection, error) {
	if len(platforms) == 0 {
		return nil, nil
	}

	placeholders := ""
	args := []any{workspaceID}
	for i, p := range platforms {
		if i > 0 {
			placeholders += ","
		}
		placeholders += fmt.Sprintf("$%d", i+2)
		args = append(args, string(p))
	}

	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, workspace_id, platform, account_id, account_name,
		       access_token, refresh_token, expires_at, created_at
		FROM platform_connections
		WHERE workspace_id=$1 AND platform IN (%s)
	`, placeholders), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conns []*domain.PlatformConnection
	for rows.Next() {
		c := &domain.PlatformConnection{}
		err := rows.Scan(&c.ID, &c.WorkspaceID, &c.Platform, &c.AccountID,
			&c.AccountName, &c.AccessToken, &c.RefreshToken, &c.ExpiresAt, &c.CreatedAt)
		if err != nil {
			return nil, err
		}
		conns = append(conns, c)
	}
	return conns, rows.Err()
}

func markPostFailed(ctx context.Context, db *sql.DB, postID, reason string) error {
	_, err := db.ExecContext(ctx,
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
