// =============================================================================
// internal/cache/redis.go — Redis Implementation of the Cache Interface
// =============================================================================
//
// WHAT DOES THIS FILE DO?
//   It implements the shortener.Cache interface using Redis. So when the shortener
//   service calls cache.Get(code), we actually do a Redis GET. When it calls cache.Set,
//   we do a Redis SET with a TTL (time-to-live) so keys expire automatically. This
//   keeps the shortener package independent of Redis; we could swap in a different
//   cache (e.g. in-memory map for tests) without changing the shortener code.
//
// WHY REDIS FOR CACHING?
//   Redis is an in-memory key-value store. It's very fast and supports TTLs. So we
//   store "shortCode" -> "https://long-url.com/..." and set a TTL (e.g. 10 minutes).
//   After 10 minutes Redis deletes the key; next request will hit Postgres and repopulate cache.
//
// GO CONCEPTS:
//   - Method receiver: (redisCache *RedisCache) means these functions are methods on RedisCache.
//   - Pointer receiver: we use *RedisCache so we don't copy the whole struct on each call.
//   - context.Context: every Redis call takes ctx so we can cancel or timeout if the request is cancelled.
//
// =============================================================================

package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache holds the Redis client and the TTL we use when we Set keys.
// client is from the go-redis library; it manages connections and is safe for concurrent use.
type RedisCache struct {
	client   *redis.Client
	cacheTTL time.Duration
}

// NewRedisClient creates a Redis client. You pass address ("localhost:6379"), password (often ""),
// and database index (0–15). The client keeps a pool of connections; many goroutines can use it at once.
func NewRedisClient(redisAddress, redisPassword string, redisDBIndex int) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     redisAddress,
		Password: redisPassword,
		DB:       redisDBIndex,
	})
}

// NewRedisCache wraps a Redis client with our TTL. So when we Set a key, we use cacheTTL
// (e.g. 10 minutes). The caller (main.go) passes applicationConfig.CacheTTL from the config.
func NewRedisCache(redisClient *redis.Client, cacheTTL time.Duration) *RedisCache {
	return &RedisCache{
		client:   redisClient,
		cacheTTL: cacheTTL,
	}
}

// Get looks up shortCode in Redis. It returns:
//   - cachedURL: the value if found
//   - found (bool): true if the key existed. We need this because Redis returns a special
//     "redis.Nil" error when the key doesn't exist — that's not a real error, just "miss".
//   - error: real errors (e.g. network failure). If err == redis.Nil, we return ("", false, nil).
func (redisCache *RedisCache) Get(ctx context.Context, shortCode string) (string, bool, error) {
	cachedURL, err := redisCache.client.Get(ctx, shortCode).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return cachedURL, true, nil
}

// Set stores shortCode -> originalURL in Redis with the TTL we configured. When the TTL
// expires, Redis automatically deletes the key, so we don't serve stale data forever.
func (redisCache *RedisCache) Set(ctx context.Context, shortCode, originalURL string) error {
	return redisCache.client.Set(ctx, shortCode, originalURL, redisCache.cacheTTL).Err()
}

// Invalidate deletes the key from Redis. We call this when a link has expired so we don't
// keep serving it from cache. Next request will hit the database, get "expired", and return 410.
func (redisCache *RedisCache) Invalidate(ctx context.Context, shortCode string) error {
	return redisCache.client.Del(ctx, shortCode).Err()
}
