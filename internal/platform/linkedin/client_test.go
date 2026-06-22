package linkedin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reni-social/internal/domain"
)

// helper: swap package URLs for a fake server, restore after test
func withFakeServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// ── AuthorizationURL ──────────────────────────────────────────────
func TestAuthorizationURL(t *testing.T) {
	url := AuthorizationURL("state123")
	if !strings.Contains(url, "state=state123") {
		t.Errorf("expected state param in URL, got: %s", url)
	}
	if !strings.Contains(url, "scope=") {
		t.Errorf("expected scope param in URL, got: %s", url)
	}
}

// ── buildTextShare (pure function) ──────────────────────────────────
func TestBuildTextShare(t *testing.T) {
	payload := buildTextShare("urn:li:person:abc123", "hello world")
	if payload["author"] != "urn:li:person:abc123" {
		t.Errorf("author mismatch: %v", payload["author"])
	}
	content := payload["specificContent"].(map[string]any)["com.linkedin.ugc.ShareContent"].(map[string]any)
	if content["shareMediaCategory"] != "NONE" {
		t.Errorf("expected shareMediaCategory NONE, got %v", content["shareMediaCategory"])
	}
	commentary := content["shareCommentary"].(map[string]any)
	if commentary["text"] != "hello world" {
		t.Errorf("text mismatch: %v", commentary["text"])
	}
}

// ── buildArticleShare (pure function) ───────────────────────────────
func TestBuildArticleShare(t *testing.T) {
	payload := buildArticleShare("urn:li:person:abc123", "check this out", "https://example.com/post", "Example Title")
	content := payload["specificContent"].(map[string]any)["com.linkedin.ugc.ShareContent"].(map[string]any)
	if content["shareMediaCategory"] != "ARTICLE" {
		t.Errorf("expected shareMediaCategory ARTICLE, got %v", content["shareMediaCategory"])
	}
	media := content["media"].([]map[string]any)
	if len(media) != 1 {
		t.Fatalf("expected 1 media entry, got %d", len(media))
	}
	if media[0]["originalUrl"] != "https://example.com/post" {
		t.Errorf("originalUrl mismatch: %v", media[0]["originalUrl"])
	}
	title := media[0]["title"].(map[string]any)
	if title["text"] != "Example Title" {
		t.Errorf("title mismatch: %v", title["text"])
	}
}

// ── buildArticleShare with no title ──────────────────────────────────
func TestBuildArticleShareNoTitle(t *testing.T) {
	payload := buildArticleShare("urn:li:person:abc123", "check this out", "https://example.com/post", "")
	content := payload["specificContent"].(map[string]any)["com.linkedin.ugc.ShareContent"].(map[string]any)
	media := content["media"].([]map[string]any)
	if _, ok := media[0]["title"]; ok {
		t.Errorf("expected no title key when title is empty, got %v", media[0]["title"])
	}
}

// ── buildMediaShare (pure function) ──────────────────────────────────
func TestBuildMediaShare(t *testing.T) {
	payload := buildMediaShare("urn:li:person:abc123", "look at this", "IMAGE", "urn:li:digitalmediaAsset:xyz")
	content := payload["specificContent"].(map[string]any)["com.linkedin.ugc.ShareContent"].(map[string]any)
	if content["shareMediaCategory"] != "IMAGE" {
		t.Errorf("expected shareMediaCategory IMAGE, got %v", content["shareMediaCategory"])
	}
	media := content["media"].([]map[string]any)
	if media[0]["media"] != "urn:li:digitalmediaAsset:xyz" {
		t.Errorf("asset URN mismatch: %v", media[0]["media"])
	}
	if media[0]["status"] != "READY" {
		t.Errorf("expected status READY, got %v", media[0]["status"])
	}
}

// ── fetchToken ─────────────────────────────────────────────────────
func TestFetchToken(t *testing.T) {
	srv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "fake-access-token",
			"refresh_token": "fake-refresh-token",
			"expires_in":    3600,
		})
	})

	old := tokenURL
	tokenURL = srv.URL
	defer func() { tokenURL = old }()

	c := New()
	tok, err := c.fetchToken(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok.AccessToken != "fake-access-token" {
		t.Errorf("got %s", tok.AccessToken)
	}
}

