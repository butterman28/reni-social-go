//go:build !prod
// +build !prod

package config

// "reni-api-gateway/internal/types"

func init() {
	C = &Config{
		Port:  "1040",
		IsDev: true,

		DBURL: "postgres://dogo:dogo@0852@127.0.0.1:5432/dogo_dev?sslmode=disable",

		USE_TLS:   false,
		CERT_FILE: "/etc/letsencrypt/live/srv1.fireswitch.ng/fullchain.pem",
		KEY_FILE:  "/etc/letsencrypt/live/srv1.fireswitch.ng/privkey.pem",

		RedisHost:     "127.0.0.1",
		RedisPort:     "6379",
		RedisPassword: "",
		RedisDB:       7,

		ReniApiKey:     "rk_test_27c1bf82baa6c653e267c08455059478",
		ReniApiBaseUrl: "https://srv.fireswitch.ng:1037",

		EventMediaJob:         "media:event_jobs_dev",
		AppUrl:                "http://srv.fireswitch.ng:1038",
		PlatformFeePercentage: 7.5,
	}

}
