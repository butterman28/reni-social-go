//go:build prod
// +build prod

package config

func init() {
	C = &Config{
		Port:    "1046",
		IsDev:   false,
		BackUrl: "https://srv.fireswitch.ng:1046",

		DBURL: getEnv("DBURL", "postgres://renisocial:renisocial@0852@127.0.0.1:5432/renisocial_live?sslmode=disable"),

		USE_TLS:   true,
		CERT_FILE: "/etc/letsencrypt/live/srv1.fireswitch.ng/fullchain.pem",
		KEY_FILE:  "/etc/letsencrypt/live/srv1.fireswitch.ng/privkey.pem",

		RedisHost:     "127.0.0.1",
		RedisPort:     "6379",
		RedisPassword: "",
		RedisDB:       8,

		ReniApiKey:         "",
		ReniApiBaseUrl:     "",
		AppUrl:             "https://srv.fireswitch.ng:1041",
		ReniCentralService: "https://srv.fireswitch.ng:1030",
		ReniCentralKey:     getEnv("RENI_CENTRAL_KEY", "$newReniCentral"),

		// LinkedIn (prod — set via environment / secrets manager)
		LinkedInClientID:     "CHANGE_ME",
		LinkedInClientSecret: "CHANGE_ME",
		LinkedInRedirectURL:  "https://srv.fireswitch.ng:1041/api/workspaces/connect/linkedin/callback",

		// Meta (prod)
		MetaAppID:       "CHANGE_ME",
		MetaAppSecret:   "CHANGE_ME",
		MetaRedirectURL: "https://srv.fireswitch.ng:1041/api/workspaces/connect/meta/callback",

		// X / Twitter (prod)
		XAPIKey:            "CHANGE_ME",
		XAPISecret:         "CHANGE_ME",
		XAccessToken:       "CHANGE_ME",
		XAccessTokenSecret: "CHANGE_ME",
		XRedirectURL:       "https://srv.fireswitch.ng:1041/api/workspaces/connect/x/callback",

		// Scheduler
		SchedulerPollIntervalSecs: 30,
		ScheduledPostsQueue:       "social:post_queue",
		MaxConcurrentPublishes:    25,
		PublishTimeoutMinutes:     5,

		// Media worker
		MediaWorkerQueue: "social:media_jobs",
		FFmpegPath:       "/usr/bin/ffmpeg",
		//notifications
		NotifyQueue: "social:notify_queue",

		// Meta Token Refresher
		MetaRefreshCheckIntervalHours: 6,   
		MetaRefreshBeforeDays:         15,   
		MetaRefreshBatchSize:          50,   
		MetaRefreshMaxConcurrency:     5,
		// LinkedIn Expiry Monitor
		LinkedInExpiryCheckIntervalHours: 6,
		LinkedInExpiryWarnBeforeDays:     7,
		LinkedInExpiryBatchSize:          50,    
	}
}
