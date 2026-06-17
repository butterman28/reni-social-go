// Package x implements the platform.Publisher interface for X (Twitter v2).
//
// Auth: OAuth 1.0a (HMAC-SHA1) for user-context endpoints.
// Posting: POST https://api.twitter.com/2/tweets
// Media upload: POST https://upload.twitter.com/1.1/media/upload.json (chunked INIT/APPEND/FINALIZE)
//
// OAuth 1.0a callback flow:
//   1. GET  /oauth/request_token  → get oauth_token (request token)
//   2. Redirect user to https://api.twitter.com/oauth/authorize?oauth_token=...
//   3. GET  /oauth/access_token   → exchange verifier for access token + secret
package x

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"reni-social/internal/domain"
	"reni-social/internal/platform"
	"reni-social/pkg/config"
)

const (
	requestTokenURL = "https://api.twitter.com/oauth/request_token"
	authorizeURL    = "https://api.twitter.com/oauth/authorize"
	accessTokenURL  = "https://api.twitter.com/oauth/access_token"
	tweetsURL       = "https://api.twitter.com/2/tweets"
	mediaUploadURL  = "https://upload.twitter.com/1.1/media/upload.json"
)

// Client is the X platform adapter.
type Client struct {
	httpClient *http.Client
}

// New returns a new X Client.
func New() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

// Ensure Client satisfies the Publisher interface at compile time.
var _ platform.Publisher = (*Client)(nil)

// ──────────────────────────────────────────────────────────────────────────────
// OAuth 1.0a Helpers
// ──────────────────────────────────────────────────────────────────────────────

// RequestToken fetches a request token and returns (oauthToken, authorizeURL, error).
// Store oauthToken in Redis keyed by state so the callback can look it up.
func (c *Client) RequestToken(ctx context.Context) (oauthToken, authorizeURL string, err error) {
	header := c.buildOAuth1Header(http.MethodPost, requestTokenURL, url.Values{
		"oauth_callback": {config.C.XRedirectURL},
	}, config.C.XAPIKey, config.C.XAPISecret, "", "")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestTokenURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", header)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("x: request token: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	params, err := url.ParseQuery(string(body))
	if err != nil {
		return "", "", fmt.Errorf("x: parse request token response: %w", err)
	}

	token := params.Get("oauth_token")
	if token == "" {
		return "", "", fmt.Errorf("x: empty oauth_token in response: %s", string(body))
	}

	return token, authorizeURL + "?oauth_token=" + token, nil
}

// ExchangeVerifier exchanges an oauth_token + oauth_verifier for permanent access credentials.
func (c *Client) ExchangeVerifier(ctx context.Context, oauthToken, oauthVerifier string) (*domain.PlatformConnection, error) {
	params := url.Values{
		"oauth_verifier": {oauthVerifier},
	}
	header := c.buildOAuth1Header(http.MethodPost, accessTokenURL, params,
		config.C.XAPIKey, config.C.XAPISecret, oauthToken, "")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		accessTokenURL+"?oauth_verifier="+url.QueryEscape(oauthVerifier), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", header)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("x: access token: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	p, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, fmt.Errorf("x: parse access token: %w", err)
	}

	return &domain.PlatformConnection{
		Platform:     domain.PlatformX,
		AccountID:    p.Get("user_id"),
		AccountName:  p.Get("screen_name"),
		AccessToken:  p.Get("oauth_token"),
		RefreshToken: p.Get("oauth_token_secret"), // stored in RefreshToken field
	}, nil
}

// RefreshToken — X OAuth 1.0a tokens don't expire; this is a no-op passthrough.
func (c *Client) RefreshToken(_ context.Context, conn *domain.PlatformConnection) (*domain.PlatformConnection, error) {
	// OAuth 1.0a tokens are permanent until revoked. Nothing to refresh.
	return conn, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Publishing
// ──────────────────────────────────────────────────────────────────────────────

// Publish posts a tweet using the X API v2.
func (c *Client) Publish(ctx context.Context, post *domain.Post, conn *domain.PlatformConnection) (*platform.PublishResult, error) {
	// conn.AccessToken = oauth_token, conn.RefreshToken = oauth_token_secret
	accessToken := conn.AccessToken
	accessSecret := conn.RefreshToken

	payload := map[string]any{
		"text": post.Content,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("x: marshal tweet payload: %w", err)
	}

	header := c.buildOAuth1Header(http.MethodPost, tweetsURL, url.Values{},
		config.C.XAPIKey, config.C.XAPISecret, accessToken, accessSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tweetsURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("x: build tweet request: %w", err)
	}
	req.Header.Set("Authorization", header)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("x: post tweet: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("x: tweet failed (%d): %s", resp.StatusCode, string(raw))
	}

	var result struct {
		Data struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("x: decode tweet response: %w", err)
	}

	return &platform.PublishResult{
		PlatformPostID: result.Data.ID,
		URL:            "https://twitter.com/i/web/status/" + result.Data.ID,
	}, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// OAuth 1.0a Signature Builder
// ──────────────────────────────────────────────────────────────────────────────

// buildOAuth1Header constructs a signed OAuth 1.0a Authorization header.
func (c *Client) buildOAuth1Header(method, rawURL string, extraParams url.Values,
	consumerKey, consumerSecret, oauthToken, tokenSecret string) string {

	nonce := generateNonce()
	ts := fmt.Sprintf("%d", time.Now().Unix())

	oauthParams := url.Values{
		"oauth_consumer_key":     {consumerKey},
		"oauth_nonce":            {nonce},
		"oauth_signature_method": {"HMAC-SHA1"},
		"oauth_timestamp":        {ts},
		"oauth_version":          {"1.0"},
	}
	if oauthToken != "" {
		oauthParams.Set("oauth_token", oauthToken)
	}
	// Merge extra params for signature base
	for k, vs := range extraParams {
		for _, v := range vs {
			oauthParams.Add(k, v)
		}
	}

	// Build signature base string
	var keys []string
	for k := range oauthParams {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var pairs []string
	for _, k := range keys {
		for _, v := range oauthParams[k] {
			pairs = append(pairs, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	paramString := strings.Join(pairs, "&")

	baseString := strings.Join([]string{
		url.QueryEscape(method),
		url.QueryEscape(rawURL),
		url.QueryEscape(paramString),
	}, "&")

	signingKey := url.QueryEscape(consumerSecret) + "&" + url.QueryEscape(tokenSecret)
	mac := hmac.New(sha1.New, []byte(signingKey))
	mac.Write([]byte(baseString))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	oauthParams.Set("oauth_signature", sig)

	// Build Authorization header (only oauth_ params)
	var headerParts []string
	for k, vs := range oauthParams {
		if strings.HasPrefix(k, "oauth_") {
			for _, v := range vs {
				headerParts = append(headerParts, `"`+url.QueryEscape(k)+`"="`+url.QueryEscape(v)+`"`)
			}
		}
	}
	sort.Strings(headerParts)
	return "OAuth " + strings.Join(headerParts, ", ")
}

func generateNonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}
