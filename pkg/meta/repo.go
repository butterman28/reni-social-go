package meta

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type PostgresRepo struct {
	db *sql.DB
}

func NewPostgresRepo(db *sql.DB) *PostgresRepo {
	return &PostgresRepo{db: db}
}

// GetTokensNeedingRefresh atomically claims a batch of rows so no two workers
// (or a crash/restart) can refresh the same token. It reserves rows with
// FOR UPDATE SKIP LOCKED, stamps refresh_claimed_at, and returns the claimed
// rows. Mirrors the CTE pattern already proven in internal/scheduler/engine.go.
//
// The lease TTL ('10 minutes') must comfortably exceed the per-token task
// timeout (45s) plus retry/backoff, so a live refresh is never stolen. A
// crashed worker's claim goes stale and becomes reclaimable after that window.
func (r *PostgresRepo) GetTokensNeedingRefresh(ctx context.Context, thresholdDays int, limit int) ([]MetaConnection, error) {
	query := `
WITH due AS (
    SELECT id
    FROM meta_connections
    WHERE is_deleted   = false
      AND platform     = 'meta_ig'                       -- ← FIX: only IG Login tokens use ig_refresh_token
      AND needs_reauth = false
      AND expires_at IS NOT NULL
      AND expires_at <= NOW() + ($1 || ' days')::INTERVAL
      AND (refresh_claimed_at IS NULL OR refresh_claimed_at < NOW() - INTERVAL '10 minutes')
    ORDER BY expires_at ASC
    LIMIT $2
    FOR UPDATE SKIP LOCKED
),
claimed AS (
    UPDATE meta_connections mc
    SET refresh_claimed_at = NOW()
    FROM due
    WHERE mc.id = due.id
    RETURNING mc.id
)
SELECT
    mc.id::text,
    mc.workspace_id::text,
    COALESCE(mc.account_name, ''),
    COALESCE(w.slug, ''),
    COALESCE(u.email, ''),
    mc.access_token,
    mc.expires_at
FROM claimed c
JOIN meta_connections mc ON mc.id = c.id
JOIN workspaces w        ON w.id = mc.workspace_id
LEFT JOIN users u        ON u.id = w.owner_id
ORDER BY mc.expires_at ASC
`
	rows, err := r.db.QueryContext(ctx, query, thresholdDays, limit)
	if err != nil {
		return nil, fmt.Errorf("claim tokens needing refresh: %w", err)
	}
	defer rows.Close()

	var list []MetaConnection
	for rows.Next() {
		var conn MetaConnection
		if err := rows.Scan(&conn.ID, &conn.WorkspaceID, &conn.AccountName, &conn.WorkspaceSlug, &conn.OwnerEmail, &conn.AccessToken, &conn.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan token: %w", err)
		}
		list = append(list, conn)
	}
	return list, rows.Err()
}

// UpdateToken persists T′ AND releases the lease.
func (r *PostgresRepo) UpdateToken(ctx context.Context, id string, newToken string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE meta_connections
		 SET access_token = $1, expires_at = $2, refresh_claimed_at = NULL
		 WHERE id = $3`,
		newToken, expiresAt, id)
	return err
}

// MarkConnectionNeedsReauth flags the row AND releases the lease.
func (r *PostgresRepo) MarkConnectionNeedsReauth(ctx context.Context, id string, reason string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE meta_connections
		 SET needs_reauth = true, reauth_reason = $1, refresh_claimed_at = NULL
		 WHERE id = $2`,
		reason, id)
	return err
}

// ReleaseClaim drops the lease without touching token state, so the row is
// eligible again on the next cycle (used when retries are exhausted but the
// failure was transient, not a permanent auth error).
func (r *PostgresRepo) ReleaseClaim(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE meta_connections SET refresh_claimed_at = NULL WHERE id = $1`, id)
	return err
}