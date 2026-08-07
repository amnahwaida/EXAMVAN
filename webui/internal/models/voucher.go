package models

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Voucher struct {
	ID           int        `json:"id"`
	Code         string     `json:"code"`
	Package      string     `json:"package"`
	DurationType string     `json:"duration_type"`
	MaxUsage     int        `json:"max_usage"`
	UsedCount    int        `json:"used_count"`
	ExpiresAt    *time.Time `json:"expires_at"`
	IsActive     bool       `json:"is_active"`
	Notes        string     `json:"notes"`
	CreatedByID  *int       `json:"created_by_id,omitempty"`
	CreatedBy    string     `json:"created_by,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`

	// Custom entitlement (used only when IsCustom is true). Sizes are in bytes.
	IsCustom                 bool   `json:"is_custom"`
	CustomLabel              string `json:"custom_label"`
	CustomMaxExams           int    `json:"custom_max_exams"`
	CustomMaxPDFSize         int64  `json:"custom_max_pdf_size"`
	CustomMaxConcurrentExams int    `json:"custom_max_concurrent_exams"`
	CustomMaxStorageSize     int64  `json:"custom_max_storage_size"`
	CustomMaxUsers           int64  `json:"custom_max_users"` // sub-account quota (0 = unlimited)
	CustomRole               string `json:"custom_role"`
}

type VoucherRedemption struct {
	ID         int       `json:"id"`
	VoucherID  int       `json:"voucher_id"`
	UserID     int       `json:"user_id"`
	Username   string    `json:"username,omitempty"`
	RedeemedAt time.Time `json:"redeemed_at"`

	// Per-redemption lifetime + entitlement snapshot. Since a user may hold
	// several claimed vouchers and choose which one is active, each redemption
	// keeps its own remaining lifetime and its own quota snapshot (independent
	// of the source voucher row). Only the ACTIVE package consumes lifetime:
	// remaining_seconds shrinks while is_active (from activated_at onwards);
	// inactive packages are paused and resume automatically when activated.
	RemainingSeconds   int64      `json:"remaining_seconds"`
	ActivatedAt        *time.Time `json:"activated_at"`
	IsActive           bool       `json:"is_active"`
	Package            string     `json:"package"`
	Code               string     `json:"code,omitempty"`
	MaxExams           int64      `json:"max_exams"`
	MaxPDFSize         int64      `json:"max_pdf_size"`
	MaxConcurrentExams int64      `json:"max_concurrent_exams"`
	MaxStorageSize     int64      `json:"max_storage_size"`
	MaxUsers           int64      `json:"max_users"` // sub-account quota (0 = unlimited)
	Role               string     `json:"role"`
}

// GenerateRandomVoucherCode generates a random code formatted like PROMO-XXXX-XXXX
func GenerateRandomVoucherCode(prefix string) string {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // avoid confusing characters (O,0,1,I)
	b := make([]byte, 8)
	for i := range b {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			b[i] = charset[i%len(charset)]
		} else {
			b[i] = charset[num.Int64()]
		}
	}
	codePart := string(b[:4]) + "-" + string(b[4:])
	if prefix != "" {
		prefix = strings.ToUpper(strings.TrimSpace(prefix))
		return prefix + "-" + codePart
	}
	return "EV-" + codePart
}

// CreateVoucher creates a single voucher code.
func CreateVoucher(ctx context.Context, pool *pgxpool.Pool, v *Voucher) (*Voucher, error) {
	v.Code = strings.ToUpper(strings.TrimSpace(v.Code))
	if v.Code == "" {
		v.Code = GenerateRandomVoucherCode("")
	}
	if v.MaxUsage <= 0 {
		v.MaxUsage = 1
	}

	sql := `INSERT INTO vouchers
		(code, package, duration_type, max_usage, expires_at, is_active, notes, created_by,
		 is_custom, custom_label, custom_max_exams, custom_max_pdf_size,
		 custom_max_concurrent_exams, custom_max_storage_size, custom_max_users, custom_role)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		RETURNING id, used_count, created_at, updated_at`

	err := pool.QueryRow(ctx, sql,
		v.Code, v.Package, v.DurationType, v.MaxUsage, v.ExpiresAt, v.IsActive, v.Notes, v.CreatedByID,
		v.IsCustom, v.CustomLabel, v.CustomMaxExams, v.CustomMaxPDFSize,
		v.CustomMaxConcurrentExams, v.CustomMaxStorageSize, v.CustomMaxUsers, v.CustomRole,
	).Scan(&v.ID, &v.UsedCount, &v.CreatedAt, &v.UpdatedAt)

	if err != nil {
		return nil, fmt.Errorf("create voucher: %w", err)
	}

	return v, nil
}

