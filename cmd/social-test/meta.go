package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"reni-social/internal/domain"
	"reni-social/internal/platform/meta"
	"reni-social/pkg/config"
)

// metaTestLocalPort is the local port this flow listens on. Point your
// ngrok tunnel at this exact port — the ngrok https URL's own "port" (443)
// is NOT the port to listen on locally.
const metaTestLocalPort = "1045"

func RunMetaTest() {
	redirectURL := config.C.InstagramRedirectURL
	if redirectURL == "" || config.C.InstagramAppID == "" {
		log.Fatal(" INSTAGRAM_APP_ID / INSTAGRAM_REDIRECT_URL not set — check your .env")
	}
	state := "meta-test-state"

	authURL := meta.InstagramLoginAuthorizationURL(state)

	fmt.Println("=================================================================")
	fmt.Println("STEP 1: Open this URL in your browser and approve the app:")
	fmt.Println(authURL)
	fmt.Println("=================================================================")

	u, err := url.Parse(redirectURL)
	if err != nil {
		log.Fatalf("invalid INSTAGRAM_REDIRECT_URL in config: %v", err)
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(u.Path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if msg := q.Get("error_description"); msg != "" {
			errCh <- fmt.Errorf("instagram error: %s", msg)
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

	server := &http.Server{Addr: ":" + metaTestLocalPort, Handler: mux}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("local server error: %v", err)
		}
	}()
	fmt.Printf("\nListening on local port %s, path %s for the redirect (via your ngrok tunnel)...\n\n", metaTestLocalPort, u.Path)

	var code string
	select {
	case code = <-codeCh:
		fmt.Println(" Received authorization code.")
	case err := <-errCh:
		log.Fatalf(" OAuth callback error: %v", err)
	case <-time.After(3 * time.Minute):
		log.Fatal(" Timed out waiting for Instagram redirect (3 min).")
	}
	_ = server.Close()

	ctx := context.Background()
	client := meta.New()

	fmt.Println("\nSTEP 2: Exchanging code for token + resolving account...")
	conn, err := client.ExchangeInstagramLoginCode(ctx, code)
	if err != nil {
		log.Fatalf(" ExchangeInstagramLoginCode failed: %v", err)
	}
	fmt.Printf(" Connected as: %s (IG user id: %s)\n", conn.AccountName, conn.AccountID)
	fmt.Printf("   Access token (truncated): %s...\n", truncate(conn.AccessToken, 12))
	if conn.ExpiresAt != nil {
		fmt.Printf("   Expires at: %s\n", conn.ExpiresAt.Format(time.RFC1123))
	}

	if askYesNo("\nSTEP 3: Publish a REAL IMAGE post to this Instagram account?") {
		post := &domain.Post{
			Content:   fmt.Sprintf("Test image post from reni-social — %s", time.Now().Format(time.RFC1123)),
			MediaURLs: []string{"https://picsum.photos/1080/1080"},
		}
		fmt.Println("Creating container + polling + publishing...")
		result, err := client.PublishInstagramLogin(ctx, post, conn)
		if err != nil {
			log.Fatalf(" PublishInstagramLogin failed: %v", err)
		}
		printResult(result)
	} else {
		fmt.Println("Skipped image post.")
	}

	fmt.Println("\nDone.")
}
