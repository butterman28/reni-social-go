//go:build integration

// Run explicitly with:
//
//	go test -tags=integration ./pkg/meta/... -run TestRefresher_Live -v
//
// if that dont work
// try this heavy duty
// "export DBURL="postgres://reni:reni@127.0.0.1:5432/reni_social_dev?sslmode=disable"
// go test -tags=integration ./pkg/meta/... -run TestRefresher_Live -v"
//
// Requires:
//   - A reachable Postgres with a meta_connections row expiring within
//     RefreshBeforeDays days, holding a real (soon-to-expire) Instagram
//     token.
//   - Live network egress to graph.instagram.com.
//
// This test mutates real rows and calls a real third-party API. It is
// deliberately excluded from `go test ./...` and from CI via the
// `integration` build tag — run it manually, on a schedule, or in a
// dedicated pipeline stage with its own credentials.
package meta

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"reni-social/pkg/config"
	"reni-social/pkg/db"
)

// postgresRepo connects directly to PostgreSQL to read/write real connection tokens.
type postgresRepo struct {
	db *sql.DB
}

func (r *postgresRepo) GetTokensNeedingRefresh(ctx context.Context, thresholdDays int, limit int) ([]MetaConnection, error) {
	query := `
    SELECT id, access_token, expires_at
    FROM meta_connections
    WHERE is_deleted = false
      AND platform = 'meta_ig'
      AND expires_at IS NOT NULL
      AND expires_at <= NOW() + ($1 || ' days')::INTERVAL
    LIMIT $2
	`
	rows, err := r.db.QueryContext(ctx, query, thresholdDays, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []MetaConnection
	for rows.Next() {
		var conn MetaConnection
		if err := rows.Scan(&conn.ID, &conn.AccessToken, &conn.ExpiresAt); err != nil {
			return nil, err
		}
		list = append(list, conn)
	}
	return list, rows.Err()
}

func (r *postgresRepo) UpdateToken(ctx context.Context, id string, newToken string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE meta_connections SET access_token = $1, expires_at = $2 WHERE id = $3`,
		newToken, expiresAt, id)
	return err
}

func (r *postgresRepo) MarkConnectionNeedsReauth(ctx context.Context, id string, reason string) error {
	fmt.Printf("[DB] would flag connection %s for reauth: %s\n", id, reason)
	return nil
}

func printDBTable(title string, records []MetaConnection) {
	fmt.Printf("\n--- %s ---\n", title)
	if len(records) == 0 {
		fmt.Println("(none)")
		return
	}
	for _, c := range records {
		tok := c.AccessToken
		if len(tok) > 20 {
			tok = tok[:10] + "..." + tok[len(tok)-8:]
		}
		fmt.Printf("  id=%s token=%s expires=%s (%v remaining)\n",
			c.ID, tok, c.ExpiresAt.Format(time.RFC3339), time.Until(c.ExpiresAt).Round(time.Minute))
	}
}

func TestRefresher_Live(t *testing.T) {
	if db.DB == nil {
		if config.C != nil && config.C.DBURL != "" {
			db.Connect()
		}
	}
	if db.DB == nil {
		t.Skip("no DB connection available — set DBURL and ensure config loads before running")
	}
	if err := db.DB.PingContext(context.Background()); err != nil {
		t.Skipf("DB unreachable: %v", err)
	}

	repo := &postgresRepo{db: db.DB}
	ctx := context.Background()

	before, err := repo.GetTokensNeedingRefresh(ctx, 15, 10)
	if err != nil {
		t.Fatalf("query before-state: %v", err)
	}
	if len(before) == 0 {
		t.Skip("no meta_connections rows within refresh threshold — nothing to exercise")
	}
	printDBTable("BEFORE", before)

	beforeByID := make(map[string]MetaConnection, len(before))
	for _, c := range before {
		beforeByID[c.ID] = c
	}

	r := NewRefresher(Config{
		CheckInterval:     time.Hour,
		RefreshBeforeDays: 15,
		BatchSize:         10,
		MaxConcurrency:    2,
		RequestTimeout:    10 * time.Second,
	}, repo)

	t.Log("calling live refresh against graph.instagram.com...")
	r.ProcessBatch(ctx)

	after, err := repo.GetTokensNeedingRefresh(ctx, 15, 10)
	if err != nil {
		t.Fatalf("query after-state: %v", err)
	}
	printDBTable("AFTER (still needing refresh)", after)

	// Meaningful assertion: for every connection that dropped out of the
	// "needs refresh" set, confirm its stored access_token actually
	// changed — not just that the row count shrank (which could happen for
	// unrelated reasons, e.g. a row hitting expires_at mid-test).
	afterByID := make(map[string]MetaConnection, len(after))
	for _, c := range after {
		afterByID[c.ID] = c
	}

	refreshed := 0
	for id, prev := range beforeByID {
		if _, stillPending := afterByID[id]; stillPending {
			continue
		}
		var cur MetaConnection
		err := db.DB.QueryRowContext(ctx,
			`SELECT id, access_token, expires_at FROM meta_connections WHERE id = $1`, id,
		).Scan(&cur.ID, &cur.AccessToken, &cur.ExpiresAt)
		if err != nil {
			t.Errorf("connection %s vanished from the table entirely: %v", id, err)
			continue
		}
		if cur.AccessToken == prev.AccessToken {
			t.Errorf("connection %s left the refresh window but access_token is unchanged", id)
			continue
		}
		if !cur.ExpiresAt.After(prev.ExpiresAt) {
			t.Errorf("connection %s expires_at did not move forward (%v -> %v)", id, prev.ExpiresAt, cur.ExpiresAt)
			continue
		}
		refreshed++
	}

	if refreshed == 0 {
		t.Fatal("no connection was verifiably refreshed — check logs for reauth flags or transient failures above")
	}
	t.Logf("verified %d connection(s) genuinely refreshed", refreshed)
}

func (r *postgresRepo) ReleaseClaim(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE meta_connections SET refresh_claimed_at = NULL WHERE id = $1`, id)
	return err
}