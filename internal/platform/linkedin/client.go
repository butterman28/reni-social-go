// Package linkedin implements the platform.Publisher interface for LinkedIn.
// It uses the LinkedIn Share API v2 (UGC Posts endpoint).
//
// OAuth2 flow:
//   Authorize → https://www.linkedin.com/oauth/v2/authorization
//   Token     → https://www.linkedin.com/oauth/v2/accessToken
//   Refresh   → same token endpoint with grant_type=refresh_token
//
// Publishing:
//   POST https://api.linkedin.com/v2/ugcPosts
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

const (
	authURL    = "https://www.linkedin.com/oauth/v2/authorization"
	tokenURL   = "https://www.linkedin.com/oauth/v2/accessToken"
	ugcPostURL = "https://api.linkedin.com/v2/ugcPosts"
	meURL      = "https://api.linkedin.com/v2/me"
)

// Client is the LinkedIn platform adapter.
type Client struct {
	httpClient *http.Client
}

// New returns a new LinkedIn Client.
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

// AuthorizationURL returns the URL the user should be redirected to for consent.
func AuthorizationURL(state string) string {
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

	// Fetch the authenticated user's LinkedIn URN (e.g. "urn:li:person:ABC123")
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
func (c *Client) Publish(ctx context.Context, post *domain.Post, conn *domain.PlatformConnection) (*platform.PublishResult, error) {
	body := buildUGCPost(conn.AccountID, post.Content)

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("linkedin: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ugcPostURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("linkedin: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+conn.AccessToken)
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
// Internal Helpers
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, meURL+"?projection=(id,localizedFirstName,localizedLastName)", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var profile struct {
		ID                 string `json:"id"`
		LocalizedFirstName string `json:"localizedFirstName"`
		LocalizedLastName  string `json:"localizedLastName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return "", "", err
	}
	return "urn:li:person:" + profile.ID,
		strings.TrimSpace(profile.LocalizedFirstName + " " + profile.LocalizedLastName),
		nil
}

// buildUGCPost constructs the LinkedIn UGC Post payload.
func buildUGCPost(authorURN, content string) map[string]any {
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
