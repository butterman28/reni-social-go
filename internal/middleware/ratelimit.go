package middleware

import (
	"strconv"
	"sync"
	"time"

	"reni-social/pkg/config"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/storage/redis/v2"
)

var (
	redisStore     fiber.Storage
	redisStoreOnce sync.Once
)

// getRedisStore initializes a singleton Redis storage instance for rate limiting
// getRedisStore godoc
// @Summary getRedisStore
// @Description Auto-generated thorough documentation for getRedisStore
// @Tags middleware
// @Produce json
// @Success 200 {object} fiber.Storage "Successful operation"
// @Failure 500 {object} map[string]string "Internal Server Error"
func getRedisStore() fiber.Storage {
	redisStoreOnce.Do(func() {
		port, err := strconv.Atoi(config.C.RedisPort)
		if err != nil {
			port = 6379
		}

		redisStore = redis.New(redis.Config{
			Host:  config.C.RedisHost,
			Port:  port,
			Reset: false,
		})
	})
	return redisStore
}

// RedisRateLimiter returns a distributed Fiber limiter backed by Redis.
// RedisRateLimiter godoc
// @Summary RedisRateLimiter
// @Description Auto-generated thorough documentation for RedisRateLimiter
// @Tags middleware
// @Produce json
// @Param max body integer true "max"
// @Param expiration body string true "expiration"
// @Failure 500 {object} map[string]string "Internal Server Error"
func RedisRateLimiter(max int, expiration time.Duration) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        max,
		Expiration: expiration,
		Storage:    getRedisStore(),
		Next: func(c *fiber.Ctx) bool {
			// Skip rate limiting for HTTP OPTIONS preflight requests
			return c.Method() == fiber.MethodOptions
		},
		KeyGenerator: func(c *fiber.Ctx) string {
			if sessionID := c.Get("X-Session-Id"); sessionID != "" {
				return "ratelimit:session:" + sessionID
			}
			return "ratelimit:ip:" + c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"status":  false,
				"message": "Too many requests. Please try again later.",
			})
		},
	})
}

// StrictAuthLimiter provides tighter limits (5 req/min) for signin/register endpoints
// StrictAuthLimiter godoc
// @Summary StrictAuthLimiter
// @Description Auto-generated thorough documentation for StrictAuthLimiter
// @Tags middleware
// @Produce json
// @Failure 500 {object} map[string]string "Internal Server Error"
func StrictAuthLimiter() fiber.Handler {
	return RedisRateLimiter(5, 1*time.Minute)
}

// GeneralApiLimiter provides baseline API limits (60 req/min) for general endpoints
// GeneralApiLimiter godoc
// @Summary GeneralApiLimiter
// @Description Auto-generated thorough documentation for GeneralApiLimiter
// @Tags middleware
// @Produce json
// @Failure 500 {object} map[string]string "Internal Server Error"
func GeneralApiLimiter() fiber.Handler {
	return RedisRateLimiter(60, 1*time.Minute)
}