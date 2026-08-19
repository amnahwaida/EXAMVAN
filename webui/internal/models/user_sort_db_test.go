package models

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// DB-backed ListUsers sort regression guards (skipped when TEST_DATABASE_URL
// is unset — same infra as authenticate_test.go / user_create_db_test.go).
//
// The Kelola User table gained optional client-side column sorting. The
// srver must (a) honor a whitelisted sort key + direction, (b) always sort
// NULL columns last so asc/desc never hide an account, and (c) silently fall
// back to the default role-priority order for an unknown sort key (never
// error, never inject SQL). These tests pin those four contracts.
// ---------------------------------------------------------------------------

type sortFixture struct {
	username string
	email    string
}

var listUsersSortFixtures = []sortFixture{
	{"sort-c", "a@sekolah.sch.id"},
	{"sort-a", ""}, // NULL email — must sort last in both directions
	{"sort-b", "b@sekolah.sch.id"},
}

func seedListUsersSortFixtures(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	for _, fx := range listUsersSortFixtures {
		email := fx.email
		_, err := CreateUser(ctx, pool, &AdminUser{
			Username: fx.username, Name: "Sort " + fx.username,
			PasswordHash: "pass-" + fx.username, Status: UserStatusActive,
			Instansi: "personal", Role: SerializeRoles([]string{RoleGuru}),
			MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 1,
			MaxStorageSize: 50 * 1024 * 1024, Package: "free",
			Email: email,
		})
		if err != nil {
			t.Fatalf("create fixture %s: %v", fx.username, err)
		}
	}
}

func listUsersUsernames(t *testing.T, pool *pgxpool.Pool, opts ListUsersOpts) []string {
	t.Helper()
	opts.PerPage = 200
	res, err := ListUsers(context.Background(), pool, opts)
	if err != nil {
		t.Fatalf("ListUsers(%+v): %v", opts, err)
	}
	got := make([]string, 0, len(res.Users))
	for _, u := range res.Users {
		got = append(got, u.Username)
	}
	return got
}

// TestListUsersSortByUsernameDir pins the whitelisted column + direction
// handling for the Kelola User table headers.
func TestListUsersSortByUsernameDir(t *testing.T) {
	pool := setupAuthTestDB(t)
	seedListUsersSortFixtures(t, pool)

	asc := listUsersUsernames(t, pool, ListUsersOpts{SortBy: "username", SortDir: "ASC"})
	if len(asc) < 3 || asc[0] != "sort-a" || asc[1] != "sort-b" || asc[2] != "sort-c" {
		t.Fatalf("sort_by=username ASC = %v, want [sort-a sort-b sort-c ...]", asc)
	}

	desc := listUsersUsernames(t, pool, ListUsersOpts{SortBy: "username", SortDir: "DESC"})
	if len(desc) < 3 || desc[0] != "sort-c" || desc[1] != "sort-b" || desc[2] != "sort-a" {
		t.Fatalf("sort_by=username DESC = %v, want [sort-c sort-b sort-a ...]", desc)
	}
}

// TestListUsersSortByEmailNullsLast pins the NULLS LAST contract: the account
// without an email sits at the END of the page for both directions, so the
// admin always finds it instead of it disappearing on asc order.
func TestListUsersSortByEmailNullsLast(t *testing.T) {
	pool := setupAuthTestDB(t)
	seedListUsersSortFixtures(t, pool)

	asc := listUsersUsernames(t, pool, ListUsersOpts{SortBy: "email", SortDir: "ASC"})
	// a@sekolah.sch.id (sort-c) < b@sekolah.sch.id (sort-b); NULL (sort-a) last.
	if len(asc) < 2 || asc[0] != "sort-c" || asc[1] != "sort-b" {
		t.Fatalf("sort_by=email ASC = %v, want [sort-c sort-b ...] then NULL last", asc)
	}
	for i, u := range asc {
		if u == "sort-a" && i != len(asc)-1 {
			t.Fatalf("sort_by=email ASC: NULL-email account at position %d, want last (got %v)", i, asc)
		}
	}

	// DESC: emails reversed (b@... before a@...), NULL still last.
	desc := listUsersUsernames(t, pool, ListUsersOpts{SortBy: "email", SortDir: "DESC"})
	if len(desc) < 2 || desc[0] != "sort-b" || desc[1] != "sort-c" {
		t.Fatalf("sort_by=email DESC = %v, want [sort-b sort-c ...] then NULL last", desc)
	}
	for i, u := range desc {
		if u == "sort-a" && i != len(desc)-1 {
			t.Fatalf("sort_by=email DESC: NULL-email account at position %d, want last (got %v)", i, desc)
		}
	}
}

// TestListUsersSortUnknownKeyFallsBack pins the whitelist contract: a junk
// sort_by (hand-crafted request) must not error nor inject — it falls back to
// the default role-priority order and still returns every account.
func TestListUsersSortUnknownKeyFallsBack(t *testing.T) {
	pool := setupAuthTestDB(t)
	seedListUsersSortFixtures(t, pool)

	got := listUsersUsernames(t, pool, ListUsersOpts{SortBy: "username; DROP TABLE admin_users--", SortDir: "DESC"})
	if len(got) < 3 {
		t.Fatalf("unknown sort key resulted in only %d users (want >= 3): %v", len(got), got)
	}
	// Default order = role priority, then username ASC: the three guru
	// fixtures are peers, so the first three entries are sort-a/b/c in
	// ascending username order.
	if got[0] != "sort-a" || got[1] != "sort-b" || got[2] != "sort-c" {
		t.Fatalf("unknown sort key = %v, want default role-priority + username asc [sort-a sort-b sort-c ...]", got)
	}
}
