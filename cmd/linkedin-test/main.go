// cmd/linkedin-test/main.go
//
// Standalone manual test for the LinkedIn adapter. Only imports
// internal/platform/linkedin, internal/domain, pkg/config — never
// touches the DB, Redis, Fiber routes, or other platforms.
//
// Usage: go run ./cmd/linkedin-test
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"reni-social/internal/domain"
	"reni-social/internal/platform"
	"reni-social/internal/platform/linkedin"
	"reni-social/pkg/config"
)

func main() {
	redirectURL := config.C.LinkedInRedirectURL
	state := ""

	authURL := linkedin.AuthorizationURL(state)

	fmt.Println("=================================================================")
	fmt.Println("STEP 1: Open this URL in your browser and approve the app:")
	fmt.Println(authURL)
	fmt.Println("=================================================================")

	u, err := url.Parse(redirectURL)
	if err != nil {
		log.Fatalf("invalid LinkedInRedirectURL in config: %v", err)
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(u.Path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if msg := q.Get("error_description"); msg != "" {
			errCh <- fmt.Errorf("linkedin error: %s", msg)
			fmt.Fprintln(w, "Error — check terminal.")
			return
		}
		if q.Get("state") != state {
			errCh <- fmt.Errorf("state mismatch: got %q", q.Get("state"))
			fmt.Fprintln(w, "State mismatch — check terminal.")
			return
		}
		code := q.Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no code in callback")
			fmt.Fprintln(w, "No code received — check terminal.")
			return
		}
		fmt.Fprintln(w, "Got it! Close this tab and check your terminal.")
		codeCh <- code
	})

	server := &http.Server{Addr: ":" + portFromURL(u), Handler: mux}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("local server error: %v", err)
		}
	}()
	fmt.Printf("\nListening on port %s, path %s for the redirect...\n\n", portFromURL(u), u.Path)

	var code string
	select {
	case code = <-codeCh:
		fmt.Println("✅ Received authorization code.")
	case err := <-errCh:
		log.Fatalf("❌ OAuth callback error: %v", err)
	case <-time.After(3 * time.Minute):
		log.Fatal("❌ Timed out waiting for LinkedIn redirect (3 min).")
	}
	_ = server.Close()

	ctx := context.Background()
	client := linkedin.New()

	fmt.Println("\nSTEP 2: Exchanging code for token + fetching profile...")
	conn, err := client.ExchangeCode(ctx, code)
	if err != nil {
		log.Fatalf("❌ ExchangeCode failed: %v", err)
	}
	fmt.Printf("✅ Connected as: %s (%s)\n", conn.AccountName, conn.AccountID)
	fmt.Printf("   Access token (truncated): %s...\n", truncate(conn.AccessToken, 12))
	if conn.ExpiresAt != nil {
		fmt.Printf("   Expires at: %s\n", conn.ExpiresAt.Format(time.RFC1123))
	}

	// ── STEP 3: Text share ──────────────────────────────────────────
	if askYesNo("\nSTEP 3: Publish a REAL TEXT post to your LinkedIn feed?") {
		post := &domain.Post{
			Content: fmt.Sprintf("Test text post from reni-social — %s", time.Now().Format(time.RFC1123)),
		}
		fmt.Println("Publishing text share...")
		result, err := client.Publish(ctx, post, conn)
		if err != nil {
			log.Fatalf("❌ Publish (text) failed: %v", err)
		}
		printResult(result)
	} else {
		fmt.Println("Skipped text share.")
	}

	// ── STEP 4: Article/URL share ────────────────────────────────────
	if askYesNo("\nSTEP 4: Publish a REAL ARTICLE/URL share to your LinkedIn feed?") {
		fmt.Println("Publishing article share (linking to anthropic.com)...")
		result, err := client.PublishArticle(
			ctx, conn,
			fmt.Sprintf("Test article share from reni-social — %s", time.Now().Format(time.RFC1123)),
			"https://www.anthropic.com",
			"Anthropic",
		)
		if err != nil {
			log.Fatalf("❌ PublishArticle failed: %v", err)
		}
		printResult(result)
	} else {
		fmt.Println("Skipped article share.")
	}

	// ── STEP 5: Image share ──────────────────────────────────────────
	if askYesNo("\nSTEP 5: Publish a REAL IMAGE post to your LinkedIn feed?") {
		// httpbin.org/image/png reliably returns a small PNG with the
		// correct Content-Type header — convenient for a one-off test
		// without needing to host your own test image.
		post := &domain.Post{
			Content:   fmt.Sprintf("Test image post from reni-social — %s", time.Now().Format(time.RFC1123)),
			MediaURLs: []string{"https://httpbin.org/image/png"},
		}
		fmt.Println("Downloading + uploading + publishing image share...")
		result, err := client.Publish(ctx, post, conn)
		if err != nil {
			log.Fatalf("❌ Publish (image) failed: %v", err)
		}
		printResult(result)
	} else {
		fmt.Println("Skipped image share.")
	}

	fmt.Println("\nDone.")
}

func askYesNo(prompt string) bool {
	fmt.Println(prompt)
	fmt.Print("Type 'yes' to proceed, anything else to skip: ")
	var confirm string
	fmt.Scanln(&confirm)
	return strings.ToLower(confirm) == "yes"
}

func printResult(result *platform.PublishResult) { // 👈 CHANGE THIS LINE
	fmt.Println("✅ Published successfully!")
	fmt.Printf("   Platform Post ID: %s\n", result.PlatformPostID)
	if result.URL != "" {
		fmt.Printf("   URL: %s\n", result.URL)
	}
}

func portFromURL(u *url.URL) string {
	if u.Port() != "" {
		return u.Port()
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}