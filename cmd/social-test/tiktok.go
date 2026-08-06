package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"reni-social/internal/domain"
	"reni-social/internal/platform/tiktok"
	"reni-social/pkg/config"
)

func RunTikTokTest() {
	redirectURL := config.C.TikTokRedirectURL
	if redirectURL == "" || config.C.TikTokClientKey == "" {
		log.Fatal("❌ TIKTOK_CLIENT_KEY / TIKTOK_REDIRECT_URL not set — check your .env")
	}

	verifier, challenge, err := tiktok.GeneratePKCE()
	if err != nil {
		log.Fatalf("❌ generate PKCE: %v", err)
	}
	state := "tiktok-test-state"

	authURL := tiktok.AuthorizationURL(state, challenge)

	fmt.Println("=================================================================")
	fmt.Println("STEP 1: Open this URL in your browser and approve the app:")
	fmt.Println(authURL)
	fmt.Println("=================================================================")

	u, err := url.Parse(redirectURL)
	if err != nil {
		log.Fatalf("invalid TIKTOK_REDIRECT_URL in config: %v", err)
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(u.Path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if msg := q.Get("error_description"); msg != "" {
			errCh <- fmt.Errorf("tiktok error: %s", msg)
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
		log.Fatal("❌ Timed out waiting for TikTok redirect (3 min).")
	}
	_ = server.Close()

	ctx := context.Background()
	client := tiktok.New()

	fmt.Println("\nSTEP 2: Exchanging code for token...")
	conn, err := client.ExchangeCode(ctx, code, verifier)
	if err != nil {
		log.Fatalf("❌ ExchangeCode failed: %v", err)
	}
	fmt.Printf("✅ Connected as: %s (open_id: %s)\n", conn.AccountName, conn.AccountID)
	fmt.Printf("   Access token (truncated): %s...\n", truncate(conn.AccessToken, 12))
	if conn.ExpiresAt != nil {
		fmt.Printf("   Expires at: %s\n", conn.ExpiresAt.Format(time.RFC1123))
	}

	var publishID string
	if askYesNo("\nSTEP 3: Publish a REAL VIDEO (SELF_ONLY visibility — unaudited apps can't post publicly) to this TikTok account?") {
		post := &domain.Post{
			Content:   fmt.Sprintf("Test video from reni-social — %s", time.Now().Format(time.RFC1123)),
			MediaURLs: []string{"https://download.samplelib.com/mp4/sample-5s.mp4"},
		}
		fmt.Println("Initiating PULL_FROM_URL publish...")
		result, err := client.Publish(ctx, post, conn)
		if err != nil {
			log.Fatalf("❌ Publish failed: %v", err)
		}
		printResult(result)
		publishID = result.PlatformPostID
	} else {
		fmt.Println("Skipped video publish.")
	}

	if publishID != "" && askYesNo("\nSTEP 4: Poll publish status using FetchPublishStatus?") {
		status, err := client.FetchPublishStatus(ctx, conn, publishID)
		if err != nil {
			log.Printf("❌ FetchPublishStatus failed: %v", err)
		} else {
			fmt.Println("✅ Publish status:")
			printJSON(status)
		}
	}

	fmt.Println("\nDone.")
}
