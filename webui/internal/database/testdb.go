package database

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Per-package schema isolation for the integration-test database. Each
// package that connects to TEST_DATABASE_URL runs inside its own schema, so
// `go test ./...` can execute packages in parallel without the packages
// deadlocking on each other's TRUNCATE locks (they used to TRUNCATE the same
// tables in the shared public schema). The schema name is derived from the
// package name ("it_<package>"), so no manual registry is needed.
//
// ADDING A NEW INTEGRATION-TEST PACKAGE: when a package needs DB-backed tests,
// it must NOT connect to TEST_DATABASE_URL with a plain pgxpool and TRUNCATE
// the shared public schema — that deadlocks against the other packages when
// `go test ./...` runs in parallel. Instead, call
// database.NewPackageTestPool(t, "<package>") in the package's test setup: it
// derives the isolation schema from the package name, creates the pool via
// TestSchemaPool, applies the schema, and wipes the data tables — everything
// resolves to the package's own schema via search_path.

// Per-schema reset guards: TEST_DATABASE_RESET=1 drops the schema (CASCADE)
// exactly once per `go test` process — the first test setup that connects to
// that schema gets a clean slate, and every later setup keeps the usual
// TRUNCATE-only reset. A per-schema sync.Once is used because TestSchemaPool
// is called once per test.
var (
	testSchemaResetMu   sync.Mutex
	testSchemaResetOnce = map[string]*sync.Once{}
)

// schemaResetOnce returns the process-wide reset guard for the given schema.
func schemaResetOnce(schema string) *sync.Once {
	testSchemaResetMu.Lock()
	defer testSchemaResetMu.Unlock()
	if once, ok := testSchemaResetOnce[schema]; ok {
		return once
	}
	once := &sync.Once{}
	testSchemaResetOnce[schema] = once
	return once
}

// dropTestSchemaOnce drops the schema (CASCADE) the first time it is called
// in this process for `schema`, and is a no-op afterwards. The DROP runs on
// the pool of whichever test setup calls it first; the error is returned to
// that caller only (later callers skip the guarded body entirely).
func dropTestSchemaOnce(ctx context.Context, pool *pgxpool.Pool, schema, quotedSchema string) error {
	var dropErr error
	schemaResetOnce(schema).Do(func() {
		if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE"); err != nil {
			dropErr = err
		}
	})
	return dropErr
}

