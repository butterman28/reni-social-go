//go:build docker
// +build docker

package utils

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"reni-social/pkg/config"
)

var RDB *redis.Client
var Ctx = context.Background()

// InitRedis godoc
// @Summary InitRedis
// @Description Auto-generated thorough documentation for InitRedis
// @Tags utils
// @Produce json
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func InitRedis() {
	addr := config.C.RedisHost + ":" + config.C.RedisPort

	RDB = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: config.C.RedisPassword,
		DB:       config.C.RedisDB,
	})

	_, err := RDB.Ping(Ctx).Result()
	if err != nil {
		log.Fatalf(" Failed to connect to Redis at %s: %v", addr, err)
	}
	log.Println(" Connected to Redis at", addr)
}

// SetSession godoc
// @Summary SetSession
// @Description Auto-generated thorough documentation for SetSession
// @Tags utils
// @Produce json
// @Param token body string true "token"
// @Param userID body string true "userID"
// @Param ttl body string true "ttl"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func SetSession(token string, userID uuid.UUID, ttl time.Duration) error {
	return RDB.Set(Ctx, "session:"+token, userID.String(), ttl).Err()
}

// GetSession godoc
// @Summary GetSession
// @Description Auto-generated thorough documentation for GetSession
// @Tags utils
// @Produce json
// @Param token body string true "token"
// @Success 200 {string} string "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func GetSession(token string) (string, error) {
	return RDB.Get(Ctx, "session:"+token).Result()
}

// DeleteSession godoc
// @Summary DeleteSession
// @Description Auto-generated thorough documentation for DeleteSession
// @Tags utils
// @Produce json
// @Param token body string true "token"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func DeleteSession(token string) error {
	return RDB.Del(Ctx, "session:"+token).Err()
}
