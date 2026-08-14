package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────
// mockRepo — in-memory TokenRepository, safe for concurrent use since
// ProcessBatch fans out across goroutines.
// ──────────────────────────────────────────────────────────────────────────

type mockRepo struct {
	mu sync.Mutex

	tokens []MetaConnection

	getErr error

	updateCalls []updateCall
	reauthCalls []reauthCall
}

type updateCall struct {
	id        string
	newToken  string
	expiresAt time.Time
}

type reauthCall struct {
	id     string
	reason string
}

func (m *mockRepo) GetTokensNeedingRefresh(ctx context.Context, thresholdDays, limit int) ([]MetaConnection, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MetaConnection, len(m.tokens))
	copy(out, m.tokens)
	return out, nil
}

func (m *mockRepo) UpdateToken(ctx context.Context, id, newToken string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCalls = append(m.updateCalls, updateCall{id, newToken, expiresAt})
	return nil
}

func (m *mockRepo) MarkConnectionNeedsReauth(ctx context.Context, id, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reauthCalls = append(m.reauthCalls, reauthCall{id, reason})
	return nil
}

func (m *mockRepo) countUpdates() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.updateCalls)
}

func (m *mockRepo) countReauths() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.reauthCalls)
}

func testConfig() Config {
	return Config{
		CheckInterval:     time.Hour,
		RefreshBeforeDays: 15,
		BatchSize:         10,
		MaxConcurrency:    5,
		RequestTimeout:    5 * time.Second,
		// Keep retry-path tests fast and deterministic — with the default
		// 1s production base, a 2-retry test would burn 3+ real seconds.
		BackoffBase: 5 * time.Millisecond,
	}
}

func withFakeMetaServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	old := instagramRefreshBaseURL
	instagramRefreshBaseURL = srv.URL
	t.Cleanup(func() { instagramRefreshBaseURL = old })
	return srv
}

// ──────────────────────────────────────────────────────────────────────────
// isPermanentAuthError — pure function, table-driven
// ──────────────────────────────────────────────────────────────────────────

func TestIsPermanentAuthError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"invalid token 190", &metaError{Code: 190}, true},
		{"session expired 102", &metaError{Code: 102}, true},
		{"rate limited", &metaError{Code: 4}, false},
		{"unknown wrapped meta error", fmt.Errorf("wrap: %w", &metaError{Code: 190}), true},
		{"generic network error", fmt.Errorf("dial tcp: timeout"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isPermanentAuthError(c.err); got != c.want {
				t.Errorf("isPermanentAuthError(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────
// Happy path: token refreshed on first attempt, repo updated once.
// ──────────────────────────────────────────────────────────────────────────

func TestProcessBatch_SuccessUpdatesRepo(t *testing.T) {
	withFakeMetaServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(metaRefreshResponse{
			AccessToken: "new-token-abc",
			TokenType:   "bearer",
			ExpiresIn:   5184000, // 60 days
		})
	})

	repo := &mockRepo{tokens: []MetaConnection{
		{ID: "conn-1", AccessToken: "old-token", ExpiresAt: time.Now().Add(5 * 24 * time.Hour)},
	}}

	r := NewRefresher(testConfig(), repo)
	r.ProcessBatch(context.Background())

	if repo.countUpdates() != 1 {
		t.Fatalf("expected 1 UpdateToken call, got %d", repo.countUpdates())
	}
	got := repo.updateCalls[0]
	if got.id != "conn-1" || got.newToken != "new-token-abc" {
		t.Errorf("unexpected update call: %+v", got)
	}
	if repo.countReauths() != 0 {
		t.Errorf("expected no reauth calls, got %d", repo.countReauths())
	}
}

// ──────────────────────────────────────────────────────────────────────────
// Permanent auth error (code 190) — must NOT retry, must flag for reauth.
// ──────────────────────────────────────────────────────────────────────────

func TestProcessBatch_PermanentErrorSkipsRetryAndFlagsReauth(t *testing.T) {
	var callCount int32

	withFakeMetaServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		json.NewEncoder(w).Encode(metaRefreshResponse{
			Error: &metaError{Message: "Invalid OAuth access token", Code: 190},
		})
	})

	repo := &mockRepo{tokens: []MetaConnection{
		{ID: "conn-revoked", AccessToken: "revoked-token", ExpiresAt: time.Now().Add(time.Hour)},
	}}

	r := NewRefresher(testConfig(), repo)
	r.ProcessBatch(context.Background())

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("expected exactly 1 API call (no retries on permanent error), got %d", callCount)
	}
	if repo.countReauths() != 1 {
		t.Fatalf("expected 1 reauth flag, got %d", repo.countReauths())
	}
	if repo.countUpdates() != 0 {
		t.Errorf("expected no token update on permanent error, got %d", repo.countUpdates())
	}
}

// ──────────────────────────────────────────────────────────────────────────
// Transient error twice, then success — must retry and eventually succeed.
// ──────────────────────────────────────────────────────────────────────────

func TestRefreshWithRetry_TransientThenSucceeds(t *testing.T) {
	var callCount int32

	withFakeMetaServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		if n < 3 {
			// transient: not a metaError at all -> not permanent -> should retry
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`not json`))
			return
		}
		json.NewEncoder(w).Encode(metaRefreshResponse{
			AccessToken: "recovered-token",
			ExpiresIn:   5184000,
		})
	})

	repo := &mockRepo{}
	cfg := testConfig()
	r := NewRefresher(cfg, repo)

	conn := MetaConnection{ID: "conn-flaky", AccessToken: "old", ExpiresAt: time.Now().Add(time.Hour)}
	err := r.refreshWithRetry(context.Background(), conn)
	if err != nil {
		t.Fatalf("expected eventual success, got error: %v", err)
	}
	if callCount != 3 {
		t.Errorf("expected 3 attempts (2 failures + 1 success), got %d", callCount)
	}
	if repo.countUpdates() != 1 {
		t.Errorf("expected 1 successful update, got %d", repo.countUpdates())
	}
}

