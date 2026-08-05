//go:build !prod && !docker
// +build !prod,!docker

package config

import (
	"os"

	"github.com/joho/godotenv" // We will install this in Step 4
)

// getEnv reads an environment variable or returns a fallback default
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func init() {
	// Load .env file if it exists (safe to ignore error if running in prod)
	_ = godotenv.Load()

	C = &Config{
		Port:    "1040",
		IsDev:   true,
		BackUrl: "localhost:1040",

		DBURL: getEnv("DBURL", "postgres://renisocial:renisocial@0852@127.0.0.1:5432/renisocial_dev?sslmode=disable"),

		USE_TLS:   false,
		CERT_FILE: "/etc/letsencrypt/live/srv1.fireswitch.ng/fullchain.pem",
		KEY_FILE:  "/etc/letsencrypt/live/srv1.fireswitch.ng/privkey.pem",

		RedisHost:     "127.0.0.1",
		RedisPort:     "6379",
		RedisPassword: "",
		RedisDB:       7,

		ReniApiKey:         os.Getenv("RENI_API_KEY"),
		ReniApiBaseUrl:     os.Getenv("RENI_API_BASE_URL"),
		AppUrl:             getEnv("APP_URL", "http://localhost:1040"),
		ReniCentralService: "https://srv.fireswitch.ng:1030",
		ReniCentralKey:     getEnv("RENI_CENTRAL_KEY", "$newReniCentral"),

		// LinkedIn (dev)
		LinkedInClientID:     os.Getenv("LINKEDIN_CLIENT_ID"),
		LinkedInClientSecret: os.Getenv("LINKEDIN_CLIENT_SECRET"),
		LinkedInRedirectURL:  getEnv("LINKEDIN_REDIRECT_URL", "http://localhost:1040/api/workspaces/connect/linkedin/callback"),

		// Meta (dev) — Facebook Login flow
		MetaAppID:       os.Getenv("META_APP_ID"),
		MetaAppSecret:   os.Getenv("META_APP_SECRET"),
		MetaRedirectURL: getEnv("META_REDIRECT_URL", "http://localhost:1040/api/workspaces/connect/meta/callback"),

		// Instagram API with Instagram Login (dev) — direct flow
		InstagramAppID:       os.Getenv("INSTAGRAM_APP_ID"),
		InstagramAppSecret:   os.Getenv("INSTAGRAM_APP_SECRET"),
		InstagramRedirectURL: os.Getenv("INSTAGRAM_REDIRECT_URL"),

		// X / Twitter (dev)
		XAPIKey:            os.Getenv("X_API_KEY"),
		XAPISecret:         os.Getenv("X_API_SECRET"),
		XAccessToken:       os.Getenv("X_ACCESS_TOKEN"),
		XAccessTokenSecret: os.Getenv("X_ACCESS_TOKEN_SECRET"),
		XRedirectURL:       getEnv("X_REDIRECT_URL", "http://localhost:1040/api/workspaces/connect/x/callback"),

		// TikTok (dev)
		TikTokClientKey:    os.Getenv("TIKTOK_CLIENT_KEY"),
		TikTokClientSecret: os.Getenv("TIKTOK_CLIENT_SECRET"),
		TikTokRedirectURL:  os.Getenv("TIKTOK_REDIRECT_URL"),

		// Scheduler
		SchedulerPollIntervalSecs: 30,
		ScheduledPostsQueue:       "social:post_queue_dev",

		// Media worker
		MediaWorkerQueue: "social:media_jobs_dev",
		FFmpegPath:       "ffmpeg",
		// notifications 
		NotifyQueue: "social:notify_queue_dev",
	}
}
