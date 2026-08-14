package workspace

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"reni-social/internal/domain"
	"reni-social/internal/types"
	"reni-social/pkg/db"
)

// ──────────────────────────────────────────────────────────────────────────────
// Request types
// ──────────────────────────────────────────────────────────────────────────────

type CreateWorkspaceRequest struct {
	Name string `json:"name"`
}

type InviteMemberRequest struct {
	Email string               `json:"email"`
	Role  domain.WorkspaceRole `json:"role"`
}

// ──────────────────────────────────────────────────────────────────────────────
// Workspace CRUD
// ──────────────────────────────────────────────────────────────────────────────

// CreateWorkspace creates a new workspace and adds the creator as owner.
// CreateWorkspace godoc
// @Summary CreateWorkspace
// @Description Auto-generated thorough documentation for CreateWorkspace
// @Tags workspace
// @Produce json
// @Param ownerID body string true "ownerID"
// @Param req body string true "req"
// @Success 200 {object} domain.Workspace "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
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
// GetWorkspace godoc
// @Summary GetWorkspace
// @Description Auto-generated thorough documentation for GetWorkspace
// @Tags workspace
// @Produce json
// @Param userID body string true "userID"
// @Success 200 {object} domain.Workspace "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
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

// GetWorkspaceBySlug fetches a workspace the user is a member of using its slug.
// GetWorkspaceBySlug godoc
// @Summary GetWorkspaceBySlug
// @Description Auto-generated thorough documentation for GetWorkspaceBySlug
// @Tags workspace
// @Produce json
// @Param userID body string true "userID"
// @Success 200 {object} domain.Workspace "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func GetWorkspaceBySlug(ctx context.Context, slug, userID string) (*domain.Workspace, error) {
	var ws domain.Workspace
	err := db.DB.QueryRowContext(ctx, `
		SELECT w.id, w.owner_id, w.name, w.slug, w.created_at
		FROM workspaces w
		JOIN workspace_members m ON m.workspace_id = w.id
		WHERE w.slug = $1 AND m.user_id = $2
	`, slug, userID).Scan(&ws.ID, &ws.OwnerID, &ws.Name, &ws.Slug, &ws.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("workspace not found or access denied")
	}
	if err != nil {
		return nil, fmt.Errorf("get workspace by slug: %w", err)
	}
	return &ws, nil
}

