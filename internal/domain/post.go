package domain

import (
	"time"

	"github.com/google/uuid"
)

// Platform represents a supported social media platform.
type Platform string

const (
	PlatformLinkedIn Platform = "linkedin"
	PlatformMetaIG   Platform = "meta_ig" // Instagram
	PlatformMetaFB   Platform = "meta_fb" // Facebook Page
	PlatformX        Platform = "x"       // Twitter / X
	PlatformTikTok   Platform = "tiktok"
)

// PostStatus represents the lifecycle state of a post.
type PostStatus string

const (
	StatusDraft      PostStatus = "draft"
	StatusScheduled  PostStatus = "scheduled"
	StatusPublishing PostStatus = "publishing" // locked by worker — being sent
	StatusPublished  PostStatus = "published"
	StatusFailed     PostStatus = "failed"
)

// Post is the core content entity.
type Post struct {
	ID                      uuid.UUID  `json:"id"`
	WorkspaceID             uuid.UUID  `json:"workspace_id"`
	AuthorID                uuid.UUID  `json:"author_id"`
	Content                 string     `json:"content"`
	MediaURLs               []string   `json:"media_urls"`
	Platforms               []Platform `json:"platforms"`
	ArticleURL              string     `json:"article_url"`
	ArticleTitle            string     `json:"article_title"`
	Status                  PostStatus `json:"status"`
	ScheduledAt             *time.Time `json:"scheduled_at"`
	PublishedAt             *time.Time `json:"published_at"`
	FailReason              string     `json:"fail_reason"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
	CurrentPublishAttemptID *uuid.UUID `json:"current_publish_attempt_id"`
	RetryCount              int        `json:"retry_count"`
	MaxRetries              int        `json:"max_retries"`
}
