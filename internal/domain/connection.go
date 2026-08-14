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
	AccountID    string
	AccountName  string
	AccessToken  string
	RefreshToken string
	ExpiresAt    *time.Time
	CreatedAt    time.Time
	IsDeleted    bool
	NeedsReauth  bool  
	ReauthReason string 
}