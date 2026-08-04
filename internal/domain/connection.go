package domain

import (
	"time"

	"github.com/google/uuid"
)

// PlatformConnection stores OAuth credentials for a social platform
// account connected to a workspace. Access tokens are stored encrypted at rest.
type PlatformConnection struct {
	ID           uuid.UUID
	WorkspaceID  uuid.UUID
	Platform     Platform
	AccountID    string // the platform's own user/page ID
	AccountName  string
	AccessToken  string // encrypted before DB write
	RefreshToken string // encrypted before DB write
	ExpiresAt    *time.Time
	CreatedAt    time.Time
	IsDeleted    bool 
}
