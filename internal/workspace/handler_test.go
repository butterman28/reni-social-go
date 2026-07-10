package workspace

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"reni-social/internal/types"
	"reni-social/pkg/db"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test harness
// ──────────────────────────────────────────────────────────────────────────────

// fakeUserMiddleware stands in for middleware.UserMiddleware — it just injects
// a userID into locals so handlers can be tested without real sessions/redis.
func fakeUserMiddleware(userID string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals("userid", userID)
		return c.Next()
	}
}

// setupTestApp wires only the workspace routes onto a fresh Fiber app and
// swaps db.DB for a sqlmock connection.
func setupTestApp(t *testing.T, userID string) (*fiber.App, sqlmock.Sqlmock) {
	t.Helper()

	mockDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to open sqlmock: %v", err)
	}
	db.DB = mockDB

	app := fiber.New()
	grp := app.Group("/api/in", fakeUserMiddleware(userID))

	grp.Post("/workspaces", Create)
	grp.Get("/workspaces", ListMine)
	grp.Get("/workspaces/in/:id", GetOne)
	grp.Get("/workspaces/in/slug/:slug", GetOneBySlug)
	grp.Post("/workspaces/in/:id/members", Invite)
	grp.Get("/workspaces/in/:id/members", Members)
	grp.Delete("/workspaces/in/:id/members/in/:userId", RemoveMemberHandler)
	grp.Get("/workspaces/in/:id/connections", Connections)
	grp.Post("/workspaces/in/:id/connect/in/:platform", ConnectPlatform)
	grp.Get("/workspaces/in/:id/connect/in/:platform/callback", OAuthCallback)
	grp.Delete("/workspaces/in/:id/connections/:connectionId", Disconnect)
	grp.Get("/workspaces/in/slug/:slug", GetOneBySlug)
	grp.Delete("/workspaces/in/:id/members/in/:userId", RemoveMemberHandler)
	grp.Get("/workspaces/in/:id/x/connections", ListXConnections)
	grp.Get("/workspaces/in/:id/x/tweets/in/:tweetId", GetXTweet)
	grp.Get("/workspaces/in/:id/x/tweets/in/:tweetId/full", GetXTweetWithIncludes)
	grp.Get("/workspaces/in/:id/x/users/in/:userId", GetXUser)
	grp.Get("/workspaces/in/:id/x/spaces/in/:spaceId", GetXSpace)
	grp.Get("/workspaces/in/:id/x/lists/in/:listId", GetXList)

	return app, mock
}

