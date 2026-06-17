//go:build !prod
// +build !prod

package config

func init() {
	C = &Config{
		Port:  "1040",
		IsDev: true,

		DBURL: "postgres://reni:reni@127.0.0.1:5432/reni_social_dev?sslmode=disable",

		USE_TLS:   false,
		CERT_FILE: "/etc/letsencrypt/live/srv1.fireswitch.ng/fullchain.pem",
		KEY_FILE:  "/etc/letsencrypt/live/srv1.fireswitch.ng/privkey.pem",

		RedisHost:     "127.0.0.1",
		RedisPort:     "6379",
		RedisPassword: "",
		RedisDB:       7,

		ReniApiKey:     "",
		ReniApiBaseUrl: "",
		AppUrl:         "http://localhost:1040",

		// LinkedIn (dev — replace with real app credentials)
		LinkedInClientID:     "dev_linkedin_client_id",
		LinkedInClientSecret: "dev_linkedin_client_secret",
		LinkedInRedirectURL:  "http://localhost:1040/api/workspaces/connect/linkedin/callback",

		// Meta (dev)
		MetaAppID:       "dev_meta_app_id",
		MetaAppSecret:   "dev_meta_app_secret",
		MetaRedirectURL: "http://localhost:1040/api/workspaces/connect/meta/callback",

		// X / Twitter (dev)
		XAPIKey:            "dev_x_api_key",
		XAPISecret:         "dev_x_api_secret",
		XAccessToken:       "dev_x_access_token",
		XAccessTokenSecret: "dev_x_access_token_secret",
		XRedirectURL:       "http://localhost:1040/api/workspaces/connect/x/callback",

		// Scheduler
		SchedulerPollIntervalSecs: 30,
		ScheduledPostsQueue:       "social:post_queue_dev",

		// Media worker
		MediaWorkerQueue: "social:media_jobs_dev",
		FFmpegPath:       "ffmpeg", // assumes ffmpeg is on PATH
	}
}