// TruncateDataTables wipes only the data tables in the pool's search_path
// schema — the same wipe every DB-backed test setup performs between runs. The
// seeded package_settings (and any saas_settings rows) are deliberately NOT
// truncated: the redeem flow and the quota snapshot depend on them. CASCADE
// covers referencing tables.
func TruncateDataTables(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
	TRUNCATE instansi, admin_users, exams, exam_pengawas, submissions,
	         student_access_logs, exam_approvals, vouchers, voucher_redemptions,
	         admin_audit_logs, system_apps
	RESTART IDENTITY CASCADE`)
	return err
}

// TestSchemaPool connects to the disposable test database named by dbURL and
// returns a pool whose connections default to the PostgreSQL schema `schema`
// (created on demand). The schema is created if missing, so repeated runs and
// parallel packages are both safe. Most packages should call
// NewPackageTestPool instead, which derives the schema from the package name
// and handles apply/truncate/cleanup; use TestSchemaPool directly only when a
// custom schema name is needed.
//
// schema.sql and the tests' TRUNCATE statements are schema-unqualified, so
// they resolve against the pool's search_path and only ever touch this
// package's schema — other packages' schemas stay untouched. The caller is
// responsible for pool.Close() (via t.Cleanup).
//
// Fresh-start mode: when the TEST_DATABASE_RESET env var is set to "1" (or
// "true"), the schema is DROPPED (CASCADE) and recreated — once per `go test`
// process per schema (see schemaResetOnce), so the first test setup gets a
// clean slate and later setups in the same package keep their usual
// TRUNCATE-only reset. Useful after schema.sql changes shape (e.g. a column
// rename), when stale objects from an old schema would otherwise survive the
// idempotent CREATE IF NOT EXISTS. Without the env var the schemas persist
// across runs, which is harmless because schema.sql is idempotent and every
// test TRUNCATEs its own data tables.
//
// Usage:
//
//	TEST_DATABASE_URL=postgresql://user:pass@localhost:5432/examvan_test \
//	  TEST_DATABASE_RESET=1 go test ./internal/models/ -run TestAuthenticateUser -v
func TestSchemaPool(ctx context.Context, dbURL, schema string) (*pgxpool.Pool, error) {
	quotedSchema := pgx.Identifier{schema}.Sanitize()

	poolCfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("testdb: parse TEST_DATABASE_URL: %w", err)
	}
	if poolCfg.ConnConfig.RuntimeParams == nil {
		poolCfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	poolCfg.ConnConfig.RuntimeParams["search_path"] = quotedSchema

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("testdb: create pool for schema %s: %w", schema, err)
	}

	// Fresh-start mode: drop the schema (and everything in it) once per
	// process so the first test setup begins from an empty slate. This runs
	// fine even though search_path already references it (a search_path may
	// name a not-yet-existing schema; only object resolution is affected).
	// The sync.Once guard keeps the expensive DROP from repeating on every
	// test; later setups rely on their usual TRUNCATE-only reset.
	if reset := strings.ToLower(strings.TrimSpace(os.Getenv("TEST_DATABASE_RESET"))); reset == "1" || reset == "true" {
		if err := dropTestSchemaOnce(ctx, pool, schema, quotedSchema); err != nil {
			pool.Close()
			return nil, fmt.Errorf("testdb: drop schema %s (TEST_DATABASE_RESET): %w", schema, err)
		}
	}

	// Create the schema itself (no-op if it already exists — including the
	// non-reset path and every setup after the process's single reset).
	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+quotedSchema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("testdb: create schema %s: %w", schema, err)
	}
	return pool, nil
}

// NewPackageTestPool is the single entry point for a package's DB-backed
// tests. It derives the isolation schema from the package name (schema =
// "it_" + packageName), connects via TestSchemaPool, applies the schema,
// wipes the data tables, and registers pool.Close on t.Cleanup. It skips (not
// fails) when TEST_DATABASE_URL is unset, so plain `go test ./...` keeps
// passing without Postgres.
//
// Usage from a package's test setup:
//
//	pool := database.NewPackageTestPool(t, "admin")
//
// The schema is unique per package (package names are unique under internal/,
// so "it_"+packageName never collides), and packages can run `go test ./...`
// in parallel: each package TRUNCATEs only the tables in its own schema, so
// no AccessExclusiveLock is ever shared. Keep package names unique across
// internal/ — a future duplicate would silently share a schema. Use
// TestSchemaPool directly instead only when a custom schema name is needed.
func NewPackageTestPool(t *testing.T, packageName string) *pgxpool.Pool {
	t.Helper()
	dbURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping integration test. " +
			"Run: TEST_DATABASE_URL=postgresql://user:pass@localhost:5432/examvan_test " +
			"go test ./...")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := TestSchemaPool(ctx, dbURL, "it_"+packageName)
	if err != nil {
		t.Fatalf("testdb: prepare pool for package %s: %v", packageName, err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("testdb: ping pool for package %s: %v", packageName, err)
	}
	if err := ApplySchema(ctx, pool); err != nil {
		t.Fatalf("testdb: apply schema for package %s: %v", packageName, err)
	}
	// Wipe the data tables in this package's schema (see TruncateDataTables —
	// the seeded package_settings/saas_settings rows are deliberately kept).
	if err := TruncateDataTables(ctx, pool); err != nil {
		t.Fatalf("testdb: truncate data tables for package %s: %v", packageName, err)
	}
	return pool
}
