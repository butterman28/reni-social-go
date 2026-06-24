package meta

import (
	"context"
	"encoding/json"
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
	if !strings.Contains(url, "instagram_content_publish") {
		t.Errorf("expected instagram_content_publish scope in URL, got: %s", url)
	}
}

// ── ExchangeCode (Facebook) ─────────────────────────────────────────
func TestExchangeCodeFacebook(t *testing.T) {
	tokenSrv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fake-long-lived-token",
			"expires_in":   5184000,
		})
	})
	graphSrv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":   "fb-user-1",
			"name": "Ada Lovelace",
		})
	})

	oldToken, oldGraph := tokenURL, graphBase
	tokenURL, graphBase = tokenSrv.URL, graphSrv.URL
	defer func() { tokenURL, graphBase = oldToken, oldGraph }()

	c := New()
	conn, err := c.ExchangeCode(context.Background(), "fake-code", "meta_fb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conn.Platform != domain.PlatformMetaFB {
		t.Errorf("expected PlatformMetaFB, got %v", conn.Platform)
	}
	if conn.AccountID != "fb-user-1" {
		t.Errorf("got account id %s", conn.AccountID)
	}
	if conn.AccessToken != "fake-long-lived-token" {
		t.Errorf("got token %s", conn.AccessToken)
	}
}

// ── ExchangeCode (Instagram) — now resolves through Page → IG account ──
func TestExchangeCodeInstagram(t *testing.T) {
	tokenSrv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fake-user-token",
			"expires_in":   5184000,
		})
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/me/accounts", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "page-1", "name": "My Page", "access_token": "page-token-1"},
			},
		})
	})
	mux.HandleFunc("/page-1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"instagram_business_account": map[string]any{"id": "ig-account-1"},
		})
	})
	mux.HandleFunc("/ig-account-1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"username": "adalovelace"})
	})
	graphSrv := httptest.NewServer(mux)
	t.Cleanup(graphSrv.Close)

	oldToken, oldGraph := tokenURL, graphBase
	tokenURL, graphBase = tokenSrv.URL, graphSrv.URL
	defer func() { tokenURL, graphBase = oldToken, oldGraph }()

	c := New()
	conn, err := c.ExchangeCode(context.Background(), "fake-code", "meta_ig")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conn.Platform != domain.PlatformMetaIG {
		t.Errorf("expected PlatformMetaIG, got %v", conn.Platform)
	}
	if conn.AccountID != "ig-account-1" {
		t.Errorf("expected real IG account id, got %s", conn.AccountID)
	}
	if conn.AccessToken != "page-token-1" {
		t.Errorf("expected Page access token, got %s", conn.AccessToken)
	}
	if conn.AccountName != "adalovelace" {
		t.Errorf("expected resolved IG username, got %s", conn.AccountName)
	}
}

// ── ExchangeCode (Instagram) — no Page with linked IG account ──────────
func TestExchangeCodeInstagramNoLinkedAccount(t *testing.T) {
	tokenSrv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fake-user-token",
			"expires_in":   5184000,
		})
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/me/accounts", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "page-1", "name": "My Page", "access_token": "page-token-1"},
			},
		})
	})
	mux.HandleFunc("/page-1", func(w http.ResponseWriter, r *http.Request) {
		// no instagram_business_account field — Page has no linked IG account
		json.NewEncoder(w).Encode(map[string]any{})
	})
	graphSrv := httptest.NewServer(mux)
	t.Cleanup(graphSrv.Close)

	oldToken, oldGraph := tokenURL, graphBase
	tokenURL, graphBase = tokenSrv.URL, graphSrv.URL
	defer func() { tokenURL, graphBase = oldToken, oldGraph }()

	c := New()
	_, err := c.ExchangeCode(context.Background(), "fake-code", "meta_ig")
	if err == nil {
		t.Fatal("expected error when no Page has a linked Instagram account, got nil")
	}
}

