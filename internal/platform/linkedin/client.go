// Package linkedin implements the platform.Publisher interface for LinkedIn.
// It uses the LinkedIn Share API v2 (UGC Posts endpoint), plus the Assets
// API for image/video uploads.
//
// OAuth2 flow:
//
//	Authorize → https://www.linkedin.com/oauth/v2/authorization
//	Token     → https://www.linkedin.com/oauth/v2/accessToken
//	Refresh   → same token endpoint with grant_type=refresh_token
//
// Publishing:
//
//	Text share        → POST /v2/ugcPosts                  (shareMediaCategory=NONE)
//	Article/URL share → POST /v2/ugcPosts                  (shareMediaCategory=ARTICLE)
//	Image/Video share → POST /v2/assets?action=registerUpload
//	                    PUT  <uploadUrl> (binary)
//	                    POST /v2/ugcPosts                  (shareMediaCategory=IMAGE|VIDEO)
package linkedin

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
	authURL           = "https://www.linkedin.com/oauth/v2/authorization"
	tokenURL          = "https://www.linkedin.com/oauth/v2/accessToken"
	ugcPostURL        = "https://api.linkedin.com/v2/ugcPosts"
	meURL             = "https://api.linkedin.com/v2/userinfo"
	registerUploadURL = "https://api.linkedin.com/v2/assets?action=registerUpload"
)

// Client is the LinkedIn platform adapter.
type Client struct {
	httpClient *http.Client
}

