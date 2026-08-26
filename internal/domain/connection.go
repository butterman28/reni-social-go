package domain

import (
	"time"

	"github.com/google/uuid"
)

// PlatformConnection stores OAuth credentials for a social platform
// account connected to a workspace. Access tokens are stored encrypted at rest.
// PlatformConnection stores OAuth credentials for a social platform
type PlatformConnection struct {
	ID           uuid.UUID  `json:"id"`
	WorkspaceID  uuid.UUID  `json:"workspace_id"`
	Platform     Platform   `json:"platform"`
	AccountID    string     `json:"account_id"`
	AccountName  string     `json:"account_name"`
	
	// SECURITY FIX: json:"-" prevents these from leaking in API responses
	AccessToken  string     `json:"-"`
	RefreshToken string     `json:"-"`
	
	ExpiresAt    *time.Time `json:"expires_at"`
	CreatedAt    time.Time  `json:"created_at"`
	IsDeleted    bool       `json:"is_deleted"`
	NeedsReauth  bool       `json:"needs_reauth"`
	ReauthReason string     `json:"reauth_reason"`
}