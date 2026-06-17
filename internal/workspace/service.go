package workspace

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"reni-social/internal/domain"
	"reni-social/pkg/db"
)

// ──────────────────────────────────────────────────────────────────────────────
// Request types
// ──────────────────────────────────────────────────────────────────────────────

type CreateWorkspaceRequest struct {
	Name string `json:"name"`
}

type InviteMemberRequest struct {
	Email string              `json:"email"`
	Role  domain.WorkspaceRole `json:"role"`
}

// ──────────────────────────────────────────────────────────────────────────────
// Workspace CRUD
// ──────────────────────────────────────────────────────────────────────────────

// CreateWorkspace creates a new workspace and adds the creator as owner.
func CreateWorkspace(ctx context.Context, ownerID string, req CreateWorkspaceRequest) (*domain.Workspace, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("workspace name is required")
	}

	ownerUUID, err := uuid.Parse(ownerID)
	if err != nil {
		return nil, fmt.Errorf("invalid owner id")
	}

	wsID := uuid.New()
	slug := generateSlug(req.Name, wsID)
	now := time.Now()

	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO workspaces (id, owner_id, name, slug, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, wsID, ownerUUID, req.Name, slug, now)
	if err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role, joined_at)
		VALUES ($1, $2, 'owner', $3)
	`, wsID, ownerUUID, now)
	if err != nil {
		return nil, fmt.Errorf("add owner member: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &domain.Workspace{
		ID:        wsID,
		OwnerID:   ownerUUID,
		Name:      req.Name,
		Slug:      slug,
		CreatedAt: now,
	}, nil
}

// GetWorkspace fetches a workspace the user is a member of.
func GetWorkspace(ctx context.Context, workspaceID, userID string) (*domain.Workspace, error) {
	var ws domain.Workspace
	err := db.DB.QueryRowContext(ctx, `
		SELECT w.id, w.owner_id, w.name, w.slug, w.created_at
		FROM workspaces w
		JOIN workspace_members m ON m.workspace_id = w.id
		WHERE w.id = $1 AND m.user_id = $2
	`, workspaceID, userID).Scan(&ws.ID, &ws.OwnerID, &ws.Name, &ws.Slug, &ws.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("workspace not found or access denied")
	}
	if err != nil {
		return nil, fmt.Errorf("get workspace: %w", err)
	}
	return &ws, nil
}

// ListMyWorkspaces returns all workspaces the user belongs to.
func ListMyWorkspaces(ctx context.Context, userID string) ([]domain.Workspace, error) {
	rows, err := db.DB.QueryContext(ctx, `
		SELECT w.id, w.owner_id, w.name, w.slug, w.created_at
		FROM workspaces w
		JOIN workspace_members m ON m.workspace_id = w.id
		WHERE m.user_id = $1
		ORDER BY w.created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()

	var workspaces []domain.Workspace
	for rows.Next() {
		var ws domain.Workspace
		if err := rows.Scan(&ws.ID, &ws.OwnerID, &ws.Name, &ws.Slug, &ws.CreatedAt); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, ws)
	}
	return workspaces, rows.Err()
}

// ──────────────────────────────────────────────────────────────────────────────
// Members
// ──────────────────────────────────────────────────────────────────────────────

// InviteMember adds a user (by email) to a workspace.
// The caller must be owner or admin.
func InviteMember(ctx context.Context, workspaceID, callerID string, req InviteMemberRequest) error {
	// Verify caller has permission
	if err := requireRole(ctx, workspaceID, callerID, domain.RoleAdmin, domain.RoleOwner); err != nil {
		return err
	}

	// Resolve email → userID
	var targetID uuid.UUID
	err := db.DB.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, req.Email).Scan(&targetID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("no user found with email %s", req.Email)
	}
	if err != nil {
		return fmt.Errorf("lookup user: %w", err)
	}

	role := req.Role
	if role == "" || role == domain.RoleOwner {
		role = domain.RoleMember // default; prevent escalation to owner via invite
	}

	_, err = db.DB.ExecContext(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role, joined_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (workspace_id, user_id) DO UPDATE SET role = EXCLUDED.role
	`, workspaceID, targetID, string(role))
	return err
}

// ListMembers returns all members of a workspace.
func ListMembers(ctx context.Context, workspaceID, callerID string) ([]map[string]any, error) {
	if err := requireRole(ctx, workspaceID, callerID, domain.RoleMember, domain.RoleAdmin, domain.RoleOwner); err != nil {
		return nil, err
	}

	rows, err := db.DB.QueryContext(ctx, `
		SELECT u.id, u.email, u.first_name, u.last_name, u.avatar_url, m.role, m.joined_at
		FROM workspace_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.workspace_id = $1
		ORDER BY m.joined_at ASC
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer rows.Close()

	var members []map[string]any
	for rows.Next() {
		var id, email, firstName, lastName, avatarURL, role string
		var joinedAt time.Time
		if err := rows.Scan(&id, &email, &firstName, &lastName, &avatarURL, &role, &joinedAt); err != nil {
			return nil, err
		}
		members = append(members, map[string]any{
			"id": id, "email": email, "first_name": firstName,
			"last_name": lastName, "avatar_url": avatarURL,
			"role": role, "joined_at": joinedAt,
		})
	}
	return members, rows.Err()
}

