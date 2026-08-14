package linkedin

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────
// mockRepo — in-memory Repository, concurrency-safe (matches pkg/meta style)
// ──────────────────────────────────────────────────────────────────────────

type mockRepo struct {
	mu        sync.Mutex
	conns     []Connection
	getErr    error
	markErrID string // MarkNeedsReauth fails for this connection ID
	markCalls []markCall
}

type markCall struct {
	id     string
	reason string
}

func (m *mockRepo) GetConnectionsNeedingReauth(ctx context.Context, thresholdDays, limit int) ([]Connection, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Connection, len(m.conns))
	copy(out, m.conns)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *mockRepo) MarkNeedsReauth(ctx context.Context, id, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.markErrID == id {
		return errors.New("simulated db failure")
	}
	m.markCalls = append(m.markCalls, markCall{id, reason})
	return nil
}

func (m *mockRepo) countMarks() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.markCalls)
}

// captureNotifier records notification calls.
type captureNotifier struct {
	mu    sync.Mutex
	calls []notifyCall
	err   error
}

type notifyCall struct {
	conn   Connection
	reason string
}

func (n *captureNotifier) NotifyReauthRequired(ctx context.Context, conn Connection, reason string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls = append(n.calls, notifyCall{conn, reason})
	return n.err
}

func (n *captureNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.calls)
}

func testConfig() Config {
	return Config{CheckInterval: time.Hour, WarnBeforeDays: 7, BatchSize: 50}
}

// ──────────────────────────────────────────────────────────────────────────
// reauthReason — pure function
// ──────────────────────────────────────────────────────────────────────────

func TestReauthReason_Expired(t *testing.T) {
	conn := Connection{ID: "c1", ExpiresAt: time.Now().Add(-24 * time.Hour)}
	reason := reauthReason(conn)
	if !strings.Contains(reason, "expired on") {
		t.Errorf("expected 'expired on' in reason, got: %s", reason)
	}
}

func TestReauthReason_ExpiringSoon(t *testing.T) {
	conn := Connection{ID: "c1", ExpiresAt: time.Now().Add(3 * 24 * time.Hour)}
	reason := reauthReason(conn)
	if strings.Contains(reason, "expired") {
		t.Errorf("expiring-soon reason must not say 'expired', got: %s", reason)
	}
	if !strings.Contains(reason, "expires on") {
		t.Errorf("expected 'expires on' in reason, got: %s", reason)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// Happy path: flag + notify
// ──────────────────────────────────────────────────────────────────────────

func TestProcessBatch_FlagsAndNotifies(t *testing.T) {
	repo := &mockRepo{conns: []Connection{
		{ID: "conn-1", WorkspaceID: "ws-1", AccountName: "Ada Lovelace",
			OwnerEmail: "owner@example.com", ExpiresAt: time.Now().Add(-time.Hour)},
	}}
	notifier := &captureNotifier{}
	m := NewMonitor(testConfig(), repo, notifier)

	m.ProcessBatch(context.Background())

	if repo.countMarks() != 1 {
		t.Fatalf("expected 1 mark call, got %d", repo.countMarks())
	}
	if repo.markCalls[0].id != "conn-1" {
		t.Errorf("unexpected mark call: %+v", repo.markCalls[0])
	}
	if !strings.Contains(repo.markCalls[0].reason, "expired") {
		t.Errorf("expected expired reason, got: %s", repo.markCalls[0].reason)
	}
	if notifier.count() != 1 {
		t.Fatalf("expected 1 notification, got %d", notifier.count())
	}
}

func TestProcessBatch_NoConnections(t *testing.T) {
	repo := &mockRepo{}
	notifier := &captureNotifier{}
	m := NewMonitor(testConfig(), repo, notifier)

	m.ProcessBatch(context.Background())

	if repo.countMarks() != 0 || notifier.count() != 0 {
		t.Errorf("expected no marks/notifications for an empty batch")
	}
}

func TestProcessBatch_GetError(t *testing.T) {
	repo := &mockRepo{getErr: errors.New("db down")}
	notifier := &captureNotifier{}
	m := NewMonitor(testConfig(), repo, notifier)

	// Must not panic — log and return.
	m.ProcessBatch(context.Background())

	if repo.countMarks() != 0 || notifier.count() != 0 {
		t.Errorf("expected no processing on repo error")
	}
}

func TestProcessBatch_MarkErrorContinuesBatch(t *testing.T) {
	repo := &mockRepo{
		conns: []Connection{
			{ID: "conn-bad", OwnerEmail: "a@example.com", ExpiresAt: time.Now().Add(-time.Hour)},
			{ID: "conn-good", OwnerEmail: "b@example.com", ExpiresAt: time.Now().Add(time.Hour)},
		},
		markErrID: "conn-bad",
	}
	notifier := &captureNotifier{}
	m := NewMonitor(testConfig(), repo, notifier)

	m.ProcessBatch(context.Background())

	if repo.countMarks() != 1 {
		t.Fatalf("expected the healthy row to still be marked, got %d marks", repo.countMarks())
	}
	if repo.markCalls[0].id != "conn-good" {
		t.Errorf("expected conn-good to be marked, got %s", repo.markCalls[0].id)
	}
	// conn-bad failed before notification; conn-good notified once.
	if notifier.count() != 1 {
		t.Errorf("expected 1 notification, got %d", notifier.count())
	}
}

func TestProcessBatch_NotifyErrorStillFlags(t *testing.T) {
	repo := &mockRepo{conns: []Connection{
		{ID: "conn-1", OwnerEmail: "owner@example.com", ExpiresAt: time.Now().Add(time.Hour)},
	}}
	notifier := &captureNotifier{err: errors.New("email queue down")}
	m := NewMonitor(testConfig(), repo, notifier)

	m.ProcessBatch(context.Background())

	if repo.countMarks() != 1 {
		t.Fatalf("flagging must succeed even when notification fails, got %d marks", repo.countMarks())
	}
	if notifier.count() != 1 {
		t.Errorf("expected the notification attempt, got %d", notifier.count())
	}
}

func TestProcessBatch_NilNotifier(t *testing.T) {
	repo := &mockRepo{conns: []Connection{
		{ID: "conn-1", ExpiresAt: time.Now().Add(-time.Hour)},
	}}
	m := NewMonitor(testConfig(), repo, nil)

	// Must not panic on a nil notifier.
	m.ProcessBatch(context.Background())

	if repo.countMarks() != 1 {
		t.Fatalf("expected 1 mark call with nil notifier, got %d", repo.countMarks())
	}
}

func TestNewMonitor_AppliesDefaults(t *testing.T) {
	m := NewMonitor(Config{}, &mockRepo{}, nil)
	if m.cfg.CheckInterval != 6*time.Hour {
		t.Errorf("expected default CheckInterval 6h, got %v", m.cfg.CheckInterval)
	}
	if m.cfg.WarnBeforeDays != 7 {
		t.Errorf("expected default WarnBeforeDays 7, got %d", m.cfg.WarnBeforeDays)
	}
	if m.cfg.BatchSize != 50 {
		t.Errorf("expected default BatchSize 50, got %d", m.cfg.BatchSize)
	}
}