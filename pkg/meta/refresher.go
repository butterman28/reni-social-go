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
	"time"
)

// instagramRefreshBaseURL is a package-level var (not a const) specifically
// so tests can point it at an httptest.Server, matching the pattern already
// used in internal/platform/linkedin, /meta, /tiktok (tokenURL, ugcPostURL,
// etc). Never mutate this outside of tests.
var instagramRefreshBaseURL = "https://graph.instagram.com/refresh_access_token"
var errReauthRequired = errors.New("connection flagged for re-authentication")
// Config defines the runtime configuration for the token worker.
type Config struct {
	CheckInterval     time.Duration // How often the worker checks DB for expiring tokens (e.g., 6 hours)
	RefreshBeforeDays int           // Trigger refresh if token expires in < X days (e.g., 15 days)
	BatchSize         int           // Max tokens to fetch per batch
	MaxConcurrency    int           // Max parallel goroutines hitting Meta API
	RequestTimeout    time.Duration // Timeout for single HTTP call

	// BackoffBase is the base delay for exponential retry backoff
	// (attempt N sleeps ~ BackoffBase * 2^(N-1), plus jitter up to BackoffBase).
	// Defaults to 1s in production if left zero. Tests should set this to a
	// few milliseconds so retry-path tests don't burn real wall-clock time.
	BackoffBase time.Duration
}

func (c Config) backoffBase() time.Duration {
	if c.BackoffBase > 0 {
		return c.BackoffBase
	}
	return 1 * time.Second
}

// MetaConnection represents the database record.
type MetaConnection struct {
	ID          string
	AccessToken string
	ExpiresAt   time.Time
}

// TokenRepository abstracts the DB layer.
type TokenRepository interface {
	GetTokensNeedingRefresh(ctx context.Context, thresholdDays int, limit int) ([]MetaConnection, error)
	UpdateToken(ctx context.Context, id string, newToken string, expiresAt time.Time) error
	MarkConnectionNeedsReauth(ctx context.Context, id string, reason string) error
}

// Refresher handles background token updates.
type Refresher struct {
	cfg        Config
	repo       TokenRepository
	httpClient *http.Client
}

func NewRefresher(cfg Config, repo TokenRepository) *Refresher {
	return &Refresher{
		cfg:  cfg,
		repo: repo,
		httpClient: &http.Client{
			Timeout: cfg.RequestTimeout,
		},
	}
}

// MetaAPI Responses
type metaRefreshResponse struct {
	AccessToken string     `json:"access_token"`
	TokenType   string     `json:"token_type"`
	ExpiresIn   int64      `json:"expires_in"` // Seconds until expiration (~5184000 for 60 days)
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

// Start runs the background service until the parent context is canceled.
func (r *Refresher) Start(ctx context.Context) {
	slog.Info("Starting Meta background token refresher", "check_interval", r.cfg.CheckInterval)
	ticker := time.NewTicker(r.cfg.CheckInterval)
	defer ticker.Stop()

	// Initial run on startup
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

	// Semaphore to cap concurrent HTTP outbound requests to Meta
	maxConcurrency := r.cfg.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for _, conn := range conns {
		wg.Add(1)
		sem <- struct{}{} // Acquire slot

		go func(c MetaConnection) {
			defer func() {
				<-sem // Release slot
				wg.Done()

				// Panic isolation guard
				if p := recover(); p != nil {
					slog.Error("Recovered from panic in token refresh worker",
						"panic", p, "connection_id", c.ID)
				}
			}()

			// Individual token timeout scope
			taskCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()

			err := r.refreshWithRetry(taskCtx, c)
			switch {
			case errors.Is(err, errReauthRequired):
				slog.Warn("Connection requires user re-authentication", "connection_id", c.ID)
			case err != nil:
				slog.Error("Token refresh failed after retries", "connection_id", c.ID, "err", err)
			default:
				slog.Info("Successfully refreshed Meta token", "connection_id", c.ID)
			}
		}(conn)
	}

	wg.Wait()
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

		resp, err := r.callRefreshEndpoint(ctx, conn.AccessToken)
		if err == nil {
			// Calculate new expiration time
			newExpiry := time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
			return r.repo.UpdateToken(ctx, conn.ID, resp.AccessToken, newExpiry)
		}

		lastErr = err

		// Non-retryable error check (e.g. User revoked access or expired token)
		if isPermanentAuthError(err) {
			slog.Warn("Permanent auth error detected. Flagging connection for re-authentication",
				"connection_id", conn.ID, "err", err)
			if reauthErr := r.repo.MarkConnectionNeedsReauth(ctx, conn.ID, err.Error()); reauthErr != nil {
				return reauthErr
			}
			return errReauthRequired   // ← signal "this was a reauth, not a refresh"
		}

		// Exponential Backoff: 2^(attempt-1) * base + jitter
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
		return nil, fmt.Errorf("error decoding Meta response: %w", err)
	}

	if apiResp.Error != nil {
		return nil, apiResp.Error
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status code %d: %s", res.StatusCode, string(bodyBytes))
	}

	return &apiResp, nil
}

// isPermanentAuthError identifies errors where retrying will never work.
func isPermanentAuthError(err error) bool {
	var mErr *metaError
	if errors.As(err, &mErr) {
		// OAuth errors: 190 = Invalid OAuth 2.0 Access Token, 102 = Session expired
		if mErr.Code == 190 || mErr.Code == 102 {
			return true
		}
	}
	return false
}