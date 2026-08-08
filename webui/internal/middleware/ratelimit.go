package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// getRateLimitKeys returns the counter keys for a request. Every request gets
// its client-IP (or, once authenticated, user) bucket; when includeFP is true,
// a device fingerprint provided via X-Device-Fingerprint adds a SECOND,
// parallel dimension. The limiter enforces the limit independently per key, so
// the request is rejected if ANY dimension exceeds the cap.
//
// This is deliberately NOT "hash(ip + fingerprint)": merging the two into one
// bucket would let an attacker who rotates IPs (or spoofs fingerprints) obtain
// a fresh bucket per attempt and defeat the limit. Two parallel counters means
// spoofing the fingerprint only resets the fingerprint counter — the IP counter
// still accumulates and still blocks. Legitimate users who share one IP (e.g. a
// school behind NAT) get separate buckets and no longer lock each other out.
//
// includeFP is true ONLY for short-lived public auth actions (login, register,
// OTP, password reset) where a second dimension is valuable and where a fresh
// page load recomputes the browser fingerprint anyway. Long-lived exam routes
// (list/PDF/submit/access-log, driven by the Android app) deliberately use the
// IP-only path: the device_id behind the app fingerprint could change mid-exam
// (reinstall/clear-data), and we never want to introduce a dimension that can
// shift under an in-progress exam. Kelancaran ujian siswa > dimensi tambahan.
func getRateLimitKeys(c *gin.Context, includeFP bool) []string {
	keys := make([]string, 0, 2)
	if uidVal, exists := c.Get("user_id"); exists {
		if uid, ok := uidVal.(int); ok {
			keys = append(keys, fmt.Sprintf("user_%d", uid))
		}
	} else {
		keys = append(keys, c.ClientIP())
	}
	if !includeFP {
		return keys
	}
	// Prefer the header (Android app); fall back to the hidden form field the
	// public web forms submit (browser fingerprint — see
	// static/js/device-fingerprint.js). Either source is an untrusted hint that
	// only ADDS a second counter; the IP/user counter is never weakened by it.
	fp := strings.TrimSpace(c.GetHeader("X-Device-Fingerprint"))
	if fp == "" {
		fp = strings.TrimSpace(c.PostForm("device_fingerprint"))
	}
	if fp != "" {
		// The header is untrusted input; hash it so a value that is huge,
		// binary, or otherwise hostile never reaches the key space or the
		// memory map (the raw value is also not kept around beyond this hash).
		sum := sha256.Sum256([]byte(fp))
		keys = append(keys, "fp:"+hex.EncodeToString(sum[:]))
	}
	return keys
}

// RateLimitIP enforces the cap on the client IP/user dimension ONLY — no
// fingerprint dimension. This is the default for the long-lived exam routes so
// a mid-exam device_id change can never interfere with an in-progress exam.
func RateLimitIP(maxAttempts int, window time.Duration) gin.HandlerFunc {
	return newRateLimit(maxAttempts, window, false)
}

// RateLimit enforces the cap on the client IP/user AND fingerprint dimensions
// (when a fingerprint is supplied). Used on short-lived public auth actions.
func RateLimit(maxAttempts int, window time.Duration) gin.HandlerFunc {
	return newRateLimit(maxAttempts, window, true)
}

func newRateLimit(maxAttempts int, window time.Duration, includeFP bool) gin.HandlerFunc {
	// Pre-create the handlers so we don't recreate them on every request
	memHandler := newMemoryRateLimit(maxAttempts, window, includeFP)
	redisHandler := newRedisRateLimit(maxAttempts, window, includeFP)

	return func(c *gin.Context) {
		rdbVal, exists := c.Get("redis")
		if exists && rdbVal.(*redis.Client) != nil {
			redisHandler(c)
		} else {
			memHandler(c)
		}
	}
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

func newMemoryRateLimit(maxAttempts int, window time.Duration, includeFP bool) gin.HandlerFunc {
	startMemCleaner()

	return func(c *gin.Context) {
		keys := getRateLimitKeys(c, includeFP)
		now := time.Now()

		memStoreMu.Lock()
		blocked := false
		for _, key := range keys {
			// Double check size bounds on write
			if len(memStore) >= maxMemEntries && memStore[key] == nil {
				// Evict oldest random entry to avoid unbounded growth
				for k := range memStore {
					delete(memStore, k)
					break
				}
			}

			e, exists := memStore[key]

			if !exists || now.Sub(e.start) > window {
				memStore[key] = &memEntry{count: 1, start: now, window: window}
				continue
			}

			e.count++
			if e.count > maxAttempts {
				blocked = true
				break
			}
		}
		memStoreMu.Unlock()

		if blocked {
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

func newRedisRateLimit(maxAttempts int, window time.Duration, includeFP bool) gin.HandlerFunc {
	windowMillis := window.Milliseconds()

	return func(c *gin.Context) {
		keys := getRateLimitKeys(c, includeFP)
		now := time.Now().UnixMilli()
		cutoff := now - windowMillis

		rdbVal, exists := c.Get("redis")
		if !exists || rdbVal.(*redis.Client) == nil {
			c.Next()
			return
		}
		rdb := rdbVal.(*redis.Client)

		ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
		defer cancel()

		// sliding window implementation using a single transaction pipeline over
		// every dimension key (IP/user + fingerprint). Add + prune + count each
		// key in one round trip; reject if any dimension exceeds the cap.
		member := fmt.Sprintf("%d-%d", now, time.Now().UnixNano())
		type keyResult struct {
			key    string
			cmdIdx int
		}
		results := make([]keyResult, 0, len(keys))
		cmdBase := 0
		pipe := rdb.Pipeline()
		for _, k := range keys {
			key := fmt.Sprintf("ratelimit:%s:%d", k, windowMillis)
			pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: member})
			pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", cutoff))
			pipe.ZCard(ctx, key)
			pipe.Expire(ctx, key, window)
			results = append(results, keyResult{key: key, cmdIdx: cmdBase + 2})
			cmdBase += 4
		}

		cmds, err := pipe.Exec(ctx)
		if err != nil {
			log.Printf("ratelimit: redis exec error: %v", err)
			c.Next()
			return
		}

		blocked := false
		for _, r := range results {
			zcardCmd, ok := cmds[r.cmdIdx].(*redis.IntCmd)
			if !ok {
				continue
			}
			count, err := zcardCmd.Result()
			if err != nil {
				continue
			}
			if count > int64(maxAttempts) {
				log.Printf("ratelimit: %s over limit (%d/%d)", r.key, count, maxAttempts)
				blocked = true
				break
			}
		}

		if blocked {
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
