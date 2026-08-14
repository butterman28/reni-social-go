package linkedin

import (
	"context"
	"database/sql"
	"fmt"
)

// PostgresRepo is the production implementation of Repository.
type PostgresRepo struct {
	db *sql.DB
}

func NewPostgresRepo(db *sql.DB) *PostgresRepo {
	return &PostgresRepo{db: db}
}

func (r *PostgresRepo) GetConnectionsNeedingReauth(ctx context.Context, thresholdDays, limit int) ([]Connection, error) {
	query := `
SELECT
    lc.id::text,
    lc.workspace_id::text,
    COALESCE(lc.account_name, ''),
    COALESCE(w.slug, ''),
    lc.expires_at,
    COALESCE(u.email, '')
FROM linkedin_connections lc
JOIN workspaces w  ON w.id = lc.workspace_id
LEFT JOIN users u  ON u.id = w.owner_id
WHERE lc.is_deleted   = false
  AND lc.needs_reauth = false
  AND lc.expires_at IS NOT NULL
  AND lc.expires_at <= NOW() + ($1 || ' days')::INTERVAL
ORDER BY lc.expires_at ASC
LIMIT $2
`
	rows, err := r.db.QueryContext(ctx, query, thresholdDays, limit)
	if err != nil {
		return nil, fmt.Errorf("query linkedin connections needing reauth: %w", err)
	}
	defer rows.Close()

	var list []Connection
	for rows.Next() {
		var c Connection
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.AccountName, &c.WorkspaceSlug, &c.ExpiresAt, &c.OwnerEmail); err != nil {
			return nil, fmt.Errorf("scan linkedin connection: %w", err)
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

func (r *PostgresRepo) MarkNeedsReauth(ctx context.Context, id, reason string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE linkedin_connections SET needs_reauth = true, reauth_reason = $1 WHERE id = $2`,
		reason, id)
	return err
}