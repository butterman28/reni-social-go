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
	ID        uuid.UUID
	OwnerID   uuid.UUID
	Name      string
	Slug      string
	CreatedAt time.Time
}

// WorkspaceMember represents a user's membership in a workspace.
type WorkspaceMember struct {
	WorkspaceID uuid.UUID
	UserID      uuid.UUID
	Role        WorkspaceRole
	JoinedAt    time.Time
}
