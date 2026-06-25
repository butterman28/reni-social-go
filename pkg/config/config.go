// config/config.go
package config

type Config struct {
	Port  string
	IsDev bool

	DBURL string

	USE_TLS   bool
	CERT_FILE string
	KEY_FILE  string

	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// Legacy fields (kept for backward compat)
	ReniApiKey     string
	ReniApiBaseUrl string
	AppUrl         string
	ReniCentralService string
	ReniCentralKey     string

	// ──────────────────────────────────────────
	// LinkedIn OAuth2
	// ──────────────────────────────────────────
	LinkedInClientID     string
	LinkedInClientSecret string
	LinkedInRedirectURL  string

	// ──────────────────────────────────────────
	// Meta (Facebook / Instagram) App
	// ──────────────────────────────────────────
	MetaAppID       string
	MetaAppSecret   string
	MetaRedirectURL string

	// ──────────────────────────────────────────
	// X (Twitter) v2
	// ──────────────────────────────────────────
	XAPIKey            string
	XAPISecret         string
	XAccessToken       string
	XAccessTokenSecret string
	XRedirectURL       string

	// ──────────────────────────────────────────
	// Scheduler
	// ──────────────────────────────────────────
	SchedulerPollIntervalSecs int    // e.g. 30
	ScheduledPostsQueue       string // Redis key, e.g. "social:post_queue"

	// ──────────────────────────────────────────
	// Media Worker
	// ──────────────────────────────────────────
	MediaWorkerQueue string // Redis key, e.g. "social:media_jobs"
	FFmpegPath       string // e.g. "/usr/bin/ffmpeg"
}

var C *Config
