# Reni Social API — Documentation

Generated from `internal/routes/routes.go`, handlers, and domain/service types in the `reni-social-go` codebase. Every example below is derived directly from the Go structs and handler logic — including a few JSON key-casing quirks that are easy to miss (flagged inline).

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
8. [System](#6-system)
9. [Async Publishing Workflow](#async-publishing-workflow-walkthrough)
10. [Domain Model Reference](#domain-model-reference)
11. [Error Handling](#error-handling)

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

**Public routes** (no auth required):
- `GET /health`
- `GET /api/workspaces/connect/:platform/callback`
- `POST /api/auth/register/email`
- `POST /api/auth/register/email/activate`
- `POST /api/auth/register/complete`
- `POST /api/auth/signin`

⚠️ Note: in `routes.go`, `/api/auth/signin` and the three registration routes are registered directly on `app` (public), but the routes test suite (`routes_test.go`, `TestRoutes_PublicAuthRoutesSkipMiddleware`) explicitly asserts that hitting `/api/auth/signin` **without** an Authorization header returns `401` — meaning at some point these were nested under the `AuthMiddleware` group and that behavior may still be in effect depending on which version of `routes.go` is deployed. Test against your running instance to confirm current behavior; the version of `routes.go` shown in this repo snapshot registers them as public.

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

### JSON key casing — important
Several domain structs (`domain.Post`, `domain.Workspace`, `domain.PlatformConnection`) have **no `json` struct tags** on most fields. Go's `encoding/json` falls back to the **exact Go field name** when no tag exists, so these come back **PascalCase**, not `snake_case`:

- `domain.Post` → `ID`, `WorkspaceID`, `AuthorID`, `Content`, `MediaURLs`, `Platforms`, `ArticleURL`, `ArticleTitle`, `Status`, `ScheduledAt`, `PublishedAt`, `FailReason`, `CreatedAt`, `UpdatedAt`, `CurrentPublishAttemptID` — but `RetryCount`/`MaxRetries` **are** tagged, so those two come back as `retry_count` / `max_retries`. Mixed casing in the same object.
- `domain.Workspace` → `ID`, `OwnerID`, `Name`, `Slug`, `CreatedAt` (all PascalCase).
- `domain.PlatformConnection` → `ID`, `WorkspaceID`, `Platform`, `AccountID`, `AccountName`, `AccessToken`, `RefreshToken`, `ExpiresAt`, `CreatedAt`, `IsDeleted` (all PascalCase).
- `domain.User` **does** have full `json` tags (`id`, `email`, `first_name`, etc. — snake_case), so `/api/in/auth/me` returns clean snake_case.

⚠️ **Security note found in code:** `GET /api/in/workspaces/in/:id/connections` (handler `Connections` → service `ListConnections`) returns the full `domain.PlatformConnection` struct as-is, which includes `AccessToken` and `RefreshToken` in the JSON response. There's no field stripping before serialization. Worth confirming this is intentional before shipping a frontend that logs or caches this response.

Manually-built `fiber.Map`/`map[string]any` responses (post list/get, members list, X connections list) **do** use snake_case since those keys are typed by hand in the handler.

---

## 1. Auth & Onboarding

### 1.1 `POST /api/auth/register/email`
Step 1 of 3-step registration — sends a verification code to an email via Reni Central.

**Auth:** none
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

**Auth:** none
**Request body:**
```json
{ "mail": "user@example.com", "authCode": "123456" }
```
**Response:** same `Shape B` envelope as above.

---

### 1.3 `POST /api/auth/register/complete`
Step 3 — completes registration. On success, the backend also creates a **local** user row (mirrored from Reni Central) and issues a local Redis session, overwriting `data.token` with that local session id before returning.

**Auth:** none
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
    "pword": "Password123!",
    "username": "johndoe99",
    "fullname": "John Doe",
    "token": "<local session token — use as X-Session-Id going forward>"
  },
  "http_status": 200
}
```
Note: `pword` is echoed back in the response `data` (see `CompleteRegisterData.Pword`) — the code that would blank it out (`// data.Pword = ""`) is commented out in `internal/auth/service.go`.

---

### 1.4 `POST /api/auth/signin`
Logs in via Reni Central, upserts a local user row, and issues a local session.

**Auth:** none
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

**Failure (401):**
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
    "ID": "8b2e...-uuid",
    "OwnerID": "b3f1...-uuid",
    "Name": "My New Workspace",
    "Slug": "my-new-workspace-8b2e6a1c",
    "CreatedAt": "2026-08-07T10:00:00Z"
  }
}
```
(PascalCase — see [key casing note](#response-shape-quirks-read-this-first). Slug format: lowercased/hyphenated name + first 8 chars of the workspace UUID, via `generateSlug`.)

---

### 2.2 `GET /api/in/workspaces`
Lists all workspaces the caller belongs to.

**Response (200):**
```json
{
  "status": true,
  "message": "OK",
  "data": [
    { "ID": "...", "OwnerID": "...", "Name": "Acme Inc", "Slug": "acme-inc-12345678", "CreatedAt": "..." }
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
Valid `role` values: `""` (defaults to `member`), `member`, `admin`, `owner` — **but** the service layer (`InviteMember`) silently downgrades any `owner` request to `member` server-side ("prevent escalation to owner via invite"), so `role: "owner"` in the request will not actually grant ownership.

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
(This one **is** snake_case — hand-built `map[string]any` in `ListMembers`.)

---

### 2.7 `DELETE /api/in/workspaces/in/:id/members/in/:userId`
Caller must be `admin`/`owner`. The workspace `owner` can never be removed (returns 400 `workspace owner cannot be removed`).

**Response (200):**
```json
{ "status": true, "message": "Member removed" }
```

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
      "ID": "...",
      "WorkspaceID": "...",
      "Platform": "linkedin",
      "AccountID": "urn:li:person:12345",
      "AccountName": "Ada Lovelace",
      "AccessToken": "AQV...",
      "RefreshToken": "",
      "ExpiresAt": "2026-10-01T00:00:00Z",
      "CreatedAt": "2026-08-01T00:00:00Z",
      "IsDeleted": false
    }
  ]
}
```
⚠️ As noted above, `AccessToken`/`RefreshToken` are included verbatim.

---

### 3.2 `POST /api/in/workspaces/in/:id/connect/in/:platform`
Caller must be `admin`/`owner`. Kicks off (or short-circuits) OAuth.

**Body:** none

**Behavior:**
1. If a non-expired connection already exists for that platform (even if soft-deleted, it gets revived), the OAuth flow is **skipped**:
   ```json
   { "status": true, "message": "Already connected", "data": { "already_connected": true } }
   ```
2. Otherwise, an `oauth_state` is generated (`<workspace_id>:<16-char random>`), stored in Redis for 10 minutes, and a platform-specific `redirect_url` is returned:
   ```json
   { "status": true, "message": "OK", "data": { "redirect_url": "https://www.linkedin.com/oauth/v2/authorization?..." } }
   ```
   - `linkedin` → LinkedIn OAuth2 consent screen
   - `meta` → Instagram Login flow consent screen (`instagram.com/oauth/authorize`) — **not** the Facebook Login flow
   - `x` → OAuth 1.0a: this call synchronously fetches a request token from X first, so `redirect_url` is the ready-to-open `https://api.twitter.com/oauth/authorize?oauth_token=...` URL.

