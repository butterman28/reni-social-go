package meta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// instagramRefreshBaseURL is a package-level var (not a const) specifically
// so tests can point it at an httptest.Server, matching the pattern already
// used in internal/platform/linkedin, /meta, /tiktok (tokenURL, ugcPostURL,
// etc). Never mutate this outside of tests.
var instagramRefreshBaseURL = "https://graph.instagram.com/refresh_access_token"

var (
	errReauthRequired = errors.New("connection flagged for re-authentication")

	// errThrottled means Meta told us to slow down. The token is healthy —
	// retrying on a 1-2s backoff would only deepen the throttle, so the
	// claim is released and the token waits for the next cycle.
	errThrottled = errors.New("meta rate limit hit")

	// errCircuitOpen means the batch's circuit breaker tripped; no further
	// Meta calls this cycle.
	errCircuitOpen = errors.New("circuit breaker open — meta calls halted")
)

// Config defines the runtime configuration for the token worker.
type Config struct {
	CheckInterval     time.Duration
	RefreshBeforeDays int
	BatchSize         int
	MaxConcurrency    int
	RequestTimeout    time.Duration
	BackoffBase       time.Duration

	// CircuitBreakerThreshold is how many consecutive platform-level
	// failures (throttle codes, 5xx, network errors) are tolerated before
	// the rest of the batch stops calling Meta. 0 → 5.
	CircuitBreakerThreshold int
}

func (c Config) backoffBase() time.Duration {
	if c.BackoffBase > 0 {
		return c.BackoffBase
	}
	return 1 * time.Second
}

// MetaConnection represents the database record.
type MetaConnection struct {
	ID            string
	WorkspaceID   string
	AccountName   string
	WorkspaceSlug string
	OwnerEmail    string
	AccessToken   string
	ExpiresAt     time.Time
}

// TokenRepository abstracts the DB layer.
type TokenRepository interface {
	GetTokensNeedingRefresh(ctx context.Context, thresholdDays int, limit int) ([]MetaConnection, error)
	UpdateToken(ctx context.Context, id string, newToken string, expiresAt time.Time) error
	MarkConnectionNeedsReauth(ctx context.Context, id string, reason string) error
	ReleaseClaim(ctx context.Context, id string) error
}

// Notifier informs the workspace owner that re-authentication is required.
type Notifier interface {
	NotifyReauthRequired(ctx context.Context, conn MetaConnection, reason string) error
}

// Refresher handles background token updates.
type Refresher struct {
	cfg        Config
	repo       TokenRepository
	notifier   Notifier // may be nil → flag only, no outbound notification
	httpClient *http.Client
	breaker    *circuitBreaker
}

func NewRefresher(cfg Config, repo TokenRepository, notifier Notifier) *Refresher {
	if cfg.CircuitBreakerThreshold <= 0 {
		cfg.CircuitBreakerThreshold = 5
	}
	return &Refresher{
		cfg:      cfg,
		repo:     repo,
		notifier: notifier,
		httpClient: &http.Client{
			Timeout: cfg.RequestTimeout,
		},
		breaker: newCircuitBreaker(cfg.CircuitBreakerThreshold),
	}
}

// MetaAPI Responses
type metaRefreshResponse struct {
	AccessToken string     `json:"access_token"`
	TokenType   string     `json:"token_type"`
	ExpiresIn   int64      `json:"expires_in"`
	Error       *metaError `json:"error,omitempty"`
}

type metaError struct {
	Message      string `json:"message"`
	Type         string `json:"type"`
	Code         int    `json:"code"`
	ErrorSubcode int    `json:"error_subcode"`
}

func (e *metaError) Error() string {
	return fmt.Sprintf("meta api error: %s (code %d, subcode %d)", e.Message, e.Code, e.ErrorSubcode)
}

// ──────────────────────────────────────────────────────────────────────────
// Circuit breaker
// ──────────────────────────────────────────────────────────────────────────

// circuitBreaker is a batch-scoped fuse: once threshold consecutive calls to
// Meta have failed (throttled, 5xx, network), the rest of the batch stops
// calling Meta and releases its claims. It never re-closes within a batch —
// each new ProcessBatch starts fresh and probes again. Without this, a Meta
// outage or platform-wide throttle burns BatchSize × maxAttempts calls every
// cycle.
type circuitBreaker struct {
	threshold int32
	failures  atomic.Int32
	open      atomic.Bool
}

func newCircuitBreaker(threshold int) *circuitBreaker {
	if threshold <= 0 {
		threshold = 5
	}
	return &circuitBreaker{threshold: int32(threshold)}
}

