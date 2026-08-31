package scheduler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// FIX: Reusable, thread-safe HTTP clients for the IG Finalizer
var (
	igStatusClient  = &http.Client{Timeout: 10 * time.Second}
	igPublishClient = &http.Client{Timeout: 15 * time.Second}
)

// StartIGContainerFinalizer polls for Instagram video containers still in
// 'processing'. When one reaches FINISHED it publishes the container and
// updates publish_results + the parent post status.
//
// Designed to run under the supervisor in cmd/worker/main.go.
// StartIGContainerFinalizer godoc
// @Summary StartIGContainerFinalizer
// @Description Auto-generated thorough documentation for StartIGContainerFinalizer
// @Tags scheduler
// @Produce json
// @Param db body object true "db"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func StartIGContainerFinalizer(db *sql.DB) {
	log.Println(" [IG Finalizer] Started — polling for pending Instagram containers")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if err := finalizeIGContainers(db); err != nil {
			log.Printf("  [IG Finalizer] error: %v", err)
		}
	}
}

// finalizeIGContainers godoc
// @Summary finalizeIGContainers
// @Description Auto-generated thorough documentation for finalizeIGContainers
// @Tags scheduler
// @Produce json
// @Param db body object true "db"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func finalizeIGContainers(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// ── STEP 1: Kill stuck containers older than 1 hour ──
	// We use a CTE with RETURNING to capture the post_id and attempt_id of the 
	// timed-out containers so we can properly finalize the parent posts.
	rows, err := db.QueryContext(ctx, `
		WITH timed_out AS (
			SELECT id, post_id, publish_attempt_id
			FROM publish_results
			WHERE platform = 'meta_ig'
			  AND status = 'processing'
			  AND container_id IS NOT NULL
			  AND created_at < NOW() - INTERVAL '1 hour'
		)
		UPDATE publish_results pr
		SET status = 'failed',
		    fail_reason = 'Instagram container timed out after 1 hour of processing',
		    updated_at = NOW(),
		    claimed_at = NULL
		FROM timed_out t
		WHERE pr.id = t.id
		RETURNING t.post_id::text, t.publish_attempt_id::text
	`)
	if err != nil {
		return fmt.Errorf("cleanup stuck containers: %w", err)
	}
	defer rows.Close()

	type timedOutPost struct {
		postID    string
		attemptID uuid.UUID
	}
	var timedOutPosts []timedOutPost
	seen := make(map[string]bool) // Prevent duplicate finalization if multiple containers timed out for the same attempt

	for rows.Next() {
		var postID, attemptStr string
		if err := rows.Scan(&postID, &attemptStr); err != nil {
			log.Printf("  [IG Finalizer] scan timed out post: %v", err)
			continue
		}
		attemptID, _ := uuid.Parse(attemptStr)
		key := postID + ":" + attemptStr
		if !seen[key] {
			seen[key] = true
			timedOutPosts = append(timedOutPosts, timedOutPost{postID: postID, attemptID: attemptID})
		}
	}

	// ── STEP 1.5: Finalize any posts whose containers just timed out ──
	// Instead of hard-failing the post, we call FinalizePostStatus. This ensures:
	// 1. If other platforms (e.g., LinkedIn) are still processing, the post isn't prematurely failed.
	// 2. If all platforms are done, the retry logic (scheduleRetry) is respected.
	// 3. The post is only marked 'failed' if retries are exhausted.
	for _, tp := range timedOutPosts {
		finalizeCtx, finalizeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := FinalizePostStatus(finalizeCtx, db, tp.postID, tp.attemptID); err != nil {
			log.Printf("  [IG Finalizer] error finalizing timed out post %s (attempt %s): %v", tp.postID, tp.attemptID, err)
		}
		finalizeCancel()
	}

	// ── STEP 2: Claim pending containers atomically ──
	const staleClaimSecs = 120 // 2 min — well above the 5s poll interval

	rows, err = db.QueryContext(ctx, `
		WITH candidates AS (
			SELECT id
			FROM publish_results
			WHERE platform = 'meta_ig'
			  AND status   = 'processing'
			  AND container_id IS NOT NULL
			  AND created_at > NOW() - INTERVAL '1 hour'
			  AND (claimed_at IS NULL OR claimed_at < NOW() - ($1 || ' seconds')::INTERVAL)
			ORDER BY created_at ASC
			LIMIT 100
			FOR UPDATE SKIP LOCKED
		)
		UPDATE publish_results pr
		SET claimed_at = NOW()
		FROM candidates c
		WHERE pr.id = c.id
		RETURNING pr.id::text, pr.post_id::text, pr.publish_attempt_id::text,
		          pr.container_id, pr.connection_id::text
	`, staleClaimSecs)
	if err != nil {
		return fmt.Errorf("claim pending containers: %w", err)
	}
	defer rows.Close()

	type pending struct {
		resultID    string
		postID      string
		attemptID   uuid.UUID
		containerID string
		connID      sql.NullString
	}

	var pendings []pending
	var connIDs []string

	for rows.Next() {
		var p pending
		var attemptStr string
		if err := rows.Scan(&p.resultID, &p.postID, &attemptStr, &p.containerID, &p.connID); err != nil {
			log.Printf("  [IG Finalizer] scan: %v", err)
			continue
		}
		p.attemptID, _ = uuid.Parse(attemptStr)
		pendings = append(pendings, p)
		if p.connID.Valid && p.connID.String != "" {
			connIDs = append(connIDs, p.connID.String)
		}
	}

	if len(pendings) == 0 {
		return nil
	}

	// ── STEP 3: Fetch credentials for claimed rows ──
	credMap := make(map[string]struct {
		accountID   string
		accessToken string
	})
	if len(connIDs) > 0 {
		credRows, err := db.QueryContext(ctx,
			`SELECT id::text, account_id, access_token FROM meta_connections WHERE id = ANY($1::uuid[]) AND is_deleted = false AND needs_reauth = false`,
			pq.Array(connIDs))
		if err != nil {
			log.Printf("  [IG Finalizer] error fetching credentials: %v", err)
		} else {
			defer credRows.Close()
			for credRows.Next() {
				var id, accountID, accessToken string
				if err := credRows.Scan(&id, &accountID, &accessToken); err == nil {
					credMap[id] = struct {
						accountID   string
						accessToken string
					}{accountID: accountID, accessToken: accessToken}
				}
			}
		}
	}

	// ── STEP 4: Fan out checks concurrently ──
	sem := make(chan struct{}, 5) // max 5 concurrent IG API calls
	var wg sync.WaitGroup

	for _, p := range pendings {
		wg.Add(1)
		sem <- struct{}{} // acquire slot

		go func(p pending) {
			defer wg.Done()
			defer func() { <-sem }() // release slot

			var connIDStr string
			if p.connID.Valid {
				connIDStr = p.connID.String
			}

			creds, ok := credMap[connIDStr]
			if !ok {
				log.Printf("  [IG Finalizer] missing credentials for connection %s (likely deleted or revoked)", connIDStr)
				// Fail the result immediately instead of releasing the claim to loop
				bgCtx := context.Background()
				_ = UpdatePublishResult(bgCtx, db, p.resultID, p.postID, p.attemptID, "meta_ig", ResultFailed, "", "", "Instagram connection was deleted or revoked while video was processing")
				_ = FinalizePostStatus(bgCtx, db, p.postID, p.attemptID)
				return
			}

			// Each goroutine gets its own fresh context for the external API calls
			apiCtx, apiCancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer apiCancel()

			status, err := checkIGContainerStatus(apiCtx, p.containerID, creds.accessToken)
			if err != nil {
				log.Printf("  [IG Finalizer] check container %s: %v", p.containerID, err)
				releaseClaim(db, p.resultID)
				return
			}

			// Use a background context for DB writes so they complete even if apiCtx times out
			dbCtx := context.Background()

			switch status {
			case "FINISHED":
				igPostID, err := publishIGContainer(apiCtx, p.containerID, creds.accountID, creds.accessToken)
				if err != nil {
					log.Printf(" [IG Finalizer] publish container %s: %v", p.containerID, err)
					_ = UpdatePublishResult(dbCtx, db, p.resultID, p.postID, p.attemptID, "meta_ig", ResultFailed, "", "", err.Error())
				} else {
					log.Printf(" [IG Finalizer] Published IG container %s → post %s", p.containerID, igPostID)
					_ = UpdatePublishResult(dbCtx, db, p.resultID, p.postID, p.attemptID, "meta_ig", ResultPublished, igPostID, "", "")
				}
				_ = FinalizePostStatus(dbCtx, db, p.postID, p.attemptID)

			case "ERROR", "EXPIRED":
				log.Printf(" [IG Finalizer] Container %s → %s", p.containerID, status)
				_ = UpdatePublishResult(dbCtx, db, p.resultID, p.postID, p.attemptID, "meta_ig", ResultFailed, "", "", "Instagram container "+status)
				_ = FinalizePostStatus(dbCtx, db, p.postID, p.attemptID)

			default:
				// Still processing — check again next tick
				log.Printf(" [IG Finalizer] Container %s still %s", p.containerID, status)
				releaseClaim(db, p.resultID)
			}
		}(p)
	}

	wg.Wait()
	return nil
}