**Failure (400):** unsupported platform → `{ "status": false, "message": "Unsupported platform: <p>" }`

---

### 3.3 `GET /api/workspaces/connect/:platform/callback`
**Public** (no auth headers). This is where the OAuth provider redirects the browser back to.

**Query params:**
- `state` — the `<workspace_id>:<random>` string issued in 3.2 (X uses its own `oauth_token`/`oauth_verifier` params instead — see below)
- `code` — auth code (LinkedIn / Meta)
- For `x`: `oauth_token`, `oauth_verifier` (standard OAuth 1.0a callback params)

**Behavior:** validates `state` against Redis, exchanges the code for tokens via the relevant platform client, persists the connection (upsert on `(workspace_id, account_id)`), then **redirects the browser** (HTTP 302) to:
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

---

## 4. Posts & Publishing

All routes require `Authorization` + `X-Session-Id`. Responses are **raw `fiber.Map`** (Shape C — no `status`/`message` wrapper), except errors, which are `{"error": "..."}`.

### 4.1 `POST /api/in/posts`
Create a draft or scheduled post.

**Body:**
```json
{
  "workspace_id": "8b2e...-uuid",
  "content": "Hello world from Reni Social! 🚀",
  "media_urls": [],
  "platforms": ["linkedin", "x"],
  "article_url": "",
  "article_title": "",
  "scheduled_at": null
}
```
- `platforms` values: `linkedin`, `meta_ig`, `meta_fb`, `x`, `tiktok`
- `scheduled_at`: ISO 8601 string in the future → status becomes `scheduled`; `null` → `draft`
- `article_url` non-empty is rejected (400) if `platforms` includes `meta_ig` or `tiktok` — those don't support link/article shares.

