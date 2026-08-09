package admin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/examvan/webui/internal/models"
)

// packageEntitlement returns (exams, pdfBytes, concurrent, storageBytes,
// maxUsers, role) for a fixed package. `concurrent` is the max_concurrent_exams
// quota: the number of exams that may RUN simultaneously (mirrors the
// "Ujian Aktif" values advertised on the pricing page, so concurrent <= total
// exams). `maxUsers` is the sub-account quota: how many accounts (besides the
// operator) may exist in one school instansi; 0 = unlimited. These defaults
// mirror the package_settings seed in schema.sql (SuperAdmin-editable).
func packageEntitlement(pkg string) (exams, pdf, concurrent, storage, maxUsers int64, role string) {
	switch pkg {
	case "guru":
		return 1, 10 * 1024 * 1024, 1, 100 * 1024 * 1024, 0, ""
	case "individu":
		return 2, 30 * 1024 * 1024, 2, 300 * 1024 * 1024, 0, ""
	case "sekolah_kecil":
		return 3, 50 * 1024 * 1024, 3, 500 * 1024 * 1024, 10, models.SerializeRoles([]string{models.RoleOperator})
	case "sekolah_menengah":
		return 5, 200 * 1024 * 1024, 5, 2000 * 1024 * 1024, 25, models.SerializeRoles([]string{models.RoleOperator})
	case "sekolah_besar":
		return 10, 500 * 1024 * 1024, 10, 5000 * 1024 * 1024, 50, models.SerializeRoles([]string{models.RoleOperator})
	case "sekolah_unggulan":
		return 99999, 99999 * 1024 * 1024, 99999, 999999 * 1024 * 1024, 0, models.SerializeRoles([]string{models.RoleOperator})
	default:
		return 1, 1 * 1024 * 1024, 1, 50 * 1024 * 1024, 0, ""
	}
}

// durationDays maps a voucher duration type ("bulanan", "semester", "tahunan",
// or a positive integer as days) to the number of days the entitlement lasts.
func durationDays(durationType string) int {
	switch durationType {
	case "bulanan":
		return 30
	case "semester":
		return 180
	case "tahunan":
		return 365
	default:
		if d, err := strconv.Atoi(durationType); err == nil && d > 0 {
			return d
		}
		return 30
	}
}

// containsRole reports whether roles contains target.
func containsRole(roles []string, target string) bool {
	for _, r := range roles {
		if r == target {
			return true
		}
	}
	return false
}

