package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"reni-social/internal/billing"
	"reni-social/internal/domain"
	"reni-social/internal/platform"
	"reni-social/internal/platform/linkedin"
	"reni-social/internal/platform/meta"
	xplatform "reni-social/internal/platform/x"
	"reni-social/internal/utils"
	"reni-social/pkg/config"

	"github.com/google/uuid"
)

// StartConsumer is the Stage 2 entry point. It blocks on BRPOP from the Redis
// queue and dispatches each post ID to publishPost in a goroutine pool.
// Concurrency cap is now configurable via config.C.MaxConcurrentPublishes.
// StartConsumer godoc
// @Summary StartConsumer
// @Description Auto-generated thorough documentation for StartConsumer
// @Tags scheduler
// @Produce json
// @Param db body object true "db"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func StartConsumer(db *sql.DB) {
	sem := make(chan struct{}, config.C.MaxConcurrentPublishes)
	log.Printf(" [Consumer] Started — listening on queue: %s (max concurrent: %d)",
		config.C.ScheduledPostsQueue, config.C.MaxConcurrentPublishes)
	for {
		result, err := utils.RDB.BRPop(utils.Ctx, 5*time.Second, config.C.ScheduledPostsQueue).Result()
		if err != nil {
			continue
		}
		postID := result[1]
		sem <- struct{}{}
		go func(id string) {
			defer func() { <-sem }()
			publishPost(db, id)
		}(postID)
	}
}

// publishPost fetches a post from DB, fans out to each platform adapter
// concurrently. Each platform gets its OWN context with its own timeout,
// so one slow platform can never starve another.
// publishPost godoc
// @Summary publishPost
// @Description Auto-generated thorough documentation for publishPost
// @Tags scheduler
// @Produce json
// @Param db body object true "db"
// @Param postID body string true "postID"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func publishPost(db *sql.DB, postID string) {
	// Short context just for DB reads
	dbCtx, dbCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer dbCancel()

	post, err := fetchPost(dbCtx, db, postID)
	if err != nil {
		log.Printf(" [Consumer] fetch post %s: %v", postID, err)
		_ = markPostFailed(dbCtx, db, postID, err.Error())
		return
	}

	connections, err := fetchConnections(dbCtx, db, post.WorkspaceID.String(), post.Platforms)
	if err != nil {
		log.Printf(" [Consumer] fetch connections for post %s: %v", postID, err)
		_ = markPostFailed(dbCtx, db, postID, err.Error())
		return
	}

	if post.CurrentPublishAttemptID == nil {
		log.Printf(" [Consumer] post %s has no attempt ID", postID)
		_ = markPostFailed(dbCtx, db, postID, "missing attempt ID")
		return
	}
	attemptID := *post.CurrentPublishAttemptID

	have := make(map[domain.Platform]bool)
	for _, c := range connections {
		have[c.Platform] = true
	}
	for _, p := range post.Platforms {
		if !have[p] {
			_ = InsertPublishResult(dbCtx, db, postID, attemptID, string(p), uuid.Nil,
				ResultFailed, "", "", "",
				fmt.Sprintf("%s: no active connection (deleted, expired, or needs re-auth)", p))
		}
	}

	perPlatformTimeout := calculatePublishTimeout(dbCtx, post.MediaURLs)
