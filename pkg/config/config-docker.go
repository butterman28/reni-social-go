//go:build docker
// +build docker

package config

import (
	"os"

	"github.com/joho/godotenv"
)

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func init() {
	// .env_docker is baked into the image at /app/.env_docker
	_ = godotenv.Load(".env_docker")

	C = &Config{
		Port:    "1040",
		IsDev:   true,
		BackUrl: "localhost:1040",

		DBURL: getEnv("DBURL", "postgres://reni:reni@postgres:5432/reni_social_dev?sslmode=disable"),

		USE_TLS:   false,
		CERT_FILE: "",
		KEY_FILE:  "",

		RedisHost:     getEnv("REDIS_HOST", "redis"),
		RedisPort:     getEnv("REDIS_PORT", "6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       7,

		ReniApiKey:         os.Getenv("RENI_API_KEY"),
		ReniApiBaseUrl:     os.Getenv("RENI_API_BASE_URL"),
		AppUrl:             getEnv("APP_URL", "http://localhost:5173"),
		ReniCentralService: "https://srv.fireswitch.ng:1030",
		ReniCentralKey:     getEnv("RENI_CENTRAL_KEY", "$newReniCentral"),

		// LinkedIn
		LinkedInClientID:     os.Getenv("LINKEDIN_CLIENT_ID"),
		LinkedInClientSecret: os.Getenv("LINKEDIN_CLIENT_SECRET"),
		LinkedInRedirectURL:  getEnv("LINKEDIN_REDIRECT_URL", "http://localhost:1040/api/workspaces/connect/linkedin/callback"),

		// Meta — Facebook Login flow
		MetaAppID:       os.Getenv("META_APP_ID"),
		MetaAppSecret:   os.Getenv("META_APP_SECRET"),
		MetaRedirectURL: getEnv("META_REDIRECT_URL", "http://localhost:1040/api/workspaces/connect/meta/callback"),

		// Instagram Login (direct flow)
		InstagramAppID:       os.Getenv("INSTAGRAM_APP_ID"),
		InstagramAppSecret:   os.Getenv("INSTAGRAM_APP_SECRET"),
		InstagramRedirectURL: os.Getenv("INSTAGRAM_REDIRECT_URL"),

		// X / Twitter
		XAPIKey:            os.Getenv("X_API_KEY"),
		XAPISecret:         os.Getenv("X_API_SECRET"),
		XAccessToken:       os.Getenv("X_ACCESS_TOKEN"),
		XAccessTokenSecret: os.Getenv("X_ACCESS_TOKEN_SECRET"),
		XRedirectURL:       getEnv("X_REDIRECT_URL", "http://localhost:1040/api/workspaces/connect/x/callback"),

		// TikTok
		TikTokClientKey:    os.Getenv("TIKTOK_CLIENT_KEY"),
		TikTokClientSecret: os.Getenv("TIKTOK_CLIENT_SECRET"),
		TikTokRedirectURL:  os.Getenv("TIKTOK_REDIRECT_URL"),

		SchedulerPollIntervalSecs: 30,
		ScheduledPostsQueue:       "social:post_queue_dev",

		MediaWorkerQueue: "social:media_jobs_dev",
		FFmpegPath:       "ffmpeg",

		NotifyQueue: "social:notify_queue_dev",
	}
}
