// Package meta implements the platform.Publisher interface for Meta platforms:
// Facebook Pages and Instagram Business accounts.
//
// OAuth2 flow:
//   Authorize → https://www.facebook.com/v19.0/dialog/oauth
//   Token     → https://graph.facebook.com/v19.0/oauth/access_token
//
// Facebook Page posting:
//   POST https://graph.facebook.com/v19.0/{page-id}/feed
//
// Instagram posting (container model):
//   1. POST /{ig-user-id}/media              → creates a container, returns container_id
//   2. POST /{ig-user-id}/media_publish      → publishes the container
package meta

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"reni-social/internal/domain"
	"reni-social/internal/platform"
	"reni-social/pkg/config"
)

const (
	graphBase  = "https://graph.facebook.com/v19.0"
	authURL    = "https://www.facebook.com/v19.0/dialog/oauth"
	tokenURL   = graphBase + "/oauth/access_token"
)

// Client is the Meta platform adapter (Facebook + Instagram).
type Client struct {
	httpClient *http.Client
}

// New returns a new Meta Client.
func New() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 15 * time.Second},
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
	// Permissions: manage pages, publish to pages, Instagram content publish
	params.Set("scope", "pages_manage_posts,pages_read_engagement,instagram_basic,instagram_content_publish,public_profile")
	params.Set("response_type", "code")
	return authURL + "?" + params.Encode()
}

// ExchangeCode exchanges an authorization code for a user access token,
// then exchanges it for a long-lived page token.
func (c *Client) ExchangeCode(ctx context.Context, code, platform string) (*domain.PlatformConnection, error) {
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
		TokenType   string `json:"token_type"`
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

	// Step 3: get user ID from /me
	accountID, accountName, err := c.fetchMe(ctx, llTok.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("meta: fetch /me: %w", err)
	}

	p := domain.PlatformMetaFB
	if platform == "meta_ig" {
		p = domain.PlatformMetaIG
	}

	return &domain.PlatformConnection{
		Platform:    p,
		AccountID:   accountID,
		AccountName: accountName,
		AccessToken: llTok.AccessToken,
		ExpiresAt:   &expiresAt,
	}, nil
}

// RefreshToken for Meta — long-lived tokens are refreshed by re-exchanging them.
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
func (c *Client) publishInstagram(ctx context.Context, post *domain.Post, conn *domain.PlatformConnection) (*platform.PublishResult, error) {
	// Step 1: create media container
	containerEndpoint := fmt.Sprintf("%s/%s/media", graphBase, conn.AccountID)
	containerPayload := map[string]any{
		"caption":      post.Content,
		"media_type":   "TEXT", // upgrade to IMAGE/VIDEO when media_urls present
		"access_token": conn.AccessToken,
	}
	// If there is media, use the first image URL
	if len(post.MediaURLs) > 0 {
		containerPayload["image_url"] = post.MediaURLs[0]
		containerPayload["media_type"] = "IMAGE"
	}

	containerResp, err := c.postJSON(ctx, containerEndpoint, containerPayload)
	if err != nil {
		return nil, fmt.Errorf("meta/ig: create container: %w", err)
	}

	containerID, ok := containerResp["id"].(string)
	if !ok || containerID == "" {
		return nil, fmt.Errorf("meta/ig: no container id returned")
	}

	// Step 2: publish container
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

// ──────────────────────────────────────────────────────────────────────────────
// Internal Helpers
// ──────────────────────────────────────────────────────────────────────────────

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