func doRequest(t *testing.T, app *fiber.App, method, target string, body any) *http.Response {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}

	req := httptest.NewRequest(method, target, &buf)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func decodeResponse(t *testing.T, resp *http.Response) types.APIResponse[json.RawMessage] {
	t.Helper()
	var out types.APIResponse[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

// q is a small helper to build a regexp-safe fragment for sqlmock.
func q(s string) string {
	return regexp.QuoteMeta(s)
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /workspaces (Create)
// ──────────────────────────────────────────────────────────────────────────────

func TestCreateWorkspace_Success(t *testing.T) {
	userID := uuid.New().String()
	app, mock := setupTestApp(t, userID)

	mock.ExpectBegin()
	mock.ExpectExec(q("INSERT INTO workspaces")).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), "Acme Inc", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(q("INSERT INTO workspace_members")).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	resp := doRequest(t, app, http.MethodPost, "/api/in/workspaces", CreateWorkspaceRequest{
		Name: "Acme Inc",
	})

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	out := decodeResponse(t, resp)
	if !out.Status {
		t.Fatalf("expected status=true, got response: %+v", out)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestCreateWorkspace_MissingName(t *testing.T) {
	userID := uuid.New().String()
	app, _ := setupTestApp(t, userID)

	resp := doRequest(t, app, http.MethodPost, "/api/in/workspaces", CreateWorkspaceRequest{
		Name: "",
	})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCreateWorkspace_InvalidUserID(t *testing.T) {
	// fakeUserMiddleware injects a non-UUID userID to exercise the
	// uuid.Parse(ownerID) failure path in CreateWorkspace.
	app, _ := setupTestApp(t, "not-a-uuid")

	resp := doRequest(t, app, http.MethodPost, "/api/in/workspaces", CreateWorkspaceRequest{
		Name: "Acme Inc",
	})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /workspaces/in/:id (GetOne)
// ──────────────────────────────────────────────────────────────────────────────

func TestGetOneWorkspace_Success(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, mock := setupTestApp(t, userID)

	createdAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{"id", "owner_id", "name", "slug", "created_at"}).
    AddRow(wsID, userID, "Acme Inc", "acme-inc-12345678", createdAt)
	mock.ExpectQuery(q("SELECT w.id, w.owner_id, w.name, w.slug, w.created_at")).
		WithArgs(wsID, userID).
		WillReturnRows(rows)

	resp := doRequest(t, app, http.MethodGet, "/api/in/workspaces/in/"+wsID, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestGetOneWorkspace_NotFound(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, mock := setupTestApp(t, userID)

	mock.ExpectQuery(q("SELECT w.id, w.owner_id, w.name, w.slug, w.created_at")).
		WithArgs(wsID, userID).
		WillReturnError(sql.ErrNoRows)

	resp := doRequest(t, app, http.MethodGet, "/api/in/workspaces/in/"+wsID, nil)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /workspaces (ListMine)
// ──────────────────────────────────────────────────────────────────────────────

func TestListMyWorkspaces_Success(t *testing.T) {
	userID := uuid.New().String()
	app, mock := setupTestApp(t, userID)

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "owner_id", "name", "slug", "created_at"}).
		AddRow(uuid.New().String(), userID, "Acme Inc", "acme-inc-12345678", now).
		AddRow(uuid.New().String(), userID, "Beta LLC", "beta-llc-87654321", now)
	mock.ExpectQuery(q("SELECT w.id, w.owner_id, w.name, w.slug, w.created_at")).
		WithArgs(userID).
		WillReturnRows(rows)

	resp := doRequest(t, app, http.MethodGet, "/api/in/workspaces", nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /workspaces/in/:id/members (Invite)
// ──────────────────────────────────────────────────────────────────────────────

func TestInviteMember_InvalidRole(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, _ := setupTestApp(t, userID)

	body := map[string]string{
		"email": "someone@example.com",
		"role":  "superadmin", // not a valid domain.WorkspaceRole
	}

	resp := doRequest(t, app, http.MethodPost, "/api/in/workspaces/in/"+wsID+"/members", body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestInviteMember_NotAMember(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, mock := setupTestApp(t, userID)

	// requireRole looks up the caller's role and finds nothing
	mock.ExpectQuery(q("SELECT role FROM workspace_members")).
		WithArgs(wsID, userID).
		WillReturnError(sql.ErrNoRows)

	body := InviteMemberRequest{Email: "someone@example.com"}

	resp := doRequest(t, app, http.MethodPost, "/api/in/workspaces/in/"+wsID+"/members", body)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /workspaces/in/:id/connections (Connections)
// ──────────────────────────────────────────────────────────────────────────────

func TestListConnections_Success(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, mock := setupTestApp(t, userID)

	// requireRole check inside ListConnections
	mock.ExpectQuery(q("SELECT role FROM workspace_members")).
		WithArgs(wsID, userID).
		WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("owner"))

	liRows := sqlmock.NewRows([]string{"id", "workspace_id", "account_id", "account_name", "expires_at", "created_at"}).
		AddRow(uuid.New().String(), wsID, "urn:li:person:1", "Ada Lovelace", nil, "2024-01-01T00:00:00Z")
	mock.ExpectQuery(q("FROM linkedin_connections")).WithArgs(wsID).WillReturnRows(liRows)

	metaRows := sqlmock.NewRows([]string{"id", "workspace_id", "platform", "account_id", "account_name", "expires_at", "created_at"})
	mock.ExpectQuery(q("FROM meta_connections")).WithArgs(wsID).WillReturnRows(metaRows)

	xRows := sqlmock.NewRows([]string{"id", "workspace_id", "platform", "account_id", "account_name", "expires_at", "created_at"})
	mock.ExpectQuery(q("FROM x_connections")).WithArgs(wsID).WillReturnRows(xRows)

	ttRows := sqlmock.NewRows([]string{"id", "workspace_id", "account_id", "account_name", "expires_at", "created_at"})
	mock.ExpectQuery(q("FROM tiktok_connections")).WithArgs(wsID).WillReturnRows(ttRows)

	resp := doRequest(t, app, http.MethodGet, "/api/in/workspaces/in/"+wsID+"/connections", nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestListConnections_Forbidden(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, mock := setupTestApp(t, userID)

	mock.ExpectQuery(q("SELECT role FROM workspace_members")).
		WithArgs(wsID, userID).
		WillReturnError(sql.ErrNoRows)

	resp := doRequest(t, app, http.MethodGet, "/api/in/workspaces/in/"+wsID+"/connections", nil)

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /workspaces/in/:id/connect/in/:platform (ConnectPlatform)
// ──────────────────────────────────────────────────────────────────────────────

func TestConnectPlatform_Unsupported(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, _ := setupTestApp(t, userID)
	setupTestRedis(t) // ConnectPlatform writes to Redis before validating platform

	resp := doRequest(t, app, http.MethodPost, "/api/in/workspaces/in/"+wsID+"/connect/in/myspace", nil)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestConnectPlatform_LinkedInReturnsRedirectURL(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, _ := setupTestApp(t, userID)

	// ConnectPlatform writes the oauth_state key to Redis via utils.RDB.
	// In a unit test without Redis running this call will error; if your
	// utils package supports an injectable/mock client, wire it here.
	// Otherwise, run this test with a real local Redis (see utils.InitRedis).
	t.Skip("requires a live/mock Redis (utils.RDB) — wire a redis mock or run against local redis to enable")

	resp := doRequest(t, app, http.MethodPost, "/api/in/workspaces/in/"+wsID+"/connect/in/linkedin", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// DELETE /workspaces/in/:id/connections/:connectionId (Disconnect)
// ──────────────────────────────────────────────────────────────────────────────

func TestDisconnect_Forbidden(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	connID := uuid.New().String()
	app, mock := setupTestApp(t, userID)

	mock.ExpectQuery(q("SELECT role FROM workspace_members")).
		WithArgs(wsID, userID).
		WillReturnError(sql.ErrNoRows)

	resp := doRequest(t, app, http.MethodDelete, "/api/in/workspaces/in/"+wsID+"/connections/"+connID, nil)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestDisconnect_Success(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	connID := uuid.New().String()
	app, mock := setupTestApp(t, userID)

	mock.ExpectQuery(q("SELECT role FROM workspace_members")).
		WithArgs(wsID, userID).
		WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("owner"))

	mock.ExpectExec(q("DELETE FROM linkedin_connections")).
		WithArgs(connID, wsID).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(q("DELETE FROM meta_connections")).
		WithArgs(connID, wsID).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(q("DELETE FROM x_connections")).
		WithArgs(connID, wsID).WillReturnResult(sqlmock.NewResult(0, 1))

	resp := doRequest(t, app, http.MethodDelete, "/api/in/workspaces/in/"+wsID+"/connections/"+connID, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /workspaces/in/:id/connect/in/:platform — now backed by miniredis
// ──────────────────────────────────────────────────────────────────────────────

func TestConnectPlatform_LinkedIn_Success(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, _ := setupTestApp(t, userID)
	mr := setupTestRedis(t)

	resp := doRequest(t, app, http.MethodPost, "/api/in/workspaces/in/"+wsID+"/connect/in/linkedin", nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var out types.APIResponse[struct {
		RedirectURL string `json:"redirect_url"`
	}]
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Data.RedirectURL == "" {
		t.Fatal("expected a non-empty redirect_url")
	}
	if !strings.Contains(out.Data.RedirectURL, "linkedin.com") {
		t.Errorf("expected linkedin.com in redirect url, got %s", out.Data.RedirectURL)
	}

	// Verify ConnectPlatform actually wrote an oauth_state:* key to Redis —
	// this is the part that previously required a live Redis to test.
	keys := mr.Keys()
	found := false
	for _, k := range keys {
		if strings.HasPrefix(k, "oauth_state:") {
			found = true
			val, _ := mr.Get(k)
			if val != wsID {
				t.Errorf("expected stored value %s, got %s", wsID, val)
			}
		}
	}
	if !found {
		t.Fatal("expected an oauth_state:* key to be set in Redis")
	}
}

func TestConnectPlatform_Meta_Success(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, _ := setupTestApp(t, userID)
	mr := setupTestRedis(t)

	resp := doRequest(t, app, http.MethodPost, "/api/in/workspaces/in/"+wsID+"/connect/in/meta", nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var out types.APIResponse[struct {
		RedirectURL string `json:"redirect_url"`
	}]
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(out.Data.RedirectURL, "instagram.com") {
		t.Errorf("expected instagram.com in redirect url, got %s", out.Data.RedirectURL)
	}

	if len(mr.Keys()) == 0 {
		t.Fatal("expected an oauth_state key to be set in Redis")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /workspaces/in/:id/connect/in/:platform/callback (OAuthCallback)
//
// Full success paths still can't be unit-tested here because linkedin.tokenURL
// / meta.tokenURL / etc. are unexported package vars only swappable from
// within their own packages (see client_test.go in those packages for the
// httptest.Server pattern). What we CAN fully exercise now, thanks to a real
// Redis, is the state-validation logic that guards those exchanges.
// ──────────────────────────────────────────────────────────────────────────────

func TestOAuthCallback_InvalidState(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, _ := setupTestApp(t, userID)
	setupTestRedis(t) // running, but no oauth_state key was ever set

	target := "/api/in/workspaces/in/" + wsID + "/connect/in/linkedin/callback?code=abc123&state=nonexistent-state"
	resp := doRequest(t, app, http.MethodGet, target, nil)

	// OAuthCallback redirects on failure rather than returning an error status.
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("expected a redirect status, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "error=") {
		t.Errorf("expected redirect Location to carry an error param, got %s", loc)
	}
	if !strings.Contains(loc, "Invalid+or+expired+OAuth+state") && !strings.Contains(loc, "Invalid%20or%20expired%20OAuth%20state") {
		t.Errorf("expected 'invalid or expired OAuth state' message, got %s", loc)
	}
}

func TestOAuthCallback_StateWorkspaceMismatch(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	otherWsID := uuid.New().String()
	app, _ := setupTestApp(t, userID)
	mr := setupTestRedis(t)

	// Seed a state key that was issued for a DIFFERENT workspace, simulating
	// someone tampering with / replaying the callback URL.
	state := "tampered-state-123"
	if err := mr.Set("oauth_state:"+state, otherWsID); err != nil {
		t.Fatalf("seed redis: %v", err)
	}

	target := "/api/in/workspaces/in/" + wsID + "/connect/in/linkedin/callback?code=abc123&state=" + state
	resp := doRequest(t, app, http.MethodGet, target, nil)

	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("expected a redirect status, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "error=") {
		t.Errorf("expected redirect Location to carry an error param for mismatched workspace, got %s", loc)
	}
}

func TestOAuthCallback_UnsupportedPlatform(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, _ := setupTestApp(t, userID)
	mr := setupTestRedis(t)

	state := "valid-state-456"
	if err := mr.Set("oauth_state:"+state, wsID); err != nil {
		t.Fatalf("seed redis: %v", err)
	}

	target := "/api/in/workspaces/in/" + wsID + "/connect/in/myspace/callback?code=abc123&state=" + state
	resp := doRequest(t, app, http.MethodGet, target, nil)

	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("expected a redirect status, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "Unsupported+platform") && !strings.Contains(loc, "Unsupported%20platform") {
		t.Errorf("expected 'Unsupported platform' error, got %s", loc)
	}

	// Also confirms OAuthCallback consumed (deleted) the state key even on
	// this early-exit path... actually it doesn't reach the Del call before
	// hitting the platform switch's default branch after Del — verify the
	// key is gone since Del happens right after the state check succeeds.
	if mr.Exists("oauth_state:" + state) {
		t.Error("expected oauth_state key to be deleted after successful state verification")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /workspaces/in/slug/:slug (GetOneBySlug)
// ──────────────────────────────────────────────────────────────────────────────

func TestGetOneWorkspaceBySlug_Success(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, mock := setupTestApp(t, userID)

	createdAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{"id", "owner_id", "name", "slug", "created_at"}).
		AddRow(wsID, userID, "Acme Inc", "acme-inc-12345678", createdAt)

	mock.ExpectQuery(q("SELECT w.id, w.owner_id, w.name, w.slug, w.created_at")).
		WithArgs("acme-inc-12345678", userID).
		WillReturnRows(rows)

	resp := doRequest(t, app, http.MethodGet, "/api/in/workspaces/in/slug/acme-inc-12345678", nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestGetOneWorkspaceBySlug_NotFound(t *testing.T) {
	userID := uuid.New().String()
	app, mock := setupTestApp(t, userID)

	mock.ExpectQuery(q("SELECT w.id, w.owner_id, w.name, w.slug, w.created_at")).
		WithArgs("ghost-slug", userID).
		WillReturnError(sql.ErrNoRows)

	resp := doRequest(t, app, http.MethodGet, "/api/in/workspaces/in/slug/ghost-slug", nil)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// GET /workspaces/in/:id/members (Members)
// ──────────────────────────────────────────────────────────────────────────────

func TestMembers_Success(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, mock := setupTestApp(t, userID)

	mock.ExpectQuery(q("SELECT role FROM workspace_members")).
		WithArgs(wsID, userID).
		WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("owner"))

	joinedAt := time.Now()
	rows := sqlmock.NewRows([]string{"id", "email", "first_name", "last_name", "avatar_url", "role", "joined_at"}).
		AddRow(userID, "owner@example.com", "Ada", "Lovelace", "", "owner", joinedAt)

	mock.ExpectQuery(q("FROM workspace_members m")).
		WithArgs(wsID).
		WillReturnRows(rows)

	resp := doRequest(t, app, http.MethodGet, "/api/in/workspaces/in/"+wsID+"/members", nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestMembers_Forbidden(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, mock := setupTestApp(t, userID)

	mock.ExpectQuery(q("SELECT role FROM workspace_members")).
		WithArgs(wsID, userID).
		WillReturnError(sql.ErrNoRows)

	resp := doRequest(t, app, http.MethodGet, "/api/in/workspaces/in/"+wsID+"/members", nil)

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// DELETE /workspaces/in/:id/members/in/:userId (RemoveMemberHandler)
// ──────────────────────────────────────────────────────────────────────────────

func TestRemoveMember_Success(t *testing.T) {
	callerID := uuid.New().String()
	wsID := uuid.New().String()
	targetID := uuid.New().String()
	app, mock := setupTestApp(t, callerID)

	// requireRole(admin/owner) check
	mock.ExpectQuery(q("SELECT role FROM workspace_members")).
		WithArgs(wsID, callerID).
		WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("owner"))

	// owner-protection lookup on the target
	mock.ExpectQuery(q("SELECT role FROM workspace_members")).
		WithArgs(wsID, targetID).
		WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("member"))

	mock.ExpectExec(q("DELETE FROM workspace_members")).
		WithArgs(wsID, targetID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	resp := doRequest(t, app, http.MethodDelete,
		"/api/in/workspaces/in/"+wsID+"/members/in/"+targetID, nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestRemoveMember_CannotRemoveOwner(t *testing.T) {
	callerID := uuid.New().String()
	wsID := uuid.New().String()
	targetID := uuid.New().String()
	app, mock := setupTestApp(t, callerID)

	mock.ExpectQuery(q("SELECT role FROM workspace_members")).
		WithArgs(wsID, callerID).
		WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("admin"))

	mock.ExpectQuery(q("SELECT role FROM workspace_members")).
		WithArgs(wsID, targetID).
		WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("owner"))

	resp := doRequest(t, app, http.MethodDelete,
		"/api/in/workspaces/in/"+wsID+"/members/in/"+targetID, nil)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// X (Twitter) read endpoints — GetXTweet / GetXUser / GetXSpace / GetXList /
// GetXTweetWithIncludes / ListXConnections
//
// These call out to api.twitter.com via internal/platform/x.Client, which is
// NOT mockable from this package (its URLs are unexported package-level vars
// only swappable from within the x package itself — see client_test.go
// there). So here we can only exercise the parts entirely within workspace's
// control: the workspace-membership check and the X-connection lookup. A
// wrong workspace or missing connection should fail BEFORE ever reaching
// the network, and that's exactly what we can assert.
// ──────────────────────────────────────────────────────────────────────────────

func TestGetXTweet_Forbidden_NotAWorkspaceMember(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, mock := setupTestApp(t, userID)

	mock.ExpectQuery(q("SELECT w.id, w.owner_id, w.name, w.slug, w.created_at")).
		WithArgs(wsID, userID).
		WillReturnError(sql.ErrNoRows)

	resp := doRequest(t, app, http.MethodGet,
		"/api/in/workspaces/in/"+wsID+"/x/tweets/in/12345", nil)

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestGetXTweet_NoConnectionFound(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, mock := setupTestApp(t, userID)

	createdAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	wsRows := sqlmock.NewRows([]string{"id", "owner_id", "name", "slug", "created_at"}).
		AddRow(wsID, userID, "Acme Inc", "acme-inc-1", createdAt)
	mock.ExpectQuery(q("SELECT w.id, w.owner_id, w.name, w.slug, w.created_at")).
		WithArgs(wsID, userID).
		WillReturnRows(wsRows)

	// GetXConnection with empty connection_id falls back to "first found"
	mock.ExpectQuery(q("FROM x_connections WHERE workspace_id = $1")).
		WithArgs(wsID).
		WillReturnError(sql.ErrNoRows)

	resp := doRequest(t, app, http.MethodGet,
		"/api/in/workspaces/in/"+wsID+"/x/tweets/in/12345", nil)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestListXConnections_Success(t *testing.T) {
	userID := uuid.New().String()
	wsID := uuid.New().String()
	app, mock := setupTestApp(t, userID)

	createdAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	wsRows := sqlmock.NewRows([]string{"id", "owner_id", "name", "slug", "created_at"}).
		AddRow(wsID, userID, "Acme Inc", "acme-inc-1", createdAt)
	mock.ExpectQuery(q("SELECT w.id, w.owner_id, w.name, w.slug, w.created_at")).
		WithArgs(wsID, userID).
		WillReturnRows(wsRows)

	xRows := sqlmock.NewRows([]string{"id", "account_id", "account_name", "expires_at", "created_at"}).
		AddRow(uuid.New().String(), "12345", "myhandle", nil, createdAt)
	mock.ExpectQuery(q("SELECT id, account_id, account_name, expires_at, created_at")).
		WithArgs(wsID).
		WillReturnRows(xRows)

	resp := doRequest(t, app, http.MethodGet,
		"/api/in/workspaces/in/"+wsID+"/x/connections", nil)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}