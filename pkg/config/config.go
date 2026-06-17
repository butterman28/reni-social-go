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

	ReniApiKey     string
	ReniApiBaseUrl string

	EventMediaJob         string
	AppUrl                string
	PlatformFeePercentage float64
}

var C *Config