// CreateBatchVouchers generates multiple unique vouchers at once, all sharing
// the given template's package/duration/usage/expiry/notes and (if set) custom
// entitlement. Only the code is randomized per voucher.
func CreateBatchVouchers(ctx context.Context, pool *pgxpool.Pool, prefix string, count int, tmpl Voucher) ([]Voucher, error) {
	if count <= 0 {
		count = 1
	}
	if count > 100 {
		count = 100 // Cap at 100 per batch
	}

	var created []Voucher
	for i := 0; i < count; i++ {
		v := tmpl // copy template
		v.Code = GenerateRandomVoucherCode(prefix)
		v.IsActive = true

		res, err := CreateVoucher(ctx, pool, &v)
		if err != nil {
			// Retry once on collision
			v.Code = GenerateRandomVoucherCode(prefix)
			res, err = CreateVoucher(ctx, pool, &v)
			if err != nil {
				continue
			}
		}
		created = append(created, *res)
	}

	return created, nil
}

// GetVoucherByCode fetches a voucher by its code.
func GetVoucherByCode(ctx context.Context, pool *pgxpool.Pool, code string) (*Voucher, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	sql := `SELECT v.id, v.code, v.package, v.duration_type, v.max_usage, v.used_count,
	               v.expires_at, v.is_active, v.notes, v.created_by, COALESCE(u.username, ''),
	               v.created_at, v.updated_at,
	               v.is_custom, COALESCE(v.custom_label, ''), COALESCE(v.custom_max_exams, 0),
	               COALESCE(v.custom_max_pdf_size, 0),
	               COALESCE(v.custom_max_concurrent_exams, 0),
	               COALESCE(v.custom_max_storage_size, 0),
	               COALESCE(v.custom_max_users, 0),
	               COALESCE(v.custom_role, '')
	        FROM vouchers v
	        LEFT JOIN admin_users u ON v.created_by = u.id
	        WHERE v.code = $1`

	var v Voucher
	err := pool.QueryRow(ctx, sql, code).Scan(
		&v.ID, &v.Code, &v.Package, &v.DurationType, &v.MaxUsage, &v.UsedCount,
		&v.ExpiresAt, &v.IsActive, &v.Notes, &v.CreatedByID, &v.CreatedBy,
		&v.CreatedAt, &v.UpdatedAt,
		&v.IsCustom, &v.CustomLabel, &v.CustomMaxExams, &v.CustomMaxPDFSize,
		&v.CustomMaxConcurrentExams,
		&v.CustomMaxStorageSize, &v.CustomMaxUsers, &v.CustomRole,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &v, nil
}

type ListVouchersOpts struct {
	Page    int
	PerPage int
	Search  string
}

type ListVouchersResult struct {
	Vouchers   []Voucher
	Total      int
	Page       int
	PerPage    int
	TotalPages int
}

// ListVouchers returns paginated vouchers for SuperAdmin.
func ListVouchers(ctx context.Context, pool *pgxpool.Pool, opts ListVouchersOpts) (*ListVouchersResult, error) {
	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.PerPage < 1 {
		opts.PerPage = 20
	}
	offset := (opts.Page - 1) * opts.PerPage

	whereClause := " WHERE 1=1"
	var args []interface{}
	argIdx := 1

	if opts.Search != "" {
		whereClause += fmt.Sprintf(" AND (v.code ILIKE $%d OR v.notes ILIKE $%d OR v.package ILIKE $%d)", argIdx, argIdx, argIdx)
		args = append(args, "%"+opts.Search+"%")
		argIdx++
	}

	countSQL := `SELECT COUNT(*) FROM vouchers v` + whereClause
	var total int
	if err := pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count vouchers: %w", err)
	}

	totalPages := (total + opts.PerPage - 1) / opts.PerPage
	if totalPages == 0 {
		totalPages = 1
	}

	querySQL := fmt.Sprintf(`
		SELECT v.id, v.code, v.package, v.duration_type, v.max_usage, v.used_count,
		       v.expires_at, v.is_active, v.notes, v.created_by, COALESCE(u.username, ''),
		       v.created_at, v.updated_at,
		       v.is_custom, COALESCE(v.custom_label, ''), COALESCE(v.custom_max_exams, 0),
		       COALESCE(v.custom_max_pdf_size, 0),
		       COALESCE(v.custom_max_concurrent_exams, 0),
		       COALESCE(v.custom_max_storage_size, 0),
		       COALESCE(v.custom_max_users, 0),
		       COALESCE(v.custom_role, '')
		FROM vouchers v
		LEFT JOIN admin_users u ON v.created_by = u.id
		%s
		ORDER BY v.created_at DESC, v.id DESC
		LIMIT $%d OFFSET $%d`, whereClause, argIdx, argIdx+1)

	args = append(args, opts.PerPage, offset)

	rows, err := pool.Query(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("query vouchers: %w", err)
	}
	defer rows.Close()

	var vouchers []Voucher
	for rows.Next() {
		var v Voucher
		if err := rows.Scan(
			&v.ID, &v.Code, &v.Package, &v.DurationType, &v.MaxUsage, &v.UsedCount,
			&v.ExpiresAt, &v.IsActive, &v.Notes, &v.CreatedByID, &v.CreatedBy,
			&v.CreatedAt, &v.UpdatedAt,
			&v.IsCustom, &v.CustomLabel, &v.CustomMaxExams, &v.CustomMaxPDFSize,
			&v.CustomMaxConcurrentExams,
			&v.CustomMaxStorageSize, &v.CustomMaxUsers, &v.CustomRole,
		); err != nil {
			return nil, fmt.Errorf("scan voucher: %w", err)
		}
		vouchers = append(vouchers, v)
	}

	if vouchers == nil {
		vouchers = []Voucher{}
	}

	return &ListVouchersResult{
		Vouchers:   vouchers,
		Total:      total,
		Page:       opts.Page,
		PerPage:    opts.PerPage,
		TotalPages: totalPages,
	}, nil
}

