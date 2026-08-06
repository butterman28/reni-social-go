// Package meta (this file): implements the newer "Instagram API with
// Instagram Login" OAuth flow — distinct from the Facebook Login flow in
// client.go. An Instagram professional account authorizes directly via
// instagram.com; no Facebook Page is involved. Kept in its own file with
// its own package-level vars so it never touches the existing
// ExchangeCode/AuthorizationURL/Publish paths in client.go.
//
// Flow:
//
//	Authorize    → https://www.instagram.com/oauth/authorize
//	Token (short)→ https://api.instagram.com/oauth/access_token
//	Token (long) → https://graph.instagram.com/access_token (ig_exchange_token)
//	Refresh      → https://graph.instagram.com/refresh_access_token (ig_refresh_token)
//	Profile      → https://graph.instagram.com/v21.0/me
//	Publish      → https://graph.instagram.com/v21.0/{ig-user-id}/media (+ /media_publish)
package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"reni-social/internal/domain"
	"reni-social/internal/platform"
	"reni-social/pkg/config"
)

var (
	instagramAuthURL    = "https://www.instagram.com/oauth/authorize"
	instagramTokenURL   = "https://api.instagram.com/oauth/access_token"
	instagramGraphBase  = "https://graph.instagram.com"
	instagramAPIVersion = "v21.0"
)

// InstagramLoginAuthorizationURL returns the consent URL for the direct
// Instagram Login flow (no Facebook Page involved).
func InstagramLoginAuthorizationURL(state string) string {
	params := url.Values{}
	params.Set("force_reauth", "true")
	params.Set("client_id", config.C.InstagramAppID)
	params.Set("redirect_uri", config.C.InstagramRedirectURL)
	params.Set("response_type", "code")
	params.Set("scope", "instagram_business_basic,instagram_business_manage_messages,instagram_business_manage_comments,instagram_business_content_publish,instagram_business_manage_insights")
	params.Set("state", state)
	return instagramAuthURL + "?" + params.Encode()
}

// ExchangeInstagramLoginCode exchanges an authorization code for a
// long-lived access token and resolves the IG user id/username via /me.
func (c *Client) ExchangeInstagramLoginCode(ctx context.Context, code string) (*domain.PlatformConnection, error) {
	// Step 1: short-lived token (api.instagram.com)
	form := url.Values{}
	form.Set("client_id", config.C.InstagramAppID)
	form.Set("client_secret", config.C.InstagramAppSecret)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", config.C.InstagramRedirectURL)
	form.Set("code", code)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, instagramTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("instagram-login: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(config.C.InstagramAppID, config.C.InstagramAppSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("instagram-login: short-lived token exchange: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("instagram-login: short-lived token status %d: %s", resp.StatusCode, string(raw))
	}

	var shortTok struct {
		AccessToken string `json:"access_token"`
		UserID      int64  `json:"user_id"`
	}
	if err := json.Unmarshal(raw, &shortTok); err != nil {
		return nil, fmt.Errorf("instagram-login: decode short-lived token: %w", err)
	}

	// Step 2: exchange for a long-lived token (60 days)
	llParams := url.Values{}
	llParams.Set("grant_type", "ig_exchange_token")
	llParams.Set("client_secret", config.C.InstagramAppSecret)
	llParams.Set("access_token", shortTok.AccessToken)

	llResp, err := c.httpClient.Get(instagramGraphBase + "/access_token?" + llParams.Encode())
	if err != nil {
		return nil, fmt.Errorf("instagram-login: long-lived token exchange: %w", err)
	}
	defer llResp.Body.Close()

	llRaw, _ := io.ReadAll(llResp.Body)
	if llResp.StatusCode < 200 || llResp.StatusCode >= 300 {
		return nil, fmt.Errorf("instagram-login: long-lived token status %d: %s", llResp.StatusCode, string(llRaw))
	}

	var longTok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(llRaw, &longTok); err != nil {
		return nil, fmt.Errorf("instagram-login: decode long-lived token: %w", err)
	}
	expiresAt := time.Now().Add(time.Duration(longTok.ExpiresIn) * time.Second)

	// Step 3: resolve the IG-scoped user id + username via /me
	meParams := url.Values{}
	meParams.Set("fields", "id,username,account_type")
	meParams.Set("access_token", longTok.AccessToken)

	meReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/%s/me?%s", instagramGraphBase, instagramAPIVersion, meParams.Encode()), nil)
	if err != nil {
		return nil, fmt.Errorf("instagram-login: build /me request: %w", err)
	}
	meReq.Header.Set("Authorization", "Bearer "+longTok.AccessToken)

	meResp, err := c.httpClient.Do(meReq)
	if err != nil {
		return nil, fmt.Errorf("instagram-login: fetch /me: %w", err)
	}
	defer meResp.Body.Close()

	meRaw, _ := io.ReadAll(meResp.Body)
	if meResp.StatusCode < 200 || meResp.StatusCode >= 300 {
		return nil, fmt.Errorf("instagram-login: /me status %d: %s", meResp.StatusCode, string(meRaw))
	}

	var me struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(meRaw, &me); err != nil {
		return nil, fmt.Errorf("instagram-login: decode /me: %w", err)
	}
	if me.ID == "" {
		me.ID = strconv.FormatInt(shortTok.UserID, 10) // fallback if /me omits id
	}

	return &domain.PlatformConnection{
		Platform:    domain.PlatformMetaIG,
		AccountID:   me.ID,
		AccountName: me.Username,
		AccessToken: longTok.AccessToken,
		ExpiresAt:   &expiresAt,
	}, nil
}