// ── fetchProfile (OIDC userinfo shape) ────────────────────────────────
func TestFetchProfile(t *testing.T) {
	srv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"sub":         "12345",
			"given_name":  "Ada",
			"family_name": "Lovelace",
		})
	})

	old := meURL
	meURL = srv.URL
	defer func() { meURL = old }()

	c := New()
	id, name, err := c.fetchProfile(context.Background(), "fake-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "urn:li:person:12345" {
		t.Errorf("got id %s", id)
	}
	if name != "Ada Lovelace" {
		t.Errorf("got name %s", name)
	}
}

// ── ExchangeCode (combines fetchToken + fetchProfile) ───────────────
func TestExchangeCode(t *testing.T) {
	tokenSrv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "tok-abc",
			"refresh_token": "ref-abc",
			"expires_in":    3600,
		})
	})
	profileSrv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"sub":         "999",
			"given_name":  "Grace",
			"family_name": "Hopper",
		})
	})

	oldTok, oldMe := tokenURL, meURL
	tokenURL, meURL = tokenSrv.URL, profileSrv.URL
	defer func() { tokenURL, meURL = oldTok, oldMe }()

	c := New()
	conn, err := c.ExchangeCode(context.Background(), "fake-auth-code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conn.AccessToken != "tok-abc" {
		t.Errorf("got %s", conn.AccessToken)
	}
	if conn.AccountName != "Grace Hopper" {
		t.Errorf("got %s", conn.AccountName)
	}
}

// ── Publish (text — no MediaURLs) ───────────────────────────────────
func TestPublishText(t *testing.T) {
	srv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fake-token" {
			t.Errorf("missing/incorrect auth header: %s", r.Header.Get("Authorization"))
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		content := body["specificContent"].(map[string]any)["com.linkedin.ugc.ShareContent"].(map[string]any)
		if content["shareMediaCategory"] != "NONE" {
			t.Errorf("expected NONE category for plain Publish, got %v", content["shareMediaCategory"])
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "post-123"})
	})

	old := ugcPostURL
	ugcPostURL = srv.URL
	defer func() { ugcPostURL = old }()

	c := New()
	post := &domain.Post{Content: "test post"}
	conn := &domain.PlatformConnection{AccessToken: "fake-token", AccountID: "urn:li:person:1"}

	result, err := c.Publish(context.Background(), post, conn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PlatformPostID != "post-123" {
		t.Errorf("got %s", result.PlatformPostID)
	}
}

// ── PublishArticle ───────────────────────────────────────────────────
func TestPublishArticle(t *testing.T) {
	srv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		content := body["specificContent"].(map[string]any)["com.linkedin.ugc.ShareContent"].(map[string]any)
		if content["shareMediaCategory"] != "ARTICLE" {
			t.Errorf("expected ARTICLE category, got %v", content["shareMediaCategory"])
		}
		media := content["media"].([]any)
		entry := media[0].(map[string]any)
		if entry["originalUrl"] != "https://example.com/article" {
			t.Errorf("originalUrl mismatch: %v", entry["originalUrl"])
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "post-article-1"})
	})

	old := ugcPostURL
	ugcPostURL = srv.URL
	defer func() { ugcPostURL = old }()

	c := New()
	conn := &domain.PlatformConnection{AccessToken: "fake-token", AccountID: "urn:li:person:1"}

	result, err := c.PublishArticle(context.Background(), conn, "check this out", "https://example.com/article", "An Example")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PlatformPostID != "post-article-1" {
		t.Errorf("got %s", result.PlatformPostID)
	}
}

// ── downloadMedia ────────────────────────────────────────────────────
func TestDownloadMedia(t *testing.T) {
	srv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake-png-bytes"))
	})

	c := New()
	data, contentType, err := c.downloadMedia(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if contentType != "image/png" {
		t.Errorf("got content type %s", contentType)
	}
	if string(data) != "fake-png-bytes" {
		t.Errorf("got data %q", string(data))
	}
}

