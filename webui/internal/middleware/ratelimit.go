package middleware

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// ---------------------------------------------------------------------------
// Redis client (optional) — set at startup to enable distributed rate limiting.
// ---------------------------------------------------------------------------

var (
	globalRedis *redis.Client
	redisMu     sync.Mutex
)

// SetRedisClient configures the global Redis client used by RateLimit.
// Pass nil to disable Redis-based rate limiting (the middleware will fall
// back to the in-memory implementation).
func SetRedisClient(client *redis.Client) {
	redisMu.Lock()
	defer redisMu.Unlock()
	globalRedis = client
}

// ---------------------------------------------------------------------------
// RateLimit middleware factory
// ---------------------------------------------------------------------------

// RateLimit returns a Gin middleware that restricts each client IP to
// maxAttempts requests per window duration.
//
// When a Redis client has been configured via SetRedisClient the middleware
// uses Redis sorted sets (ZADD / ZREMRANGEBYSCORE / ZCARD) for distributed
// rate limiting. Otherwise it falls back to a per-node in-memory map.
//
// Usage:
//
//	r := gin.Default()
//	r.POST("/api/exams/token", middleware.RateLimit(10, time.Minute), handler)
func RateLimit(maxAttempts int, window time.Duration) gin.HandlerFunc {
	redisMu.Lock()
	useRedis := globalRedis != nil
	redisMu.Unlock()

	if useRedis {
		return newRedisRateLimit(maxAttempts, window)
	}
	return newMemoryRateLimit(maxAttempts, window)
}

// ---------------------------------------------------------------------------
// In-memory rate limiter (per-node, single-process)
// ---------------------------------------------------------------------------

type memEntry struct {
	count int
	start time.Time
}

func newMemoryRateLimit(maxAttempts int, window time.Duration) gin.HandlerFunc {
	var mu sync.Mutex
	store := make(map[string]*memEntry)

	// Periodic cleanup of stale entries to prevent unbounded growth.
	go func() {
		ticker := time.NewTicker(window)
		defer ticker.Stop()
		for range ticker.C {
			mu.Lock()
			cutoff := time.Now().Add(-window * 2)
			for ip, e := range store {
				if e.start.Before(cutoff) {
					delete(store, ip)
				}
			}
			mu.Unlock()
		}
	}()

	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()

		mu.Lock()
		e, exists := store[ip]

		if !exists || now.Sub(e.start) > window {
			// New window.
			store[ip] = &memEntry{count: 1, start: now}
			mu.Unlock()
			c.Next()
			return
		}

		e.count++
		if e.count > maxAttempts {
			mu.Unlock()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"message": fmt.Sprintf(
					"Terlalu banyak permintaan. Silakan coba lagi dalam %d detik.",
					int(window.Seconds()),
				),
			})
			return
		}
		mu.Unlock()
		c.Next()
	}
}

// ---------------------------------------------------------------------------
// Redis-backed rate limiter (distributed)
// ---------------------------------------------------------------------------

func newRedisRateLimit(maxAttempts int, window time.Duration) gin.HandlerFunc {
	windowMillis := window.Milliseconds()

	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now().UnixMilli()
		key := fmt.Sprintf("ratelimit:%s:%d", ip, windowMillis)
		cutoff := now - windowMillis

		redisMu.Lock()
		rdb := globalRedis
		redisMu.Unlock()

		if rdb == nil {
			// Redis became unavailable between middleware construction and
			// this request — let it through rather than blocking.
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
		defer cancel()

		// Remove entries outside the current sliding window.
		if err := rdb.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", cutoff)).Err(); err != nil {
			// Transient Redis error; allow the request.
			c.Next()
			return
		}

		// Count entries in the current window.
		count, err := rdb.ZCard(ctx, key).Result()
		if err != nil {
			c.Next()
			return
		}

		if count >= int64(maxAttempts) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"message": fmt.Sprintf(
					"Terlalu banyak permintaan. Silakan coba lagi dalam %d detik.",
					int(window.Seconds()),
				),
			})
			return
		}

		// Record this request.
		member := fmt.Sprintf("%d", now)
		pipe := rdb.Pipeline()
		pipe.ZAddNX(ctx, key, redis.Z{Score: float64(now), Member: member})
		pipe.Expire(ctx, key, window)
		if _, err := pipe.Exec(ctx); err != nil {
			// Non-critical; request still goes through.
		}

		c.Next()
	}
}
