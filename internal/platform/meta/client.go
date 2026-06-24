// Package meta implements the platform.Publisher interface for Meta platforms:
// Facebook Pages and Instagram Business accounts.
//
// OAuth2 flow:
//
//	Authorize → https://www.facebook.com/v19.0/dialog/oauth
//	Token     → https://graph.facebook.com/v19.0/oauth/access_token
//
// Facebook Page posting:
//
//	POST https://graph.facebook.com/v19.0/{page-id}/feed
//
// Instagram posting (container model) — requires media, no text-only posts:
//  1. POST /{ig-user-id}/media              → creates a container, returns container_id
//     (for video: poll the container's status_code until FINISHED)
//  2. POST /{ig-user-id}/media_publish      → publishes the container
//
// Instagram Business Accounts are always resolved through a connected
// Facebook Page: GET /me/accounts → pages (+ page access tokens), then
// GET /{page-id}?fields=instagram_business_account for each page.
package meta

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"reni-social/internal/domain"
	"reni-social/internal/platform"
	"reni-social/pkg/config"
)

var (
	graphBase = "https://graph.facebook.com/v19.0"
	authURL   = "https://www.facebook.com/v19.0/dialog/oauth"
	tokenURL  = graphBase + "/oauth/access_token"
)

// Client is the Meta platform adapter (Facebook + Instagram).
type Client struct {
	httpClient *http.Client
}

// New returns a new Meta Client.
func New() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

// Ensure Client satisfies the Publisher interface at compile time.
var _ platform.Publisher = (*Client)(nil)

// ──────────────────────────────────────────────────────────────────────────────
// OAuth2 Helpers
// ──────────────────────────────────────────────────────────────────────────────

// AuthorizationURL returns the Meta OAuth dialog URL.
func AuthorizationURL(state string) string {
	params := url.Values{}
	params.Set("client_id", config.C.MetaAppID)
	params.Set("redirect_uri", config.C.MetaRedirectURL)
	params.Set("state", state)
	params.Set("scope", "pages_manage_posts,pages_read_engagement,instagram_basic,instagram_content_publish,public_profile")
	params.Set("response_type", "code")
	return authURL + "?" + params.Encode()
}

// ExchangeCode exchanges an authorization code for a user access token,
// then exchanges it for a long-lived token, then resolves the correct
// publishing identity for the requested platform ("meta_fb" or "meta_ig").
func (c *Client) ExchangeCode(ctx context.Context, code, platformType string) (*domain.PlatformConnection, error) {
	// Step 1: short-lived user token
	params := url.Values{}
	params.Set("client_id", config.C.MetaAppID)
	params.Set("client_secret", config.C.MetaAppSecret)
	params.Set("redirect_uri", config.C.MetaRedirectURL)
	params.Set("code", code)

	resp, err := c.httpClient.Get(tokenURL + "?" + params.Encode())
	if err != nil {
		return nil, fmt.Errorf("meta: token exchange: %w", err)
	}
	defer resp.Body.Close()

	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return nil, fmt.Errorf("meta: decode token: %w", err)
	}

	// Step 2: exchange for long-lived token (60-day)
	llParams := url.Values{}
	llParams.Set("grant_type", "fb_exchange_token")
	llParams.Set("client_id", config.C.MetaAppID)
	llParams.Set("client_secret", config.C.MetaAppSecret)
	llParams.Set("fb_exchange_token", tok.AccessToken)

	llResp, err := c.httpClient.Get(tokenURL + "?" + llParams.Encode())
	if err != nil {
		return nil, fmt.Errorf("meta: long-lived token: %w", err)
	}
	defer llResp.Body.Close()

	var llTok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(llResp.Body).Decode(&llTok); err != nil {
		return nil, fmt.Errorf("meta: decode long-lived token: %w", err)
	}
	expiresAt := time.Now().Add(time.Duration(llTok.ExpiresIn) * time.Second)

	if platformType == "meta_ig" {
		pageAccessToken, igAccountID, igUsername, err := c.fetchInstagramAccount(ctx, llTok.AccessToken)
		if err != nil {
			return nil, fmt.Errorf("meta: resolve instagram account: %w", err)
		}
		return &domain.PlatformConnection{
			Platform:    domain.PlatformMetaIG,
			AccountID:   igAccountID,
			AccountName: igUsername,
			AccessToken: pageAccessToken, // Page token — required for IG Graph API calls
			ExpiresAt:   &expiresAt,
		}, nil
	}

	// TODO: Facebook Page publishing has the same underlying issue —
	// fetchMe returns the personal user ID/token, but publishFacebook
	// needs a Page ID + Page access token (via /me/accounts), not the
	// personal identity. Deferred for now; only Instagram is fixed here.
	accountID, accountName, err := c.fetchMe(ctx, llTok.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("meta: fetch /me: %w", err)
	}

	return &domain.PlatformConnection{
		Platform:    domain.PlatformMetaFB,
		AccountID:   accountID,
		AccountName: accountName,
		AccessToken: llTok.AccessToken,
		ExpiresAt:   &expiresAt,
	}, nil
}

