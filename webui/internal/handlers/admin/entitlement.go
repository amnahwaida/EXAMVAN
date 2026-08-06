package admin

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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
// expiry to the redemption's own lifetime, and MERGES the snapshot role into
// the existing roles (never stripping them, never touching a SuperAdmin).
// Package column becomes the snapshot's package label.
func applyRedemptionEntitlement(ctx context.Context, tx pgx.Tx, userID int, r *models.VoucherRedemption) error {
	if r.ExpiresAt == nil {
		return fmt.Errorf("redemption has no expiry")
	}

	var currentRoleJSON string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(role, '') FROM admin_users WHERE id = $1`, userID).Scan(&currentRoleJSON); err != nil {
		return err
	}
	currentRoles := models.ParseRoles(currentRoleJSON)

	role := strings.TrimSpace(r.Role)
	if containsRole(currentRoles, models.RoleSuperAdmin) {
		role = "" // never change a SuperAdmin's role
	}

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

	if role == "" {
		_, err := tx.Exec(ctx, `UPDATE admin_users SET
			package = $1, max_exams = $2, max_pdf_size = $3,
			max_concurrent_exams = $4, max_storage_size = $5,
			expires_at = $6, status = 'active'
			WHERE id = $7`,
			pkgLabel, r.MaxExams, r.MaxPDFSize, concurrent, r.MaxStorageSize, *r.ExpiresAt, userID)
		return err
	}

	// Union the snapshot role(s) with the existing roles.
	merged := append([]string{}, currentRoles...)
	for _, rr := range models.ParseRoles(role) {
		if !containsRole(merged, rr) {
			merged = append(merged, rr)
		}
	}

	_, err := tx.Exec(ctx, `UPDATE admin_users SET
		package = $1, max_exams = $2, max_pdf_size = $3,
		max_concurrent_exams = $4, max_storage_size = $5,
		expires_at = $6, status = 'active', role = $7
		WHERE id = $8`,
		pkgLabel, r.MaxExams, r.MaxPDFSize, concurrent, r.MaxStorageSize, *r.ExpiresAt,
		models.SerializeRoles(merged), userID)
	return err
}

// applyCustomVoucherEntitlement applies a custom voucher's entitlement to a user
// by OVERWRITING their quota limits with the voucher's values and extending
// expiry to expiresAt. When the voucher specifies a custom role it is MERGED
// into the user's existing roles (never stripping them); a SuperAdmin's role is