var wg sync.WaitGroup

	// Check platforms that have already succeeded or are actively processing
	// across previous attempts to avoid duplicate posts (e.g. IG video containers).
	skipPlatforms := make(map[string]bool)
	pRows, pErr := db.QueryContext(dbCtx, `
		SELECT DISTINCT platform FROM publish_results
		WHERE post_id = $1 AND status IN ('published', 'processing')
	`, postID)
	if pErr == nil {
		defer pRows.Close()
		for pRows.Next() {
			var pStr string
			if err := pRows.Scan(&pStr); err == nil {
				skipPlatforms[pStr] = true
			}
		}
	}

	for _, conn := range connections {
		if skipPlatforms[string(conn.Platform)] {
			log.Printf("ℹ️  [Consumer] Platform %s already published or processing for post %s — skipping retry", conn.Platform, postID)
			continue
		}

		conn := conn
		adapter := adapterForPlatform(conn.Platform)
		if adapter == nil {
			log.Printf("  [Consumer] no adapter for platform %s", conn.Platform)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Each platform gets its OWN context with its OWN timeout
			platCtx, platCancel := context.WithTimeout(context.Background(), perPlatformTimeout)
			defer platCancel()
			//result, pubErr := adapter.Publish(platCtx, post, conn)
			writeCtx := context.Background()

			//CHECK & DEDUCT CREDITS FOR X API
			if conn.Platform == domain.PlatformX {
				cost := config.C.XPostCreditCost // e.g., 5 credits per tweet
				err := billing.DeductCredits(writeCtx, post.AuthorID.String(), cost, "x_post_publish", attemptID.String())
				if err != nil {
					log.Printf(" [Consumer] %s publish blocked for post %s: %v", conn.Platform, postID, err)
					_ = InsertPublishResult(writeCtx, db, postID, attemptID, string(conn.Platform), conn.ID,
						ResultFailed, "", "", "", "Insufficient X API credits. Please top up your wallet.")
					return
				}
			}

			// Proceed with actual X API call
			optimizedURLs := resolveMediaURLs(platCtx, db, post.ID, string(conn.Platform), post.MediaURLs)
		
		// Create a shallow copy of the post with the optimized URLs
			postForPlatform := *post
			postForPlatform.MediaURLs = optimizedURLs

			// Proceed with actual platform API call using the optimized post
			result, pubErr := adapter.Publish(platCtx, &postForPlatform, conn)

			if pubErr != nil {
				log.Printf(" [Consumer] %s publish error for post %s: %v", conn.Platform, postID, pubErr)
				_ = InsertPublishResult(writeCtx, db, postID, attemptID, string(conn.Platform), conn.ID,
					ResultFailed, "", "", "", pubErr.Error())
				return
			}

			if result != nil && result.Pending {
				log.Printf(" [Consumer] %s container %s pending for post %s",
					conn.Platform, result.ContainerID, postID)
				_ = InsertPublishResult(writeCtx, db, postID, attemptID, string(conn.Platform), conn.ID,
					ResultProcessing, "", "", result.ContainerID, "")
				return
			}

			log.Printf(" [Consumer] %s published post %s", conn.Platform, postID)
			_ = InsertPublishResult(writeCtx, db, postID, attemptID, string(conn.Platform), conn.ID,
				ResultPublished, result.PlatformPostID, result.URL, "", "")
		}()
	}
	wg.Wait()

	finalCtx, finalCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer finalCancel()
	if err := FinalizePostStatus(finalCtx, db, postID, attemptID); err != nil {
		log.Printf(" [Consumer] finalize post %s: %v", postID, err)
	}
}
// ──────────────────────────────────────────────────────────────────────────────
// DB helpers
// ──────────────────────────────────────────────────────────────────────────────

// fetchPost godoc
// @Summary fetchPost
// @Description Auto-generated thorough documentation for fetchPost
// @Tags scheduler
// @Produce json
// @Param db body object true "db"
// @Param postID body string true "postID"
// @Success 200 {object} domain.Post "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func fetchPost(ctx context.Context, db *sql.DB, postID string) (*domain.Post, error) {
	var p domain.Post
	var mediaJSON, platformJSON []byte
	var status string
	var attemptIDStr sql.NullString
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

// fetchConnections godoc
// @Summary fetchConnections
// @Description Auto-generated thorough documentation for fetchConnections
// @Tags scheduler
// @Produce json
// @Param db body object true "db"
// @Param workspaceID body string true "workspaceID"
// @Param platforms body array true "platforms"
// @Success 200 {object} []domain.PlatformConnection "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
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
			FROM linkedin_connections
			WHERE workspace_id = $1
			  AND is_deleted = false
			  AND needs_reauth = false
			  AND expires_at > NOW()
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
			FROM meta_connections
			WHERE workspace_id = $1
			  AND is_deleted = false
			  AND needs_reauth = false
			  AND expires_at > NOW()
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
			FROM x_connections
			WHERE workspace_id = $1
			  AND is_deleted = false
			  AND (expires_at IS NULL OR expires_at > NOW())
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

