// Package redis provides Redis connectivity and helpers for the EXAMVAN webui.
//
// All exported functions degrade gracefully: if Redis is unavailable they
// return zero values / nil errors, so callers do not need special error
// handling for a missing Redis instance.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Key prefixes.
const (
	heartbeatPrefix = "examvan:heartbeat:"
	cachePrefix     = "examvan:cache:"
)

// ConnectDialer is a function type that creates and returns a *redis.Client.
// The default is Connect.
type ConnectDialer func(ctx context.Context, redisURL string) (*goredis.Client, error)

// defaultDial is the dialer used by Open and helpers.
var defaultDial ConnectDialer = Connect

// SetDialer overrides the default dialer (used in tests).
func SetDialer(d ConnectDialer) { defaultDial = d }

// ---------------------------------------------------------------------------
// Connection
// ---------------------------------------------------------------------------

// Open creates a Redis client from a URL string. If the URL is empty, it
// returns nil (callers treat nil client as "Redis unavailable").
func Open(ctx context.Context, redisURL string) (*goredis.Client, error) {
	return Connect(ctx, redisURL)
}

// Connect dials Redis and verifies connectivity with a PING. Returns nil
// client when redisURL is empty (graceful degradation).
func Connect(ctx context.Context, redisURL string) (*goredis.Client, error) {
	if redisURL == "" {
		return nil, nil
	}

	opts, err := goredis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("redis: parse URL: %w", err)
	}

	client := goredis.NewClient(opts)

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis: ping: %w", err)
	}

	log.Println("redis: connected")
	return client, nil
}

// isUnavailable returns true when the error is because Redis is nil or the
// connection is closed. Used to implement graceful degradation.
func isUnavailable(rdb *goredis.Client) bool {
	return rdb == nil
}

// ---------------------------------------------------------------------------
// Student Heartbeat helpers
// ---------------------------------------------------------------------------

// heartbeatKey builds the Redis key for a student's heartbeat.
func heartbeatKey(examID string, identifier string) string {
	return heartbeatPrefix + examID + ":" + identifier
}

// HeartbeatData holds the information stored for each student heartbeat.
type HeartbeatData struct {
	StudentName string `json:"student_name"`
	ExamNumber  string `json:"exam_number"`
	Class       string `json:"class"`
	LastSeen    string `json:"last_seen"` // ISO 8601 UTC
	MACAddress  string `json:"mac_address"`
	Status      string `json:"status"` // "active", "idle", "submitted"
}

// SetHeartbeat records or refreshes a student's heartbeat. Returns nil when
// Redis is unavailable.
func SetHeartbeat(rdb *goredis.Client, examID, identifier string, data *HeartbeatData) error {
	if isUnavailable(rdb) {
		return nil
	}

	if data == nil {
		return errors.New("heartbeat data is nil")
	}

	key := heartbeatKey(examID, identifier)
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("redis: marshal heartbeat: %w", err)
	}

	// 300 seconds (5 min) TTL — student must refresh within this window.
	return rdb.Set(context.Background(), key, payload, 5*time.Minute).Err()
}

// GetHeartbeat retrieves the heartbeat data for a specific student. Returns
// nil data without error when the key does not exist or Redis is unavailable.
func GetHeartbeat(rdb *goredis.Client, examID, identifier string) (*HeartbeatData, error) {
	if isUnavailable(rdb) {
		return nil, nil
	}

	key := heartbeatKey(examID, identifier)
	payload, err := rdb.Get(context.Background(), key).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis: get heartbeat: %w", err)
	}

	var data HeartbeatData
	if err := json.Unmarshal(payload, &data); err != nil {
		return nil, fmt.Errorf("redis: unmarshal heartbeat: %w", err)
	}
	return &data, nil
}

// GetActiveStudents returns heartbeat data for all active students in an exam.
// Returns an empty slice when Redis is unavailable.
func GetActiveStudents(rdb *goredis.Client, examID string) ([]HeartbeatData, error) {
	if isUnavailable(rdb) {
		return nil, nil
	}

	pattern := heartbeatPrefix + examID + ":*"
	ctx := context.Background()

	iter := rdb.Scan(ctx, 0, pattern, 0).Iterator()
	var results []HeartbeatData

	for iter.Next(ctx) {
		payload, err := rdb.Get(ctx, iter.Val()).Bytes()
		if err != nil {
			// Key may have expired between SCAN and GET.
			continue
		}
		var data HeartbeatData
		if err := json.Unmarshal(payload, &data); err != nil {
			continue
		}
		results = append(results, data)
	}

	if err := iter.Err(); err != nil {
		return results, fmt.Errorf("redis: scan heartbeats: %w", err)
	}

	if results == nil {
		results = []HeartbeatData{}
	}
	return results, nil
}

// DeleteHeartbeat removes a specific student heartbeat. Returns nil when
// Redis is unavailable.
func DeleteHeartbeat(rdb *goredis.Client, examID, identifier string) error {
	if isUnavailable(rdb) {
		return nil
	}
	key := heartbeatKey(examID, identifier)
	return rdb.Del(context.Background(), key).Err()
}

// ---------------------------------------------------------------------------
// Caching helpers
// ---------------------------------------------------------------------------

// CachedGet retrieves a cached value by key. Returns nil, nil when the key
// does not exist or Redis is unavailable.
func CachedGet(rdb *goredis.Client, key string) ([]byte, error) {
	if isUnavailable(rdb) {
		return nil, nil
	}

	fullKey := cachePrefix + key
	val, err := rdb.Get(context.Background(), fullKey).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis: cache get %s: %w", key, err)
	}
	return val, nil
}

// CachedSet stores a value in the cache with a TTL. Returns nil when Redis
// is unavailable.
func CachedSet(rdb *goredis.Client, key string, value interface{}, ttl time.Duration) error {
	if isUnavailable(rdb) {
		return nil
	}

	fullKey := cachePrefix + key

	switch v := value.(type) {
	case []byte:
		return rdb.Set(context.Background(), fullKey, v, ttl).Err()
	case string:
		return rdb.Set(context.Background(), fullKey, v, ttl).Err()
	default:
		payload, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("redis: cache marshal: %w", err)
		}
		return rdb.Set(context.Background(), fullKey, payload, ttl).Err()
	}
}

// CachedDelete removes a key from the cache. Returns nil when Redis is
// unavailable.
func CachedDelete(rdb *goredis.Client, key string) error {
	if isUnavailable(rdb) {
		return nil
	}
	return rdb.Del(context.Background(), cachePrefix+key).Err()
}
