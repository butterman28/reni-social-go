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
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	AuthorID    uuid.UUID
	Content     string
	MediaURLs   []string   // stored as JSONB in DB
	Platforms   []Platform // stored as JSONB in DB
	//optional for linkedin 
	ArticleURL   string
	ArticleTitle string
	Status      PostStatus
	ScheduledAt *time.Time
	PublishedAt *time.Time
	FailReason  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
