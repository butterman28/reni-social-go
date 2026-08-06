package platform

import (
	"context"

	"reni-social/internal/domain"
)

// PublishResult holds identifiers returned by a platform after a successful post.
type PublishResult struct {
	PlatformPostID string // the platform's own ID for the created post
	URL            string // direct link to the published post (if available)
	Pending        bool   // true = platform is still processing async
    ContainerID    string // Instagram container ID to poll later
}

// Publisher is the contract every social platform adapter must satisfy.
type Publisher interface {
	// Publish sends a post to the platform using the provided connection credentials.
	Publish(ctx context.Context, post *domain.Post, conn *domain.PlatformConnection) (*PublishResult, error)

	// RefreshToken exchanges an expired access token for a new one and returns
	// an updated PlatformConnection that should be persisted back to the DB.
	RefreshToken(ctx context.Context, conn *domain.PlatformConnection) (*domain.PlatformConnection, error)
}