// recordSuccess resets the consecutive-failure streak. It never re-closes an
// open breaker — once tripped, this batch stays halted.
func (cb *circuitBreaker) recordSuccess() {
	cb.failures.Store(0)
}

// recordFailure counts a platform-level failure and opens the breaker when
// the streak reaches the threshold.
func (cb *circuitBreaker) recordFailure() {
	if cb.failures.Add(1) >= cb.threshold {
		cb.open.Store(true)
	}
}

func (cb *circuitBreaker) isOpen() bool {
	return cb.open.Load()
}

// Start runs the background service until the parent context is canceled.
func (r *Refresher) Start(ctx context.Context) {
	slog.Info("Starting Meta background token refresher", "check_interval", r.cfg.CheckInterval)

	ticker := time.NewTicker(r.cfg.CheckInterval)
	defer ticker.Stop()

	r.ProcessBatch(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("Context canceled, stopping Meta token refresher...")
			return
		case <-ticker.C:
			r.ProcessBatch(ctx)
		}
	}
}

// ProcessBatch fetches eligible tokens and refreshes them asynchronously.
func (r *Refresher) ProcessBatch(ctx context.Context) {
	conns, err := r.repo.GetTokensNeedingRefresh(ctx, r.cfg.RefreshBeforeDays, r.cfg.BatchSize)
	if err != nil {
		slog.Error("Failed to fetch tokens for refresh batch", "err", err)
		return
	}

	if len(conns) == 0 {
		slog.Debug("No Meta tokens require refreshing at this time")
		return
	}

	slog.Info("Dispatched Meta token refresh batch", "total", len(conns))

	// Fresh fuse each cycle: this batch probes Meta's health, and if it
	// trips, the remainder is skipped and retried next cycle.
	r.breaker = newCircuitBreaker(r.cfg.CircuitBreakerThreshold)

	maxConcurrency := r.cfg.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for _, conn := range conns {
		// Breaker tripped: stop dispatching, release claims so the rows
		// are eligible again next cycle.
		if r.breaker.isOpen() {
			r.releaseClaim(conn.ID)
			continue
		}

		wg.Add(1)
		sem <- struct{}{}

		go func(c MetaConnection) {
			defer func() {
				<-sem
				wg.Done()
				if p := recover(); p != nil {
					slog.Error("Recovered from panic in token refresh worker",
						"panic", p, "connection_id", c.ID)
				}
			}()

			// Breaker may have tripped while we waited for a semaphore slot.
			if r.breaker.isOpen() {
				r.releaseClaim(c.ID)
				return
			}

			taskCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()

			err := r.refreshWithRetry(taskCtx, c)

			switch {
			case errors.Is(err, errReauthRequired):
				slog.Warn("Connection requires user re-authentication", "connection_id", c.ID)
			case errors.Is(err, errCircuitOpen):
				// Batch-level condition; logged once after wg.Wait.
				r.releaseClaim(c.ID)
			case errors.Is(err, errThrottled):
				r.releaseClaim(c.ID)
				slog.Warn("Rate limited; deferring token to next cycle", "connection_id", c.ID)
			case err != nil:
				r.releaseClaim(c.ID)
				slog.Error("Token refresh failed after retries", "connection_id", c.ID, "err", err)
			default:
				slog.Info("Successfully refreshed Meta token", "connection_id", c.ID)
			}
		}(conn)
	}

	wg.Wait()

	if r.breaker.isOpen() {
		slog.Error("Circuit breaker tripped — Meta looks down or rate-limited; batch halted early, remaining tokens deferred to next cycle")
	}
}

