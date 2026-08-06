package admin

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/examvan/webui/internal/models"
)

// packageEntitlement returns (exams, pdfBytes, concurrent, storageBytes, role)
// for a fixed package. `concurrent` is the max_concurrent_exams quota: the
// number of exams that may RUN simultaneously (mirrors the "Ujian Aktif"
// values advertised on the pricing page, so concurrent <= total exams).
func packageEntitlement(pkg string) (exams, pdf, concurrent, storage int64, role string) {
	switch pkg {
	case "guru":
		return 1, 10 * 1024 * 1024, 1, 100 * 1024 * 1024, ""
	case "individu":
		return 2, 30 * 1024 * 1024, 2, 300 * 1024 * 1024, ""
	case "sekolah_kecil":
		return 3, 50 * 1024 * 1024, 3, 500 * 1024 * 1024, models.SerializeRoles([]string{models.RoleOperator})
	case "sekolah_menengah":
		return 5, 200 * 1024 * 1024, 5, 2000 * 1024 * 1024, models.SerializeRoles([]string{models.RoleOperator})
	case "sekolah_besar":
		return 10, 500 * 1024 * 1024, 10, 5000 * 1024 * 1024, models.SerializeRoles([]string{models.RoleOperator})
	case "sekolah_unggulan":
		return 99999, 99999 * 1024 * 1024, 99999, 999999 * 1024 * 1024, models.SerializeRoles([]string{models.RoleOperator})
	default:
		return 1, 1 * 1024 * 1024, 1, 50 * 1024 * 1024, ""
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

// applyPackageEntitlement applies a fixed package's quota entitlement to a
// user, extending expiry to expiresAt. When role is non-empty it is MERGED into
// the user's existing roles (never stripping them); a SuperAdmin's role is never
// changed.
func applyPackageEntitlement(ctx context.Context, tx pgx.Tx, userID int, pkg, durationType string, expiresAt time.Time, role string) error {
	exams, pdf, concurrent, storage, newRole := packageEntitlement(pkg)
	if role == "" {
		role = newRole
	}

	// Load current roles so applying a package role neither demotes a
	// SuperAdmin nor strips existing functional roles (guru/pengawas).
	var currentRoleJSON string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(role, '') FROM admin_users WHERE id = $1`, userID).Scan(&currentRoleJSON); err != nil {
		return err
	}
	currentRoles := models.ParseRoles(currentRoleJSON)
	if containsRole(currentRoles, models.RoleSuperAdmin) {
		role = "" // never change a SuperAdmin's role
	}

	if role == "" {
		// Apply package/limits/expiry without touching the role column.
		_, err := tx.Exec(ctx, `UPDATE admin_users SET
			package = $1, max_exams = $2, max_pdf_size = $3,
			max_concurrent_exams = $4, max_storage_size = $5,
			expires_at = $6, status = 'active'
			WHERE id = $7`, pkg, exams, pdf, concurrent, storage, expiresAt, userID)
		return err
	}

	// Union the package role(s) with the existing roles.
	merged := append([]string{}, currentRoles...)
	for _, r := range models.ParseRoles(role) {
		if !containsRole(merged, r) {
			merged = append(merged, r)
		}
	}
	mergedJSON := models.SerializeRoles(merged)

	_, err := tx.Exec(ctx, `UPDATE admin_users SET
		package = $1, max_exams = $2, max_pdf_size = $3,
		max_concurrent_exams = $4, max_storage_size = $5,
		expires_at = $6, status = 'active', role = $7
		WHERE id = $8`, pkg, exams, pdf, concurrent, storage, expiresAt, mergedJSON, userID)
	return err
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

// applyCustomVoucherEntitlement applies a custom voucher's entitlement to a user
// by OVERWRITING their quota limits with the voucher's values and extending
// expiry to expiresAt. When the voucher specifies a custom role it is MERGED
// into the user's existing roles (never stripping them); a SuperAdmin's role is
// never changed. Package label defaults to "custom" when the voucher gives none.
func applyCustomVoucherEntitlement(ctx context.Context, tx pgx.Tx, userID int, v *models.Voucher, expiresAt time.Time) error {
	var currentRoleJSON string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(role, '') FROM admin_users WHERE id = $1`, userID).Scan(&currentRoleJSON); err != nil {
		return err
	}
	currentRoles := models.ParseRoles(currentRoleJSON)

	pkgLabel := strings.TrimSpace(v.CustomLabel)
	if pkgLabel == "" {
		pkgLabel = "custom"
	}

	role := strings.TrimSpace(v.CustomRole)

	// Custom vouchers created before the max_concurrent_exams column existed
	// carry a 0 in custom_max_concurrent_exams; fall back to the exam quota
	// (a sane ceiling: you cannot run more exams than you can create) rather
	// than silently granting unlimited concurrency.
	concurrent := v.CustomMaxConcurrentExams
	if concurrent <= 0 {
		concurrent = v.CustomMaxExams
	}
	if concurrent <= 0 {
		concurrent = 1
	}

	// Only change the role when the voucher specifies one AND the target is not
	// a SuperAdmin (whose role must never be altered by a voucher).
	if role != "" && role != models.RoleSuperAdmin && !containsRole(currentRoles, models.RoleSuperAdmin) {
		merged := append([]string{}, currentRoles...)
		if !containsRole(merged, role) {
			merged = append(merged, role)
		}
		_, err := tx.Exec(ctx, `UPDATE admin_users SET
			package = $1, max_exams = $2, max_pdf_size = $3,
			max_concurrent_exams = $4, max_storage_size = $5, expires_at = $6,
			status = 'active', role = $7
			WHERE id = $8`,
			pkgLabel, v.CustomMaxExams, v.CustomMaxPDFSize,
			concurrent, v.CustomMaxStorageSize, expiresAt,
			models.SerializeRoles(merged), userID)
		return err
	}

	_, err := tx.Exec(ctx, `UPDATE admin_users SET
		package = $1, max_exams = $2, max_pdf_size = $3,
		max_concurrent_exams = $4, max_storage_size = $5, expires_at = $6, status = 'active'
		WHERE id = $7`,
		pkgLabel, v.CustomMaxExams, v.CustomMaxPDFSize,
		concurrent, v.CustomMaxStorageSize, expiresAt, userID)
	return err
}