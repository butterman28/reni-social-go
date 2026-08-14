# 🔄 Background Token Management: Auto-Refresh & Re-authentication

## 📖 Overview

Social media platform APIs rely on OAuth access tokens that inevitably expire. To ensure uninterrupted publishing, the `reni-social-go` backend implements a robust, fault-tolerant background worker system to manage token lifecycles. 

Because platform policies differ, the system handles the two primary platforms differently:
*   **Meta (Instagram):** Tokens are **automatically refreshed** in the background before they expire. If a user revokes access, the system detects the permanent failure and flags the connection for re-authentication.
*   **LinkedIn:** The OAuth scope deliberately excludes `offline_access`, meaning LinkedIn **does not issue refresh tokens**. The system proactively monitors expiration dates, flags connections that are expiring soon, and automatically emails the workspace owner to prompt a manual reconnection.

---

## 🏗️ Architecture & Worker Supervision

Both token management systems run as independent, long-lived goroutines inside the background worker (`cmd/worker/main.go`). They are wrapped in a `supervise()` function, which provides:
1.  **Panic Recovery:** If a background job panics, the supervisor catches it, logs the error, and restarts the worker after a 5-second backoff.
2.  **Continuous Execution:** The workers run on `time.Ticker` intervals and will automatically resume if they exit cleanly or crash.

---

## 📸 Meta (Instagram) Auto-Refresh

**Core Files:** `pkg/meta/refresher.go`, `pkg/meta/repo.go`

Instagram long-lived tokens typically expire after 60 days. The Meta Refresher proactively extends them.

### How it Works
1.  **Batch Discovery:** Every `MetaRefreshCheckIntervalHours` (default: 6 hours), the worker queries the `meta_connections` table for active tokens expiring within `MetaRefreshBeforeDays` (default: 15 days).
2.  **Concurrency Control:** It fans out the refresh requests using a semaphore (`MaxConcurrency`) to prevent hammering the Meta Graph API and triggering rate limits.
3.  **Exponential Backoff:** If a transient network error occurs, the worker retries up to 3 times using exponential backoff with randomized jitter (`BackoffBase`).
4.  **Permanent Failure Detection:** If the Meta API returns specific OAuth errors (e.g., Code `190` Invalid Token, Code `102` Session Expired), the system recognizes that the user has revoked access or changed their password. It aborts retries and flags the database row with `needs_reauth = true`.

---

## 💼 LinkedIn Expiry & Re-authentication

**Core Files:** `pkg/linkedin/monitor.go`, `pkg/linkedin/repo.go`, `internal/notify/linkedin.go`

Because the LinkedIn integration uses the `openid profile email w_member_social` scopes (omitting `offline_access`), tokens hard-expire after ~60 days and cannot be refreshed programmatically.

### How it Works
1.  **Proactive Monitoring:** Every `LinkedInExpiryCheckIntervalHours` (default: 6 hours), the monitor scans for tokens that have already expired or will expire within `LinkedInExpiryWarnBeforeDays` (default: 7 days).
2.  **Flagging:** It updates the database, setting `needs_reauth = true` and populating the `reauth_reason` column with a human-readable string (e.g., *"LinkedIn access token expires on [Date] — reconnect now to avoid publishing interruptions"*).
3.  **Automated Notifications:** If the workspace has an owner email on file, the monitor pushes a job to the Redis notification queue (`internal/notify/worker.go`). The email worker then sends a transactional email to the owner with a deep link directly to the workspace's connection settings page.
4.  **Idempotency:** Once a connection is flagged, it drops out of the monitor's query until the user manually reconnects, preventing duplicate emails.

---

## 🗄️ Database Schema Updates

To support these background workflows, the connection tables (`linkedin_connections` and `meta_connections`) include advisory columns:

```yaml
needs_reauth:
  type: boolean
  not_null: true
  default: false

reauth_reason:
  type: text
  not_null: false
```

*   **`needs_reauth`**: A boolean flag read by the frontend to display UI banners. When a user successfully completes the OAuth flow again, the `SaveConnection` service automatically resets this to `false`.
*   **`reauth_reason`**: Stores the exact reason for the failure (e.g., "Token expired", "OAuth Error 190: User revoked access") to display to the user in the UI.

---

## 🖥️ Frontend Integration Guide

The frontend should implement a global or workspace-scoped polling mechanism to alert users about broken connections.

### 1. Polling for Alerts
Use the tokenless advisory endpoint to check for connections requiring attention. This endpoint is safe for global app-wide polling because it returns **no access tokens**.

**Endpoint:** `GET /api/in/workspaces/in/:id/connections/reauth-status`

**Response Shape:**
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
        "reason": "LinkedIn access token expires on 2026-08-20T10:00:00Z...",
        "expired": false,
        "expires_at": "2026-08-20T10:00:00Z"
      }
    ]
  }
}
```
*Frontend Action:* If `needs_attention` is `true`, render a persistent banner or badge in the UI warning the user that publishing will fail until they reconnect.

### 2. Triggering Re-authentication
When the user clicks "Reconnect" in the UI, you must bypass the standard "Already Connected" short-circuit logic.

**Endpoint:** `POST /api/in/workspaces/in/:id/connect/in/:platform?force=true`

*   **`force=true`**: This query parameter tells the backend to ignore the `CheckAndReviveConnection` logic and immediately generate a fresh `oauth_state` and `redirect_url`, forcing the user through the platform's consent screen again.
*   Upon successful callback, the backend's `SaveConnection` function will automatically clear the `needs_reauth` flag and nullify the `reauth_reason`.

---

## ⚙️ Configuration Reference

These parameters are defined in `pkg/config/config.go` and can be tuned via environment variables or the `.env` files depending on your deployment target.

| Variable / Config Field | Default | Description |
| :--- | :--- | :--- |
| **Meta Refresher** | | |
| `MetaRefreshCheckIntervalHours` | `6` | How often the worker wakes up to scan the DB. |
| `MetaRefreshBeforeDays` | `15` | Refresh tokens that expire in less than X days. |
| `MetaRefreshBatchSize` | `50` | Max tokens to process per batch cycle. |
| `MetaRefreshMaxConcurrency` | `5` | Max parallel outbound HTTP requests to Meta API. |
| **LinkedIn Monitor** | | |
| `LinkedInExpiryCheckIntervalHours`| `6` | How often the monitor scans for expiring tokens. |
| `LinkedInExpiryWarnBeforeDays` | `7` | Flag tokens that expire in less than X days. |
| `LinkedInExpiryBatchSize` | `50` | Max connections to process per batch cycle. |

ok we want this in markdown 