// ── registerUpload ────────────────────────────────────────────────────
func TestRegisterUpload(t *testing.T) {
	uploadDestSrv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	srv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		req := body["registerUploadRequest"].(map[string]any)
		recipes := req["recipes"].([]any)
		if recipes[0] != "urn:li:digitalmediaRecipe:feedshare-image" {
			t.Errorf("recipe mismatch: %v", recipes[0])
		}

		json.NewEncoder(w).Encode(map[string]any{
			"value": map[string]any{
				"uploadMechanism": map[string]any{
					"com.linkedin.digitalmedia.uploading.MediaUploadHttpRequest": map[string]any{
						"uploadUrl": uploadDestSrv.URL,
						"headers":   map[string]string{"X-Test": "1"},
					},
				},
				"asset": "urn:li:digitalmediaAsset:abc",
			},
		})
	})

	old := registerUploadURL
	registerUploadURL = srv.URL
	defer func() { registerUploadURL = old }()

	c := New()
	uploadURL, asset, headers, err := c.registerUpload(context.Background(), "fake-token", "urn:li:person:1", "urn:li:digitalmediaRecipe:feedshare-image")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uploadURL != uploadDestSrv.URL {
		t.Errorf("uploadURL mismatch: %s", uploadURL)
	}
	if asset != "urn:li:digitalmediaAsset:abc" {
		t.Errorf("asset mismatch: %s", asset)
	}
	if headers["X-Test"] != "1" {
		t.Errorf("expected extra header to be parsed, got %v", headers)
	}
}

// ── uploadAsset ────────────────────────────────────────────────────────
func TestUploadAsset(t *testing.T) {
	var receivedBody []byte
	var receivedAuth, receivedExtra string

	srv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		receivedAuth = r.Header.Get("Authorization")
		receivedExtra = r.Header.Get("X-Test")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	})

	c := New()
	err := c.uploadAsset(context.Background(), srv.URL, "fake-token", []byte("binary-data"), "image/png", map[string]string{"X-Test": "yes"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedAuth != "Bearer fake-token" {
		t.Errorf("auth header mismatch: %s", receivedAuth)
	}
	if receivedExtra != "yes" {
		t.Errorf("extra header mismatch: %s", receivedExtra)
	}
	if string(receivedBody) != "binary-data" {
		t.Errorf("body mismatch: %s", string(receivedBody))
	}
}

// ── Publish end-to-end with media (register → upload → share) ──────────
func TestPublishWithMedia(t *testing.T) {
	mediaSrv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake-image-bytes"))
	})

	uploadDestSrv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != "fake-image-bytes" {
			t.Errorf("upload body mismatch: %s", string(body))
		}
		w.WriteHeader(http.StatusCreated)
	})

	registerSrv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"value": map[string]any{
				"uploadMechanism": map[string]any{
					"com.linkedin.digitalmedia.uploading.MediaUploadHttpRequest": map[string]any{
						"uploadUrl": uploadDestSrv.URL,
						"headers":   map[string]string{},
					},
				},
				"asset": "urn:li:digitalmediaAsset:img1",
			},
		})
	})

	shareSrv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		content := body["specificContent"].(map[string]any)["com.linkedin.ugc.ShareContent"].(map[string]any)
		if content["shareMediaCategory"] != "IMAGE" {
			t.Errorf("expected IMAGE category, got %v", content["shareMediaCategory"])
		}
		media := content["media"].([]any)
		entry := media[0].(map[string]any)
		if entry["media"] != "urn:li:digitalmediaAsset:img1" {
			t.Errorf("asset URN mismatch: %v", entry["media"])
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "post-media-1"})
	})

	oldRegister, oldShare := registerUploadURL, ugcPostURL
	registerUploadURL, ugcPostURL = registerSrv.URL, shareSrv.URL
	defer func() { registerUploadURL, ugcPostURL = oldRegister, oldShare }()

	c := New()
	post := &domain.Post{Content: "check out this image", MediaURLs: []string{mediaSrv.URL}}
	conn := &domain.PlatformConnection{AccessToken: "fake-token", AccountID: "urn:li:person:1"}

	result, err := c.Publish(context.Background(), post, conn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PlatformPostID != "post-media-1" {
		t.Errorf("got %s", result.PlatformPostID)
	}
}

// ── RefreshToken ───────────────────────────────────────────────────
func TestRefreshToken(t *testing.T) {
	srv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-token",
			"expires_in":   3600,
		})
	})

	old := tokenURL
	tokenURL = srv.URL
	defer func() { tokenURL = old }()

	c := New()
	conn := &domain.PlatformConnection{AccessToken: "old", RefreshToken: "ref"}
	updated, err := c.RefreshToken(context.Background(), conn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.AccessToken != "new-token" {
		t.Errorf("got %s", updated.AccessToken)
	}
}