// Package redis provides Redis connectivity for the EXAMVAN webui.
package redis

import (
	"context"
	"fmt"
	"log"

	goredis "github.com/redis/go-redis/v9"
)

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
