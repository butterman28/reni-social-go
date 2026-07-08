// Package tiktok implements the platform.Publisher interface for TikTok,
// using TikTok's OAuth v2 (Login Kit, PKCE-required) and the Content
// Posting API (video publish via PULL_FROM_URL).
//
// OAuth2 flow (PKCE mandatory):
//
//	Authorize → https://www.tiktok.com/v2/auth/authorize/
//	Token     → https://open.tiktokapis.com/v2/oauth/token/
//	Refresh   → same token endpoint with grant_type=refresh_token
//
// Publishing:
//
//	Init   → POST /v2/post/publish/video/init/   (source: PULL_FROM_URL)
//	Status → POST /v2/post/publish/status/fetch/
//
// IMPORTANT: unaudited TikTok apps can only publish with privacy_level
// SELF_ONLY (visible only to the connected account) — public posting
// requires TikTok app review. TikTok has no text-only or image-only post
// type through this API; a video URL is always required.
package tiktok

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	authURL      = "https://www.tiktok.com/v2/auth/authorize/"
	tokenURL     = "https://open.tiktokapis.com/v2/oauth/token/"
	userInfoURL  = "https://open.tiktokapis.com/v2/user/info/"
	videoInitURL = "https://open.tiktokapis.com/v2/post/publish/video/init/"
	statusURL    = "https://open.tiktokapis.com/v2/post/publish/status/fetch/"
)

// Client is the TikTok platform adapter.
type Client struct {
	httpClient *http.Client
}

// New returns a new TikTok Client.
func New() *Client {
	return &Client{httpClient: &http.Client{Timeout: 30 * time.Second}}
}

// Ensure Client satisfies the Publisher interface at compile time.
var _ platform.Publisher = (*Client)(nil)

// ──────────────────────────────────────────────────────────────────────────────
// PKCE
// ──────────────────────────────────────────────────────────────────────────────

// GeneratePKCE returns a random code_verifier and its S256 code_challenge.
// TikTok's OAuth v2 requires PKCE on every authorization request.
func GeneratePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = hex.EncodeToString(b) // 64 chars, valid PKCE charset
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// OAuth2 Helpers
// ──────────────────────────────────────────────────────────────────────────────

// AuthorizationURL returns the consent URL. codeChallenge comes from GeneratePKCE.
func AuthorizationURL(state, codeChallenge string) string {
	params := url.Values{}
	params.Set("client_key", config.C.TikTokClientKey)
	params.Set("scope", "user.info.basic,video.publish,video.upload")
	params.Set("response_type", "code")
	params.Set("redirect_uri", config.C.TikTokRedirectURL)
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	return authURL + "?" + params.Encode()
}

// ExchangeCode exchanges an authorization code + PKCE verifier for an access token.
func (c *Client) ExchangeCode(ctx context.Context, code, codeVerifier string) (*domain.PlatformConnection, error) {
	form := url.Values{}
	form.Set("client_key", config.C.TikTokClientKey)
	form.Set("client_secret", config.C.TikTokClientSecret)
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", config.C.TikTokRedirectURL)
	form.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("tiktok: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tiktok: token exchange: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tiktok: token status %d: %s", resp.StatusCode, string(raw))
	}

	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		OpenID       string `json:"open_id"`
	}
	if err := json.Unmarshal(raw, &tok); err != nil {
		return nil, fmt.Errorf("tiktok: decode token: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("tiktok: empty access_token in response: %s", string(raw))
	}

	expiresAt := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	return &domain.PlatformConnection{
		Platform:     domain.PlatformTikTok,
		AccountID:    tok.OpenID,
		AccountName:  c.fetchDisplayName(ctx, tok.AccessToken),
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    &expiresAt,
	}, nil
}

// RefreshToken exchanges a refresh token for a new access token.
func (c *Client) RefreshToken(ctx context.Context, conn *domain.PlatformConnection) (*domain.PlatformConnection, error) {
	form := url.Values{}
	form.Set("client_key", config.C.TikTokClientKey)
	form.Set("client_secret", config.C.TikTokClientSecret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", conn.RefreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("tiktok: build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tiktok: refresh token: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tiktok: refresh status %d: %s", resp.StatusCode, string(raw))
	}

	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &tok); err != nil {
		return nil, fmt.Errorf("tiktok: decode refresh: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	updated := *conn
	updated.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		updated.RefreshToken = tok.RefreshToken
	}
	updated.ExpiresAt = &expiresAt
	return &updated, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Publishing
// ──────────────────────────────────────────────────────────────────────────────

// Publish posts a video to TikTok, pulling it from a public URL
// (post.MediaURLs[0]). Requires at least one video URL — TikTok's Content
// Posting API has no text-only or image-only post type.
func (c *Client) Publish(ctx context.Context, post *domain.Post, conn *domain.PlatformConnection) (*platform.PublishResult, error) {
	if len(post.MediaURLs) == 0 {
		return nil, fmt.Errorf("tiktok: requires a video URL — text-only/image-only posts are not supported by the API")
	}

	payload := map[string]any{
		"post_info": map[string]any{
			"title": post.Content,
			// Unaudited apps are restricted to SELF_ONLY by TikTok policy.
			"privacy_level": "SELF_ONLY",
		},
		"source_info": map[string]any{
			"source":    "PULL_FROM_URL",
			"video_url": post.MediaURLs[0],
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("tiktok: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, videoInitURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tiktok: build publish request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+conn.AccessToken)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tiktok: publish init: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tiktok: publish status %d: %s", resp.StatusCode, string(raw))
	}

	var result struct {
		Data struct {
			PublishID string `json:"publish_id"`
		} `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("tiktok: decode publish response: %w", err)
	}
	if result.Error.Code != "" && result.Error.Code != "ok" {
		return nil, fmt.Errorf("tiktok: publish error %s: %s", result.Error.Code, result.Error.Message)
	}

	return &platform.PublishResult{PlatformPostID: result.Data.PublishID}, nil
}

// FetchPublishStatus checks the processing status of a publish_id returned
// by Publish. Not part of the Publisher interface — call directly to poll.
func (c *Client) FetchPublishStatus(ctx context.Context, conn *domain.PlatformConnection, publishID string) (map[string]any, error) {
	body, _ := json.Marshal(map[string]string{"publish_id": publishID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, statusURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+conn.AccessToken)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tiktok: status check %d: %s", resp.StatusCode, string(raw))
	}

	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) fetchDisplayName(ctx context.Context, accessToken string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoURL+"?fields=open_id,display_name", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Data struct {
			User struct {
				DisplayName string `json:"display_name"`
			} `json:"user"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return result.Data.User.DisplayName
}
