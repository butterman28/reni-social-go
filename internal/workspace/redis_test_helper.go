package workspace

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"reni-social/internal/utils"
)

// setupTestRedis spins up an in-memory Redis server and points utils.RDB at
// it, so ConnectPlatform/OAuthCallback (which read/write oauth_state keys)
// can be tested without a real Redis instance. Call this once per test that
// needs Redis; it's cleaned up automatically via t.Cleanup.
func setupTestRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	utils.RDB = redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	utils.Ctx = utils.Ctx // no-op, keeps import if Ctx already set elsewhere

	return mr
}