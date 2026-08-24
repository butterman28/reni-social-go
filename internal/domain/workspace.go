package domain

import (
	"time"

	"github.com/google/uuid"
)

// WorkspaceRole defines a member's permission level within a workspace.
type WorkspaceRole string

const (
	RoleOwner  WorkspaceRole = "owner"
	RoleAdmin  WorkspaceRole = "admin"
	RoleMember WorkspaceRole = "member"
)

// Workspace is the top-level organisational unit.
// A user can own or belong to multiple workspaces.
type Workspace struct {
	ID        uuid.UUID `json:"id"`
	OwnerID   uuid.UUID `json:"owner_id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

// WorkspaceMember represents a user's membership in a workspace.
type WorkspaceMember struct {
	WorkspaceID uuid.UUID       `json:"workspace_id"`
	UserID      uuid.UUID       `json:"user_id"`
	Role        WorkspaceRole   `json:"role"`
	JoinedAt    time.Time       `json:"joined_at"`
}