// markPostFailed godoc
// @Summary markPostFailed
// @Description Auto-generated thorough documentation for markPostFailed
// @Tags scheduler
// @Produce json
// @Param db body object true "db"
// @Param reason body string true "reason"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func markPostFailed(ctx context.Context, db *sql.DB, postID, reason string) error {
	scheduled, err := scheduleRetry(ctx, db, postID, reason)
	if err != nil {
		return err
	}
	if scheduled {
		return nil
	}

	// Max retries exceeded, mark as permanently failed
	_, err = db.ExecContext(ctx,
		`UPDATE posts SET status='failed', fail_reason=$1, updated_at=NOW() WHERE id=$2`,
		reason, postID)
	return err
}


// calculatePublishTimeout checks media URLs to determine the appropriate timeout.
// Small videos get the base timeout; large videos get an extended timeout.
func calculatePublishTimeout(ctx context.Context, mediaURLs []string) time.Duration {
    baseTimeout := time.Duration(config.C.PublishTimeoutMinutes) * time.Minute
    videoTimeout := 20 * time.Minute
    sizeThreshold := int64(20 * 1024 * 1024) // 20 MB

    for _, u := range mediaURLs {
        if domain.IsVideoURL(u) {
            // Quick HEAD request to check file size (3s timeout)
            headCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
            req, _ := http.NewRequestWithContext(headCtx, http.MethodHead, u, nil)
            
            resp, err := http.DefaultClient.Do(req)
            cancel()
            
            if err == nil && resp.StatusCode == http.StatusOK {
                resp.Body.Close()
                // If the server tells us the size, use it to decide
                if resp.ContentLength > 0 {
                    if resp.ContentLength > sizeThreshold {
                        log.Printf("🎥 [Consumer] Video is large (%.2f MB) — extending timeout to 20m", float64(resp.ContentLength)/(1024*1024))
                        return videoTimeout
                    }
                    log.Printf("🎥 [Consumer] Video is small (%.2f MB) — using base timeout", float64(resp.ContentLength)/(1024*1024))
                    return baseTimeout
                }
            }
            
            // If HEAD fails or server doesn't send Content-Length, fallback to safe video timeout
            log.Printf("⚠️ [Consumer] Could not determine video size for %s — defaulting to 20m timeout", u)
            return videoTimeout
        }
    }
    
    return baseTimeout
}
// ──────────────────────────────────────────────────────────────────────────────
// Platform adapter factory
// ──────────────────────────────────────────────────────────────────────────────

// adapterForPlatform godoc
// @Summary adapterForPlatform
// @Description Auto-generated thorough documentation for adapterForPlatform
// @Tags scheduler
// @Produce json
// @Param p body string true "p"
// @Success 200 {object} platform.Publisher "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
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
func resolveMediaURLs(ctx context.Context, db *sql.DB, postID uuid.UUID, platform string, fallbackURLs []string) []string {
	// 1. Try exact platform match
	rows, err := db.QueryContext(ctx, `
		SELECT processed_url FROM media_assets 
		WHERE post_id = $1 AND platform = $2 AND status = 'completed' AND processed_url IS NOT NULL
		ORDER BY created_at ASC
	`, postID, platform)
	
	var optimized []string
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var procURL string
			if err := rows.Scan(&procURL); err == nil {
				optimized = append(optimized, procURL)
			}
		}
	}
	if len(optimized) > 0 {
		return optimized
	}
	
	// 2. Fallback to 'universal' platform assets
	rows, err = db.QueryContext(ctx, `
		SELECT processed_url FROM media_assets 
		WHERE post_id = $1 AND platform = 'universal' AND status = 'completed' AND processed_url IS NOT NULL
		ORDER BY created_at ASC
	`, postID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var procURL string
			if err := rows.Scan(&procURL); err == nil {
				optimized = append(optimized, procURL)
			}
		}
	}
	if len(optimized) > 0 {
		return optimized
	}

	// 3. Final fallback to original URLs
	return fallbackURLs
}
var _ = uuid.Nil