// releaseClaim drops the claim so the row is eligible again on the next tick.
func releaseClaim(db *sql.DB, resultID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx,
		`UPDATE publish_results SET claimed_at = NULL WHERE id = $1`, resultID); err != nil {
		log.Printf("  [IG Finalizer] failed to release claim for %s: %v", resultID, err)
	}
}

// checkIGContainerStatus asks the Instagram Graph API for the container's
// current processing status.
// checkIGContainerStatus godoc
// @Summary checkIGContainerStatus
// @Description Auto-generated thorough documentation for checkIGContainerStatus
// @Tags scheduler
// @Produce json
// @Param accessToken body string true "accessToken"
// @Success 200 {string} string "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func checkIGContainerStatus(ctx context.Context, containerID, accessToken string) (string, error) {
	endpoint := fmt.Sprintf("https://graph.instagram.com/v21.0/%s?fields=status_code&access_token=%s",
		containerID, url.QueryEscape(accessToken))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := igStatusClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		StatusCode string `json:"status_code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.StatusCode, nil
}

// publishIGContainer calls /media_publish with the container ID.
// publishIGContainer godoc
// @Summary publishIGContainer
// @Description Auto-generated thorough documentation for publishIGContainer
// @Tags scheduler
// @Produce json
// @Param accessToken body string true "accessToken"
// @Success 200 {string} string "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func publishIGContainer(ctx context.Context, containerID, accountID, accessToken string) (string, error) {
	endpoint := fmt.Sprintf("https://graph.instagram.com/v21.0/%s/media_publish", accountID)
	payload, _ := json.Marshal(map[string]any{
		"creation_id":  containerID,
		"access_token": accessToken,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	// Bumped timeout slightly to survive Meta's intentional throttling during bursts
	resp, err := igPublishClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("publish status %d: %s", resp.StatusCode, string(raw))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.ID, nil
}