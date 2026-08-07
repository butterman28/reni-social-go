package post

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"reni-social/internal/domain"
	"reni-social/internal/platform"
	"reni-social/internal/platform/linkedin"
	"reni-social/internal/platform/meta"
	xplatform "reni-social/internal/platform/x"
	"reni-social/internal/utils"
	"reni-social/pkg/config"
	"reni-social/pkg/db"

	"github.com/google/uuid"
)

// ──────────────────────────────────────────────────────────────────────────────
// Request / Response types
// ──────────────────────────────────────────────────────────────────────────────

type CreatePostRequest struct {
	WorkspaceID  string            `json:"workspace_id"`
	Content      string            `json:"content"`
	MediaURLs    []string          `json:"media_urls"`
	Platforms    []domain.Platform `json:"platforms"`
	ArticleURL   string            `json:"article_url"`    // optional — LinkedIn article share
	ArticleTitle string            `json:"article_title"`  // optional — preview card title
	ScheduledAt  *time.Time        `json:"scheduled_at"`   // nil = draft
}

type UpdatePostRequest struct {
	Content      *string           `json:"content"`
	MediaURLs    []string          `json:"media_urls"`
	Platforms    []domain.Platform `json:"platforms"`
	ArticleURL   *string           `json:"article_url"`
	ArticleTitle *string           `json:"article_title"`
	ScheduledAt  *time.Time        `json:"scheduled_at"`
}

type PostFilter struct {
	Status string
	Page   int
	Limit  int
}

// ──────────────────────────────────────────────────────────────────────────────
// Service
// ──────────────────────────────────────────────────────────────────────────────

// CreatePost persists a new post. If ScheduledAt is set, the status is "scheduled";
// otherwise it is saved as "draft".

func validateArticlePlatforms(platforms []domain.Platform) error {
	// validateArticlePlatforms ensures that if an article URL is provided,
	// the selected platforms actually support link/article shares.
	for _, p := range platforms {
		switch p {
		case domain.PlatformMetaIG:
			return fmt.Errorf("instagram does not support article/link shares (requires media)")
		case domain.PlatformTikTok:
			return fmt.Errorf("tiktok does not support article/link shares (requires video)")
		}
	}
	return nil
}