**Response (201):**
```json
{
  "post": {
    "ID": "post-uuid",
    "WorkspaceID": "8b2e...-uuid",
    "AuthorID": "b3f1...-uuid",
    "Content": "Hello world from Reni Social! 🚀",
    "MediaURLs": [],
    "Platforms": ["linkedin", "x"],
    "ArticleURL": "",
    "ArticleTitle": "",
    "Status": "draft",
    "ScheduledAt": null,
    "PublishedAt": null,
    "FailReason": "",
    "CreatedAt": "2026-08-07T10:00:00Z",
    "UpdatedAt": "2026-08-07T10:00:00Z",
    "CurrentPublishAttemptID": null,
    "retry_count": 0,
    "max_retries": 0
  }
}
```
(Note `retry_count`/`max_retries` are snake_case; everything else on this object is PascalCase — see the key-casing note. Also, since this particular object is built manually in `CreatePost` rather than round-tripped from the DB, `retry_count`/`max_retries` will actually be `0`/`0` here even though the DB defaults them to `0`/`3` — the in-memory struct returned to the client was never re-fetched after insert.)

**Failure (400):** missing/invalid `workspace_id`, empty `content`, no `platforms`, `scheduled_at` in the past, or article/platform mismatch.

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
  "posts": [ { "...": "domain.Post objects, same PascalCase shape as 4.1" } ],
  "page": 0,
  "limit": 20
}
```

---

### 4.3 `GET /api/in/posts/in/:id?workspace_id=`
**Response (200):** `{ "post": { ...domain.Post... } }`
**Failure (404):** `{ "error": "post not found" }`

---

### 4.4 `PUT /api/in/posts/in/:id?workspace_id=`
Partial update — every field in the body is a pointer/optional; only provided fields are changed. Only `draft`/`scheduled` posts can be edited (`published`/`publishing` return 400).

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
Note: setting `scheduled_at` to a non-null value also flips `status` to `scheduled` as a side effect.

**Response (200):** `{ "post": { ...updated domain.Post... } }`

---

### 4.5 `DELETE /api/in/posts/in/:id?workspace_id=`
Only allowed if current status is **not** `publishing` or `published`.

**Response (200):** `{ "message": "post deleted" }`
**Failure (400):** `{ "error": "post not found or cannot be deleted in its current state" }`

---

### 4.6 `POST /api/in/posts/in/:id/publish?workspace_id=`
Triggers **asynchronous** publishing (see [workflow section](#async-publishing-workflow-walkthrough)). Fails if already `published`/`publishing`.

**Body:** none
**Response (200):**
```json
{
  "message": "publishing_initiated",
  "attempt_id": "8f4e2d1c-9b3a-4e5f-8c7d-6e5f4a3b2c1d"
}
```
Save `attempt_id` and start polling 4.8.

---

### 4.7 `POST /api/in/posts/in/:id/schedule?workspace_id=`
**Body:**
```json
{ "scheduled_at": "2026-12-31T23:59:59Z", "platforms": ["linkedin", "meta_ig"] }
```
Note: the `platforms` field in the request body is parsed but **not actually used** by the handler — `SchedulePost` only takes `postID, workspaceID, scheduledAt`, so the platforms on the post itself are unchanged by this call. Only `draft`/`scheduled` posts can be (re)scheduled.

**Response (200):**
```json
{ "message": "post scheduled", "scheduled_at": "2026-12-31T23:59:59Z" }
```
**Failure (400):** `scheduled_at` missing/zero, or in the past.

---

### 4.8 `GET /api/in/posts/in/:id/results?attempt_id=`
Polls per-platform publish status. `attempt_id` is optional but strongly recommended (without it, all historical results for the post are returned, not just the latest attempt).

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
- Map `processing` → spinner, `published` → checkmark + `platform_post_url` link, `failed` → red X + `fail_reason`
- Stop when **every** entry in `results` is `published` or `failed`

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
Lists all X accounts connected to the workspace (safe subset — no tokens).

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

---

## 6. System

### 6.1 `GET /health`
**Auth:** none
**Response (200):** `{ "status": "healthy" }`

---

## Async Publishing Workflow Walkthrough

```
User clicks "Publish"
       │
       ▼