// RemoveMember removes a user from a workspace. Owners cannot be removed.
func RemoveMember(ctx context.Context, workspaceID, callerID, targetUserID string) error {
	if err := requireRole(ctx, workspaceID, callerID, domain.RoleAdmin, domain.RoleOwner); err != nil {
		return err
	}

	// Protect owner
	var role string
	_ = db.DB.QueryRowContext(ctx,
		`SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`,
		workspaceID, targetUserID).Scan(&role)
	if role == string(domain.RoleOwner) {
		return fmt.Errorf("workspace owner cannot be removed")
	}

	_, err := db.DB.ExecContext(ctx,
		`DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`,
		workspaceID, targetUserID)
	return err
}

// ──────────────────────────────────────────────────────────────────────────────
// Platform Connections
// ──────────────────────────────────────────────────────────────────────────────

// ListConnections returns all connected social platforms for a workspace.
func ListConnections(ctx context.Context, workspaceID, userID string) ([]domain.PlatformConnection, error) {
	if err := requireRole(ctx, workspaceID, userID, domain.RoleMember, domain.RoleAdmin, domain.RoleOwner); err != nil {
		return nil, err
	}

	rows, err := db.DB.QueryContext(ctx, `
		SELECT id, workspace_id, platform, account_id, account_name, expires_at, created_at
		FROM platform_connections WHERE workspace_id = $1
		ORDER BY created_at DESC
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conns []domain.PlatformConnection
	for rows.Next() {
		var c domain.PlatformConnection
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.Platform, &c.AccountID, &c.AccountName, &c.ExpiresAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		conns = append(conns, c)
	}
	return conns, rows.Err()
}

// SaveConnection upserts a platform connection for a workspace.
func SaveConnection(ctx context.Context, conn *domain.PlatformConnection) error {
	conn.ID = uuid.New()
	_, err := db.DB.ExecContext(ctx, `
		INSERT INTO platform_connections
		    (id, workspace_id, platform, account_id, account_name, access_token, refresh_token, expires_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW())
		ON CONFLICT (workspace_id, platform, account_id)
		DO UPDATE SET
		    access_token  = EXCLUDED.access_token,
		    refresh_token = EXCLUDED.refresh_token,
		    account_name  = EXCLUDED.account_name,
		    expires_at    = EXCLUDED.expires_at
	`, conn.ID, conn.WorkspaceID, string(conn.Platform), conn.AccountID, conn.AccountName,
		conn.AccessToken, conn.RefreshToken, conn.ExpiresAt)
	return err
}

// DisconnectPlatform removes a platform connection.
func DisconnectPlatform(ctx context.Context, workspaceID, connectionID, callerID string) error {
	if err := requireRole(ctx, workspaceID, callerID, domain.RoleAdmin, domain.RoleOwner); err != nil {
		return err
	}
	_, err := db.DB.ExecContext(ctx,
		`DELETE FROM platform_connections WHERE id=$1 AND workspace_id=$2`,
		connectionID, workspaceID)
	return err
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

func requireRole(ctx context.Context, workspaceID, userID string, allowed ...domain.WorkspaceRole) error {
	var role string
	err := db.DB.QueryRowContext(ctx,
		`SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`,
		workspaceID, userID).Scan(&role)
	if err == sql.ErrNoRows {
		return fmt.Errorf("access denied: not a member of this workspace")
	}
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	for _, a := range allowed {
		if domain.WorkspaceRole(role) == a {
			return nil
		}
	}
	return fmt.Errorf("access denied: insufficient role (%s)", role)
}

func generateSlug(name string, id uuid.UUID) string {
	short := id.String()[:8]
	slug := ""
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			slug += string(ch)
		} else if ch >= 'A' && ch <= 'Z' {
			slug += string(ch + 32)
		} else if ch == ' ' || ch == '-' {
			slug += "-"
		}
	}
	if slug == "" {
		slug = "workspace"
	}
	return slug + "-" + short
}