// refreshWithRetry executes the refresh call using exponential backoff + jitter.
func (r *Refresher) refreshWithRetry(ctx context.Context, conn MetaConnection) error {
	const maxAttempts = 3

	var lastErr error
	base := r.cfg.backoffBase()

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Another token tripped the breaker while we were backing off.
		if r.breaker.isOpen() {
			return errCircuitOpen
		}

		resp, err := r.callRefreshEndpoint(ctx, conn.AccessToken)
		if err == nil {
			r.breaker.recordSuccess()
			newExpiry := time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
			return r.persistNewToken(conn.ID, resp.AccessToken, newExpiry)
		}

		lastErr = err

		if isPermanentAuthError(err) {
			// Token-level verdict — says nothing about Meta's health, so
			// it deliberately does NOT count against the breaker.
			slog.Warn("Permanent auth error detected. Flagging connection for re-authentication",
				"connection_id", conn.ID, "err", err)
			if reauthErr := r.repo.MarkConnectionNeedsReauth(ctx, conn.ID, err.Error()); reauthErr != nil {
				return reauthErr
			}
			if r.notifier != nil {
				if notifyErr := r.notifier.NotifyReauthRequired(ctx, conn, err.Error()); notifyErr != nil {
					// Non-fatal: the row is already flagged, so the UI still
					// prompts the user even if the email fails.
					slog.Error("Failed to notify user about Meta re-authentication",
						"connection_id", conn.ID, "err", notifyErr)
				}
			}
			return errReauthRequired
		}

		// From here on it's a platform-level failure: count it.
		r.breaker.recordFailure()

		if isThrottleError(err) {
			// "Slow down" ≠ "broken token". Retrying in 1-2s is exactly
			// what amplifies throttling — release the claim and wait for
			// the next cycle instead. The 15-day refresh window has ample
			// slack; there is zero urgency on any individual token.
			slog.Warn("Meta rate limit hit; deferring token to next cycle",
				"connection_id", conn.ID, "err", err)
			return fmt.Errorf("%w: %v", errThrottled, err)
		}

		if r.breaker.isOpen() {
			return errCircuitOpen
		}

		if attempt < maxAttempts {
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * base
			jitterCapMs := int(base / time.Millisecond)
			if jitterCapMs < 1 {
				jitterCapMs = 1
			}
			jitter := time.Duration(rand.Intn(jitterCapMs)) * time.Millisecond

			sleepTime := backoff + jitter

			slog.Warn("Transient error refreshing token, retrying...",
				"connection_id", conn.ID,
				"attempt", attempt,
				"retry_in", sleepTime,
				"err", err,
			)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(sleepTime):
			}
		}
	}

	return fmt.Errorf("exceeded max retries (%d): %w", maxAttempts, lastErr)
}

func (r *Refresher) callRefreshEndpoint(ctx context.Context, currentToken string) (*metaRefreshResponse, error) {
	endpoint := fmt.Sprintf(
		"%s?grant_type=ig_refresh_token&access_token=%s",
		instagramRefreshBaseURL, currentToken,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	res, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error during request: %w", err)
	}
	defer res.Body.Close()

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed reading response body: %w", err)
	}

	var apiResp metaRefreshResponse
	if err := json.Unmarshal(bodyBytes, &apiResp); err != nil {
		// A 429 with no parseable error body must still classify as throttle.
		if res.StatusCode == http.StatusTooManyRequests {
			return nil, &metaError{Message: "rate limited", Type: "throttle", Code: 4}
		}
		return nil, fmt.Errorf("error decoding Meta response: %w", err)
	}

	if apiResp.Error != nil {
		return nil, apiResp.Error
	}

	if res.StatusCode == http.StatusTooManyRequests {
		return nil, &metaError{Message: "rate limited", Type: "throttle", Code: 4}
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status code %d: %s", res.StatusCode, string(bodyBytes))
	}

	return &apiResp, nil
}

// isPermanentAuthError: the token is dead (revoked / session expired).
func isPermanentAuthError(err error) bool {
	var mErr *metaError
	if errors.As(err, &mErr) {
		if mErr.Code == 190 || mErr.Code == 102 {
			return true
		}
	}
	return false
}

// isThrottleError: Meta is telling us to slow down — app limit (4), account
// limit (17), call limit (32), sliding-window limit (613). The token itself
// is healthy.
func isThrottleError(err error) bool {
	var mErr *metaError
	if errors.As(err, &mErr) {
		switch mErr.Code {
		case 4, 17, 32, 613:
			return true
		}
	}
	return false
}

// persistNewToken retries the DB write on a detached context. Meta has already
// invalidated the old token by the time we get here, so losing the new one
// would permanently brick the connection — one best-effort write is not enough.
func (r *Refresher) persistNewToken(id, token string, expiresAt time.Time) error {
	const attempts = 3
	var lastErr error
	for i := 1; i <= attempts; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		lastErr = r.repo.UpdateToken(ctx, id, token, expiresAt)
		cancel()
		if lastErr == nil {
			return nil
		}
		slog.Error("failed to persist refreshed token, retrying",
			"connection_id", id, "attempt", i, "err", lastErr)
		time.Sleep(time.Duration(i) * 500 * time.Millisecond)
	}
	return fmt.Errorf("persist refreshed token after %d attempts: %w", attempts, lastErr)
}

// releaseClaim drops a row's lease so it can be re-claimed next cycle. Runs
// on a detached context: a canceled batch must not strand claims (the
// 10-minute stale lease is only a safety net; explicit release is the rule).
func (r *Refresher) releaseClaim(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.repo.ReleaseClaim(ctx, id); err != nil {
		slog.Error("failed to release claim", "connection_id", id, "err", err)
	}
}