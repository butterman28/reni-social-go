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
	// Bumped context slightly to 15s to comfortably accommodate the extra cleanup query
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// ── STEP 1: Kill stuck containers older than 1 hour ──
	_, err := db.ExecContext(ctx, `
		UPDATE publish_results
		SET status = 'failed',
		    fail_reason = 'Instagram container timed out after 1 hour of processing',
		    updated_at = NOW()
		WHERE platform = 'meta_ig'
		  AND status = 'processing'
		  AND container_id IS NOT NULL
		  AND created_at < NOW() - INTERVAL '1 hour'
	`)
	if err != nil {
		return fmt.Errorf("cleanup stuck containers: %w", err)
	}

	// ── STEP 1.5: Finalize any posts whose containers just timed out ──
	// Now that the individual platform results are marked as failed, update the 
	// parent post status so it doesn't stay stuck in 'publishing' forever.
	_, err = db.ExecContext(ctx, `
		UPDATE posts SET status = 'failed',
			fail_reason = 'Instagram video processing timed out',
			updated_at = NOW()
		WHERE id IN (
			SELECT DISTINCT post_id FROM publish_results
			WHERE platform = 'meta_ig'
			  AND status = 'failed'
			  AND fail_reason LIKE '%timed out%'
			  AND updated_at > NOW() - INTERVAL '5 seconds'
		)
		AND status = 'publishing'
	`)
	if err != nil {
		log.Printf("  [IG Finalizer] error finalizing timed out parent posts: %v", err)
		// We log but don't return, so we can still proceed to poll active containers
	}

	// ── STEP 2: Only poll containers created within the last hour ──
	rows, err := db.QueryContext(ctx, `
		SELECT
			pr.id::text,
			pr.post_id::text,
			pr.publish_attempt_id::text,
			pr.container_id,
			mc.account_id,
			mc.access_token
		FROM publish_results pr
		JOIN meta_connections mc ON mc.id = pr.connection_id
		WHERE pr.platform = 'meta_ig'
		  AND pr.status   = 'processing'
		  AND pr.container_id IS NOT NULL
		  AND pr.created_at > NOW() - INTERVAL '1 hour'
	`)
	if err != nil {
		return fmt.Errorf("query pending containers: %w", err)
	}
	defer rows.Close()

	type pending struct {
		resultID    string
		postID      string
		attemptID   uuid.UUID
		containerID string
		accountID   string
		accessToken string
	}

	var pendings []pending
	for rows.Next() {
		var p pending
		var attemptStr string
		if err := rows.Scan(&p.resultID, &p.postID, &attemptStr, &p.containerID, &p.accountID, &p.accessToken); err != nil {
			log.Printf("  [IG Finalizer] scan: %v", err)
			continue
		}
		p.attemptID, _ = uuid.Parse(attemptStr)
		pendings = append(pendings, p)
	}

	if len(pendings) == 0 {
		return nil
	}

	// Fan out checks concurrently with a semaphore to prevent hammering the Meta API
	// and to ensure one slow container doesn't block the others or eat up a shared timeout.
	sem := make(chan struct{}, 5) // max 5 concurrent IG API calls
	var wg sync.WaitGroup

	for _, p := range pendings {
		wg.Add(1)
		sem <- struct{}{} // acquire slot

		go func(p pending) {
			defer wg.Done()
			defer func() { <-sem }() // release slot

			// Each goroutine gets its own fresh context for the external API calls
			apiCtx, apiCancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer apiCancel()

			status, err := checkIGContainerStatus(apiCtx, p.containerID, p.accessToken)
			if err != nil {
				log.Printf("  [IG Finalizer] check container %s: %v", p.containerID, err)
				return
			}

			// Use a background context for DB writes so they complete even if apiCtx times out
			dbCtx := context.Background()

			switch status {
			case "FINISHED":
				igPostID, err := publishIGContainer(apiCtx, p.containerID, p.accountID, p.accessToken)
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
			}
		}(p)
	}

	wg.Wait()
	return nil
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

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
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
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
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