// ── RefreshToken ───────────────────────────────────────────────────
func TestRefreshToken(t *testing.T) {
	srv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "refreshed-token",
			"expires_in":   5184000,
		})
	})

	old := tokenURL
	tokenURL = srv.URL
	defer func() { tokenURL = old }()

	c := New()
	conn := &domain.PlatformConnection{AccessToken: "old-token"}
	updated, err := c.RefreshToken(context.Background(), conn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.AccessToken != "refreshed-token" {
		t.Errorf("got %s", updated.AccessToken)
	}
}

// ── Publish (Facebook) ───────────────────────────────────────────────
func TestPublishFacebook(t *testing.T) {
	srv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["message"] != "hello facebook" {
			t.Errorf("message mismatch: %v", body["message"])
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "fb-post-1"})
	})

	old := graphBase
	graphBase = srv.URL
	defer func() { graphBase = old }()

	c := New()
	post := &domain.Post{Content: "hello facebook"}
	conn := &domain.PlatformConnection{Platform: domain.PlatformMetaFB, AccountID: "page-1", AccessToken: "fake-token"}

	result, err := c.Publish(context.Background(), post, conn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PlatformPostID != "fb-post-1" {
		t.Errorf("got %s", result.PlatformPostID)
	}
}

// ── Publish (Instagram) — text-only is now correctly rejected ──────────
func TestPublishInstagramRequiresMedia(t *testing.T) {
	c := New()
	post := &domain.Post{Content: "no media here"}
	conn := &domain.PlatformConnection{Platform: domain.PlatformMetaIG, AccountID: "ig-account-1", AccessToken: "fake-token"}

	_, err := c.Publish(context.Background(), post, conn)
	if err == nil {
		t.Fatal("expected error for text-only Instagram post, got nil")
	}
	if !strings.Contains(err.Error(), "requires at least one image or video") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ── Publish (Instagram, image) ───────────────────────────────────────
func TestPublishInstagramWithImage(t *testing.T) {
	callCount := 0
	var containerPayload map[string]any

	srv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			json.NewDecoder(r.Body).Decode(&containerPayload)
			json.NewEncoder(w).Encode(map[string]any{"id": "container-1"})
			return
		}
		var publishPayload map[string]any
		json.NewDecoder(r.Body).Decode(&publishPayload)
		if publishPayload["creation_id"] != "container-1" {
			t.Errorf("expected creation_id container-1, got %v", publishPayload["creation_id"])
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "ig-post-1"})
	})

	old := graphBase
	graphBase = srv.URL
	defer func() { graphBase = old }()

	c := New()
	post := &domain.Post{Content: "look at this", MediaURLs: []string{"https://example.com/photo.jpg"}}
	conn := &domain.PlatformConnection{Platform: domain.PlatformMetaIG, AccountID: "ig-account-1", AccessToken: "fake-token"}

	result, err := c.Publish(context.Background(), post, conn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containerPayload["media_type"] != nil {
		t.Errorf("image containers should not set media_type, got %v", containerPayload["media_type"])
	}
	if containerPayload["image_url"] != "https://example.com/photo.jpg" {
		t.Errorf("image_url mismatch: %v", containerPayload["image_url"])
	}
	if result.PlatformPostID != "ig-post-1" {
		t.Errorf("got %s", result.PlatformPostID)
	}
}

// ── Publish (Instagram, video) — exercises waitForContainerReady too ───
func TestPublishInstagramWithVideo(t *testing.T) {
	var containerPayload map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc("/ig-account-1/media", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&containerPayload)
		json.NewEncoder(w).Encode(map[string]any{"id": "container-vid-1"})
	})
	mux.HandleFunc("/container-vid-1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"status_code": "FINISHED"})
	})
	mux.HandleFunc("/ig-account-1/media_publish", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": "ig-post-vid-1"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	old := graphBase
	graphBase = srv.URL
	defer func() { graphBase = old }()

	c := New()
	post := &domain.Post{Content: "check this video", MediaURLs: []string{"https://example.com/clip.mp4"}}
	conn := &domain.PlatformConnection{Platform: domain.PlatformMetaIG, AccountID: "ig-account-1", AccessToken: "fake-token"}

	result, err := c.Publish(context.Background(), post, conn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containerPayload["media_type"] != "VIDEO" {
		t.Errorf("expected media_type VIDEO, got %v", containerPayload["media_type"])
	}
	if containerPayload["video_url"] != "https://example.com/clip.mp4" {
		t.Errorf("video_url mismatch: %v", containerPayload["video_url"])
	}
	if result.PlatformPostID != "ig-post-vid-1" {
		t.Errorf("got %s", result.PlatformPostID)
	}
}

// ── waitForContainerReady — error states ────────────────────────────────
func TestWaitForContainerReadyError(t *testing.T) {
	srv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"status_code": "ERROR"})
	})

	old := graphBase
	graphBase = srv.URL
	defer func() { graphBase = old }()

	c := New()
	err := c.waitForContainerReady(context.Background(), "container-1", "fake-token")
	if err == nil {
		t.Fatal("expected error for ERROR status_code, got nil")
	}
}