// applyRedemptionEntitlement applies a voucher redemption's snapshot to a user:
// overwrites the quota columns with the snapshot values, sets the account
// expiry to the redemption's current running period (activated_at +
// remaining_seconds), and makes the role follow the ACTIVE package. The caller
// must have set ActivatedAt (the start of the currently-running period) and
// RemainingSeconds before calling.
//
// Roles are tracked in two columns:
//   - base_role: roles the user holds independently of packages (identity,
//     admin grants, delegated pengawas);
//   - package_role: roles granted by the currently-ACTIVE package.
//
// On activation the role becomes base_role ∪ new package roles, so
// package-granted roles are transient (an operator from a sekolah package
// disappears when a guru voucher is activated and returns when a sekolah
// package is active again) while admin-granted roles survive a switch even
// when they coincide with what a package grants. base_role is initialized
// lazily from the current roles minus the current package's grant (fresh
// accounts and pre-migration rows). A SuperAdmin's role is never changed.
func applyRedemptionEntitlement(ctx context.Context, tx pgx.Tx, userID int, r *models.VoucherRedemption) error {
	if r.ActivatedAt == nil {
		return fmt.Errorf("redemption has no running period (activated_at is nil)")
	}
	if r.RemainingSeconds <= 0 {
		return fmt.Errorf("redemption has no remaining lifetime")
	}
	expiry := r.ActivatedAt.Add(time.Duration(r.RemainingSeconds) * time.Second)

	var currentRoleJSON, currentPkgRoleJSON, currentBaseRoleJSON string
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(role, ''), COALESCE(package_role, ''), COALESCE(base_role, '') FROM admin_users WHERE id = $1`,
		userID).Scan(&currentRoleJSON, &currentPkgRoleJSON, &currentBaseRoleJSON); err != nil {
		return err
	}

	// Roles granted by the package being activated ('' = grants no role).
	pkgRole := strings.TrimSpace(r.Role)
	roleJSON, nextPkgRole, nextBaseRole := nextRolesState(currentRoleJSON, currentPkgRoleJSON, currentBaseRoleJSON, pkgRole)

	pkgLabel := strings.TrimSpace(r.Package)
	if pkgLabel == "" {
		pkgLabel = "custom"
	}

	concurrent := r.MaxConcurrentExams
	if concurrent <= 0 {
		concurrent = r.MaxExams
	}
	if concurrent <= 0 {
		concurrent = 1
	}

	// Never touch the account's status here: expired accounts keep status
	// 'active' (expiry is time-based), and a suspended account must STAY
	// suspended — writing status = 'active' would let a suspended user
	// self-reactivate by redeeming/activating a voucher (the caller already
	// rejects suspended accounts, this is the last line of defense).
	_, err := tx.Exec(ctx, `UPDATE admin_users SET
		package = $1, max_exams = $2, max_pdf_size = $3,
		max_concurrent_exams = $4, max_storage_size = $5,
		expires_at = $6, role = $7, package_role = $8, base_role = $9
		WHERE id = $10`,
		pkgLabel, r.MaxExams, r.MaxPDFSize, concurrent, r.MaxStorageSize, expiry,
		roleJSON, nextPkgRole, nextBaseRole, userID)
	if err != nil {
		return err
	}

	// Keep the school's sub-accounts consistent with the package-granted
	// operator role (see syncInstansiWithOperatorRole).
	return syncInstansiWithOperatorRole(ctx, tx, userID, currentRoleJSON, roleJSON, expiry)
}

// syncInstansiWithOperatorRole keeps the accounts of a school instansi in sync
// with the package-granted operator role after a package activation:
//
//   - role HAS operator: restore accounts that were cascade-suspended when the
//     operator left the school package (freezing their clocks for the
//     suspension period), then align the expiry of sub-accounts that do not
//     run their own active package to the operator's new expiry — a school
//     operator's accounts follow the operator's expiry;
//   - role LOST operator: suspend the instansi's active non-operator accounts
//     (suspended_by_cascade), so accounts created under a school package stop
//     being usable once the operator leaves the school package. Their exams
//     that are active but have never been started (exam_started_at IS NULL)
//     are tombstoned to inactive too, so the school's unpublished exams go
//     dormant the moment the operator role is lost — exams already running
//     are left untouched so students working on them can finish.
//
// Pure guru → guru switches (no operator involved at all) are no-ops. The
// operator's own row, SuperAdmin, and manually suspended accounts are never
// touched. Must be called inside the redemption transaction, after the user's
// role columns have been updated.
func syncInstansiWithOperatorRole(ctx context.Context, tx pgx.Tx, userID int, prevRoleJSON, roleJSON string, expiry time.Time) error {
	if containsRole(models.ParseRoles(roleJSON), models.RoleSuperAdmin) {
		return nil // SuperAdmin role is never touched anywhere.
	}

	var instansi string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(instansi, '') FROM admin_users WHERE id = $1`, userID).Scan(&instansi); err != nil {
		return err
	}
	instansi = strings.TrimSpace(instansi)
	if instansi == "" || strings.EqualFold(instansi, "personal") {
		return nil // No school, no sub-accounts.
	}

	switch operatorRoleTransition(prevRoleJSON, roleJSON) {
	case "none":
		return nil // e.g. guru → guru: nothing to reconcile.
	case "suspend":
		// The operator role is gone: suspend every active non-operator account
		// in the instansi so accounts created under the school package stop
		// working. suspended_by_cascade lets them come back (with a clock
		// freeze) when the operator returns to a school package.
		if _, err := tx.Exec(ctx, `
			UPDATE admin_users
			SET status = 'suspended', suspended_by_cascade = TRUE, suspended_at = now()
			WHERE instansi = $1 AND status = 'active' AND id <> $2
			  AND NOT (role ILIKE '%"operator"%')`, instansi, userID); err != nil {
			return err
		}
		// Tombstone the school's unpublished exams (policy B): the accounts
		// cut off from the school package (the operator who just lost the
		// role and every cascade-suspended sub) have their active-but-unstarted
		// exams set inactive. Accounts that still hold the operator role (their
		// own school voucher) stay valid operators, so their exams are spared.
		return tombstoneUnstartedInstansiExams(ctx, tx, instansi, true)
	default: // restore
		// Operator role is present: restore accounts that were cascade-suspended
		// when the operator last left the school package (clock freeze applies)
		// and realign their active package clocks with the frozen expiry.
		if err := models.RestoreCascadeSuspendedInstansi(ctx, tx, instansi, userID); err != nil {
			return err
		}
		// Sub-accounts that do not run their own active package follow the
		// operator's expiry. GREATEST only ever EXTENDS an existing expiry
		// (never shortens a sub-account when the operator switches to a
		// shorter school package); when the operator truly leaves the school
		// package the suspend branch above takes over.
		_, err := tx.Exec(ctx, `
			UPDATE admin_users u
			SET expires_at = GREATEST(COALESCE(u.expires_at, $2::timestamptz), $2::timestamptz)
			WHERE u.instansi = $1 AND u.id <> $3
			  AND NOT (u.role ILIKE '%"operator"%')
			  AND NOT EXISTS (
			      SELECT 1 FROM voucher_redemptions vr
			      WHERE vr.user_id = u.id AND vr.is_active
			  )`, instansi, expiry, userID)
		return err
	}
}

