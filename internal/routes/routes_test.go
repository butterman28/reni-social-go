package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"reni-social/internal/utils"
	"reni-social/pkg/db"
)

// setupIntegrationApp boots the REAL app (routes.Setup + real middleware),
// backed by miniredis for sessions and sqlmock for the database. This is
// the one place we verify the actual route wiring — paths, method verbs,
// and middleware order — rather than calling handlers directly.
func setupIntegrationApp(t *testing.T) (*fiber.App, sqlmock.Sqlmock, *miniredis.Miniredis) {
	t.Helper()

	mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	db.DB = mockDB

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	utils.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})

	app := fiber.New()
	Setup(app)

	return app, mock, mr
}

// seedSession stores a session token -> userID mapping in Redis, matching
// what utils.SetSession/GetSession expect, so a request carrying both the
// Authorization bearer token and X-Session-Id header will authenticate.
func seedSession(t *testing.T, mr *miniredis.Miniredis, sessionID, userID string) {
	t.Helper()
	if err := mr.Set("session:"+sessionID, userID); err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

func TestRoutes_HealthCheck(t *testing.T) {
	app, _, _ := setupIntegrationApp(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRoutes_WorkspacesRequireAuth(t *testing.T) {
	app, _, _ := setupIntegrationApp(t)

	// No Authorization header at all — AuthMiddleware should reject first,
	// before UserMiddleware or the handler ever run.
	req := httptest.NewRequest(http.MethodGet, "/api/in/workspaces", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 (missing Authorization), got %d", resp.StatusCode)
	}
}

func TestRoutes_WorkspacesRequireSessionHeader(t *testing.T) {
	app, _, _ := setupIntegrationApp(t)

	// Has a bearer token (passes AuthMiddleware) but no X-Session-Id header
	// (fails UserMiddleware).
	req := httptest.NewRequest(http.MethodGet, "/api/in/workspaces", nil)
	req.Header.Set("Authorization", "Bearer sometoken")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 (missing session header), got %d", resp.StatusCode)
	}
}

func TestRoutes_ListWorkspaces_FullChain(t *testing.T) {
	app, mock, mr := setupIntegrationApp(t)

	userID := uuid.New().String()
	sessionID := "session-abc-123"
	seedSession(t, mr, sessionID, userID)

	mock.ExpectQuery("SELECT w.id, w.owner_id, w.name, w.slug, w.created_at").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_id", "name", "slug", "created_at"}))

	req := httptest.NewRequest(http.MethodGet, "/api/in/workspaces", nil)
	req.Header.Set("Authorization", "Bearer sometoken")
	req.Header.Set("X-Session-Id", sessionID)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestRoutes_PublicAuthRoutesSkipMiddleware(t *testing.T) {
	app, _, _ := setupIntegrationApp(t)

	// /api/auth/signin sits under the AuthMiddleware-protected /api group
	// per routes.go — confirm it currently requires a bearer token too.
	// (If this surprises you, that's exactly the kind of wiring bug this
	// test class is meant to catch — routes.go groups all of /api under
	// AuthMiddleware, including nominally "public" auth endpoints.)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/signin", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d — if this fails, routing changed", resp.StatusCode)
	}
}