// ── isVideoURL (pure function) ──────────────────────────────────────────
func TestIsVideoURL(t *testing.T) {
	cases := map[string]bool{
		"https://example.com/clip.mp4":  true,
		"https://example.com/movie.MOV": true,
		"https://example.com/photo.jpg": false,
		"https://example.com/photo.png": false,
		"https://example.com/no-ext":    false,
	}
	for input, want := range cases {
		if got := isVideoURL(input); got != want {
			t.Errorf("isVideoURL(%q) = %v, want %v", input, got, want)
		}
	}
}

// ── fetchPages ────────────────────────────────────────────────────────
func TestFetchPages(t *testing.T) {
	srv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "page-1", "name": "Page One", "access_token": "token-1"},
				{"id": "page-2", "name": "Page Two", "access_token": "token-2"},
			},
		})
	})

	old := graphBase
	graphBase = srv.URL
	defer func() { graphBase = old }()

	c := New()
	pages, err := c.fetchPages(context.Background(), "fake-user-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages))
	}
	if pages[0].AccessToken != "token-1" {
		t.Errorf("got %s", pages[0].AccessToken)
	}
}

// ── fetchInstagramAccount — skips pages without linked IG, finds the right one ──
func TestFetchInstagramAccountSkipsUnlinkedPages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/me/accounts", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "page-no-ig", "name": "No IG Page", "access_token": "token-a"},
				{"id": "page-with-ig", "name": "Has IG Page", "access_token": "token-b"},
			},
		})
	})
	mux.HandleFunc("/page-no-ig", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{}) // no instagram_business_account
	})
	mux.HandleFunc("/page-with-ig", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"instagram_business_account": map[string]any{"id": "ig-real-1"},
		})
	})
	mux.HandleFunc("/ig-real-1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"username": "realuser"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	old := graphBase
	graphBase = srv.URL
	defer func() { graphBase = old }()

	c := New()
	pageToken, igID, igUsername, err := c.fetchInstagramAccount(context.Background(), "fake-user-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pageToken != "token-b" {
		t.Errorf("expected page-with-ig's token, got %s", pageToken)
	}
	if igID != "ig-real-1" {
		t.Errorf("got %s", igID)
	}
	if igUsername != "realuser" {
		t.Errorf("got %s", igUsername)
	}
}

// ── fetchMe ────────────────────────────────────────────────────────────
func TestFetchMe(t *testing.T) {
	srv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":   "user-99",
			"name": "Grace Hopper",
		})
	})

	old := graphBase
	graphBase = srv.URL
	defer func() { graphBase = old }()

	c := New()
	id, name, err := c.fetchMe(context.Background(), "fake-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "user-99" {
		t.Errorf("got id %s", id)
	}
	if name != "Grace Hopper" {
		t.Errorf("got name %s", name)
	}
}

// ── postJSON (success + error paths) ───────────────────────────────────
func TestPostJSONSuccess(t *testing.T) {
	srv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": "ok-1"})
	})

	c := New()
	result, err := c.postJSON(context.Background(), srv.URL, map[string]any{"foo": "bar"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["id"] != "ok-1" {
		t.Errorf("got %v", result["id"])
	}
}

func TestPostJSONErrorStatus(t *testing.T) {
	srv := withFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_request"}`))
	})

	c := New()
	_, err := c.postJSON(context.Background(), srv.URL, map[string]any{"foo": "bar"})
	if err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}
}