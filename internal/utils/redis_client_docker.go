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

func SetSession(token string, userID uuid.UUID, ttl time.Duration) error {
	return RDB.Set(Ctx, "session:"+token, userID.String(), ttl).Err()
}

func GetSession(token string) (string, error) {
	return RDB.Get(Ctx, "session:"+token).Result()
}

func DeleteSession(token string) error {
	return RDB.Del(Ctx, "session:"+token).Err()
}
