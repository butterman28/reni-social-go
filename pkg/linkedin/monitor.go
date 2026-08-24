// Package linkedin implements the expiry monitor for LinkedIn connections.
//
// The current LinkedIn integration authorizes with the scopes
// "openid profile email w_member_social" — deliberately WITHOUT
// "offline_access" — so LinkedIn never issues a refresh token and access
// tokens expire after ~60 days. There is nothing to refresh: the only
// recovery path is a full re-authorization through the OAuth consent screen.
//
// This monitor periodically scans linkedin_connections for tokens that are
// already expired or expiring soon, flags them with needs_reauth = true
// (surfaced to the frontend via the connections endpoint so it can render a
// "reconnect LinkedIn" banner), and notifies the workspace owner once per
// expiry cycle. When the user reconnects, SaveConnection resets the flag.
package linkedin

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Config defines the runtime configuration for the expiry monitor.
type Config struct {
	CheckInterval  time.Duration // how often to scan the DB (e.g. 6 hours)
	WarnBeforeDays int           // flag connections whose token expires within X days
	BatchSize      int           // max connections to process per scan
}

// Connection is a LinkedIn connection that needs re-authorization.
type Connection struct {
	ID            string
	WorkspaceID   string
	AccountName   string
	WorkspaceSlug string // for building the settings-page link in notifications
	OwnerEmail    string // workspace owner's email; "" if unavailable
	ExpiresAt     time.Time
}

// Repository abstracts the DB layer.
type Repository interface {
	// GetConnectionsNeedingReauth returns active (non-deleted, not yet
	// flagged) connections whose token expires within thresholdDays.
	// Already-expired tokens are included (expires_at is in the past).
	GetConnectionsNeedingReauth(ctx context.Context, thresholdDays, limit int) ([]Connection, error)
	// MarkNeedsReauth flags the connection so the UI prompts a reconnect.
	MarkNeedsReauth(ctx context.Context, id, reason string) error
}

// Notifier informs the user that re-authentication is required.
type Notifier interface {
	NotifyReauthRequired(ctx context.Context, conn Connection, reason string) error
}

// Monitor handles background expiry detection.
type Monitor struct {
	cfg      Config
	repo     Repository
	notifier Notifier // may be nil → flag only, no outbound notification
}

func NewMonitor(cfg Config, repo Repository, notifier Notifier) *Monitor {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 6 * time.Hour
	}
	if cfg.WarnBeforeDays <= 0 {
		cfg.WarnBeforeDays = 7
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	return &Monitor{cfg: cfg, repo: repo, notifier: notifier}
}

// Start runs the background service until the parent context is canceled.
func (m *Monitor) Start(ctx context.Context) {
	slog.Info("Starting LinkedIn expiry monitor",
		"check_interval", m.cfg.CheckInterval,
		"warn_before_days", m.cfg.WarnBeforeDays)
	ticker := time.NewTicker(m.cfg.CheckInterval)
	defer ticker.Stop()

	// Initial run on startup — catches anything already expired.
	m.ProcessBatch(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("Context canceled, stopping LinkedIn expiry monitor...")
			return
		case <-ticker.C:
			m.ProcessBatch(ctx)
		}
	}
}

// ProcessBatch scans for expiring/expired connections, flags each one, and
// notifies the owner. Flagging is idempotent per expiry cycle: once
// needs_reauth = true, the row drops out of the query until the user
// reconnects (SaveConnection resets the flag).
func (m *Monitor) ProcessBatch(ctx context.Context) {
	conns, err := m.repo.GetConnectionsNeedingReauth(ctx, m.cfg.WarnBeforeDays, m.cfg.BatchSize)
	if err != nil {
		slog.Error("Failed to fetch LinkedIn connections needing reauth", "err", err)
		return
	}
	if len(conns) == 0 {
		slog.Debug("No LinkedIn connections require re-authentication at this time")
		return
	}
	slog.Info("Flagging LinkedIn connections for re-authentication", "total", len(conns))

	for _, conn := range conns {
		reason := reauthReason(conn)

		if err := m.repo.MarkNeedsReauth(ctx, conn.ID, reason); err != nil {
			slog.Error("Failed to flag LinkedIn connection for reauth",
				"connection_id", conn.ID, "err", err)
			continue // one bad row must not kill the batch
		}
		slog.Info("LinkedIn connection flagged for re-authentication",
			"connection_id", conn.ID, "reason", reason)

		if m.notifier != nil {
			if err := m.notifier.NotifyReauthRequired(ctx, conn, reason); err != nil {
				// Non-fatal: the row is already flagged, so the UI still
				// prompts the user to reconnect even if the email fails.
				slog.Error("Failed to notify user about LinkedIn re-authentication",
					"connection_id", conn.ID, "err", err)
			}
		}
	}
}

// reauthReason builds the human-readable reason: distinct wording for
// "already expired" vs "expiring soon".
func reauthReason(conn Connection) string {
	if time.Now().After(conn.ExpiresAt) {
		return fmt.Sprintf(
			"LinkedIn access token expired on %s — reconnection required to continue publishing",
			conn.ExpiresAt.Format(time.RFC3339))
	}
	return fmt.Sprintf(
		"LinkedIn access token expires on %s — reconnect now to avoid publishing interruptions",
		conn.ExpiresAt.Format(time.RFC3339))
}