// tombstoneUnstartedInstansiExams (policy B) sets every exam in the instansi
// that is active but has never been started (exam_started_at IS NULL) to
// inactive — the school's unpublished exams go dormant when the school's
// operator is cut off (voucher switch or manual suspension). Exams already
// running are deliberately left untouched so students working on them can
// finish, and the tombstone is never auto-reversed: the owner re-activates
// manually, so an admin's explicit inactivation is never clobbered.
//
// spareOperatorRoleCreators controls which exams are covered. When true
// (voucher switch), accounts that still hold the operator role (their own
// school voucher) remain valid operators, so their exams are spared; when
// false (a manual operator suspension freezes the whole school), every
// account in the instansi is covered.
func tombstoneUnstartedInstansiExams(ctx context.Context, exec models.Executor, instansi string, spareOperatorRoleCreators bool) error {
	creatorFilter := `TRUE`
	if spareOperatorRoleCreators {
		creatorFilter = `NOT (role ILIKE '%"operator"%')`
	}
	// tombstoned_at marks the exam as auto-inactivated (policy B) so the admin
	// UI can tell it apart from a manual inactivation; the marker is cleared
	// whenever the exam is (re)activated.
	_, err := exec.Exec(ctx, fmt.Sprintf(`
		UPDATE exams e
		SET status = 'inactive', tombstoned_at = now()
		WHERE e.status = 'active' AND e.exam_started_at IS NULL
		  AND e.created_by IN (
		      SELECT id FROM admin_users
		      WHERE instansi = $1 AND %s
		  )`, creatorFilter), instansi)
	return err
}

// operatorRoleTransition decides how the school's sub-accounts must react to a
// package activation based on the operator role before and after:
//   - "suspend": the operator role was lost — sub-accounts are deactivated;
//   - "restore": the operator role is present (gained or retained) —
//     cascade-suspended sub-accounts are restored and their expiry realigned;
//   - "none": no operator role on either side — nothing to reconcile.
//
// Pure function (no DB access) so the school → guru → school transition can be
// unit-tested directly; the SQL wrapper is syncInstansiWithOperatorRole.
func operatorRoleTransition(prevRoleJSON, roleJSON string) string {
	prevHasOp := containsRole(models.ParseRoles(prevRoleJSON), models.RoleOperator)
	newHasOp := containsRole(models.ParseRoles(roleJSON), models.RoleOperator)
	if !prevHasOp && !newHasOp {
		return "none"
	}
	if newHasOp {
		return "restore"
	}
	return "suspend"
}

