// Package database provides PostgreSQL connectivity using pgx v5.
package database

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/config"
)

//go:embed schema.sql
var schemaSQL string

// Connect initializes a pgxpool connection to PostgreSQL using the
// DATABASE_URL from the provided config. It pings the database, runs
// the embedded schema.sql to ensure all tables exist, and returns the
// ready-to-use pool. The caller is responsible for calling pool.Close()
// on graceful shutdown.
func Connect(cfg *config.Config) (*pgxpool.Pool, error) {
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("database: DATABASE_URL is required")
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("database: failed to parse DATABASE_URL: %w", err)
	}

	poolCfg.MaxConns = int32(cfg.DatabaseMaxConns)
	poolCfg.MinConns = int32(cfg.DatabaseMaxConns / 5)
	if poolCfg.MinConns < 2 {
		poolCfg.MinConns = 2
	}

	poolCfg.ConnConfig.ConnectTimeout = 15 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("database: failed to create connection pool: %w", err)
	}

	// Verify reachability.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping failed: %w", err)
	}

	log.Println("database: connected to PostgreSQL")

	// Run schema DDL. All statements use CREATE IF NOT EXISTS so this
	// is safe to run on every startup (zero-downtime migrations).
	if err := runSchema(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: schema initialization failed: %w", err)
	}

	return pool, nil
}

// runSchema executes the embedded schema.sql against the pool.
func runSchema(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("exec schema: %w", err)
	}
	return nil
}