// RefreshToken for Meta — long-lived tokens are refreshed by re-exchanging them.
// For Instagram connections this re-exchanges the stored Page token, which
// works the same way as a user token for this endpoint.
func (c *Client) RefreshToken(ctx context.Context, conn *domain.PlatformConnection) (*domain.PlatformConnection, error) {
	params := url.Values{}
	params.Set("grant_type", "fb_exchange_token")
	params.Set("client_id", config.C.MetaAppID)
	params.Set("client_secret", config.C.MetaAppSecret)
	params.Set("fb_exchange_token", conn.AccessToken)

	resp, err := c.httpClient.Get(tokenURL + "?" + params.Encode())
	if err != nil {
		return nil, fmt.Errorf("meta: refresh token: %w", err)
	}
	defer resp.Body.Close()

	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return nil, fmt.Errorf("meta: decode refresh: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	updated := *conn
	updated.AccessToken = tok.AccessToken
	updated.ExpiresAt = &expiresAt
	return &updated, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Publishing
// ──────────────────────────────────────────────────────────────────────────────

// Publish routes to the correct publishing method based on platform type.
func (c *Client) Publish(ctx context.Context, post *domain.Post, conn *domain.PlatformConnection) (*platform.PublishResult, error) {
	switch conn.Platform {
	case domain.PlatformMetaFB:
		return c.publishFacebook(ctx, post, conn)
	case domain.PlatformMetaIG:
		return c.publishInstagram(ctx, post, conn)
	default:
		return nil, fmt.Errorf("meta: unsupported platform: %s", conn.Platform)
	}
}

// publishFacebook posts to a Facebook Page feed.
func (c *Client) publishFacebook(ctx context.Context, post *domain.Post, conn *domain.PlatformConnection) (*platform.PublishResult, error) {
	endpoint := fmt.Sprintf("%s/%s/feed", graphBase, conn.AccountID)

	payload := map[string]any{
		"message":      post.Content,
		"access_token": conn.AccessToken,
	}

	result, err := c.postJSON(ctx, endpoint, payload)
	if err != nil {
		return nil, fmt.Errorf("meta/fb: publish: %w", err)
	}

	postID, _ := result["id"].(string)
	return &platform.PublishResult{
		PlatformPostID: postID,
		URL:            "https://www.facebook.com/" + postID,
	}, nil
}

// publishInstagram creates a media container then publishes it.
// Instagram has no text-only post type — at least one image or video URL
// is required in post.MediaURLs.
func (c *Client) publishInstagram(ctx context.Context, post *domain.Post, conn *domain.PlatformConnection) (*platform.PublishResult, error) {
	if len(post.MediaURLs) == 0 {
		return nil, fmt.Errorf("meta/ig: Instagram requires at least one image or video — text-only posts are not supported by the API")
	}
	mediaURL := post.MediaURLs[0]

	containerEndpoint := fmt.Sprintf("%s/%s/media", graphBase, conn.AccountID)
	containerPayload := map[string]any{
		"caption":      post.Content,
		"access_token": conn.AccessToken,
	}

	isVideo := isVideoURL(mediaURL)
	if isVideo {
		containerPayload["media_type"] = "VIDEO"
		containerPayload["video_url"] = mediaURL
	} else {
		containerPayload["image_url"] = mediaURL
	}

	containerResp, err := c.postJSON(ctx, containerEndpoint, containerPayload)
	if err != nil {
		return nil, fmt.Errorf("meta/ig: create container: %w", err)
	}

	containerID, ok := containerResp["id"].(string)
	if !ok || containerID == "" {
		return nil, fmt.Errorf("meta/ig: no container id returned")
	}

	// Videos process asynchronously — publishing immediately after creating
	// the container usually fails with "media ID is not available" until
	// status_code reaches FINISHED. Images are near-instant, so skip this.
	if isVideo {
		if err := c.waitForContainerReady(ctx, containerID, conn.AccessToken); err != nil {
			return nil, fmt.Errorf("meta/ig: %w", err)
		}
	}

	publishEndpoint := fmt.Sprintf("%s/%s/media_publish", graphBase, conn.AccountID)
	publishPayload := map[string]any{
		"creation_id":  containerID,
		"access_token": conn.AccessToken,
	}

	publishResp, err := c.postJSON(ctx, publishEndpoint, publishPayload)
	if err != nil {
		return nil, fmt.Errorf("meta/ig: publish container: %w", err)
	}

	postID, _ := publishResp["id"].(string)
	return &platform.PublishResult{
		PlatformPostID: postID,
	}, nil
}

// waitForContainerReady polls an Instagram media container's processing
// status until it's FINISHED, errors out, or times out (60s, every 3s).
func (c *Client) waitForContainerReady(ctx context.Context, containerID, accessToken string) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		endpoint := fmt.Sprintf("%s/%s?fields=status_code&access_token=%s", graphBase, containerID, url.QueryEscape(accessToken))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
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

// isVideoURL is a simple extension-based heuristic. domain.Post has no
// media-type field yet, so we infer from the URL's file extension.
func isVideoURL(mediaURL string) bool {
	lower := strings.ToLower(mediaURL)
	for _, ext := range []string{".mp4", ".mov", ".m4v", ".avi", ".webm"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// ──────────────────────────────────────────────────────────────────────────────
// Internal Helpers — Page / Instagram account resolution
// ──────────────────────────────────────────────────────────────────────────────

type metaPage struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AccessToken string `json:"access_token"`
}

// fetchPages lists the Facebook Pages the authenticated user manages,
// each with its own Page access token (distinct from the user token).
func (c *Client) fetchPages(ctx context.Context, userAccessToken string) ([]metaPage, error) {
	endpoint := graphBase + "/me/accounts?fields=id,name,access_token&access_token=" + url.QueryEscape(userAccessToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(raw))
	}

	var result struct {
		Data []metaPage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

// fetchInstagramAccount finds the first connected Page that has a linked
// Instagram Business/Creator account, and returns the Page access token
// (needed for all subsequent IG Graph API calls) + the IG account ID + username.
func (c *Client) fetchInstagramAccount(ctx context.Context, userAccessToken string) (pageAccessToken, igAccountID, igUsername string, err error) {
	pages, err := c.fetchPages(ctx, userAccessToken)
	if err != nil {
		return "", "", "", fmt.Errorf("fetch pages: %w", err)
	}
	if len(pages) == 0 {
		return "", "", "", fmt.Errorf("no Facebook Pages found — an Instagram Business account must be linked to a Page you manage")
	}

	for _, page := range pages {
		endpoint := fmt.Sprintf("%s/%s?fields=instagram_business_account&access_token=%s",
			graphBase, page.ID, url.QueryEscape(page.AccessToken))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			continue
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			continue
		}

		var result struct {
			InstagramBusinessAccount struct {
				ID string `json:"id"`
			} `json:"instagram_business_account"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if decodeErr != nil || result.InstagramBusinessAccount.ID == "" {
			continue
		}

		username := c.fetchIGUsername(ctx, result.InstagramBusinessAccount.ID, page.AccessToken)
		return page.AccessToken, result.InstagramBusinessAccount.ID, username, nil
	}

	return "", "", "", fmt.Errorf("no Page with a linked Instagram Business account was found — connect an Instagram Business/Creator account to one of your Pages first")
}

// fetchIGUsername is best-effort (purely cosmetic); a failure here doesn't
// block resolving the account, it just leaves AccountName blank.
func (c *Client) fetchIGUsername(ctx context.Context, igAccountID, pageAccessToken string) string {
	endpoint := fmt.Sprintf("%s/%s?fields=username&access_token=%s", graphBase, igAccountID, url.QueryEscape(pageAccessToken))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Username string `json:"username"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return result.Username
}

func (c *Client) fetchMe(ctx context.Context, accessToken string) (id, name string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		graphBase+"/me?fields=id,name&access_token="+accessToken, nil)
	if err != nil {
		return "", "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var me struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil {
		return "", "", err
	}
	return me.ID, me.Name, nil
}

func (c *Client) postJSON(ctx context.Context, endpoint string, payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(raw))
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}