// New returns a new LinkedIn Client.
func New() *Client {
	return &Client{
		// 30s instead of 15s — media downloads/uploads need more headroom
		// than a simple JSON API call.
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Ensure Client satisfies the Publisher interface at compile time.
var _ platform.Publisher = (*Client)(nil)

// ──────────────────────────────────────────────────────────────────────────────
// OAuth2 Helpers
// ──────────────────────────────────────────────────────────────────────────────

// AuthorizationURL returns the URL the user should be redirected to for consent.
func AuthorizationURL(state string, id string, platform string) string {
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", config.C.LinkedInClientID)
	params.Set("redirect_uri", config.C.LinkedInRedirectURL)
	params.Set("state", state)
	params.Set("scope", "openid profile email w_member_social")
	return authURL + "?" + params.Encode()
}

// ExchangeCode exchanges an authorization code for an access token.
func (c *Client) ExchangeCode(ctx context.Context, code string) (*domain.PlatformConnection, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", config.C.LinkedInRedirectURL)
	data.Set("client_id", config.C.LinkedInClientID)
	data.Set("client_secret", config.C.LinkedInClientSecret)

	tokenResp, err := c.fetchToken(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("linkedin: token exchange failed: %w", err)
	}

	accountID, accountName, err := c.fetchProfile(ctx, tokenResp.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("linkedin: profile fetch failed: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	return &domain.PlatformConnection{
		Platform:     domain.PlatformLinkedIn,
		AccountID:    accountID,
		AccountName:  accountName,
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    &expiresAt,
	}, nil
}

// RefreshToken exchanges a refresh token for a new access token.
func (c *Client) RefreshToken(ctx context.Context, conn *domain.PlatformConnection) (*domain.PlatformConnection, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", conn.RefreshToken)
	data.Set("client_id", config.C.LinkedInClientID)
	data.Set("client_secret", config.C.LinkedInClientSecret)

	tokenResp, err := c.fetchToken(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("linkedin: token refresh failed: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	updated := *conn
	updated.AccessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		updated.RefreshToken = tokenResp.RefreshToken
	}
	updated.ExpiresAt = &expiresAt
	return &updated, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Publishing
// ──────────────────────────────────────────────────────────────────────────────

// Publish posts content to LinkedIn via the UGC Posts API.
//
// Share type is chosen automatically:
//   - post.MediaURLs non-empty → Image or Video share (first URL only; the
//     content type is auto-detected — image/* → IMAGE, video/* → VIDEO).
//   - otherwise                → plain Text share.
//
// For an Article/URL share (a link + preview card, no uploaded media), use
// PublishArticle directly — domain.Post has no article URL field yet, so it
// can't be auto-detected here.
// Publish posts content to LinkedIn via the UGC Posts API.
//
// Share type is chosen automatically:
//   - post.ArticleURL non-empty → Article/URL share (preview card via PublishArticle)
//   - post.MediaURLs non-empty  → Image or Video share (first URL only)
//   - otherwise                 → plain Text share
func (c *Client) Publish(ctx context.Context, post *domain.Post, conn *domain.PlatformConnection) (*platform.PublishResult, error) {
	// 1. Article share takes priority — explicit preview-card intent.
	if post.ArticleURL != "" {
		return c.PublishArticle(ctx, conn, post.Content, post.ArticleURL, post.ArticleTitle)
	}

	// 2. Media share (image or video, auto-detected from content type).
	if len(post.MediaURLs) > 0 {
		return c.publishMedia(ctx, post.Content, post.MediaURLs[0], conn)
	}

	// 3. Plain text share.
	return c.publishShare(ctx, buildTextShare(conn.AccountID, post.Content), conn.AccessToken)
}
// PublishArticle creates an Article/URL share: a link with a preview card
// (title + optional description), as opposed to an uploaded image/video.
//
// Not part of the platform.Publisher interface — call it directly when you
// need this share type.
func (c *Client) PublishArticle(ctx context.Context, conn *domain.PlatformConnection, content, articleURL, title string) (*platform.PublishResult, error) {
	body := buildArticleShare(conn.AccountID, content, articleURL, title)
	return c.publishShare(ctx, body, conn.AccessToken)
}

// publishMedia handles the 3-step image/video flow: register → upload → share.
func (c *Client) publishMedia(ctx context.Context, content, mediaURL string, conn *domain.PlatformConnection) (*platform.PublishResult, error) {
	data, contentType, err := c.downloadMedia(ctx, mediaURL)
	if err != nil {
		return nil, fmt.Errorf("linkedin: download media: %w", err)
	}

	var recipe, mediaCategory string
	switch {
	case strings.HasPrefix(contentType, "video/"):
		recipe = "urn:li:digitalmediaRecipe:feedshare-video"
		mediaCategory = "VIDEO"
	case strings.HasPrefix(contentType, "image/"):
		recipe = "urn:li:digitalmediaRecipe:feedshare-image"
		mediaCategory = "IMAGE"
	default:
		return nil, fmt.Errorf("linkedin: unsupported media content type %q for %s", contentType, mediaURL)
	}

	uploadURL, asset, extraHeaders, err := c.registerUpload(ctx, conn.AccessToken, conn.AccountID, recipe)
	if err != nil {
		return nil, fmt.Errorf("linkedin: register upload: %w", err)
	}

	if err := c.uploadAsset(ctx, uploadURL, conn.AccessToken, data, contentType, extraHeaders); err != nil {
		return nil, fmt.Errorf("linkedin: upload asset: %w", err)
	}

	body := buildMediaShare(conn.AccountID, content, mediaCategory, asset)
	return c.publishShare(ctx, body, conn.AccessToken)
}

// publishShare sends a fully-built UGC post payload to the ugcPosts endpoint.
// Shared by text, article, and media shares — only the payload differs.
func (c *Client) publishShare(ctx context.Context, body map[string]any, accessToken string) (*platform.PublishResult, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("linkedin: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ugcPostURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("linkedin: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Restli-Protocol-Version", "2.0.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("linkedin: publish request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("linkedin: publish failed (%d): %s", resp.StatusCode, string(raw))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("linkedin: decode response: %w", err)
	}

	return &platform.PublishResult{
		PlatformPostID: result.ID,
		URL:            "https://www.linkedin.com/feed/update/" + result.ID,
	}, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Asset upload helpers (image/video)
// ──────────────────────────────────────────────────────────────────────────────

// registerUpload asks LinkedIn to prepare an upload slot for an image or
// video asset. Returns the URL to PUT the binary to, the asset URN to
// reference in the share payload, and any extra headers LinkedIn wants set
// on the upload PUT request.
func (c *Client) registerUpload(ctx context.Context, accessToken, authorURN, recipe string) (uploadURL, asset string, extraHeaders map[string]string, err error) {
	payload := map[string]any{
		"registerUploadRequest": map[string]any{
			"recipes": []string{recipe},
			"owner":   authorURN,
			"serviceRelationships": []map[string]any{
				{
					"relationshipType": "OWNER",
					"identifier":       "urn:li:userGeneratedContent",
				},
			},
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", "", nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registerUploadURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", "", nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Restli-Protocol-Version", "2.0.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return "", "", nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(raw))
	}

	var result struct {
		Value struct {
			UploadMechanism struct {
				HTTPRequest struct {
					UploadURL string            `json:"uploadUrl"`
					Headers   map[string]string `json:"headers"`
				} `json:"com.linkedin.digitalmedia.uploading.MediaUploadHttpRequest"`
			} `json:"uploadMechanism"`
			Asset string `json:"asset"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", nil, err
	}

	if result.Value.UploadMechanism.HTTPRequest.UploadURL == "" || result.Value.Asset == "" {
		return "", "", nil, fmt.Errorf("incomplete registerUpload response")
	}

	return result.Value.UploadMechanism.HTTPRequest.UploadURL,
		result.Value.Asset,
		result.Value.UploadMechanism.HTTPRequest.Headers,
		nil
}

// uploadAsset PUTs the raw media bytes to the upload URL LinkedIn issued.
func (c *Client) uploadAsset(ctx context.Context, uploadURL, accessToken string, data []byte, contentType string, extraHeaders map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(raw))
	}
	return nil
}

// downloadMedia fetches the bytes at a media URL (e.g. a post.MediaURLs
// entry) and returns them with the detected content type. Prefers the
// response's Content-Type header; falls back to sniffing the bytes.
func (c *Client) downloadMedia(ctx context.Context, mediaURL string) (data []byte, contentType string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return nil, "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("fetch media: status %d", resp.StatusCode)
	}

	data, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	contentType = resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	if idx := strings.Index(contentType, ";"); idx != -1 {
		contentType = contentType[:idx] // strip "; charset=..."
	}

	return data, contentType, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Internal Helpers — OAuth (unchanged)
// ──────────────────────────────────────────────────────────────────────────────

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func (c *Client) fetchToken(ctx context.Context, data url.Values) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(raw))
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	return &tr, nil
}

func (c *Client) fetchProfile(ctx context.Context, accessToken string) (id, name string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, meURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("userinfo status %d: %s", resp.StatusCode, string(raw))
	}

	var profile struct {
		Sub        string `json:"sub"`
		GivenName  string `json:"given_name"`
		FamilyName string `json:"family_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return "", "", err
	}
	return "urn:li:person:" + profile.Sub,
		strings.TrimSpace(profile.GivenName + " " + profile.FamilyName),
		nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Share payload builders
// ──────────────────────────────────────────────────────────────────────────────

// buildTextShare constructs a plain text share payload (no media, no link).
func buildTextShare(authorURN, content string) map[string]any {
	return map[string]any{
		"author":         authorURN,
		"lifecycleState": "PUBLISHED",
		"specificContent": map[string]any{
			"com.linkedin.ugc.ShareContent": map[string]any{
				"shareCommentary": map[string]any{
					"text": content,
				},
				"shareMediaCategory": "NONE",
			},
		},
		"visibility": map[string]any{
			"com.linkedin.ugc.MemberNetworkVisibility": "PUBLIC",
		},
	}
}

// buildArticleShare constructs an Article/URL share payload — a link shown
// as a preview card, no uploaded media.
func buildArticleShare(authorURN, content, articleURL, title string) map[string]any {
	media := map[string]any{
		"status":      "READY",
		"originalUrl": articleURL,
	}
	if title != "" {
		media["title"] = map[string]any{"text": title}
	}

	return map[string]any{
		"author":         authorURN,
		"lifecycleState": "PUBLISHED",
		"specificContent": map[string]any{
			"com.linkedin.ugc.ShareContent": map[string]any{
				"shareCommentary": map[string]any{
					"text": content,
				},
				"shareMediaCategory": "ARTICLE",
				"media":              []map[string]any{media},
			},
		},
		"visibility": map[string]any{
			"com.linkedin.ugc.MemberNetworkVisibility": "PUBLIC",
		},
	}
}

// buildMediaShare constructs an Image or Video share payload, referencing an
// asset URN obtained from registerUpload + uploadAsset. mediaCategory must
// be "IMAGE" or "VIDEO".
func buildMediaShare(authorURN, content, mediaCategory, assetURN string) map[string]any {
	return map[string]any{
		"author":         authorURN,
		"lifecycleState": "PUBLISHED",
		"specificContent": map[string]any{
			"com.linkedin.ugc.ShareContent": map[string]any{
				"shareCommentary": map[string]any{
					"text": content,
				},
				"shareMediaCategory": mediaCategory,
				"media": []map[string]any{
					{
						"status": "READY",
						"media":  assetURN,
					},
				},
			},
		},
		"visibility": map[string]any{
			"com.linkedin.ugc.MemberNetworkVisibility": "PUBLIC",
		},
	}
}