// ToggleVoucherStatus enables or disables a voucher.
func ToggleVoucherStatus(ctx context.Context, pool *pgxpool.Pool, id int) (bool, error) {
	var newStatus bool
	err := pool.QueryRow(ctx, `
		UPDATE vouchers
		SET is_active = NOT is_active, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING is_active`, id).Scan(&newStatus)
	if err != nil {
		return false, fmt.Errorf("toggle voucher status: %w", err)
	}
	return newStatus, nil
}

// DeleteVoucher deletes a voucher.
func DeleteVoucher(ctx context.Context, pool *pgxpool.Pool, id int) error {
	_, err := pool.Exec(ctx, `DELETE FROM vouchers WHERE id = $1`, id)
	return err
}

// ListVoucherRedemptions lists all users who redeemed a specific voucher.
func ListVoucherRedemptions(ctx context.Context, pool *pgxpool.Pool, voucherID int) ([]VoucherRedemption, error) {
	sql := `SELECT r.id, r.voucher_id, r.user_id, u.username, r.redeemed_at
	        FROM voucher_redemptions r
	        JOIN admin_users u ON r.user_id = u.id
	        WHERE r.voucher_id = $1
	        ORDER BY r.redeemed_at DESC`

	rows, err := pool.Query(ctx, sql, voucherID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var redemptions []VoucherRedemption
	for rows.Next() {
		var r VoucherRedemption
		if err := rows.Scan(&r.ID, &r.VoucherID, &r.UserID, &r.Username, &r.RedeemedAt); err != nil {
			return nil, err
		}
		redemptions = append(redemptions, r)
	}
	if redemptions == nil {
		redemptions = []VoucherRedemption{}
	}
	return redemptions, nil
}

// SyncActiveRedemptionToExpiry realigns the user's currently-active
// redemption clock with the account's expires_at (the authoritative expiry
// used to gate login): the active package's remaining lifetime is rewritten to
// `newExpiry - now` and its clock restarts now. Called whenever an admin
// manually changes an account's expiry (renewal, user edit, instansi-wide
// cascade) so the billing display and later pause computations never disagree
// with the account state. No-op when the user has no active redemption.
func SyncActiveRedemptionToExpiry(ctx context.Context, pool *pgxpool.Pool, userID int, newExpiry time.Time) error {
	_, err := pool.Exec(ctx, `
		UPDATE voucher_redemptions
		SET remaining_seconds = GREATEST(EXTRACT(EPOCH FROM ($1 - now()))::bigint, 0),
		    activated_at = now()
		WHERE user_id = $2 AND is_active`, newExpiry, userID)
	if err != nil {
		return fmt.Errorf("sync active redemption expiry: %w", err)
	}
	return nil
}

// ListMyRedemptions returns the currently-logged-in user's claimed vouchers
// (with their entitlement snapshots and the source voucher code), most recent
// first. Expired ones are included so the UI can mark them, but the handler
// may filter.
func ListMyRedemptions(ctx context.Context, pool *pgxpool.Pool, userID int) ([]VoucherRedemption, error) {
	rows, err := pool.Query(ctx, `
		SELECT r.id, r.voucher_id, r.user_id, r.redeemed_at,
		       r.remaining_seconds, r.activated_at, r.is_active, COALESCE(r.package, ''),
		       COALESCE(v.code, ''),
		       r.max_exams, r.max_pdf_size, r.max_concurrent_exams,
		       r.max_storage_size, r.max_users, COALESCE(r.role, '')
		FROM voucher_redemptions r
		LEFT JOIN vouchers v ON r.voucher_id = v.id
		WHERE r.user_id = $1
		ORDER BY r.redeemed_at DESC, r.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var redemptions []VoucherRedemption
	for rows.Next() {
		var r VoucherRedemption
		if err := rows.Scan(
			&r.ID, &r.VoucherID, &r.UserID, &r.RedeemedAt,
			&r.RemainingSeconds, &r.ActivatedAt, &r.IsActive, &r.Package, &r.Code,
			&r.MaxExams, &r.MaxPDFSize, &r.MaxConcurrentExams,
			&r.MaxStorageSize, &r.MaxUsers, &r.Role,
		); err != nil {
			return nil, err
		}
		redemptions = append(redemptions, r)
	}
	if redemptions == nil {
		redemptions = []VoucherRedemption{}
	}
	return redemptions, nil
}