// ListMyWorkspaces returns all workspaces the user belongs to.
// ListMyWorkspaces godoc
// @Summary ListMyWorkspaces
// @Description Auto-generated thorough documentation for ListMyWorkspaces
// @Tags workspace
// @Produce json
// @Param userID body string true "userID"
// @Success 200 {array} domain.Workspace "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
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
// InviteMember godoc
// @Summary InviteMember
// @Description Auto-generated thorough documentation for InviteMember
// @Tags workspace
// @Produce json
// @Param callerID body string true "callerID"
// @Param req body string true "req"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
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

	// Check if the user is already a member
	var exists bool
	err = db.DB.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM workspace_members 
			WHERE workspace_id = $1 AND user_id = $2
		)
	`, workspaceID, targetID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if exists {
		return fmt.Errorf("user is already a member of this workspace")
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
// ListMembers godoc
// @Summary ListMembers
// @Description Auto-generated thorough documentation for ListMembers
// @Tags workspace
// @Produce json
// @Param callerID body string true "callerID"
// @Success 200 {array} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
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
// RemoveMember godoc
// @Summary RemoveMember
// @Description Auto-generated thorough documentation for RemoveMember
// @Tags workspace
// @Produce json
// @Param targetUserID body string true "targetUserID"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
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
// CheckAndReviveConnection checks if a non-expired connection exists.
// If it exists but is soft-deleted, it automatically revives it (sets is_deleted = false).
// Returns true if an active connection is available (skipping OAuth), false otherwise.
// CheckAndReviveConnection godoc
// @Summary CheckAndReviveConnection
// @Description Auto-generated thorough documentation for CheckAndReviveConnection
// @Tags workspace
// @Produce json
// @Param workspaceID body string true "workspaceID"
// @Param platform body string true "platform"
// @Success 200 {boolean} bool "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func CheckAndReviveConnection(ctx context.Context, workspaceID string, platform string) (bool, error) {
	var query, updateQuery string

	switch platform {
		case "linkedin":
			query = `SELECT is_deleted FROM linkedin_connections WHERE workspace_id = $1 AND expires_at > NOW() AND needs_reauth = false LIMIT 1`
			updateQuery = `UPDATE linkedin_connections SET is_deleted = false WHERE workspace_id = $1 AND expires_at > NOW() AND needs_reauth = false`
		case "meta":
			query = `SELECT is_deleted FROM meta_connections WHERE workspace_id = $1 AND expires_at > NOW() AND needs_reauth = false LIMIT 1`
			updateQuery = `UPDATE meta_connections SET is_deleted = false WHERE workspace_id = $1 AND expires_at > NOW() AND needs_reauth = false`
		case "x":
			// unchanged — OAuth 1.0a tokens never expire
			query = `SELECT is_deleted FROM x_connections WHERE workspace_id = $1 AND expires_at > NOW() LIMIT 1`
			updateQuery = `UPDATE x_connections SET is_deleted = false WHERE workspace_id = $1 AND expires_at > NOW()`
		default:
			return false, fmt.Errorf("unsupported platform: %s", platform)
		}
	var isDeleted bool
	err := db.DB.QueryRowContext(ctx, query, workspaceID).Scan(&isDeleted)
	
	if err == sql.ErrNoRows {
		// No connection exists, or it has expired. Must go through OAuth.
		return false, nil
	}
	if err != nil {
		return false, err
	}

	// Connection exists and is NOT expired.
	// If it was soft-deleted, revive it now!
	if isDeleted {
		_, err = db.DB.ExecContext(ctx, updateQuery, workspaceID)
		if err != nil {
			return false, fmt.Errorf("failed to revive connection: %w", err)
		}
	}

	// It's active (or just got revived). Skip OAuth.
	return true, nil
}
// ListConnections returns all connected social platforms for a workspace.
// ListConnections godoc
// @Summary ListConnections
// @Description Auto-generated thorough documentation for ListConnections
// @Tags workspace
// @Produce json
// @Param userID body string true "userID"
// @Success 200 {array} domain.PlatformConnection "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func ListConnections(ctx context.Context, workspaceID, userID string) ([]domain.PlatformConnection, error) {
	if err := requireRole(ctx, workspaceID, userID, domain.RoleMember, domain.RoleAdmin, domain.RoleOwner); err != nil {
		return nil, err
	}

	var conns []domain.PlatformConnection

		// Query LinkedIn (includes re-auth flag so the frontend can show a banner)
	rowsLI, err := db.DB.QueryContext(ctx, `
		SELECT id, workspace_id, account_id, account_name, expires_at, created_at,
		       is_deleted, needs_reauth, reauth_reason
		FROM linkedin_connections
		WHERE workspace_id = $1
	`, workspaceID)
	if err == nil {
		defer rowsLI.Close()
		for rowsLI.Next() {
			var c domain.PlatformConnection
			var reason sql.NullString
			if err := rowsLI.Scan(&c.ID, &c.WorkspaceID, &c.AccountID, &c.AccountName,
				&c.ExpiresAt, &c.CreatedAt, &c.IsDeleted, &c.NeedsReauth, &reason); err == nil {
				if reason.Valid {
					c.ReauthReason = reason.String
				}
				conns = append(conns, c)
			}
		}
	}

	// Query Meta
	rowsMeta, err := db.DB.QueryContext(ctx, `
		SELECT id, workspace_id, platform, account_id, account_name, expires_at, created_at,
		       is_deleted, needs_reauth, reauth_reason
		FROM meta_connections
		WHERE workspace_id = $1
	`, workspaceID)
	if err == nil {
		defer rowsMeta.Close()
		for rowsMeta.Next() {
			var c domain.PlatformConnection
			var platformStr string
			var reason sql.NullString
			if err := rowsMeta.Scan(&c.ID, &c.WorkspaceID, &platformStr, &c.AccountID, &c.AccountName,
				&c.ExpiresAt, &c.CreatedAt, &c.IsDeleted, &c.NeedsReauth, &reason); err == nil {
				c.Platform = domain.Platform(platformStr)
				if reason.Valid {
					c.ReauthReason = reason.String
				}
				conns = append(conns, c)
			}
		}
	}

	// Query X
	rowsX, err := db.DB.QueryContext(ctx, `
		SELECT id, workspace_id, platform, account_id, account_name, expires_at, created_at, is_deleted
		FROM x_connections 
		WHERE workspace_id = $1
	`, workspaceID)
	if err == nil {
		defer rowsX.Close()
		for rowsX.Next() {
			var c domain.PlatformConnection
			var platformStr string
			if err := rowsX.Scan(&c.ID, &c.WorkspaceID, &platformStr, &c.AccountID, &c.AccountName, &c.ExpiresAt, &c.CreatedAt, &c.IsDeleted); err == nil {
				c.Platform = domain.Platform(platformStr)
				conns = append(conns, c)
			}
		}
	}

	return conns, nil
}
// SaveConnection upserts a platform connection for a workspace.
// SaveConnection godoc
// @Summary SaveConnection
// @Description Auto-generated thorough documentation for SaveConnection
// @Tags workspace
// @Produce json
// @Param conn body object true "conn"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func SaveConnection(ctx context.Context, conn *domain.PlatformConnection) error {
		conn.ID = uuid.New()
		var err error
		switch conn.Platform {
		case domain.PlatformLinkedIn:
			_, err = db.DB.ExecContext(ctx, `
	INSERT INTO linkedin_connections
	(id, workspace_id, account_id, account_name, access_token, refresh_token, expires_at, created_at)
	VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())
	ON CONFLICT (workspace_id, account_id)
	DO UPDATE SET
    access_token  = EXCLUDED.access_token,
    refresh_token = EXCLUDED.refresh_token,
    account_name  = EXCLUDED.account_name,
    expires_at    = EXCLUDED.expires_at,
    is_deleted    = false,
    needs_reauth  = false,
    reauth_reason = NULL
	`, conn.ID, conn.WorkspaceID, conn.AccountID, conn.AccountName,
				conn.AccessToken, conn.RefreshToken, conn.ExpiresAt)

		case domain.PlatformMetaFB, domain.PlatformMetaIG:
			_, err = db.DB.ExecContext(ctx, `
	INSERT INTO meta_connections
	(id, workspace_id, platform, account_id, account_name, access_token, refresh_token, expires_at, created_at)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW())
	ON CONFLICT (workspace_id, account_id)
	DO UPDATE SET
    access_token  = EXCLUDED.access_token,
    refresh_token = EXCLUDED.refresh_token,
    account_name  = EXCLUDED.account_name,
    expires_at    = EXCLUDED.expires_at,
    is_deleted    = false,
    needs_reauth  = false,
    reauth_reason = NULL
	`, conn.ID, conn.WorkspaceID, string(conn.Platform), conn.AccountID, conn.AccountName,
				conn.AccessToken, conn.RefreshToken, conn.ExpiresAt)

		case domain.PlatformX:
			_, err = db.DB.ExecContext(ctx, `
	INSERT INTO x_connections
	(id, workspace_id, platform, account_id, account_name, access_token, refresh_token, expires_at, created_at)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW())
	ON CONFLICT (workspace_id, account_id)
	DO UPDATE SET
	access_token  = EXCLUDED.access_token,
	refresh_token = EXCLUDED.refresh_token,
	account_name  = EXCLUDED.account_name,
	expires_at    = EXCLUDED.expires_at,
	is_deleted    = false
	`, conn.ID, conn.WorkspaceID, string(conn.Platform), conn.AccountID, conn.AccountName,
				conn.AccessToken, conn.RefreshToken, conn.ExpiresAt)

		default:
			err = fmt.Errorf("unsupported platform: %s", conn.Platform)
		}
		return err
	}

// DisconnectPlatform removes a platform connection.
// DisconnectPlatform godoc
// @Summary DisconnectPlatform
// @Description Auto-generated thorough documentation for DisconnectPlatform
// @Tags workspace
// @Produce json
// @Param callerID body string true "callerID"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func DisconnectPlatform(ctx context.Context, workspaceID, connectionID, callerID string) error {
	if err := requireRole(ctx, workspaceID, callerID, domain.RoleAdmin, domain.RoleOwner); err != nil {
		return err
	}
	
	// Soft delete: set is_deleted = true instead of hard deleting
	_, _ = db.DB.ExecContext(ctx, `UPDATE linkedin_connections SET is_deleted = true WHERE id=$1 AND workspace_id=$2`, connectionID, workspaceID)
	_, _ = db.DB.ExecContext(ctx, `UPDATE meta_connections SET is_deleted = true WHERE id=$1 AND workspace_id=$2`, connectionID, workspaceID)
	_, _ = db.DB.ExecContext(ctx, `UPDATE x_connections SET is_deleted = true WHERE id=$1 AND workspace_id=$2`, connectionID, workspaceID)
	
	return nil
}
// GetXConnectionByID fetches a specific X (Twitter) connection for a workspace,
// scoped by both workspace_id and connection id so one workspace can't reach
// into another's connection by guessing a UUID.
// GetXConnectionByID godoc
// @Summary GetXConnectionByID
// @Description Auto-generated thorough documentation for GetXConnectionByID
// @Tags workspace
// @Produce json
// @Param connectionID body string true "connectionID"
// @Success 200 {object} domain.PlatformConnection "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func GetXConnectionByID(ctx context.Context, workspaceID, connectionID string) (*domain.PlatformConnection, error) {
	var c domain.PlatformConnection
	var platformStr string
	err := db.DB.QueryRowContext(ctx, `
		SELECT id, workspace_id, platform, account_id, account_name, access_token, refresh_token, expires_at, created_at
		FROM x_connections WHERE workspace_id = $1 AND id = $2
	`, workspaceID, connectionID).Scan(
		&c.ID, &c.WorkspaceID, &platformStr, &c.AccountID, &c.AccountName,
		&c.AccessToken, &c.RefreshToken, &c.ExpiresAt, &c.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("X connection not found for this workspace")
	}
	if err != nil {
		return nil, fmt.Errorf("get x connection: %w", err)
	}
	c.Platform = domain.Platform(platformStr)
	return &c, nil
}

// GetXConnection fetches an X (Twitter) connection for a workspace.
// If connectionID is empty, it falls back to the first connected account
// found (kept for backward compatibility with single-account workspaces).
// Prefer GetXConnectionByID once the caller knows which account it wants.
// GetXConnection godoc
// @Summary GetXConnection
// @Description Auto-generated thorough documentation for GetXConnection
// @Tags workspace
// @Produce json
// @Param connectionID body string true "connectionID"
// @Success 200 {object} domain.PlatformConnection "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func GetXConnection(ctx context.Context, workspaceID, connectionID string) (*domain.PlatformConnection, error) {
	if connectionID != "" {
		return GetXConnectionByID(ctx, workspaceID, connectionID)
	}

	var c domain.PlatformConnection
	var platformStr string
	err := db.DB.QueryRowContext(ctx, `
		SELECT id, workspace_id, platform, account_id, account_name, access_token, refresh_token, expires_at, created_at
		FROM x_connections WHERE workspace_id = $1
		ORDER BY created_at ASC
		LIMIT 1
	`, workspaceID).Scan(
		&c.ID, &c.WorkspaceID, &platformStr, &c.AccountID, &c.AccountName,
		&c.AccessToken, &c.RefreshToken, &c.ExpiresAt, &c.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no X connection found for this workspace")
	}
	if err != nil {
		return nil, fmt.Errorf("get x connection: %w", err)
	}
	c.Platform = domain.Platform(platformStr)
	return &c, nil
}

// ListXConnections handles GET /api/workspaces/in/:id/x/connections
// Returns all connected X accounts for the workspace (no access tokens).
// ListXConnections godoc
// @Summary GET /api/in/workspaces/in/:id/x/connections
// @Tags workspace
// @Produce json
// @Security BearerAuth
// @Security SessionId
// @Param id path string true "id"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/in/workspaces/in/{id}/x/connections [get]
func ListXConnections(c *fiber.Ctx) error {
	userID := c.Locals("userid").(string)
	wsID := c.Params("id")

	if _, err := GetWorkspace(c.Context(), wsID, userID); err != nil {
		return c.Status(fiber.StatusForbidden).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}

	rows, err := db.DB.QueryContext(c.Context(), `
		SELECT id, account_id, account_name, expires_at, created_at
		FROM x_connections WHERE workspace_id = $1
		ORDER BY created_at ASC
	`, wsID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(types.APIResponse[any]{
			Status: false, Message: err.Error(),
		})
	}
	defer rows.Close()

	var out []fiber.Map
	for rows.Next() {
		var id, accountID, accountName string
		var expiresAt *time.Time
		var createdAt time.Time
		if err := rows.Scan(&id, &accountID, &accountName, &expiresAt, &createdAt); err != nil {
			continue
		}
		out = append(out, fiber.Map{
			"id": id, "account_id": accountID, "account_name": accountName,
			"expires_at": expiresAt, "created_at": createdAt,
		})
	}

	return c.Status(fiber.StatusOK).JSON(types.APIResponse[any]{
		Status: true, Message: "OK", Data: out,
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

// requireRole godoc
// @Summary requireRole
// @Description Auto-generated thorough documentation for requireRole
// @Tags workspace
// @Produce json
// @Param userID body string true "userID"
// @Param allowed body string true "allowed"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
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

// generateSlug godoc
// @Summary generateSlug
// @Description Auto-generated thorough documentation for generateSlug
// @Tags workspace
// @Produce json
// @Param name body string true "name"
// @Param id body string true "id"
// @Success 200 {string} string "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
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
// ──────────────────────────────────────────────────────────────────────────────
// Re-auth alerts (tokenless — safe for global frontend polling)
// ──────────────────────────────────────────────────────────────────────────────

// ReauthAlert describes one platform connection that needs the user to
// re-authorize. Deliberately contains NO tokens — this endpoint is meant
// to be polled app-wide for banner display.
type ReauthAlert struct {
	Platform    string     `json:"platform"`     // "linkedin" | "meta_ig" | "meta_fb"
	AccountID   string     `json:"account_id"`
	AccountName string     `json:"account_name"`
	Reason      string     `json:"reason"`       // human-readable, from the monitor
	Expired     bool       `json:"expired"`      // true = already dead, false = expiring soon
	ExpiresAt   *time.Time `json:"expires_at"`   // for "expires in X days" countdown UI
}

// ListReauthAlerts returns every flagged connection for a workspace.
// Any member can read this (it's advisory), but only admin/owner can act
// on it — the frontend should vary the banner text by role.
// ListReauthAlerts godoc
// @Summary ListReauthAlerts
// @Description Auto-generated thorough documentation for ListReauthAlerts
// @Tags workspace
// @Produce json
// @Param userID body string true "userID"
// @Success 200 {array} ReauthAlert "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func ListReauthAlerts(ctx context.Context, workspaceID, userID string) ([]ReauthAlert, error) {
	if err := requireRole(ctx, workspaceID, userID, domain.RoleMember, domain.RoleAdmin, domain.RoleOwner); err != nil {
		return nil, err
	}

	rows, err := db.DB.QueryContext(ctx, `
		SELECT 'linkedin' AS platform,
		       account_id,
		       COALESCE(account_name, ''),
		       COALESCE(reauth_reason, ''),
		       expires_at,
		       COALESCE(expires_at <= NOW(), true) AS expired
		FROM linkedin_connections
		WHERE workspace_id = $1 AND is_deleted = false AND needs_reauth = true
		UNION ALL
		SELECT platform,
		       account_id,
		       COALESCE(account_name, ''),
		       COALESCE(reauth_reason, ''),
		       expires_at,
		       COALESCE(expires_at <= NOW(), true)
		FROM meta_connections
		WHERE workspace_id = $1 AND is_deleted = false AND needs_reauth = true
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list reauth alerts: %w", err)
	}
	defer rows.Close()

	alerts := []ReauthAlert{} // non-nil → serializes as [] not null
	for rows.Next() {
		var a ReauthAlert
		if err := rows.Scan(&a.Platform, &a.AccountID, &a.AccountName, &a.Reason, &a.ExpiresAt, &a.Expired); err != nil {
			return nil, fmt.Errorf("scan reauth alert: %w", err)
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}