// ──────────────────────────────────────────────────────────────────────────
// Always transient-failing — must exhaust retries and return an error,
// without ever flagging reauth (that's only for permanent errors).
// ──────────────────────────────────────────────────────────────────────────

func TestRefreshWithRetry_ExhaustsRetries(t *testing.T) {
	var callCount int32

	withFakeMetaServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"temporary outage","code":2}}`))
	})

	repo := &mockRepo{}
	r := NewRefresher(testConfig(), repo)

	conn := MetaConnection{ID: "conn-down", AccessToken: "old", ExpiresAt: time.Now().Add(time.Hour)}
	err := r.refreshWithRetry(context.Background(), conn)
	if err == nil {
		t.Fatal("expected error after exhausting retries, got nil")
	}
	if callCount != 3 {
		t.Errorf("expected exactly 3 attempts (maxAttempts), got %d", callCount)
	}
	if repo.countReauths() != 0 {
		t.Errorf("code 2 is not a permanent error — should not flag reauth, got %d calls", repo.countReauths())
	}
}

// ──────────────────────────────────────────────────────────────────────────
// Context cancellation mid-retry should abort promptly, not sleep through
// the remaining backoff.
// ──────────────────────────────────────────────────────────────────────────

func TestRefreshWithRetry_ContextCanceledStopsRetrying(t *testing.T) {
	withFakeMetaServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	repo := &mockRepo{}
	r := NewRefresher(testConfig(), repo)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	conn := MetaConnection{ID: "conn-x", AccessToken: "old"}
	err := r.refreshWithRetry(ctx, conn)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error after context cancellation")
	}
	// Should bail out fast, not sit through a ~3s+ backoff schedule.
	if elapsed > 2*time.Second {
		t.Errorf("refreshWithRetry took %v after cancellation — not aborting promptly", elapsed)
	}
}

// ──────────────────────────────────────────────────────────────────────────
// Concurrency cap: MaxConcurrency must actually bound simultaneous calls.
// ──────────────────────────────────────────────────────────────────────────

func TestProcessBatch_RespectsMaxConcurrency(t *testing.T) {
	const maxConcurrency = 3
	var inFlight, peak int32

	withFakeMetaServer(t, func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&inFlight, 1)
		defer atomic.AddInt32(&inFlight, -1)

		for {
			p := atomic.LoadInt32(&peak)
			if cur <= p || atomic.CompareAndSwapInt32(&peak, p, cur) {
				break
			}
		}

		time.Sleep(50 * time.Millisecond) // hold the slot so overlap is observable
		json.NewEncoder(w).Encode(metaRefreshResponse{
			AccessToken: "tok",
			ExpiresIn:   5184000,
		})
	})

	tokens := make([]MetaConnection, 12)
	for i := range tokens {
		tokens[i] = MetaConnection{
			ID:          fmt.Sprintf("conn-%d", i),
			AccessToken: "old",
			ExpiresAt:   time.Now().Add(time.Hour),
		}
	}
	repo := &mockRepo{tokens: tokens}

	cfg := testConfig()
	cfg.MaxConcurrency = maxConcurrency
	cfg.BatchSize = len(tokens)

	r := NewRefresher(cfg, repo)
	r.ProcessBatch(context.Background())

	if peak > maxConcurrency {
		t.Errorf("observed %d concurrent calls, want <= %d (semaphore not enforced)", peak, maxConcurrency)
	}
	if repo.countUpdates() != len(tokens) {
		t.Errorf("expected all %d tokens refreshed, got %d", len(tokens), repo.countUpdates())
	}
}

// ──────────────────────────────────────────────────────────────────────────
// Panic isolation: one goroutine panicking must not stop the batch or
// crash the process.
// ──────────────────────────────────────────────────────────────────────────

type panicOnFirstUpdateRepo struct {
	mockRepo
	panicked int32
}

func (p *panicOnFirstUpdateRepo) UpdateToken(ctx context.Context, id, newToken string, expiresAt time.Time) error {
	if atomic.CompareAndSwapInt32(&p.panicked, 0, 1) {
		panic("simulated repo panic")
	}
	return p.mockRepo.UpdateToken(ctx, id, newToken, expiresAt)
}

func TestProcessBatch_PanicInOneGoroutineDoesNotStopBatch(t *testing.T) {
	withFakeMetaServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(metaRefreshResponse{
			AccessToken: "tok",
			ExpiresIn:   5184000,
		})
	})

	repo := &panicOnFirstUpdateRepo{}
	repo.tokens = []MetaConnection{
		{ID: "conn-panics", AccessToken: "old", ExpiresAt: time.Now().Add(time.Hour)},
		{ID: "conn-fine", AccessToken: "old", ExpiresAt: time.Now().Add(time.Hour)},
	}

	cfg := testConfig()
	r := NewRefresher(cfg, repo)

	done := make(chan struct{})
	go func() {
		r.ProcessBatch(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ProcessBatch did not return — panic likely took down a goroutine without recovering")
	}

	// The non-panicking connection should still have gone through.
	if repo.countUpdates() != 1 {
		t.Errorf("expected the surviving connection to still update, got %d updates", repo.countUpdates())
	}
}