package models

import (
	"fmt"
	"strconv"
	"strings"
)

// InstansiScope identifies a tenant for scoping queries: the linked
// instansi_id when the row carries one, plus the free-text instansi name
// (display value, and the only identity legacy rows have).
//
// Why both: admin_users.instansi is a free-text NAME, not a unique identifier
// — two schools may share a name and tenant queries that compare names bleed
// across them (finding A, review 13 Sep 2026). instansi_id is the canonical
// identity, but rows written before the column existed (and buckets like
// "personal") have NULL instansi_id, so matching ids only would silently
// unscope them. Every tenant match is therefore dual-form: id equality when
// the row has an id, falling back to case-insensitive name equality for rows
// that do not.
type InstansiScope struct {
	ID   *int
	Name string
}

// IsBucket reports whether the scope points at a shared system bucket
// ("personal" and friends) rather than a real school. Bucket accounts are
// never a tenant: queries must not treat one operator's personal accounts as
// another's just because the label matches.
func (s InstansiScope) IsBucket() bool {
	if s.Name == "" {
		return true
	}
	switch lower(s.Name) {
	case "personal", "owner":
		return true
	}
	return false
}

// lower is a tiny local helper so this file does not need the strings import
// for one call site.
func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// InstansiScopeFromTenantKey rebuilds a scope from a tenant key produced by
// RunningExamCountsAfterActivationByInstansi ("<id>" for id-linked tenants,
// "n:<lowercased name>" for legacy id-less ones). Unknown formats map to a
// name-only scope — the caller's query then matches nothing harmful.
func InstansiScopeFromTenantKey(key string) InstansiScope {
	if key == "" {
		return InstansiScope{}
	}
	if id, err := strconv.Atoi(key); err == nil {
		return InstansiScope{ID: &id}
	}
	if strings.HasPrefix(key, "n:") {
		return InstansiScope{Name: strings.TrimPrefix(key, "n:")}
	}
	return InstansiScope{Name: key}
}

// InstansiMatchSQL returns a WHERE fragment and its arguments matching rows of
// admin_users that belong to the given tenant scope. col is the table alias
// (may be empty for unaliased columns); startIdx is the placeholder index the
// fragment's arguments begin at. The caller is responsible for prepending AND
// / WHERE and for any role/status filters.
//
// With an id: `(col.instansi_id = $n OR (col.instansi_id IS NULL AND
// LOWER(col.instansi) = LOWER($n+1)))` — id-first, legacy-name fallback for
// sibling rows not yet backfilled. Without an id (legacy rows, buckets, or a
// destination instansi that does not exist yet): plain case-insensitive name
// match, the pre-migration semantics.
func InstansiMatchSQL(col string, startIdx int, s InstansiScope) (string, []interface{}) {
	col = strings.TrimSuffix(col, ".")
	if col != "" {
		col = col + "."
	}
	if s.ID != nil {
		// The name argument is cast to TEXT explicitly: in fragments where it
		// is only consumed inside LOWER($n) (a legacy-id-less sibling row)
		// Postgres cannot infer the parameter type (42P18).
		return fmt.Sprintf("(%[1]sinstansi_id = $%[2]d OR (%[1]sinstansi_id IS NULL AND LOWER(%[1]sinstansi) = LOWER($%[3]d::text)))",
			col, startIdx, startIdx+1),
		[]interface{}{*s.ID, s.Name}
	}
	return fmt.Sprintf("LOWER(%[1]sinstansi) = LOWER($%[2]d::text)", col, startIdx), []interface{}{s.Name}
}

// InstansiMatchSelfSQL returns a parameterless WHERE fragment asserting that
// two aliased admin_users rows (me, owner) belong to the same tenant. Used by
// the exam authorization gates, where both sides are rows of the query itself
// and neither can be bound as a placeholder:
//
//   - both rows carry the same non-null instansi_id (canonical), or
//   - both are legacy rows without an id whose names match
//     case-insensitively and are not a system bucket.
//
// A row WITH an id never matches a legacy row, even by name: once a school has
// a canonical id, that id — not the mutable name — is the tenant boundary, and
// the schema backfill assigns ids to every named row precisely so this branch
// is not reachable for real schools.
func InstansiMatchSelfSQL(me, owner string) string {
	return `(
		(` + me + `.instansi_id IS NOT NULL AND ` + me + `.instansi_id = ` + owner + `.instansi_id)
		OR (` + me + `.instansi_id IS NULL AND ` + owner + `.instansi_id IS NULL
		    AND ` + me + `.instansi NOT IN ('', 'personal')
		    AND LOWER(` + me + `.instansi) = LOWER(` + owner + `.instansi))
	)`
}