func CreatePost(ctx context.Context, authorID string, req CreatePostRequest) (*domain.Post, error) {
	workspaceID, err := uuid.Parse(req.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace_id: %w", err)
	}
	authorUUID, err := uuid.Parse(authorID)
	if err != nil {
		return nil, fmt.Errorf("invalid author id: %w", err)
	}
	if req.Content == "" {
		return nil, fmt.Errorf("content cannot be empty")
	}
	if len(req.Platforms) == 0 {
		return nil, fmt.Errorf("at least one platform is required")
	}
	//Validate platform compatibility for article shares
	if req.ArticleURL != "" {
		if err := validateArticlePlatforms(req.Platforms); err != nil {
			return nil, err
		}
	}

	status := domain.StatusDraft
	if req.ScheduledAt != nil {
		if req.ScheduledAt.Before(time.Now()) {
			return nil, fmt.Errorf("scheduled_at must be in the future")
		}
		status = domain.StatusScheduled
	}

	mediaJSON, _ := json.Marshal(req.MediaURLs)
	platformJSON, _ := json.Marshal(req.Platforms)

	postID := uuid.New()
	now := time.Now()

	query := `
		INSERT INTO posts (
			id, workspace_id, author_id, content, media_urls, platforms,
			article_url, article_title,
			status, scheduled_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $8, $9, $10, $11, $11)
	`
	_, err = db.DB.ExecContext(ctx, query,
		postID, workspaceID, authorUUID, req.Content,
		mediaJSON, platformJSON,
		req.ArticleURL, req.ArticleTitle,
		string(status), req.ScheduledAt, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create post: %w", err)
	}

	return &domain.Post{
		ID:           postID,
		WorkspaceID:  workspaceID,
		AuthorID:     authorUUID,
		Content:      req.Content,
		MediaURLs:    req.MediaURLs,
		Platforms:    req.Platforms,
		ArticleURL:   req.ArticleURL,
		ArticleTitle: req.ArticleTitle,
		Status:       status,
		ScheduledAt:  req.ScheduledAt,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// GetPost fetches a single post scoped to a workspace.
func GetPost(ctx context.Context, postID, workspaceID string) (*domain.Post, error) {
	query := `
		SELECT id, workspace_id, author_id, content, media_urls, platforms,
		       article_url, article_title, current_publish_attempt_id,
		       status, scheduled_at, published_at, fail_reason, created_at, updated_at,
		       retry_count, max_retries
		FROM posts
		WHERE id = $1 AND workspace_id = $2
	`
	row := db.DB.QueryRowContext(ctx, query, postID, workspaceID)
	return scanPost(row)
}

// ListPosts returns paginated posts for a workspace, optionally filtered by status.
func ListPosts(ctx context.Context, workspaceID string, f PostFilter) ([]domain.Post, error) {
	if f.Limit == 0 {
		f.Limit = 20
	}
	offset := (f.Page) * f.Limit
	args := []any{workspaceID, f.Limit, offset}

	where := "WHERE workspace_id = $1"
	if f.Status != "" {
		where += " AND status = $4"
		args = append(args, f.Status)
	}

	query := fmt.Sprintf(`
		SELECT id, workspace_id, author_id, content, media_urls, platforms,
		       article_url, article_title, current_publish_attempt_id,
		       status, scheduled_at, published_at, fail_reason, created_at, updated_at,
		       retry_count, max_retries
		FROM posts %s
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, where)

	rows, err := db.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list posts: %w", err)
	}
	defer rows.Close()

	var posts []domain.Post
	for rows.Next() {
		p, err := scanPostRow(rows)
		if err != nil {
			return nil, err
		}
		posts = append(posts, *p)
	}
	return posts, rows.Err()
}

// UpdatePost modifies a post's content, media, platforms, article fields, or schedule.
// Only draft and scheduled posts can be updated.
func UpdatePost(ctx context.Context, postID, workspaceID string, req UpdatePostRequest) (*domain.Post, error) {
	existing, err := GetPost(ctx, postID, workspaceID)
	if err != nil {
		return nil, err
	}
	if existing.Status == domain.StatusPublished || existing.Status == domain.StatusPublishing {
		return nil, fmt.Errorf("cannot edit a post that has already been published")
	}

	if req.Content != nil {
		existing.Content = *req.Content
	}
	if req.MediaURLs != nil {
		existing.MediaURLs = req.MediaURLs
	}
	if req.Platforms != nil {
		existing.Platforms = req.Platforms
	}
	if req.ArticleURL != nil {
		existing.ArticleURL = *req.ArticleURL
	}
	if req.ArticleTitle != nil {
		existing.ArticleTitle = *req.ArticleTitle
	}
	if req.ScheduledAt != nil {
		existing.ScheduledAt = req.ScheduledAt
		existing.Status = domain.StatusScheduled
	}
	//Determine final state and validate for article shares
	finalPlatforms := existing.Platforms
	if req.Platforms != nil {
		finalPlatforms = req.Platforms
	}
	finalArticleURL := existing.ArticleURL
	if req.ArticleURL != nil {
		finalArticleURL = *req.ArticleURL
	}

	if finalArticleURL != "" {
		if err := validateArticlePlatforms(finalPlatforms); err != nil {
			return nil, err
		}
	}

	mediaJSON, _ := json.Marshal(existing.MediaURLs)
	platformJSON, _ := json.Marshal(existing.Platforms)

	_, err = db.DB.ExecContext(ctx, `
		UPDATE posts SET
			content=$1, media_urls=$2::jsonb, platforms=$3::jsonb,
			article_url=$4, article_title=$5,
			status=$6, scheduled_at=$7, updated_at=NOW()
		WHERE id=$8 AND workspace_id=$9
	`, existing.Content, mediaJSON, platformJSON,
		existing.ArticleURL, existing.ArticleTitle,
		string(existing.Status), existing.ScheduledAt, postID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("update post: %w", err)
	}

	return existing, nil
}

// DeletePost removes a post from the DB. Only draft/failed/scheduled posts can be deleted.
func DeletePost(ctx context.Context, postID, workspaceID string) error {
	result, err := db.DB.ExecContext(ctx,
		`DELETE FROM posts WHERE id=$1 AND workspace_id=$2 AND status NOT IN ('publishing','published')`,
		postID, workspaceID)
	if err != nil {
		return fmt.Errorf("delete post: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("post not found or cannot be deleted in its current state")
	}
	return nil
}

// SchedulePost sets or updates the scheduled time for an existing post.
func SchedulePost(ctx context.Context, postID, workspaceID string, scheduledAt time.Time) error {
	if scheduledAt.Before(time.Now()) {
		return fmt.Errorf("scheduled_at must be in the future")
	}
	_, err := db.DB.ExecContext(ctx, `
		UPDATE posts SET status='scheduled', scheduled_at=$1, updated_at=NOW()
		WHERE id=$2 AND workspace_id=$3 AND status IN ('draft','scheduled')
	`, scheduledAt, postID, workspaceID)
	return err
}

// PublishNow enqueues a post to Redis for asynchronous publishing by the background worker.
func PublishNow(ctx context.Context, postID, workspaceID string) (uuid.UUID, error) {
	post, err := GetPost(ctx, postID, workspaceID)
	if err != nil {
		return uuid.Nil, err
	}
	if post.Status == domain.StatusPublished || post.Status == domain.StatusPublishing {
		return uuid.Nil, fmt.Errorf("post is already published or being published")
	}

	// 1. Generate the Idempotency Key for this specific button click
	attemptID := uuid.New()

	_, err = db.DB.ExecContext(ctx,
		`UPDATE posts SET status='publishing', current_publish_attempt_id=$1, updated_at=NOW() WHERE id=$2`,
		attemptID, postID)
	if err != nil {
		return uuid.Nil, err
	}

	// 2. Enqueue the post to the Redis queue for the worker to publish it asynchronously
	if err := utils.RDB.LPush(ctx, config.C.ScheduledPostsQueue, postID).Err(); err != nil {
		// Revert status on failure
		_, _ = db.DB.ExecContext(ctx,
			`UPDATE posts SET status=$1, current_publish_attempt_id=NULL, updated_at=NOW() WHERE id=$2`,
			post.Status, postID)
		return uuid.Nil, fmt.Errorf("enqueue post: %w", err)
	}

	return attemptID, nil
}
// ──────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────────────────────

func adapterFor(p domain.Platform) platform.Publisher {
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

// loadConnections, markFailed, scanPost, scanPostRow, scanPostFromScanner...
// (unchanged except scanPostFromScanner — see below)

func loadConnections(ctx context.Context, workspaceID string, platforms []domain.Platform) ([]*domain.PlatformConnection, error) {
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
		rows, err := db.DB.QueryContext(ctx, `
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
		rows, err := db.DB.QueryContext(ctx, `
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
		rows, err := db.DB.QueryContext(ctx, `
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

func markFailed(ctx context.Context, postID, reason string) error {
	_, err := db.DB.ExecContext(ctx,
		`UPDATE posts SET status='failed', fail_reason=$1, updated_at=NOW() WHERE id=$2`,
		reason, postID)
	return err
}

type scannable interface {
	Scan(dest ...any) error
}

func scanPost(row *sql.Row) (*domain.Post, error)    { return scanPostFromScanner(row) }
func scanPostRow(row *sql.Rows) (*domain.Post, error) { return scanPostFromScanner(row) }

func scanPostFromScanner(s scannable) (*domain.Post, error) {
	var p domain.Post
	var mediaJSON, platformJSON []byte
	var status string
	
	// Use sql.NullString for columns that can be NULL in the DB
	var articleURL, articleTitle, failReason, attemptIDStr sql.NullString

	err := s.Scan(
		&p.ID, &p.WorkspaceID, &p.AuthorID, &p.Content,
		&mediaJSON, &platformJSON,
		&articleURL, &articleTitle, &attemptIDStr,
		&status,
		&p.ScheduledAt, &p.PublishedAt, 
		&failReason,              
		&p.CreatedAt, &p.UpdatedAt,
		&p.RetryCount, &p.MaxRetries,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("post not found")
	}
	if err != nil {
		return nil, err
	}
	
	// Parse the attempt ID safely
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