// nextRolesState computes the (role, package_role, base_role) columns for a
// package activation from the user's current columns and the package's
// snapshot role. It is a pure function (no DB access) so the "role follows
// the active package" semantics can be unit-tested directly; the SQL wrapper
// is applyRedemptionEntitlement.
//
// Roles are tracked in two columns:
//   - base_role: roles the user holds independently of packages (identity,
//     admin grants, delegated pengawas);
//   - package_role: roles granted by the currently-ACTIVE package.
//
// The resulting role is base_role ∪ new package roles, so package-granted
// roles are transient (an operator from a sekolah package disappears when a
// guru voucher is activated and returns when a sekolah package is active
// again) while admin-granted roles survive a switch even when they coincide
// with what a package grants. base_role is initialized lazily from the
// current roles minus the current package's grant (fresh accounts and
// pre-migration rows). A SuperAdmin's role is never changed.
func nextRolesState(currentRoleJSON, currentPkgRoleJSON, currentBaseRoleJSON, newPkgRole string) (roleJSON, nextPkgRole, nextBaseRole string) {
	newPkgRoles := parsePackageRoles(newPkgRole)
	currentRoles := models.ParseRoles(currentRoleJSON)

	nextPkgRole = newPkgRole
	nextBaseRole = currentBaseRoleJSON

	if containsRole(currentRoles, models.RoleSuperAdmin) {
		// SuperAdmin: role is never touched, and package roles are never
		// applied to (or tracked on) the account.
		return models.SerializeRoles(currentRoles), "", currentBaseRoleJSON
	}

	// Defense-in-depth: a package must never grant the superadmin role to a
	// user who is not already superadmin. Every creation path (custom voucher
	// role, package_settings) whitelists roles, but a direct DB edit could
	// inject "superadmin" — strip it here at the last line of defense and
	// never track it in package_role.
	sanitized := newPkgRoles[:0]
	for _, r := range newPkgRoles {
		if r != models.RoleSuperAdmin {
			sanitized = append(sanitized, r)
		}
	}
	if len(sanitized) != len(newPkgRoles) {
		newPkgRoles = sanitized
		nextPkgRole = models.SerializeRoles(sanitized)
		if len(sanitized) == 0 {
			nextPkgRole = ""
		}
	}

	// The base is the user's roles beyond packages. Once tracked it is
	// preserved across switches; for fresh/pre-migration accounts it is
	// derived lazily as the current roles minus the current package's grant
	// (so a legacy accumulated operator is not baked in when the active
	// package never granted it).
	base := parsePackageRoles(currentBaseRoleJSON)
	if currentBaseRoleJSON == "" {
		prevPkgRoles := parsePackageRoles(currentPkgRoleJSON)
		base = make([]string, 0, len(currentRoles))
		for _, rr := range currentRoles {
			if !containsRole(prevPkgRoles, rr) {
				base = append(base, rr)
			}
		}
	}

	// role = base ∪ new package roles.
	merged := make([]string, 0, len(base)+len(newPkgRoles))
	for _, rr := range base {
		if !containsRole(merged, rr) {
			merged = append(merged, rr)
		}
	}
	for _, rr := range newPkgRoles {
		if !containsRole(merged, rr) {
			merged = append(merged, rr)
		}
	}
	if len(merged) == 0 {
		merged = []string{models.RoleGuru} // always keep the base role
	}
	if len(base) == 0 {
		base = []string{models.RoleGuru}
	}
	return models.SerializeRoles(merged), nextPkgRole, models.SerializeRoles(base)
}

// parsePackageRoles parses a package-granted role string, treating an empty
// value as "no roles". Unlike models.ParseRoles, which defaults an empty
// string to [guru], a package only grants exactly what its snapshot stores.
// An empty JSON array ("[]") is also treated as no roles, so hand-edited
// rows never produce a literal "[]" role via ParseRoles' fallback.
func parsePackageRoles(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "[]" {
		return nil
	}
	return models.ParseRoles(s)
}

// pauseActiveRedemption freezes the user's currently-active redemption: its
// remaining_seconds is reduced by the time elapsed since activated_at, then the
// row is marked inactive. No-op when the user has no active package. This is
// the automatic "pause" — there is no user-facing pause/resume control.
func pauseActiveRedemption(ctx context.Context, tx pgx.Tx, userID int) error {
	_, err := tx.Exec(ctx, `
		UPDATE voucher_redemptions
		SET remaining_seconds = GREATEST(
				remaining_seconds -
				COALESCE(EXTRACT(EPOCH FROM (now() - activated_at))::bigint, 0),
				0),
			activated_at = NULL,
			is_active = false
		WHERE user_id = $1 AND is_active`, userID)
	return err
}
