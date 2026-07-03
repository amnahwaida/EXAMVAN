package middleware

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

var (
	globalRedis *redis.Client
	redisMu     sync.Mutex
)

func SetRedisClient(client *redis.Client) {
	redisMu.Lock()
	defer redisMu.Unlock()
	globalRedis = client
}

func RateLimit(maxAttempts int, window time.Duration) gin.HandlerFunc {
	redisMu.Lock()
	useRedis := globalRedis != nil
	redisMu.Unlock()

	if useRedis {
		return newRedisRateLimit(maxAttempts, window)
	}
	return newMemoryRateLimit(maxAttempts, window)
}

type memEntry struct {
	count  int
	start  time.Time
	window time.Duration
}

const maxMemEntries = 5000

var (
	memStore   = make(map[string]*memEntry)
	memStoreMu sync.Mutex
	memCleaner sync.Once
)

func startMemCleaner() {
	memCleaner.Do(func() {
		go func() {
			// Check every 10 seconds to keep memory bounded and clean stale entries
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				memStoreMu.Lock()
				now := time.Now()
				for ip, e := range memStore {
					if now.Sub(e.start) > e.window {
						delete(memStore, ip)
					}
				}
				// If still exceeding, evict oldest entries
				if len(memStore) > maxMemEntries {
					evict := len(memStore) - maxMemEntries
					for ip := range memStore {
						if evict <= 0 {
							break
						}
						delete(memStore, ip)
						evict--
					}
				}
				memStoreMu.Unlock()
			}
		}()
	})
}

func newMemoryRateLimit(maxAttempts int, window time.Duration) gin.HandlerFunc {
	startMemCleaner()

	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()

		memStoreMu.Lock()
		// Double check size bounds on write
		if len(memStore) >= maxMemEntries && memStore[ip] == nil {
			// Evict oldest random entry to avoid unbounded growth
			for k := range memStore {
				delete(memStore, k)
				break
			}
		}

		e, exists := memStore[ip]

		if !exists || now.Sub(e.start) > window {
			memStore[ip] = &memEntry{count: 1, start: now, window: window}
			memStoreMu.Unlock()
			c.Next()
			return
		}

		e.count++
		if e.count > maxAttempts {
			memStoreMu.Unlock()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"message": fmt.Sprintf(
					"Terlalu banyak permintaan. Silakan coba lagi dalam %d detik.",
					int(window.Seconds()),
				),
			})
			return
		}
		memStoreMu.Unlock()
		c.Next()
	}
}

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
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
		defer cancel()

		// sliding window implementation using transaction pipeline
		member := fmt.Sprintf("%d-%d", now, time.Now().UnixNano())
		pipe := rdb.Pipeline()
		pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: member})
		pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", cutoff))
		pipe.ZCard(ctx, key)
		pipe.Expire(ctx, key, window)

		cmds, err := pipe.Exec(ctx)
		if err != nil {
			log.Printf("ratelimit: redis exec error: %v", err)
			c.Next()
			return
		}

		zcardCmd, ok := cmds[2].(*redis.IntCmd)
		if !ok {
			c.Next()
			return
		}
		count, err := zcardCmd.Result()
		if err != nil {
			c.Next()
			return
		}

		if count > int64(maxAttempts) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"message": fmt.Sprintf(
					"Terlalu banyak permintaan. Silakan coba lagi dalam %d detik.",
					int(window.Seconds()),
				),
			})
			return
		}

		c.Next()
	}
}
