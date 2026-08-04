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
	"time"

	"github.com/google/uuid"
)

// StartIGContainerFinalizer polls for Instagram video containers still in
// 'processing'. When one reaches FINISHED it publishes the container and
// updates publish_results + the parent post status.
//
// Designed to run under the supervisor in cmd/worker/main.go.
func StartIGContainerFinalizer(db *sql.DB) {
	log.Println("📸 [IG Finalizer] Started — polling for pending Instagram containers")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if err := finalizeIGContainers(db); err != nil {
			log.Printf("⚠️  [IG Finalizer] error: %v", err)
		}
	}
}

func finalizeIGContainers(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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
	`)
	if err != nil {
		return fmt.Errorf("query pending containers: %w", err)
	}
	defer rows.Close()

	type pending struct {
		resultID    string
		postID      string
		attemptID   uuid.UUID // <--- Added to struct
		containerID string
		accountID   string
		accessToken string
	}

	var pendings []pending
	for rows.Next() {
		var p pending
		var attemptStr string
		// 👇 Scan the new attemptStr column
		if err := rows.Scan(&p.resultID, &p.postID, &attemptStr, &p.containerID, &p.accountID, &p.accessToken); err != nil {
			log.Printf("⚠️  [IG Finalizer] scan: %v", err)
			continue
		}
		p.attemptID, _ = uuid.Parse(attemptStr) // <--- Parse it
		pendings = append(pendings, p)
	}

	for _, p := range pendings {
		status, err := checkIGContainerStatus(ctx, p.containerID, p.accessToken)
		if err != nil {
			log.Printf("⚠️  [IG Finalizer] check container %s: %v", p.containerID, err)
			continue
		}

		switch status {
		case "FINISHED":
			igPostID, err := publishIGContainer(ctx, p.containerID, p.accountID, p.accessToken)
			if err != nil {
				log.Printf("❌ [IG Finalizer] publish container %s: %v", p.containerID, err)
				_ = UpdatePublishResult(ctx, db, p.resultID,
					ResultFailed, "", "", err.Error())
			} else {
				log.Printf("✅ [IG Finalizer] Published IG container %s → post %s", p.containerID, igPostID)
				_ = UpdatePublishResult(ctx, db, p.resultID,
					ResultPublished, igPostID, "", "")
			}
			// 👇 Fixed: Pass the attemptID
			_ = FinalizePostStatus(ctx, db, p.postID, p.attemptID)

		case "ERROR", "EXPIRED":
			log.Printf("❌ [IG Finalizer] Container %s → %s", p.containerID, status)
			_ = UpdatePublishResult(ctx, db, p.resultID,
				ResultFailed, "", "", "Instagram container "+status)
			// 👇 Fixed: Pass the attemptID
			_ = FinalizePostStatus(ctx, db, p.postID, p.attemptID)

		default:
			// Still processing — check again next tick
			log.Printf("⏳ [IG Finalizer] Container %s still %s", p.containerID, status)
		}
	}

	return nil
}

// checkIGContainerStatus asks the Instagram Graph API for the container's
// current processing status.
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