POST /api/in/posts/in/:id/publish?workspace_id=...
       │  - post.status → "publishing"
       │  - generates attempt_id, stores as current_publish_attempt_id
       │  - LPUSHes post_id onto Redis queue (config.ScheduledPostsQueue)
       │  - returns attempt_id IMMEDIATELY (does not wait on any platform)
       ▼
Background worker (cmd/worker) — Consumer
       │  - BRPOPs post_id off the queue
       │  - fetches post + all connected platform credentials
       │  - fans out Publish() calls to each platform CONCURRENTLY,
       │    each with its own timeout (config.PublishTimeoutMinutes)
       │  - writes one publish_results row per platform as each completes
       │    (status: published / failed / processing)
       │  - "processing" happens specifically for IG video containers
       │    (Publish() returns Pending=true + a container_id)
       ▼
Frontend polls GET /api/in/posts/in/:id/results?attempt_id=...
       │  - every ~2s
       │  - reads from Redis cache first (2h TTL), Postgres fallback
       ▼
For any IG video still "processing":
       │
       ▼
IG Container Finalizer (separate background loop, polls every 5s)
       │  - checks Instagram container status_code via Graph API
       │  - once FINISHED: calls /media_publish, updates that
       │    publish_results row to "published" (or "failed" on error/expiry)
       ▼
FinalizePostStatus (runs after every publish_results write)
       │  - once ALL platforms in the attempt are done (published/failed):
       │      - all succeeded → post.status = "published"
       │      - any failed AND retry_count < max_retries →
       │            auto re-enqueues with a NEW attempt_id (retry_count++)
       │      - any failed AND retries exhausted → post.status = "failed"
```

**Retries:** every post has `retry_count` (starts at 0) and `max_retries` (DB default `3`, though note the 4.1 create-response quirk above where the in-memory object briefly shows `0`). Retries are automatic and generate a **new** `attempt_id` each time — if you're polling, watch for the possibility that the `attempt_id` you're polling on stops updating because a retry has moved on to a new one. Safer to re-fetch `GET /api/in/posts/in/:id` and check `CurrentPublishAttemptID` if a poll seems stuck.

**Scheduled posts** follow the same pipeline but are picked up by a separate `StartPoller` loop (every `SchedulerPollIntervalSecs`, default 30s) that finds `status='scheduled' AND scheduled_at <= NOW()` and pushes them onto the same Redis queue — so scheduled and instant publishes share the exact same worker/consumer path.

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
| `failed` | At least one platform failed and retries are exhausted |

### `scheduler.PublishResultStatus` (per-platform, string enum)
`pending`, `processing`, `published`, `failed`

### `domain.WorkspaceRole` (string enum)
`owner`, `admin`, `member`

### Per-platform publishing capability notes (from the platform adapters)
| Platform | Text-only | Requires media | Notes |
|---|---|---|---|
| LinkedIn | ✅ | optional | Supports text, article/link (preview card), image, video shares |
| Meta / Instagram | ❌ | ✅ | No text-only posts; images publish near-instantly, videos go through the container-finalizer async path |
| Meta / Facebook Page | ✅ | optional | Appends `article_url` to the message text if not already present, so FB can generate a preview card |
| X / Twitter | ✅ | n/a in current adapter | `Publish()` here only sends text (media upload path exists in the OAuth1 client but isn't wired into `Publish()`) |
| TikTok | ❌ | ✅ (video only) | Uses `PULL_FROM_URL`; unaudited apps are hard-locked to `privacy_level: SELF_ONLY` — can't post publicly until TikTok app review passes |

---

## Error Handling

There's no single global error shape. In practice:

- **Workspace/Auth handlers** (`types.APIResponse[any]`): `{"status": false, "message": "<reason>"}`
- **Post handlers** (raw `fiber.Map`): `{"error": "<reason>"}`
- **Registration/signin** (Reni Central passthrough): `{"status": false, "message": "<reason>", "data": {}, "http_status": <code>}`

HTTP status codes used across the API: `200`, `201`, `400`, `401`, `403`, `404`, `500`, `502` (X API upstream failures). There is no dedicated `422` usage — validation failures return `400`.

---

## Quick-Reference Endpoint Table

| Method | Path | Auth | Category |
|---|---|---|---|
| GET | `/health` | none | System |
| GET | `/api/workspaces/connect/:platform/callback` | none (browser redirect) | Connections |
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

*"full" = `Authorization: Bearer <token>` + `X-Session-Id: <session>`*
