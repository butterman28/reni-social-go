// Package x implements the platform.Publisher interface for X (Twitter v2).
//
// Auth: OAuth 1.0a (HMAC-SHA1) for user-context endpoints.
// Posting: POST https://api.twitter.com/2/tweets
// Media upload: POST https://upload.twitter.com/1.1/media/upload.json (chunked INIT/APPEND/FINALIZE)
//
// OAuth 1.0a callback flow:
//  1. GET  /oauth/request_token  → get oauth_token (request token)
//  2. Redirect user to https://api.twitter.com/oauth/authorize?oauth_token=...
//  3. GET  /oauth/access_token   → exchange verifier for access token + secret
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
	xAuthorizeURL   = "https://api.twitter.com/oauth/authorize"
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
func (c *Client) RequestToken(ctx context.Context, id string, platform string) (oauthToken, authorizeURL string, err error) {
	header := c.buildOAuth1Header(http.MethodPost, requestTokenURL, url.Values{
		"oauth_callback": {config.C.BackUrl + "/workspaces/in/" + id + "/connect/in/" + platform + "/callback"},
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
	fmt.Println("AUTH HEADER:")
	fmt.Println(header)

	return token, xAuthorizeURL + "?oauth_token=" + token, nil
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
				headerParts = append(headerParts, k+`="`+url.QueryEscape(v)+`"`)
			}
		}
	}
	sort.Strings(headerParts)
	return "OAuth " + strings.Join(headerParts, ", ")
}

// ──────────────────────────────────────────────────────────────────────────────
// Fetching Objects (X API v2)
// ──────────────────────────────────────────────────────────────────────────────

// GetTweet fetches a single tweet by ID.
// Use params to specify tweet.fields, expansions, etc.
func (c *Client) GetTweet(ctx context.Context, conn *domain.PlatformConnection, tweetID string, params url.Values) (map[string]any, error) {
	endpoint := fmt.Sprintf("https://api.twitter.com/2/tweets/%s", tweetID)
	return c.doV2Get(ctx, conn, endpoint, params)
}

// GetUser fetches a single user by ID.
// Use params to specify user.fields.
func (c *Client) GetUser(ctx context.Context, conn *domain.PlatformConnection, userID string, params url.Values) (map[string]any, error) {
	endpoint := fmt.Sprintf("https://api.twitter.com/2/users/%s", userID)
	return c.doV2Get(ctx, conn, endpoint, params)
}

// GetSpace fetches a single space by ID.
// Use params to specify space.fields.
func (c *Client) GetSpace(ctx context.Context, conn *domain.PlatformConnection, spaceID string, params url.Values) (map[string]any, error) {
	endpoint := fmt.Sprintf("https://api.twitter.com/2/spaces/%s", spaceID)
	return c.doV2Get(ctx, conn, endpoint, params)
}

// GetList fetches a single list by ID.
// Use params to specify list.fields.
func (c *Client) GetList(ctx context.Context, conn *domain.PlatformConnection, listID string, params url.Values) (map[string]any, error) {
	endpoint := fmt.Sprintf("https://api.twitter.com/2/lists/%s", listID)
	return c.doV2Get(ctx, conn, endpoint, params)
}

// GetTweetWithIncludes fetches a tweet and expands all available includes:
// Media, Polls, Places, and the Author (User).
// This demonstrates how to use media.fields, poll.fields, and place.fields,
// as X API v2 does not have direct endpoints for these objects.
func (c *Client) GetTweetWithIncludes(ctx context.Context, conn *domain.PlatformConnection, tweetID string) (map[string]any, error) {
	params := url.Values{}

	// Expansions tell X to include the full objects for these IDs
	params.Set("expansions", "attachments.media_keys,attachments.poll_ids,geo.place_id,author_id")

	// Fields for the tweet itself
	params.Set("tweet.fields", "created_at,author_id,geo,attachments,entities")

	// Fields for the expanded objects (Media, Poll, Place, User)
	params.Set("media.fields", "media_key,type,url,preview_image_url,alt_text")
	params.Set("poll.fields", "id,options,voting_status,end_datetime")
	params.Set("place.fields", "id,full_name,country,country_code,geo")
	params.Set("user.fields", "id,name,username,profile_image_url")

	return c.GetTweet(ctx, conn, tweetID, params)
}

// doV2Get is a helper that signs a GET request with OAuth 1.0a and returns the decoded JSON.
func (c *Client) doV2Get(ctx context.Context, conn *domain.PlatformConnection, endpoint string, params url.Values) (map[string]any, error) {
	accessToken := conn.AccessToken
	accessSecret := conn.RefreshToken

	// buildOAuth1Header will automatically include the query params in the signature base string
	header := c.buildOAuth1Header(http.MethodGet, endpoint, params,
		config.C.XAPIKey, config.C.XAPISecret, accessToken, accessSecret)

	rawURL := endpoint
	if len(params) > 0 {
		rawURL += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("x: build request: %w", err)
	}
	req.Header.Set("Authorization", header)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("x: do request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("x: request failed (%d): %s", resp.StatusCode, string(bodyBytes))
	}

	// We return map[string]any because X API v2 returns dynamic fields based on what you request
	var result map[string]any
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("x: decode response: %w", err)
	}

	return result, nil
}

func generateNonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}
