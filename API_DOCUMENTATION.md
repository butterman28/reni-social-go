# Reni Social API — Documentation

Generated from `internal/routes/routes.go`, handlers, and domain/service types in the `reni-social-go` codebase. Every example below is derived directly from the current Go structs and handler logic. This revision corrects several places where the previous documentation had drifted from the code (see the changelog at the very bottom) — most importantly: **all API responses are now consistently snake_case**, access/refresh tokens are no longer leaked in the connections endpoint, a wallet/credit system now gates X publishing, and the retry/timeout behavior of the publishing pipeline has changed materially.

- **Base URL (local dev):** `http://127.0.0.1:1040`
- **Base URL (docker):** same, port `1040`
- **Content-Type:** `application/json` for all request/response bodies unless noted.

---

## Table of Contents

1. [Authentication Model](#authentication-model)
2. [Response Shape Quirks (read this first)](#response-shape-quirks-read-this-first)
3. [Auth & Onboarding](#1-auth--onboarding)
4. [Workspaces & Members](#2-workspaces--members)
5. [Platform Connections (OAuth)](#3-platform-connections-oauth)
6. [Posts & Publishing](#4-posts--publishing)
7. [X (Twitter) Read API](#5-x-twitter-read-api)
8. [Billing & Credits](#6-billing--credits)
9. [System](#7-system)
10. [Async Publishing Workflow](#async-publishing-workflow-walkthrough)
11. [Domain Model Reference](#domain-model-reference)
12. [Error Handling](#error-handling)
13. [Quick-Reference Endpoint Table](#quick-reference-endpoint-table)
14. [Changelog vs previous revision](#changelog-vs-previous-revision-of-this-document)

---

## Authentication Model

Two layers of middleware protect everything under `/api/in/*`:

| Middleware | Header required | What it does |
|---|---|---|
| `AuthMiddleware` (`internal/middleware/auth.go`) | `Authorization: Bearer <token>` | Only checks that the header is present and formatted as `Bearer <non-empty>`. It does **not** validate the token against anything — any non-empty string works. Stores the raw token string in `c.Locals("token")`. |
| `UserMiddleware` (`internal/middleware/user.go`) | `Authorization: Bearer <token>` **and** `X-Session-Id: <session>` | Re-validates the `Authorization` header format, requires `X-Session-Id`, then looks up `X-Session-Id` in Redis (`session:<id>` → userID) via `utils.GetSession`. Stores resolved `userID` in `c.Locals("userid")`. |

**Practical implication:** the `Authorization` bearer value is essentially a dummy/placeholder (the Postman collection literally sets it to `dummy_token`) — the *real* identity check happens entirely through `X-Session-Id`, which must be a valid Redis-backed session token obtained from `/api/auth/signin`.

Every request under `/api/in/*` must send **both** headers:
```
Authorization: Bearer dummy_token
X-Session-Id: <token returned by /api/auth/signin>
```

**Public routes** (no auth required — see `internal/routes/routes.go`):
- `GET /health`
- `GET /api/workspaces/connect/:platform/callback`
- `POST /api/webhooks/payment`
- `POST /api/auth/register/email`
- `POST /api/auth/register/email/activate`
- `POST /api/auth/register/complete`
- `POST /api/auth/signin`

✅ **Correction from an earlier revision of this document:** `routes.go` registers the four `/api/auth/*` routes on their own `app.Group("/api/auth", middleware.StrictAuthLimiter())`, entirely separate from the `AuthMiddleware`-protected `/api` group. They are genuinely public — only rate-limited, not auth-gated. The routing test `TestRoutes_PublicAuthRoutesSkipMiddleware` asserts a `401` for `POST /api/auth/signin` with no body/headers, but that `401` comes from `SigninHandler` mapping *any* error returned by the `Signin()` service call (including the plain validation error `"mail and password are required"` from an empty request body) to HTTP 401 — **not** from an auth middleware rejecting the request. Don't be misled by the status code into thinking this route needs a token.

### Rate limiting

| Scope | Limiter | Limit | Key |
|---|---|---|---|
| Global (`cmd/api/main.go`, applied before `routes.Setup`) | `middleware.RedisRateLimiter(300, 1m)` | 300 req/min | `X-Session-Id` if present, else client IP |
| `/api/auth/*` (register/activate/complete/signin) | `middleware.StrictAuthLimiter()` | 50 req/min | same key logic |
| `/api/in/*` (everything behind `AuthMiddleware`) | `middleware.GeneralApiLimiter()` | 300 req/min | same key logic |

All three are Redis-backed (`gofiber/storage/redis/v2`) and skip `OPTIONS` preflight requests. Exceeding a limit returns `429` with `{"status": false, "message": "Too many requests. Please try again later."}`.

---

## Response Shape Quirks (read this first)

This backend does **not** use one consistent envelope. There are three different shapes in play:

### Shape A — `types.APIResponse[T]` (auth "me"/"signout", all workspace handlers)
```json
{
  "status": true,
  "message": "OK",
  "data": { }
}
```

### Shape B — Reni Central passthrough (signin, register/*)
```json
{
  "status": true,
  "message": "string",
  "data": { },
  "http_status": 200
}
```

### Shape C — Raw `fiber.Map` (all post handlers)
No `status`/`message` wrapper at all — just the raw keys, e.g. `{"post": {...}}` or `{"error": "..."}`.

### JSON key casing — now consistent (this is the big fix from the previous doc revision)

Every domain struct that is serialized to a client (`domain.Post`, `domain.Workspace`, `domain.PlatformConnection`, `domain.User`) now carries full `json` struct tags, so **all API responses use snake_case consistently**. This was not always true — an earlier version of `domain.Post` and `domain.PlatformConnection` had no tags at all and fell back to Go's default PascalCase field names, and `domain.PlatformConnection` used to serialize `AccessToken`/`RefreshToken` in plaintext. Both of those have been fixed in the current codebase:

- `domain.Post` → `id`, `workspace_id`, `author_id`, `content`, `media_urls`, `platforms`, `article_url`, `article_title`, `status`, `scheduled_at`, `published_at`, `fail_reason`, `created_at`, `updated_at`, `current_publish_attempt_id`, `retry_count`, `max_retries` — **all snake_case**.
- `domain.Workspace` → `id`, `owner_id`, `name`, `slug`, `created_at` — all snake_case.
- `domain.PlatformConnection` → `id`, `workspace_id`, `platform`, `account_id`, `account_name`, `expires_at`, `created_at`, `is_deleted`, `needs_reauth`, `reauth_reason` — all snake_case. **`access_token` and `refresh_token` are tagged `json:"-"` and are never serialized to any client**, closing the token-leak that a previous revision of this documentation flagged as an open security concern on `GET /api/in/workspaces/in/:id/connections`.
- `domain.User` → `id`, `email`, `first_name`, `last_name`, `fullname`, `phone_number`, `username`, `gender`, `address`, `avatar_url`, `token`, `is_email_verified`, `created_at` — unchanged, was already fully tagged.

Manually-built `fiber.Map`/`map[string]any` responses (post list/get, members list, X connections list) continue to use snake_case since those keys are typed by hand in the handler — this is unchanged.

⚠️ **If you integrated against an older version of this API** that returned `ID`, `WorkspaceID`, `Content`, etc. in PascalCase for posts/workspaces/connections, you will need to update your client — every example in this document below reflects the current snake_case shape.

---

## 1. Auth & Onboarding

### 1.1 `POST /api/auth/register/email`
Step 1 of 3-step registration — sends a verification code to an email via Reni Central.

**Auth:** none (rate-limited: 50 req/min)
**Request body:**
```json
{ "mail": "user@example.com" }
```
**Response (200):**
```json
{
  "status": true,
  "message": "string from Reni Central",
  "data": {},
  "http_status": 200
}
```

---

### 1.2 `POST /api/auth/register/email/activate`
Step 2 — verify the emailed code.

**Auth:** none (rate-limited: 50 req/min)
**Request body:**
```json
{ "mail": "user@example.com", "authCode": "123456" }
```
**Response:** same `Shape B` envelope as above.

---

### 1.3 `POST /api/auth/register/complete`
Step 3 — completes registration. On success, the backend also creates a **local** user row (mirrored from Reni Central) and issues a local Redis session, overwriting `data.token` with that local session id before returning. `data.pword` is scrubbed to `""` before the response is returned.

**Auth:** none (rate-limited: 50 req/min)
**Request body:**
```json
{
  "mail": "user@example.com",
  "fname": "John",
  "lname": "Doe",
  "phone": "1234567890",
  "pword": "Password123!",
  "cpword": "Password123!",
  "username": "johndoe99",
  "gender": "male"
}
```
**Response (200):**
```json
{
  "status": true,
  "message": "string",
  "data": {
    "fname": "John",
    "lname": "Doe",
    "phone": "1234567890",
    "gender": "male",
    "mail": "user@example.com",
    "pword": "",
    "username": "johndoe99",
    "fullname": "John Doe",
    "token": "<local session token — use as X-Session-Id going forward>"
  },
  "http_status": 200
}
```

### 1.4 `POST /api/auth/signin`
Logs in via Reni Central, upserts a local user row, and issues a local session.

**Auth:** none (rate-limited: 50 req/min)
**Request body:**
```json
{ "mail": "user@example.com", "pword": "Password123!" }
```
**Response (200):**
```json
{
  "status": true,
  "message": "string",
  "data": {
    "id": 123,
    "token": "<local session token>",
    "mail": "user@example.com",
    "username": "johndoe99",
    "fullname": "John Doe",
    "fname": "John",
    "lname": "Doe",
    "phone": "1234567890",
    "gender": "male",
    "verified": 1,
    "picture": "https://...",
    "...": "many other passthrough fields from Reni Central — see SigninUserData in internal/auth/service.go"
  },
  "http_status": 200
}
```
**Save `data.token`** — this is what you send as `X-Session-Id` on every `/api/in/*` request afterward. The Postman collection auto-captures this via a test script.

**Failure (401):** returned for both actual credential failures from Reni Central *and* for a missing/empty `mail`/`pword` in the request body (both cases return an `error` from the `Signin` service, and the handler maps every error to 401):
```json
{ "status": false, "message": "invalid credentials or similar" }
```

---

### 1.5 `POST /api/in/auth/signout`
**Auth:** `Authorization` + `X-Session-Id`
Deletes the Redis session for the current `Authorization` bearer token (note: it deletes based on the raw bearer token stored in `c.Locals("token")` from `AuthMiddleware`, not the `X-Session-Id`).

**Response (200):**
```json
{ "status": true, "message": "Signed out successfully" }
```

---

### 1.6 `GET /api/in/auth/me`
**Auth:** `Authorization` + `X-Session-Id`

**Response (200):**
```json
{
  "status": true,
  "message": "OK",
  "data": {
    "id": "b3f1...-uuid",
    "email": "user@example.com",
    "first_name": "John",
    "last_name": "Doe",
    "fullname": "",
    "phone_number": "",
    "username": "",
    "gender": "",
    "address": "",
    "avatar_url": "",
    "token": "",
    "is_email_verified": false,
    "created_at": "2026-01-01T00:00:00Z"
  }
}
```
Note: the `GetMe` query only selects `id, email, first_name, last_name, avatar_url, created_at` — every other field on `domain.User` (`fullname`, `phone_number`, `username`, `gender`, `address`, `token`, `is_email_verified`) is left at its Go zero value in this specific response, regardless of what's actually stored.

**Failure (404):**
```json
{ "status": false, "message": "user not found" }
```

---

## 2. Workspaces & Members

All routes below require `Authorization` + `X-Session-Id`, and all responses use `types.APIResponse[any]` (**Shape A**).

### 2.1 `POST /api/in/workspaces`
Creates a workspace; caller becomes `owner`.

**Body:**
```json
{ "name": "My New Workspace" }
```
**Response (201):**
```json
{
  "status": true,
  "message": "Workspace created",
  "data": {
    "id": "8b2e...-uuid",
    "owner_id": "b3f1...-uuid",
    "name": "My New Workspace",
    "slug": "my-new-workspace-8b2e6a1c",
    "created_at": "2026-08-07T10:00:00Z"
  }
}
```
Slug format: lowercased/hyphenated name + first 8 chars of the workspace UUID, via `generateSlug`.

---

### 2.2 `GET /api/in/workspaces`
Lists all workspaces the caller belongs to.

**Response (200):**
```json
{
  "status": true,
  "message": "OK",
  "data": [
    { "id": "...", "owner_id": "...", "name": "Acme Inc", "slug": "acme-inc-12345678", "created_at": "..." }
  ]
}
```

---

### 2.3 `GET /api/in/workspaces/in/:id`
Fetch one workspace (caller must be a member).

**Response (200):** single object, same shape as 2.1's `data`.
**Failure (404):** `{ "status": false, "message": "workspace not found or access denied" }`

---

### 2.4 `GET /api/in/workspaces/in/slug/:slug`
Same as 2.3 but looked up by slug.

---

### 2.5 `POST /api/in/workspaces/in/:id/members`
Invite a member by email. Caller must be `admin` or `owner`.

**Body:**
```json
{ "email": "teammate@example.com", "role": "admin" }
```
Valid `role` values at the handler level: `""` (defaults to `member`), `member`, `admin`, `owner` — **but** the service layer (`InviteMember`) silently downgrades any `owner` request to `member` server-side ("prevent escalation to owner via invite"), so `role: "owner"` in the request will not actually grant ownership.

**Response (200):**
```json
{ "status": true, "message": "Member invited" }
```
**Failure (400)** if:
- role is invalid at the handler level (anything other than `""`, `member`, `admin`, `owner`)
- caller lacks permission
- no user exists with that email
- user is already a member

---

### 2.6 `GET /api/in/workspaces/in/:id/members`
Any member (`member`/`admin`/`owner`) can list.

**Response (200):**
```json
{
  "status": true,
  "message": "OK",
  "data": [
    {
      "id": "b3f1...-uuid",
      "email": "owner@example.com",
      "first_name": "Ada",
      "last_name": "Lovelace",
      "avatar_url": "",
      "role": "owner",
      "joined_at": "2026-01-01T00:00:00Z"
    }
  ]
}
```
Hand-built `map[string]any` in `ListMembers` — already snake_case, unaffected by the casing fix above.

---

### 2.7 `DELETE /api/in/workspaces/in/:id/members/in/:userId`
Caller must be `admin`/`owner`. The workspace `owner` can never be removed (returns 400 `workspace owner cannot be removed`).

**Response (200):**
```json
{ "status": true, "message": "Member removed" }
```

---

### 2.8 `GET /api/in/workspaces/in/:id/connections/reauth-status`

**⚠️ Not present in earlier revisions of this document — this route exists in `routes.go` and is fully implemented.**

Tokenless advisory endpoint — safe for global/frequent frontend polling, since it never returns access tokens. Any workspace member can call it (any of `member`/`admin`/`owner`); the frontend should vary the banner copy by role since only `admin`/`owner` can actually act on a "reconnect" prompt.

**Response (200):**
```json
{
  "status": true,
  "message": "OK",
  "data": {
    "needs_attention": true,
    "alerts": [
      {
        "platform": "linkedin",
        "account_id": "urn:li:person:123",
        "account_name": "Ada Lovelace",
        "reason": "LinkedIn access token expires on 2026-08-20T10:00:00Z — reconnect now to avoid publishing interruptions",
        "expired": false,
        "expires_at": "2026-08-20T10:00:00Z"
      }
    ]
  }
}
```
`alerts` is populated from `linkedin_connections` and `meta_connections` rows where `needs_reauth = true` (set by the background token-refresh/expiry-monitor workers — see [Async Publishing Workflow](#async-publishing-workflow-walkthrough) for the surrounding worker architecture, and `docs/refresh and reauthing.md` in the repo for the full background-worker writeup). `alerts` is always an array (never `null`), even when empty.

**Failure (403):** `{ "status": false, "message": "access denied: not a member of this workspace" }`

---

## 3. Platform Connections (OAuth)

**Supported platforms** at the `POST .../connect/in/:platform` route: `linkedin`, `meta`, `x` (note: **not** `tiktok`, despite `domain.PlatformTikTok` existing elsewhere — the connect handler's allow-list is `linkedin`/`meta`/`x` only).

### 3.1 `GET /api/in/workspaces/in/:id/connections`
Caller must be a member. Returns LinkedIn + Meta + X connections (TikTok is **not** queried here despite having its own schema table — see `ListConnections` in `internal/workspace/service.go`).

**Response (200):**
```json
{
  "status": true,
  "message": "OK",
  "data": [
    {
      "id": "...",
      "workspace_id": "...",
      "platform": "linkedin",
      "account_id": "urn:li:person:12345",
      "account_name": "Ada Lovelace",
      "expires_at": "2026-10-01T00:00:00Z",
      "created_at": "2026-08-01T00:00:00Z",
      "is_deleted": false,
      "needs_reauth": false,
      "reauth_reason": ""
    }
  ]
}
```
✅ **Correction from an earlier revision of this document:** `access_token`/`refresh_token` are **no longer** included in this response — `domain.PlatformConnection` now tags both fields `json:"-"`. The X-connections branch of this same query does not currently select `is_deleted`, `needs_reauth`, `reauth_reason` from the DB (those columns don't exist on `x_connections`), so X entries in this list will simply omit `needs_reauth`/`reauth_reason` (Go zero values: `false`/`""`) since X tokens don't expire the same way (OAuth 1.0a).

---

### 3.2 `POST /api/in/workspaces/in/:id/connect/in/:platform`
Caller must be `admin`/`owner`. Kicks off (or short-circuits) OAuth.

**Query params:**
| Param | Required | Notes |
|---|---|---|
| `force` | no | `?force=true` bypasses the "already connected" short-circuit entirely and always generates a fresh `oauth_state`/`redirect_url`, forcing the consent screen again. Used by the frontend's "Reconnect" button when a connection is flagged `needs_reauth` but hasn't hard-expired yet — without `force=true`, `CheckAndReviveConnection` would just silently revive the still-technically-valid row and skip OAuth. |

**Pre-flight credential checks (400):** before anything else, the handler checks that the relevant app credentials are configured:
```json
{ "status": false, "message": "LinkedIn client credentials are not configured. Please contact support." }
```
```json
{ "status": false, "message": "Instagram (Meta) client credentials are not configured. Please contact support." }
```
(No equivalent check exists for `x`, since `X_API_KEY`/`X_API_SECRET` are read at package-init time rather than validated per-request here.)

**Body:** none

**Behavior:**
1. Unless `force=true`, if a non-expired connection already exists for that platform (even if soft-deleted, it gets revived), the OAuth flow is **skipped**:
   ```json
   { "status": true, "message": "Already connected", "data": { "already_connected": true } }
   ```
2. Otherwise (or with `force=true`), an `oauth_state` is generated (`<workspace_id>:<16-char random>`), stored in Redis for 10 minutes, and a platform-specific `redirect_url` is returned:
   ```json
   { "status": true, "message": "OK", "data": { "redirect_url": "https://www.linkedin.com/oauth/v2/authorization?..." } }
   ```
   - `linkedin` → LinkedIn OAuth2 consent screen
   - `meta` → Instagram Login flow consent screen (`instagram.com/oauth/authorize`) — **not** the Facebook Login flow
   - `x` → OAuth 1.0a: this call synchronously fetches a request token from X first, so `redirect_url` is the ready-to-open `https://api.twitter.com/oauth/authorize?oauth_token=...` URL.

**Failure (400):** unsupported platform → `{ "status": false, "message": "Unsupported platform: <p>" }`
**Failure (403):** caller isn't `admin`/`owner` → `{ "status": false, "message": "access denied: insufficient role (<role>)" }`

---

### 3.3 `GET /api/workspaces/connect/:platform/callback`
**Public** (no auth headers). This is where the OAuth provider redirects the browser back to.

**Query params:**
- `state` — the `<workspace_id>:<random>` string issued in 3.2 (X uses its own `oauth_token`/`oauth_verifier` params instead — see below)
- `code` — auth code (LinkedIn / Meta)
- For `x`: `oauth_token`, `oauth_verifier` (standard OAuth 1.0a callback params)

**Behavior:** validates `state` against Redis, exchanges the code for tokens via the relevant platform client (Meta uses the Instagram Login exchange, `client.ExchangeInstagramLoginCode`, matching the flow started in 3.2), persists the connection (upsert on `(workspace_id, account_id)`, which also clears `needs_reauth`/`reauth_reason` on success), then **redirects the browser** (HTTP 302) to:
```
{APP_URL}/{workspace_slug}/settings?tab=accounts&connected={platform}
```
or on error:
```
{APP_URL}/{workspace_slug}/settings?tab=accounts&error=<url-encoded message>
```
or, if `state` itself couldn't be parsed at all:
```
{APP_URL}/?error=<url-encoded message>
```
This route returns no JSON body — it's a redirect, meant to be hit by the browser, not called directly by API clients.

---

### 3.4 `DELETE /api/in/workspaces/in/:id/connections/:connectionId`
Caller must be `admin`/`owner`. **Soft-deletes** (`is_deleted = true`) — it attempts the update against all three connection tables (`linkedin_connections`, `meta_connections`, `x_connections`) with the same `connectionId`, ignoring errors, so it will silently no-op if the ID doesn't exist in any of them.

**Response (200):**
```json
{ "status": true, "message": "Platform disconnected" }
```
### 3.5 Media Upload & Processing

**⚠️ New Workflow:** Media is now uploaded *before* post creation. The backend processes the media asynchronously based on the selected platforms and publish intent (instant vs. scheduled), ensuring optimal formatting (e.g., 9:16 vertical for TikTok/Reels, 16:9 horizontal for LinkedIn) without blocking the user.

#### `POST /api/in/media/upload`
Uploads a media file, saves it locally, and fans out platform-specific FFmpeg processing jobs in the background.

**Auth:** `Authorization` + `X-Session-Id`

**Query params:**
| Param | Required | Notes |
|---|---|---|
| `workspace_id` | ✅ | UUID of the workspace |
| `priority` | optional | `high` (instant publish), `normal` (default), or `low` (scheduled/draft). Determines queue priority. |

**Form Data (`multipart/form-data`):**
| Field | Required | Notes |
|---|---|---|
| `file` | ✅ | The media file (`.jpg`, `.jpeg`, `.png`, `.gif`, `.webp`, `.mp4`, `.mov`, `.m4v`, `.avi`, `.webm`) |
| `platforms` | ✅ | Comma-separated list of target platforms (e.g., `linkedin,meta_ig,tiktok`). The backend will fan out a specific transcoding job for *each* platform to ensure correct aspect ratios. |
| `media_ids` | ✅ | Comma-separated list of frontend-generated UUIDs (one for each platform). E.g., `uuid-1,uuid-2`. |

**Response (201):**
```json
{
  "url": "https://api.renisocial.com/media/uploads/ws-123/abc123.mp4",
  "media_ids": ["uuid-1", "uuid-2"],
  "filename": "abc123.mp4",
  "size": 1048576,
  "mime_type": "video/mp4"
}

---

## 4. Posts & Publishing

All routes require `Authorization` + `X-Session-Id`. Responses are **raw `fiber.Map`** (Shape C — no `status`/`message` wrapper), except errors, which are `{"error": "..."}`.

**⚠️ Every route below now performs an explicit workspace-membership check** (`workspace.GetWorkspace(ctx, workspaceID, userID)`) before doing anything else, and returns:
```json
{ "error": "access denied to workspace" }
```
with HTTP **403** if the caller isn't a member of the workspace the post belongs to. This applies to Create, List, Get, Update, Delete, Publish, Schedule, and Results — an earlier revision of this document didn't call this out.

### 4.1 `POST /api/in/posts`
Create a draft or scheduled post.

**Body:**
```json
{
  "workspace_id": "8b2e...-uuid",
  "content": "Hello world from Reni Social! 🚀",
  "media_ids": ["uuid-1", "uuid-2"],
  "media_urls": [],
  "platforms": ["linkedin", "x"],
  "article_url": "",
  "article_title": "",
  "scheduled_at": null
}
```
- `platforms` values: `linkedin`, `meta_ig`, `meta_fb`, `x`, `tiktok`
- `media_ids`: Array of frontend-generated UUIDs returned from the `/api/in/media/upload` endpoint. The backend uses these to link the pre-processed media to the post.
- `scheduled_at`: ISO 8601 string in the future → status becomes `scheduled`; `null` → `draft`
- Every platform in `platforms` is validated against the post's content via `Platform.ValidateForPost`:
  - `article_url` non-empty is rejected (400) for any platform that doesn't support link/article shares (`meta_ig`, `tiktok`) — `linkedin`, `meta_fb`, `x` do support it.
  - Platforms that require media (`meta_ig`, `tiktok`) are rejected (400, `"<platform> requires at least one media file"`) if `media_urls` is empty.
  - `tiktok` additionally requires the media to actually be a video (matched by file extension: `.mp4`, `.mov`, `.m4v`, `.avi`, `.webm`) — otherwise 400 `"tiktok requires a video file (e.g., .mp4, .mov)"`.

**Response (201):**
```json
{
  "post": {
    "id": "post-uuid",
    "workspace_id": "8b2e...-uuid",
    "author_id": "b3f1...-uuid",
    "content": "Hello world from Reni Social! 🚀",
  "media_ids": ["uuid-1", "uuid-2"],
    "media_urls": [],
    "platforms": ["linkedin", "x"],
    "article_url": "",
    "article_title": "",
    "status": "draft",
    "scheduled_at": null,
    "published_at": null,
    "fail_reason": "",
    "created_at": "2026-08-07T10:00:00Z",
    "updated_at": "2026-08-07T10:00:00Z",
    "current_publish_attempt_id": null,
    "retry_count": 0,
    "max_retries": 3
  }
}
```
✅ **Corrections from an earlier revision of this document, both from the same underlying fix:**
1. Every field is now `snake_case` (see [Response Shape Quirks](#response-shape-quirks-read-this-first)) — earlier revisions showed `ID`, `WorkspaceID`, `Content`, etc.
2. `max_retries` correctly shows `3` (the DB default), not `0`. `CreatePost` now re-fetches the row via `GetPost` immediately after `INSERT` instead of returning the in-memory struct it built for the insert — so this response is a genuine round-trip from Postgres, not a manually-assembled object, and both `retry_count`/`max_retries` reflect the real row.

**Failure (400):** missing/invalid `workspace_id`, empty `content`, no `platforms`, `scheduled_at` in the past, or any of the per-platform validation failures above.
**Failure (403):** caller isn't a member of `workspace_id`.

---

### 4.2 `GET /api/in/posts?workspace_id=&status=&page=&limit=`
**Query params:**
| Param | Required | Notes |
|---|---|---|
| `workspace_id` | ✅ | 400 if missing |
| `status` | optional | `draft`, `scheduled`, `publishing`, `published`, `failed` |
| `page` | optional | default `0` |
| `limit` | optional | default `20`, hard-capped at `100` |

**Response (200):**
```json
{
  "posts": [ { "...": "domain.Post objects, snake_case (see 4.1)" } ],
  "page": 0,
  "limit": 20
}
```

---

### 4.3 `GET /api/in/posts/in/:id?workspace_id=`
**Response (200):** `{ "post": { ...domain.Post, snake_case... } }`
**Failure (404):** `{ "error": "post not found" }`

---

### 4.4 `PUT /api/in/posts/in/:id?workspace_id=`
Partial update — every field in the body except `media_urls` is a pointer/optional; only provided fields are changed. Only `draft`/`scheduled` posts can be edited (`published`/`publishing` return 400 `"cannot edit a post that has already been published"`).

**Body:**
```json
{
  "content": "Updated post content!",
  "media_urls": [],
  "platforms": ["linkedin"],
  "article_url": null,
  "article_title": null,
  "scheduled_at": null
}
```

Behavior details worth knowing:
- `media_urls` is a plain `[]string`, not a pointer, so there is no way to distinguish "omit this field" from "explicitly send an empty array" — sending the key at all with any value (including `[]`) overwrites the stored media URLs; omitting the key entirely leaves them unchanged.
- `platforms`, if provided, must be non-empty (`400` otherwise) and replaces the post's platform list outright.
- `scheduled_at` uses a tri-state wrapper (`NullableTime`) to distinguish "key omitted" (`IsSet == false`, no change) from "explicit `null`" from "a real timestamp":
  - **explicit `null`** → the post is **unscheduled**: `scheduled_at` is cleared, and if the post's current status was `scheduled` it reverts to `draft`. *(Not documented in an earlier revision of this doc.)*
  - **a future timestamp** → `scheduled_at` is set and `status` becomes `scheduled`.
  - **a past timestamp** → `400 "scheduled_at must be in the future"`.
- After any successful field change, every platform on the (possibly updated) post is re-validated with `Platform.ValidateForPost` against the (possibly updated) `article_url`/`media_urls` — the same rules as `POST /api/in/posts` in §4.1.
- **Any successful update resets `retry_count` to `0` and `fail_reason` to `""`** — the reasoning in the code comment is that a manual edit represents deliberate user intervention and should get a clean publishing lifecycle. *(Not documented in an earlier revision of this doc.)*

**Response (200):** `{ "post": { ...updated domain.Post, snake_case... } }`
**Failure (403):** caller isn't a member of `workspace_id`.

---

### 4.5 `DELETE /api/in/posts/in/:id?workspace_id=`
Only allowed if current status is **not** `publishing` or `published`.

**Response (200):** `{ "message": "post deleted" }`
**Failure (400):** `{ "error": "post not found or cannot be deleted in its current state" }`

---

### 4.6 `POST /api/in/posts/in/:id/publish?workspace_id=`
Triggers **asynchronous** publishing (see [workflow section](#async-publishing-workflow-walkthrough)). Fails if already `published`/`publishing`.

**⚠️ New pre-flight credit check, not present in an earlier revision of this document:** if `post.Platforms` includes `x`, the handler checks the post author's wallet balance (`billing.GetWalletBalance`) *before* enqueueing anything. If the balance is below `config.C.XPostCreditCost` (default **5** credits), the request is rejected outright:

**Failure (402 Payment Required):**
```json
{
  "error": "Insufficient X API credits. Please top up your wallet to publish to X.",
  "current_balance": 2,
  "required": 5
}
```
This is a *pre-flight* check only — it does not itself deduct credits. The actual deduction happens per-platform inside the background worker at publish time (see §6 and the workflow walkthrough) and can independently fail the `x` platform's publish result even if this pre-flight check passed (e.g. a race between two publishes draining the balance in between).

**Body:** none
**Response (200):**
```json
{
  "message": "publishing_initiated",
  "attempt_id": "8f4e2d1c-9b3a-4e5f-8c7d-6e5f4a3b2c1d"
}
```
Save `attempt_id` and start polling 4.8.
**Failure (403):** caller isn't a member of `workspace_id`.

---

### 4.7 `POST /api/in/posts/in/:id/schedule?workspace_id=`
**Body:**
```json
{ "scheduled_at": "2026-12-31T23:59:59Z", "platforms": ["linkedin", "meta_ig"] }
```

✅ **Correction from an earlier revision of this document** — the `platforms` field is **no longer** silently ignored. Current behavior (`SchedulePost` in `internal/post/service.go`):
- `platforms` **omitted** from the body → the post's existing platforms are left unchanged; only `scheduled_at`/`status` are updated (and this is only allowed when the post is currently `draft`/`scheduled`/`failed`).
- `platforms` present but an **empty array** → `400 "platforms cannot be empty"`.
- `platforms` present and **non-empty** → the post's `platforms` list is **replaced** with the given value (subject to the same `ValidateForPost` checks as §4.1), and this path is only allowed when the post is not already `published`/`publishing`.
- Either way, a successful call also resets `retry_count` to `0` and `fail_reason` to `""`, and sets `status` to `scheduled`.

Only `draft`/`scheduled`/`failed` posts can be (re)scheduled this way.

**Response (200):**
```json
{ "message": "post scheduled", "scheduled_at": "2026-12-31T23:59:59Z" }
```
**Failure (400):** `scheduled_at` missing/zero, in the past, or `platforms: []`.
**Failure (403):** caller isn't a member of `workspace_id`.

---

### 4.8 `GET /api/in/posts/in/:id/results?attempt_id=&workspace_id=`
Polls per-platform publish status.

✅ **Correction from an earlier revision of this document:** `workspace_id` is now a **required** query parameter (the handler returns `400 "workspace_id is required"` without it, then does the same membership check as every other post route). `attempt_id` remains optional but strongly recommended — without it, all historical results for the post are returned, not just the latest attempt.

**⚠️ New response field, not present in an earlier revision of this document:** before consulting the cache or Postgres, the handler compares the `attempt_id` you're polling against the post's current `current_publish_attempt_id` in the DB. If they differ (which happens when a retry has moved the post on to a new attempt — see the workflow section), it short-circuits with:
```json
{
  "results": [],
  "attempt_superseded": true,
  "new_attempt_id": "9a1b2c3d-...-newer-attempt"
}
```
Frontends should check for `attempt_superseded` and, if `true`, switch to polling `new_attempt_id` instead of continuing to poll the stale one (which will otherwise appear to hang forever).

**Fast path:** reads from a Redis cache (`results:<postID>:<attemptID>`, 2h TTL) written by the worker as results land.
**Fallback:** queries Postgres `publish_results` directly if the cache misses.

**Response (200) — from cache:**
```json
{
  "results": [
    {
      "id": "a1b2c3d4-...",
      "platform": "linkedin",
      "status": "published",
      "platform_post_id": "urn:li:share:7123456789",
      "platform_post_url": "https://www.linkedin.com/feed/update/urn:li:share:7123456789",
      "container_id": "",
      "fail_reason": "",
      "created_at": "2026-08-08T10:00:00Z",
      "updated_at": "2026-08-08T10:00:05Z"
    },
    {
      "id": "b2c3d4e5-...",
      "platform": "meta_ig",
      "status": "processing",
      "platform_post_id": "",
      "platform_post_url": "",
      "container_id": "17954998877665544",
      "fail_reason": "",
      "created_at": "2026-08-08T10:00:00Z",
      "updated_at": "2026-08-08T10:00:00Z"
    },
    {
      "id": "c3d4e5f6-...",
      "platform": "x",
      "status": "failed",
      "platform_post_id": "",
      "platform_post_url": "",
      "container_id": "",
      "fail_reason": "x: tweet failed (403): Forbidden",
      "created_at": "2026-08-08T10:00:00Z",
      "updated_at": "2026-08-08T10:00:02Z"
    }
  ]
}
```

**Status values:** `pending`, `processing`, `published`, `failed` (see `scheduler.PublishResultStatus`). `processing` is specifically used for Instagram video containers awaiting the finalizer worker.

**Recommended polling logic (frontend):**
- Poll every ~2s
- If `attempt_superseded` is `true`, switch to polling `new_attempt_id`
- Map `processing` → spinner, `published` → checkmark + `platform_post_url` link, `failed` → red X + `fail_reason`
- Stop when **every** entry in `results` is `published` or `failed`

**Failure (400):** `workspace_id` missing.
**Failure (403):** caller isn't a member of `workspace_id`.

---

## 5. X (Twitter) Read API

All require `Authorization` + `X-Session-Id`, and all return `types.APIResponse[any]` (**Shape A**), where `data` is a raw `map[string]any` decoded from X API v2's response (field shape depends entirely on what you query for). Every route resolves the workspace's X connection first via `GetXConnection` (workspace-scoped; explicit `connection_id` query param picks a specific account, otherwise the first-created connection is used).

### 5.1 `GET /api/in/workspaces/in/:id/x/tweets/in/:tweetId?connection_id=`
Passes through **any** query params you send as `tweet.fields`/`expansions`/etc. to X's `GET /2/tweets/:id`.

### 5.2 `GET /api/in/workspaces/in/:id/x/tweets/in/:tweetId/full?connection_id=`
Fixed rich-expansion variant — auto-sets `expansions=attachments.media_keys,attachments.poll_ids,geo.place_id,author_id` plus `tweet.fields`, `media.fields`, `poll.fields`, `place.fields`, `user.fields`. No query params needed.

### 5.3 `GET /api/in/workspaces/in/:id/x/users/in/:userId?connection_id=`
Defaults `user.fields=description,profile_image_url,public_metrics,created_at` if not overridden.

### 5.4 `GET /api/in/workspaces/in/:id/x/spaces/in/:spaceId?connection_id=`
Defaults `space.fields=title,state,created_at,lang`.

### 5.5 `GET /api/in/workspaces/in/:id/x/lists/in/:listId?connection_id=`
Defaults `list.fields=name,description,member_count,follower_count`.

### 5.6 `GET /api/in/workspaces/in/:id/x/connections`
Lists all X accounts connected to the workspace (safe subset — no tokens; this handwritten map never included tokens, unlike §3.1's old bug).

**Response (200):**
```json
{
  "status": true,
  "message": "OK",
  "data": [
    {
      "id": "conn-uuid",
      "account_id": "12345",
      "account_name": "myhandle",
      "expires_at": null,
      "created_at": "2026-08-01T00:00:00Z"
    }
  ]
}
```

**Common failures for all 5.x routes:**
- `403` if caller isn't a workspace member
- `404` if no X connection exists for the workspace (or the given `connection_id`)
- `502` if the upstream X API call itself fails

**On credits:** unlike publishing to X (§4.6), none of these read endpoints currently deduct wallet credits, even though `config.C.XReadCreditCost` (default `1`) is defined for exactly this purpose — it is not yet wired into any handler in the current codebase. Don't rely on it being enforced.

---

## 6. Billing & Credits

**⚠️ Not documented in an earlier revision of this document — a wallet/credit system now gates X publishing.**

### 6.1 `POST /api/webhooks/payment`
**Public** (no auth headers — verified instead by HMAC signature). Payment-gateway webhook (Paystack-shaped payload) that tops up a user's credit wallet on a successful charge.

**Headers:**
```
x-paystack-signature: sha512=<hex-encoded HMAC-SHA512 of the raw request body, keyed by PAYSTACK_SECRET_KEY>
```
Requests with a missing or invalid signature are rejected with `401` and never processed.

**Body (subset of fields actually read):**
```json
{
  "event": "charge.success",
  "data": {
    "reference": "unique-gateway-transaction-id",
    "amount": 5000,
    "metadata": { "user_id": "b3f1...-uuid" }
  }
}
```
Only `event == "charge.success"` is processed; other event types are accepted (`200`) and ignored. `amount` is assumed to be in the gateway's minor unit (e.g. Kobo) and is divided by 100 to get the credit amount (`1 credit == 1 unit of currency` in the current implementation). Credit-adding is idempotent on the DB's `UNIQUE` constraint over `transactions.reference`, so gateway retries of the same webhook don't double-credit the wallet.

**Response:** always `200 OK` regardless of outcome (by design — returning a non-2xx makes Paystack/Stripe-style gateways aggressively retry, which would risk duplicate processing even with the idempotency guard in place).

### 6.2 Credit costs (not separately exposed via any GET endpoint)
| Config key | Default | Enforced where |
|---|---|---|
| `XPostCreditCost` | `5` | Pre-flight check in `POST /api/in/posts/in/:id/publish` (§4.6, `402` if insufficient) **and** deducted for real inside the background worker immediately before each `x` platform publish attempt (`billing.DeductCredits`, transaction type `x_post_publish`). If the deduction itself fails (e.g. balance dropped in the meantime), that platform's `publish_results` row is marked `failed` with reason `"Insufficient X API credits. Please top up your wallet."` — this can happen even after the pre-flight check passed. |
| `XReadCreditCost` | `1` | Defined in config but **not currently enforced anywhere** — none of the §5 X read routes deduct credits in the current code. |

There is no HTTP endpoint to read a wallet balance directly — `billing.GetWalletBalance` is only called internally by the publish pre-flight check in §4.6. If you need to surface a balance in a UI, you'll currently have to derive it from the `402` error body's `current_balance` field, or add a dedicated endpoint.

---

## 7. System

### 7.1 `GET /health`
**Auth:** none
**Response (200):** `{ "status": "healthy" }`

---

## Async Publishing Workflow Walkthrough

```
User clicks "Publish"
       │
       ▼
POST /api/in/posts/in/:id/publish?workspace_id=...
       │  - pre-flight: if platforms includes "x", checks wallet balance ≥ XPostCreditCost, else 402
       │  - post.status → "publishing"
       │  - generates attempt_id, stores as current_publish_attempt_id
       │  - LPUSHes post_id onto Redis queue (config.ScheduledPostsQueue)
       │  - returns attempt_id IMMEDIATELY (does not wait on any platform)
       ▼
Background worker (cmd/worker) — Consumer
       │  - BRPOPs post_id off the queue (bounded to config.MaxConcurrentPublishes, default 25, in-flight goroutines)
       │  - fetches post + all connected platform credentials
       │  - skips any platform whose result is already "published" or "processing" from a prior attempt on
       │    this same post (avoids duplicate posts on retry, e.g. an IG video container already submitted)
       │  - computes a per-post publish timeout: base is PublishTimeoutMinutes (default 5m); if any media URL
       │    looks like a video, it HEAD-requests the URL to check Content-Length and extends the timeout to
       │    20m for videos over ~20MB (or if the size can't be determined at all — safe default)
       │  - fans out Publish() calls to each platform CONCURRENTLY, each with its own timeout derived above
       │  - for the "x" platform specifically, deducts XPostCreditCost credits from the author's wallet
       │    immediately before calling Publish(); if the deduction fails, that platform's result is marked
       │    "failed" without ever calling the X API
       │  - writes one publish_results row per platform as each completes
       │    (status: published / failed / processing)
       │  - "processing" happens specifically for IG video containers
       │    (Publish() returns Pending=true + a container_id)
       ▼
Frontend polls GET /api/in/posts/in/:id/results?attempt_id=...&workspace_id=...
       │  - every ~2s
       │  - if the response says attempt_superseded: true, switch to polling new_attempt_id instead
       │  - reads from Redis cache first (2h TTL), Postgres fallback
       ▼
For any IG video still "processing":
       │
       ▼
IG Container Finalizer (separate background loop, polls every 5s)
       │  - atomically claims pending "processing" rows (2-minute stale-claim window, so a crashed
       │    finalizer cycle doesn't permanently starve a row)
       │  - checks Instagram container status_code via Graph API
       │  - once FINISHED: calls /media_publish, updates that publish_results row to "published"
       │    (or "failed" on error/expiry)
       │  - containers still stuck "processing" after 1 hour are force-marked "failed" with reason
       │    "Instagram container timed out after 1 hour of processing"
       ▼
FinalizePostStatus (runs after every publish_results write)
       │  - once ALL platforms in the attempt are done (published/failed):
       │      - all succeeded → post.status = "published"
       │      - any failed → scheduleRetry() decides what happens next (see Retries below)
```

### Retries — materially different from a simple "retry N times" model

⚠️ **This section is substantially rewritten from an earlier revision of this document, which described automatic retries uniformly for every failed post.** In the current code (`internal/scheduler/retry.go`), **only posts that were originally scheduled** (i.e. have a non-null `scheduled_at`) are eligible for automatic retry at all:

- **Instant "Publish Now" posts** (`scheduled_at` was never set) get **no automatic retry**. If any platform fails, `FinalizePostStatus` marks the post `failed` immediately — full stop. Separately, if an instant-publish post is somehow left stuck in `publishing` for longer than `InstantPublishTimeoutMinutes` (default **5** minutes) with no platform actively `processing`, a fast-fail sweep in the poller (`failInstantPublishTimeouts`, runs every 5s alongside the scheduler poll) force-marks it `failed` with reason `"Instant publish timeout: stuck in publishing for >5 minutes"`.
- **Scheduled posts** get real retries: every post has `retry_count` (starts at `0`) and `max_retries` (DB default `3`). On failure, `scheduleRetry` checks `retry_count < max_retries`; if so, it generates a **new** `attempt_id`, bumps `retry_count`, and schedules the retry with **exponential backoff** — `10s, 20s, 40s, ...` capped at **5 minutes** — by `ZADD`-ing the post ID into a Redis sorted set (`config.RetryPostsQueue`) with the computed ready-time as its score. A separate **Retry Poller** (`StartRetryPoller`, polls every 5s) pulls anything past its ready-time out of that sorted set and pushes it onto the main `ScheduledPostsQueue` for the Consumer to pick up again. If you're polling `GET .../results`, watch for the `attempt_superseded` flag (§4.8) — the `attempt_id` you were polling has moved on.
- **Zombie recovery** (a scheduled post stuck in `publishing` because the worker crashed mid-publish) is handled by the same poller that finds due scheduled posts (`pollAndEnqueue`, ticks every 5s — **not** every 30s as an earlier revision of this document claimed; `config.SchedulerPollIntervalSecs` is defined but the ticker interval is hardcoded to 5s and does not actually read that config value). A scheduled post stuck in `publishing` for more than `ZombiePostTimeoutMinutes` (configured default **25** minutes across all three environments; the query itself falls back to 15 minutes only if that config value is ever unset/≤0) with no outstanding `processing` publish_results row is transitioned **directly to `failed`** (not silently retried again) with reason `"Post exceeded maximum processing time (15 minutes) and was marked as failed"` (the message text is a static 15-minute string left over from an earlier default and does not reflect the actually-configured 25-minute threshold — cosmetic, but worth knowing if you're surfacing this string to users). The workspace owner is emailed about the failure (`notifyZombieFailures`) if their email is on file.
- **Manual edits reset the slate:** any successful `PUT`/`schedule` call on a post zeroes out `retry_count` and `fail_reason` (§4.4, §4.7), so a user editing and re-scheduling a previously-failed post effectively gets a fresh set of retry attempts.

---

## Domain Model Reference

### `domain.Platform` (string enum)
| Value | Meaning |
|---|---|
| `linkedin` | LinkedIn |
| `meta_ig` | Instagram (via Meta) |
| `meta_fb` | Facebook Page (via Meta) |
| `x` | X / Twitter |
| `tiktok` | TikTok |

### `domain.PostStatus` (string enum)
| Value | Meaning |
|---|---|
| `draft` | Not scheduled, not published |
| `scheduled` | Will be picked up by the poller at `scheduled_at` |
| `publishing` | Currently locked by the worker (in-flight) |
| `published` | All requested platforms succeeded |
| `failed` | At least one platform failed — see Retries above for who gets a second try |

### `scheduler.PublishResultStatus` (per-platform, string enum)
`pending`, `processing`, `published`, `failed`

### `domain.WorkspaceRole` (string enum)
`owner`, `admin`, `member`

### `domain.PlatformConnection` — reauth fields
Every connection row now carries `needs_reauth` (bool) and `reauth_reason` (string), maintained by two background workers:
- **Meta (Instagram):** tokens are auto-refreshed before expiry; on a permanent auth failure (Meta error codes `190`/`102`) the connection is flagged `needs_reauth = true` and the owner is emailed.
- **LinkedIn:** has no refresh token (`offline_access` is deliberately excluded from the OAuth scope), so tokens hard-expire ~60 days after issuance; a monitor proactively flags connections expiring within `LinkedInExpiryWarnBeforeDays` (default 7) and emails the owner.

Poll `GET /api/in/workspaces/in/:id/connections/reauth-status` (§2.8) to surface these without needing to inspect the full connections list.

### Per-platform publishing capability notes (from the platform adapters)
| Platform | Text-only | Requires media | Notes |
|---|---|---|---|
| LinkedIn | ✅ | optional | Supports text, article/link (preview card), image, video shares |
| Meta / Instagram | ❌ | ✅ | No text-only posts; images publish near-instantly, videos go through the container-finalizer async path |
| Meta / Facebook Page | ✅ | optional | Appends `article_url` to the message text if not already present, so FB can generate a preview card |
| X / Twitter | ✅ | n/a in current adapter | `Publish()` here only sends text (media upload path exists in the OAuth1 client but isn't wired into `Publish()`); each publish attempt costs `XPostCreditCost` credits from the author's wallet |
| TikTok | ❌ | ✅ (video only) | Uses `PULL_FROM_URL`; unaudited apps are hard-locked to `privacy_level: SELF_ONLY` — can't post publicly until TikTok app review passes |

---

## Error Handling

There's no single global error shape. In practice:

- **Workspace/Auth handlers** (`types.APIResponse[any]`): `{"status": false, "message": "<reason>"}`
- **Post handlers** (raw `fiber.Map`): `{"error": "<reason>"}` — the publish pre-flight credit check additionally includes `current_balance`/`required` (§4.6)
- **Registration/signin** (Reni Central passthrough): `{"status": false, "message": "<reason>", "data": {}, "http_status": <code>}`
- **Payment webhook** (§6.1): always `200`, regardless of internal outcome

HTTP status codes used across the API: `200`, `201`, `400`, `401`, `402`, `403`, `404`, `429`, `500`, `502` (X API upstream failures). There is no dedicated `422` usage — validation failures return `400`. `402 Payment Required` is new (§4.6) and specific to the X-publishing credit gate. `429` comes from the rate limiters (see [Rate limiting](#rate-limiting)).

---

## Quick-Reference Endpoint Table

| Method | Path | Auth | Category |
|---|---|---|---|
| GET | `/health` | none | System |
| GET | `/api/workspaces/connect/:platform/callback` | none (browser redirect) | Connections |
| POST | `/api/webhooks/payment` | none (HMAC-signed) | Billing |
| POST | `/api/auth/register/email` | none | Auth |
| POST | `/api/auth/register/email/activate` | none | Auth |
| POST | `/api/auth/register/complete` | none | Auth |
| POST | `/api/auth/signin` | none | Auth |
| POST | `/api/in/auth/signout` | full | Auth |
| GET | `/api/in/auth/me` | full | Auth |
| POST | `/api/in/workspaces` | full | Workspaces |
| GET | `/api/in/workspaces` | full | Workspaces |
| GET | `/api/in/workspaces/in/:id` | full | Workspaces |
| GET | `/api/in/workspaces/in/slug/:slug` | full | Workspaces |
| POST | `/api/in/workspaces/in/:id/members` | full (admin/owner) | Workspaces |
| GET | `/api/in/workspaces/in/:id/members` | full | Workspaces |
| DELETE | `/api/in/workspaces/in/:id/members/in/:userId` | full (admin/owner) | Workspaces |
| GET | `/api/in/workspaces/in/:id/connections/reauth-status` | full | Workspaces |
| GET | `/api/in/workspaces/in/:id/connections` | full | Connections |
| POST | `/api/in/workspaces/in/:id/connect/in/:platform` | full (admin/owner) | Connections |
| DELETE | `/api/in/workspaces/in/:id/connections/:connectionId` | full (admin/owner) | Connections |
| GET | `/api/in/workspaces/in/:id/x/connections` | full | X Read |
| GET | `/api/in/workspaces/in/:id/x/tweets/in/:tweetId` | full | X Read |
| GET | `/api/in/workspaces/in/:id/x/tweets/in/:tweetId/full` | full | X Read |
| GET | `/api/in/workspaces/in/:id/x/users/in/:userId` | full | X Read |
| GET | `/api/in/workspaces/in/:id/x/spaces/in/:spaceId` | full | X Read |
| GET | `/api/in/workspaces/in/:id/x/lists/in/:listId` | full | X Read |
| POST | `/api/in/posts` | full | Posts |
| GET | `/api/in/posts` | full | Posts |
| GET | `/api/in/posts/in/:id` | full | Posts |
| PUT | `/api/in/posts/in/:id` | full | Posts |
| DELETE | `/api/in/posts/in/:id` | full | Posts |
| POST | `/api/in/posts/in/:id/publish` | full | Posts |
| POST | `/api/in/posts/in/:id/schedule` | full | Posts |
| GET | `/api/in/posts/in/:id/results` | full | Posts |
| POST | `/api/in/media/upload` | full | Media |

*"full" = `Authorization: Bearer <token>` + `X-Session-Id: <session>`*

---

## Changelog vs previous revision of this document

This revision was generated by re-reading the actual current source (`internal/`, `pkg/`, `cmd/`, `schema/`) end-to-end. Substantive corrections made:

1. **Response casing:** `domain.Post`, `domain.Workspace`, and `domain.PlatformConnection` now carry full `json` struct tags and serialize as snake_case everywhere — the "mixed PascalCase/snake_case" behavior described previously no longer exists. Every JSON example in this document was updated accordingly.
2. **Token leak closed:** `domain.PlatformConnection.AccessToken`/`RefreshToken` are now `json:"-"` and never appear in `GET /api/in/workspaces/in/:id/connections` or anywhere else. The security warning from the previous revision no longer applies.
3. **`POST /api/in/posts` response quirk fixed:** `CreatePost` now re-fetches the row from Postgres after insert, so `max_retries` correctly shows `3` instead of the stale `0`.
4. **`POST /api/in/posts/in/:id/schedule` `platforms` field is now honored** — previously documented as parsed-but-ignored; it now actually replaces the post's platforms when non-empty.
5. **New wallet/credit system** gates publishing to `x` (§4.6, §6) with a `402` pre-flight check and per-attempt deduction inside the worker; a new public payment webhook (§6.1) tops up balances.
6. **`GET /api/in/posts/in/:id/results` now requires `workspace_id`** and can return `attempt_superseded`/`new_attempt_id` when the polled attempt has been superseded by a retry.
7. **Every post route now enforces workspace membership** with an explicit `403`, not previously documented.
8. **Retry model rewritten:** retries only apply to originally-scheduled posts, use exponential backoff via a delayed Redis queue, and zombie recovery/instant-publish timeouts now fail directly instead of quietly retrying forever. The scheduler poll interval is actually hardcoded to 5s, not the previously-documented 30s.
9. **New endpoint documented:** `GET /api/in/workspaces/in/:id/connections/reauth-status`.
10. **`POST /api/in/workspaces/in/:id/connect/in/:platform` `?force=true`** and the LinkedIn/Meta credential pre-flight checks are now documented.
11. **`/api/auth/*` public-route ambiguity resolved:** confirmed genuinely public (not nested under `AuthMiddleware`); the `401` in the routing test comes from empty-credential validation, not middleware.
12. **Rate limiting documented** for the first time (global 300/min, auth 50/min, general API 300/min).
