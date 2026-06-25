// cmd/x-test/main.go
//
// Standalone manual test for the X (Twitter) adapter. Only imports
// internal/platform/x, internal/domain, pkg/config — never
// touches the DB, Redis, Fiber routes, or other platforms.
//
// Usage: go run ./cmd/x-test
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"reni-social/internal/domain"
	"reni-social/internal/platform"
	"reni-social/internal/platform/x"
	"reni-social/pkg/config"
)

func main() {
	redirectURL := config.C.XRedirectURL
	ctx := context.Background()
	client := x.New()

	fmt.Println("=================================================================")
	fmt.Println("STEP 1: Fetching request token...")
	oauthToken, authURL, err := client.RequestToken(ctx, "manual-test-ws-id", "x")
	
	if err != nil {
		log.Fatalf("❌ RequestToken failed: %v", err)
	}
	fmt.Println("✅ Got request token.")
	fmt.Println("=================================================================")
	fmt.Println("STEP 2: Open this URL in your browser and approve the app:")
	fmt.Println(authURL)
	fmt.Println("=================================================================")

	u, err := url.Parse(redirectURL)
	if err != nil {
		log.Fatalf("invalid XRedirectURL in config: %v", err)
	}

	tokenCh := make(chan string, 1)
	verifierCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(u.Path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if msg := q.Get("error_description"); msg != "" {
			errCh <- fmt.Errorf("x error: %s", msg)
			fmt.Fprintln(w, "Error — check terminal.")
			return
		}
		
		returnedToken := q.Get("oauth_token")
		verifier := q.Get("oauth_verifier")
		
		if returnedToken == "" || verifier == "" {
			errCh <- fmt.Errorf("missing oauth_token or oauth_verifier in callback")
			fmt.Fprintln(w, "Missing tokens — check terminal.")
			return
		}
		
		if returnedToken != oauthToken {
			errCh <- fmt.Errorf("oauth_token mismatch: got %q, expected %q", returnedToken, oauthToken)
			fmt.Fprintln(w, "Token mismatch — check terminal.")
			return
		}

		fmt.Fprintln(w, "Got it! Close this tab and check your terminal.")
		tokenCh <- returnedToken
		verifierCh <- verifier
	})

	server := &http.Server{Addr: ":" + portFromURL(u), Handler: mux}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("local server error: %v", err)
		}
	}()
	fmt.Printf("\nListening on port %s, path %s for the redirect...\n\n", portFromURL(u), u.Path)

	var returnedToken, verifier string
	select {
	case returnedToken = <-tokenCh:
		verifier = <-verifierCh
		fmt.Println("✅ Received oauth_token and oauth_verifier.")
	case err := <-errCh:
		log.Fatalf("❌ OAuth callback error: %v", err)
	case <-time.After(3 * time.Minute):
		log.Fatal("❌ Timed out waiting for X redirect (3 min).")
	}
	_ = server.Close()

	fmt.Println("\nSTEP 3: Exchanging verifier for access token...")
	conn, err := client.ExchangeVerifier(ctx, returnedToken, verifier)
	if err != nil {
		log.Fatalf("❌ ExchangeVerifier failed: %v", err)
	}
	fmt.Printf("✅ Connected as: %s (%s)\n", conn.AccountName, conn.AccountID)
	fmt.Printf("   Access token (truncated): %s...\n", truncate(conn.AccessToken, 12))
	fmt.Printf("   Access secret (truncated): %s...\n", truncate(conn.RefreshToken, 12))

	// ── STEP 4: Text tweet ──────────────────────────────────────────
	var publishedTweetID string
	if askYesNo("\nSTEP 4: Publish a REAL TEXT tweet to your X timeline?") {
		post := &domain.Post{
			Content: fmt.Sprintf("Test tweet from reni-social — %s", time.Now().Format(time.RFC1123)),
		}
		fmt.Println("Publishing tweet...")
		result, err := client.Publish(ctx, post, conn)
		if err != nil {
			// 🚨 CHANGED: Don't crash the script if publishing fails (e.g., 402 Credits Depleted)
			log.Printf("❌ Publish (text) failed: %v", err)
			fmt.Println("⚠️  Skipping STEP 6 auto-fetch since no tweet was published.")
		} else {
			printResult(result)
			publishedTweetID = result.PlatformPostID // Save ID for STEP 6
		}
	} else {
		fmt.Println("Skipped text tweet.")
	}

	// ── STEP 5: Fetch User Profile ──────────────────────────────────
	if askYesNo("\nSTEP 5: Fetch your own X user profile using GetUser?") {
		params := url.Values{}
		params.Set("user.fields", "description,profile_image_url,public_metrics,created_at")
		
		fmt.Println("Fetching user profile...")
		userData, err := client.GetUser(ctx, conn, conn.AccountID, params)
		if err != nil {
			log.Printf("❌ GetUser failed: %v", err)
		} else {
			fmt.Println("✅ Fetched User Profile:")
			printJSON(userData)
		}
	}

	// ── STEP 6: Fetch a Tweet with Includes ─────────────
	// If we didn't publish a tweet, ask the user to provide a Tweet ID manually!
	if publishedTweetID == "" {
		publishedTweetID = promptInput("\nEnter any Tweet ID to test fetching (or press Enter to skip STEP 6): ")
	}
	
	if publishedTweetID != "" {
		if askYesNo("\nSTEP 6: Fetch this tweet using GetTweetWithIncludes?") {
			fmt.Println("Fetching tweet with media/poll/place expansions...")
			tweetData, err := client.GetTweetWithIncludes(ctx, conn, publishedTweetID)
			if err != nil {
				log.Printf("❌ GetTweetWithIncludes failed: %v", err)
			} else {
				fmt.Println("✅ Fetched Tweet with Includes:")
				printJSON(tweetData)
			}
		}
	} else {
		fmt.Println("\nSkipping STEP 6 (no tweet ID provided).")
	}

	// ── STEP 7: Fetch a Space ───────────────────────────────────────
	if askYesNo("\nSTEP 7: Test fetching a Space using GetSpace?") {
		spaceID := promptInput("Enter Space ID (or press Enter to skip): ")
		if spaceID != "" {
			params := url.Values{}
			params.Set("space.fields", "title,state,created_at,lang")
			
			fmt.Println("Fetching space...")
			spaceData, err := client.GetSpace(ctx, conn, spaceID, params)
			if err != nil {
				log.Printf("❌ GetSpace failed: %v", err)
			} else {
				fmt.Println("✅ Fetched Space:")
				printJSON(spaceData)
			}
		} else {
			fmt.Println("Skipped fetching Space.")
		}
	}

	// ── STEP 8: Fetch a List ────────────────────────────────────────
	if askYesNo("\nSTEP 8: Test fetching a List using GetList?") {
		listID := promptInput("Enter List ID (or press Enter to skip): ")
		if listID != "" {
			params := url.Values{}
			params.Set("list.fields", "name,description,member_count,follower_count")
			
			fmt.Println("Fetching list...")
			listData, err := client.GetList(ctx, conn, listID, params)
			if err != nil {
				log.Printf("❌ GetList failed: %v", err)
			} else {
				fmt.Println("✅ Fetched List:")
				printJSON(listData)
			}
		} else {
			fmt.Println("Skipped fetching List.")
		}
	}

	fmt.Println("\nDone.")
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

func askYesNo(prompt string) bool {
	fmt.Println(prompt)
	fmt.Print("Type 'yes' to proceed, anything else to skip: ")
	var confirm string
	fmt.Scanln(&confirm)
	return strings.ToLower(confirm) == "yes"
}

// promptInput safely reads a full line of input from the user.
func promptInput(prompt string) string {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

func printResult(result *platform.PublishResult) {
	fmt.Println("✅ Published successfully!")
	fmt.Printf("   Platform Post ID: %s\n", result.PlatformPostID)
	if result.URL != "" {
		fmt.Printf("   URL: %s\n", result.URL)
	}
}

// printJSON nicely formats the dynamic map[string]any response from the X API.
func printJSON(data map[string]any) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Printf("Error formatting JSON: %v\n", err)
		return
	}
	fmt.Println(string(b))
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