// RefreshInstagramLoginToken refreshes a long-lived Instagram Login token.
// Must be called before the existing token expires (60-day lifetime).
func (c *Client) RefreshInstagramLoginToken(ctx context.Context, conn *domain.PlatformConnection) (*domain.PlatformConnection, error) {
	params := url.Values{}
	params.Set("grant_type", "ig_refresh_token")
	params.Set("access_token", conn.AccessToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, instagramGraphBase+"/refresh_access_token?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("instagram-login: build refresh request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+conn.AccessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("instagram-login: refresh token: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("instagram-login: refresh status %d: %s", resp.StatusCode, string(raw))
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &tok); err != nil {
		return nil, fmt.Errorf("instagram-login: decode refresh: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	updated := *conn
	updated.AccessToken = tok.AccessToken
	updated.ExpiresAt = &expiresAt
	return &updated, nil
}

// PublishInstagramLogin posts a media container then publishes it, hitting
// graph.instagram.com directly against the IG-scoped user id — no Page
// lookup needed under this flow.
func (c *Client) PublishInstagramLogin(ctx context.Context, post *domain.Post, conn *domain.PlatformConnection) (*platform.PublishResult, error) {
	if len(post.MediaURLs) == 0 {
		return nil, fmt.Errorf("instagram-login: Instagram requires at least one image or video — text-only posts are not supported by the API")
	}

	mediaURL := post.MediaURLs[0]
	base := fmt.Sprintf("%s/%s", instagramGraphBase, instagramAPIVersion)
	containerEndpoint := fmt.Sprintf("%s/%s/media", base, conn.AccountID)

	containerPayload := map[string]any{
		"caption":      post.Content,
		"access_token": conn.AccessToken,
	}

	isVideo := isVideoURL(mediaURL)
	if isVideo {
		containerPayload["media_type"] = "REELS"
		containerPayload["video_url"] = mediaURL
	} else {
		containerPayload["image_url"] = mediaURL
	}

	containerResp, err := c.postJSON(ctx, containerEndpoint, containerPayload, conn.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("instagram-login: create container: %w", err)
	}

	containerID, ok := containerResp["id"].(string)
	if !ok || containerID == "" {
		return nil, fmt.Errorf("instagram-login: no container id returned")
	}

	// ── KEY CHANGE ──
	// Videos: don't wait. Return pending + container_id.
	// The IG Container Finalizer worker will poll and publish later.
	if isVideo {
		return &platform.PublishResult{
			Pending:     true,
			ContainerID: containerID,
		}, nil
	}

	// Images: near-instant processing, publish immediately.
	publishEndpoint := fmt.Sprintf("%s/%s/media_publish", base, conn.AccountID)
	publishResp, err := c.postJSON(ctx, publishEndpoint, map[string]any{
		"creation_id":  containerID,
		"access_token": conn.AccessToken,
	}, conn.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("instagram-login: publish container: %w", err)
	}

	postID, _ := publishResp["id"].(string)
	return &platform.PublishResult{PlatformPostID: postID}, nil
}
// waitForInstagramLoginContainerReady polls a media container's processing
// status until FINISHED, errors out, or times out (60s, every 3s).
func (c *Client) waitForInstagramLoginContainerReady(ctx context.Context, base, containerID, accessToken string) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		endpoint := fmt.Sprintf("%s/%s?fields=status_code&access_token=%s", base, containerID, url.QueryEscape(accessToken))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		var result struct {
			StatusCode string `json:"status_code"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		switch result.StatusCode {
		case "FINISHED":
			return nil
		case "ERROR", "EXPIRED":
			return fmt.Errorf("media container failed processing: status %s", result.StatusCode)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return fmt.Errorf("timed out waiting for media container to finish processing")
}
