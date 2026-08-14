package meta

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// PostgresRepo is the real, production implementation of TokenRepository.
type PostgresRepo struct {
	db *sql.DB
}

func NewPostgresRepo(db *sql.DB) *PostgresRepo {
	return &PostgresRepo{db: db}
}

func (r *PostgresRepo) GetTokensNeedingRefresh(ctx context.Context, thresholdDays int, limit int) ([]MetaConnection, error) {
	query := `
		SELECT id::text, access_token, expires_at
		FROM meta_connections
		WHERE is_deleted = false
		  AND needs_reauth = false
		  AND expires_at IS NOT NULL
		  AND expires_at <= NOW() + ($1 || ' days')::INTERVAL
		LIMIT $2
	`
	rows, err := r.db.QueryContext(ctx, query, thresholdDays, limit)
	if err != nil {
		return nil, fmt.Errorf("query tokens needing refresh: %w", err)
	}
	defer rows.Close()

	var list []MetaConnection
	for rows.Next() {
		var conn MetaConnection
		if err := rows.Scan(&conn.ID, &conn.AccessToken, &conn.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan token: %w", err)
		}
		list = append(list, conn)
	}
	return list, rows.Err()
}

func (r *PostgresRepo) UpdateToken(ctx context.Context, id string, newToken string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE meta_connections SET access_token = $1, expires_at = $2 WHERE id = $3`,
		newToken, expiresAt, id)
	return err
}

// MarkConnectionNeedsReauth flips the flag so the frontend knows to prompt the user.
func (r *PostgresRepo) MarkConnectionNeedsReauth(ctx context.Context, id string, reason string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE meta_connections SET needs_reauth = true, reauth_reason = $1 WHERE id = $2`,
		reason, id)
	return err
}