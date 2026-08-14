// config/config.go
package config

type Config struct {
	Port    string
	IsDev   bool
	BackUrl string

	DBURL string

	USE_TLS   bool
	CERT_FILE string
	KEY_FILE  string

	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	AppUrl string

	// Legacy fields (kept for backward compat)
	ReniApiKey         string
	ReniApiBaseUrl     string
	ReniCentralService string
	ReniCentralKey     string

	// ──────────────────────────────────────────
	// LinkedIn OAuth2
	// ──────────────────────────────────────────
	LinkedInClientID     string
	LinkedInClientSecret string
	LinkedInRedirectURL  string

	// ──────────────────────────────────────────
	// Meta (Facebook / Instagram) App — Facebook Login flow
	// ──────────────────────────────────────────
	MetaAppID       string
	MetaAppSecret   string
	MetaRedirectURL string

	// ──────────────────────────────────────────
	// Instagram API with Instagram Login (direct, non-Facebook-Login flow)
	// ──────────────────────────────────────────
	InstagramAppID       string
	InstagramAppSecret   string
	InstagramRedirectURL string

	// ──────────────────────────────────────────
	// X (Twitter) v2
	// ──────────────────────────────────────────
	XAPIKey            string
	XAPISecret         string
	XAccessToken       string
	XAccessTokenSecret string
	XRedirectURL       string

	// ──────────────────────────────────────────
	// TikTok (Login Kit + Content Posting API)
	// ──────────────────────────────────────────
	TikTokClientKey    string
	TikTokClientSecret string
	TikTokRedirectURL  string

	// ──────────────────────────────────────────
	// Scheduler
	// ──────────────────────────────────────────
	SchedulerPollIntervalSecs int    // e.g. 30
	ScheduledPostsQueue       string // Redis key, e.g. "social:post_queue"
	MaxConcurrentPublishes    int    
	PublishTimeoutMinutes     int  

	// ──────────────────────────────────────────
	// Media Worker
	// ──────────────────────────────────────────
	MediaWorkerQueue string // Redis key, e.g. "social:media_jobs"
	FFmpegPath       string // e.g. "/usr/bin/ffmpeg"
	  
	// ──────────────────────────────────────────
	// Notifications (email/SMS)
	// ──────────────────────────────────────────
	NotifyQueue string
	// ──────────────────────────────────────────
	// Meta Token Refresher
	// ──────────────────────────────────────────
	MetaRefreshCheckIntervalHours int
	MetaRefreshBeforeDays         int
	MetaRefreshBatchSize          int
	MetaRefreshMaxConcurrency     int
	// ──────────────────────────────────────────
	// linked reauth refresher 
	// ──────────────────────────────────────────
	LinkedInExpiryCheckIntervalHours int
	LinkedInExpiryWarnBeforeDays     int
	LinkedInExpiryBatchSize          int
}

var C *Config
