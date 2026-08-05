//go:build !docker
// +build !docker

package utils

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var RDB *redis.Client
var Ctx = context.Background()

func InitRedis() {
	RDB = redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6379", // or your Redis server
		Password: "",               // set password if any
		DB:       7,
	})

	_, err := RDB.Ping(Ctx).Result()
	if err != nil {
		log.Fatalf("❌ Failed to connect to Redis: %v", err)
	}

	log.Println("✅ Connected to Redis")
}

// Store session
func SetSession(token string, userID uuid.UUID, ttl time.Duration) error {
	return RDB.Set(Ctx, "session:"+token, userID.String(), ttl).Err()
}

// Retrieve session
func GetSession(token string) (string, error) {
	return RDB.Get(Ctx, "session:"+token).Result()
}

// Delete session
func DeleteSession(token string) error {
	return RDB.Del(Ctx, "session:"+token).Err()
}
