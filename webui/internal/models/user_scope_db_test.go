package models

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// Operator user-list scope regression guards (skipped when TEST_DATABASE_URL
// is unset — same infra as user_sort_db_test.go, reusing setupAuthTestDB).
//
// Bug class: an operator's sub-accounts vanished from Kelola User because the
// list scoped by free-text instansi NAME only. Any label drift between the
// operator row and its sub-accounts (a lingering "personal" label from before
// the school claim, a pre-rename label, or padded legacy values) made the
// equality miss and the accounts invisible — while the quota/pool paths
// already matched ID-first.
//
// The scope is now ID-first (canonical instansi_id, legacy name fallback for
// id-less rows). These tests pin those contracts.
// ---------------------------------------------------------------------------

// TestListUsersOperatorScopeSurvivesLabelDrift pins the reported bug: a
// sub-account whose instansi NAME drifted from the operator's current name
// must still be listed via the shared instansi_id or via created_by.
func TestListUsersOperatorScopeSurvivesLabelDrift(t *testing.T) {
	pool := setupAuthTestDB(t)
	ctx := context.Background()

	var schoolID int
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMK Scope Drift', 'SCH-SCOPE-DRIFT') RETURNING id`).Scan(&schoolID); err != nil {
		t.Fatalf("create instansi row: %v", err)
	}

	op, err := CreateUser(ctx, pool, &AdminUser{
		Username: "op-scope-drift", Name: "Op Scope Drift",
		PasswordHash: "pass-op-scope-drift", Status: UserStatusActive,
		Instansi: "SMK Scope Drift", InstansiID: &schoolID, Package: "free",
		Role: SerializeRoles([]string{RoleOperator}),
	})
	if err != nil {
		t.Fatalf("create operator fixture: %v", err)
	}

	// Sub-account with a DRIFTED name label ("personal", e.g. created before
	// the school claim) but the same canonical instansi_id + created_by.
	if _, err := CreateUser(ctx, pool, &AdminUser{
		Username: "sub-scope-drifted", Name: "Sub Drifted",
		PasswordHash: "pass-sub-scope-drifted", Status: UserStatusActive,
		Instansi: "personal", InstansiID: &schoolID, Package: "free",
		Role: SerializeRoles([]string{RoleGuru}),
		OperatorCreated: true, CreatedBy: &op.ID,
	}); err != nil {
		t.Fatalf("create drifted sub fixture: %v", err)
	}

	// Legacy id-less sibling row with the same name — matched by fallback.
	if _, err := CreateUser(ctx, pool, &AdminUser{
		Username: "sub-scope-legacy", Name: "Sub Legacy",
		PasswordHash: "pass-sub-scope-legacy", Status: UserStatusActive,
		Instansi: "SMK Scope Drift", Package: "free",
		Role: SerializeRoles([]string{RoleGuru}),
	}); err != nil {
		t.Fatalf("create legacy sub fixture: %v", err)
	}

	// Another school's account — must stay hidden from this operator.
	if _, err := CreateUser(ctx, pool, &AdminUser{
		Username: "sub-scope-other", Name: "Sub Other",
		PasswordHash: "pass-sub-scope-other", Status: UserStatusActive,
		Instansi: "Sekolah Lain", Package: "free",
		Role: SerializeRoles([]string{RoleGuru}),
	}); err != nil {
		t.Fatalf("create other-school fixture: %v", err)
	}

	got := listUsersUsernames(t, pool, ListUsersOpts{
		Instansi: "SMK Scope Drift", InstansiID: &schoolID,
		ExcludeSuperAdmin: true, ExcludeOperator: true,
	})
	seen := map[string]bool{}
	for _, u := range got {
		seen[u] = true
	}
	for _, want := range []string{"sub-scope-drifted", "sub-scope-legacy"} {
		if !seen[want] {
			t.Errorf("ListUsers operator scope missing %q (got %v)", want, got)
		}
	}
	if seen["sub-scope-other"] {
		t.Errorf("ListUsers operator scope leaked other-school account (got %v)", got)
	}
	if seen["op-scope-drift"] {
		t.Errorf("ListUsers operator scope must exclude operator accounts (got %v)", got)
	}
}
