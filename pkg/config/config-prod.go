//go:build prod
// +build prod

package config

// "github.com/google/uuid"

func init() {
	C = &Config{
		Port:             "1041",
		
		DBURL:            "postgres://bikiname:bikiname@localhost:5432/bikiname?sslmode=disable",

		USE_TLS:   true,
		CERT_FILE: "/etc/letsencrypt/live/srv1.fireswitch.ng/fullchain.pem",
		KEY_FILE:  "/etc/letsencrypt/live/srv1.fireswitch.ng/privkey.pem",

		RedisHost:     "127.0.0.1:6379",
		RedisPort:     "6379",
		RedisPassword: "",
		RedisDB:       6,

		ReniApiKey:     "rk_live_2b605eb34b8f86ffa9bfdd3802240140",
		ReniApiBaseUrl: "https://srv.fireswitch.ng:1037",

		EventMediaJob:         "media:event_jobs_prod",
		AppUrl:                "https://srv.fireswitch.ng:1039",
		PlatformFeePercentage: 7.5,
	}

}
