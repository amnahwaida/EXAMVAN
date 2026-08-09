package database

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestTestSchemaPoolIsolatesSchemas verifies the core isolation guarantee of
// TestSchemaPool: two pools bound to different schemas of the same test
// database do NOT see each other's tables. An unqualified query resolves to
// each pool's own schema via search_path, so (1) rows written through pool A
// into a table that exists in both schemas are invisible to pool B, and (2) a
// table created only in A does not exist for B at all. This is the property
// that lets packages run `go test ./...` in parallel without deadlocking on
// shared TRUNCATE locks. Skips (not fails) when TEST_DATABASE_URL is unset.
func TestTestSchemaPoolIsolatesSchemas(t *testing.T) {
	dbURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping integration test. " +
			"Run: TEST_DATABASE_URL=postgresql://user:pass@localhost:5432/examvan_test " +
			"go test ./internal/database/ -run TestTestSchemaPool -v")
	}
	ctx := context.Background()

	poolA, err := TestSchemaPool(ctx, dbURL, "it_iso_a")
	if err != nil {
		t.Fatalf("TestSchemaPool(it_iso_a): %v", err)
	}
	// Cleanup runs LIFO: poolA.Close is registered first, so the DROP below
	// executes while the pool is still open (a closed pool could not run it).
	t.Cleanup(poolA.Close)
	t.Cleanup(func() {
		if _, err := poolA.Exec(ctx, `DROP SCHEMA IF EXISTS it_iso_a CASCADE`); err != nil {
			t.Errorf("drop schema it_iso_a: %v", err)
		}
	})

	poolB, err := TestSchemaPool(ctx, dbURL, "it_iso_b")
	if err != nil {
		t.Fatalf("TestSchemaPool(it_iso_b): %v", err)
	}
	t.Cleanup(poolB.Close)
	t.Cleanup(func() {
		if _, err := poolB.Exec(ctx, `DROP SCHEMA IF EXISTS it_iso_b CASCADE`); err != nil {
			t.Errorf("drop schema it_iso_b: %v", err)
		}
	})

	// Both schemas get the same-named table — the real deadlock scenario is two
	// packages each owning admin_users/exams/etc., so prove rows do not leak
	// between identical table names.
	for _, pool := range []*pgxpool.Pool{poolA, poolB} {
		if _, err := pool.Exec(ctx, `CREATE TABLE iso_tbl (id int)`); err != nil {
			t.Fatalf("create iso_tbl: %v", err)
		}
	}

	// 1. Data isolation: a row inserted through A is invisible through B.
	if _, err := poolA.Exec(ctx, `INSERT INTO iso_tbl VALUES (1)`); err != nil {
		t.Fatalf("insert via A: %v", err)
	}
	var inA, inB int
	if err := poolA.QueryRow(ctx, `SELECT count(*) FROM iso_tbl`).Scan(&inA); err != nil {
		t.Fatalf("count via A: %v", err)
	}
	if err := poolB.QueryRow(ctx, `SELECT count(*) FROM iso_tbl`).Scan(&inB); err != nil {
		t.Fatalf("count via B: %v", err)
	}
	if inA != 1 {
		t.Errorf("pool A sees %d rows in iso_tbl, want 1 (its own insert)", inA)
	}
	if inB != 0 {
		t.Errorf("pool B sees %d rows in iso_tbl, want 0 (isolated from A's insert)", inB)
	}

	// 2. Table isolation: a table created only in A does not exist for B — the
	// query fails with Postgres 42P01 (undefined_table), proving B resolved it
	// against its own schema and found nothing.
	if _, err := poolA.Exec(ctx, `CREATE TABLE only_in_a (id int)`); err != nil {
		t.Fatalf("create only_in_a via A: %v", err)
	}
	var n int
	if err := poolB.QueryRow(ctx, `SELECT count(*) FROM only_in_a`).Scan(&n); err == nil {
		t.Errorf("pool B can see A-only table only_in_a (count=%d), want relation-not-found", n)
	} else if !isUndefinedTable(err) {
		t.Errorf("pool B querying only_in_a: unexpected error %v", err)
	}
}

// isUndefinedTable reports whether err is Postgres error 42P01
// (undefined_table) — the proof that a table genuinely does not exist in the
// pool's schema rather than some other failure.
func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P01"
}
