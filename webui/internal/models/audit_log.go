package models

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AdminAuditLog is a single immutable record of a sensitive admin action:
// who did what, on which exam, and when. Rows are append-only — the table is
// never updated or deleted by application code — so the trail stays
// trustworthy for accountability questions ("who enabled auto-approve?").
// user_id/exam_id use ON DELETE SET NULL in the schema; username and detail
// keep denormalized snapshots so the row stays readable after the actor or
// exam is deleted.
type AdminAuditLog struct {
	ID        int
	UserID    *int
	Username  string
	Action    string
	ExamID    *int
	Detail    string
	CreatedAt time.Time
}

// Audit action identifiers. Stored in admin_audit_logs.action and exposed to
// the admin API so the UI can phrase the "last changed" hint per action.
const (
	ActionAutoApproveEnable  = "auto_approve_enable"
	ActionAutoApproveDisable = "auto_approve_disable"
	// Per-device approval decisions made by a pengawas on the monitoring
	// page (SetApprovalStatus). detail carries the device identity
	// (MAC + student) so the history panel stays readable without a join.
	ActionApprovalApproved = "approval_approved"
	ActionApprovalRejected = "approval_rejected"
	// An exam's PDF was replaced through EditExam (a new file uploaded, the
	// old one removed). detail carries "old -> new" file names so the trail
	// stays readable without a join.
	ActionExamPDFReplaced = "exam_pdf_replaced"
	// An exam was deleted (DeleteExam / BulkDelete). The row is written BEFORE
	// the exam row is removed: exam_id then becomes NULL via the FK's
	// ON DELETE SET NULL, and detail keeps the exam's name snapshot — exactly
	// the survival path the schema comment describes, so the trail documents
	// the deletion even after the exam is gone.
	ActionExamDeleted = "exam_deleted"
	// An exam was created (UploadExam). Written AFTER the row committed (the
	// exam_id FK targets the new row), opening the lifecycle trail: created →
	// pdf_replaced → deleted.
	ActionExamCreated = "exam_created"
	// An admin/operator account was created through the management panel
	// (CreateUser — the Tambah User form; self-registration /register does NOT
	// go through this handler and is not audited here). Written after the row
	// committed; detail snapshots username/name/role of the NEW account, and
	// the audit's exam_id is NULL (no exam involved).
	ActionUserCreated = "user_created"
	// An existing account was edited through the management panel (EditUser —
	// the Atur Limit modal). Written after the update committed; detail lists
	// which fields were changed. exam_id is NULL.
	ActionUserEdited = "user_edited"
	// An account was deleted (DeleteUser — the Hapus action on the Kelola
	// Users page). Written BEFORE the rows are removed so user_id survives via
	// the FK's ON DELETE SET NULL with the username/role snapshotted in
	// detail; when the target is an operator with a real instansi, one row is
	// written for EVERY account the cascade removes (the sub-accounts sharing
	// the instansi) so the school-wide wipe leaves a complete trail. exam_id
	// is NULL (no exam involved).
	ActionUserDeleted = "user_deleted"
	// A voucher code was claimed (RedeemVoucherHandler — the billing page's
	// "Klaim" action). Written after the redemption committed; detail
	// snapshots the code, the resulting package and the account expiry, so
	// "who claimed which voucher and when" is answerable from the same
	// append-only trail as the exam/user actions. exam_id is NULL (no exam
	// involved).
	ActionVoucherRedeemed = "voucher_redeemed"
	// A previously-claimed package was made the active one
	// (ActivateVoucherHandler — the "Aktifkan" button on the claimed list).
	// Written after the activation committed; detail snapshots the package
	// and the new account expiry. exam_id is NULL.
	ActionVoucherActivated = "voucher_activated"
	// An admin manually deactivated the account's ACTIVE package
	// (DeactivateUserPackage — the "Nonaktifkan Paket" action on the Kelola
	// User page, SuperAdmin only). The active redemption is burned and the
	// account either falls back to the best remaining claimed package or
	// reverts to the free trial. Written after the deactivation committed;
	// detail snapshots the deactivated package and the resulting state.
	// exam_id is NULL.
	ActionVoucherDeactivated = "voucher_deactivated"
)

// CreateAdminAuditLog appends one audit row. username/detail are snapshotted
// at write time, so the row survives later account renames or deletion.
// examID is the target exam when the action is exam-scoped; pass 0 for
// actions without an exam (e.g. user_created/user_edited) — the row stores
// NULL in exam_id via NULLIF, so a 0 never collides with the FK.
// Failures are non-fatal to the caller's main flow (the handler logs them),
// which is why this returns only an error and no context of its own.
func CreateAdminAuditLog(ctx context.Context, pool *pgxpool.Pool, userID int, username, action string, examID int, detail string) error {
	if _, err := pool.Exec(ctx, `
		INSERT INTO admin_audit_logs (user_id, username, action, exam_id, detail)
		VALUES ($1, $2, $3, NULLIF($4::int, 0), $5)`,
		userID, username, action, examID, detail); err != nil {
		return fmt.Errorf("create admin audit log: %w", err)
	}
	return nil
}

// LatestExamAuditLog returns the most recent audit row for an exam, or nil
// when the exam has no audit trail yet. Used by the auto-approve endpoint to
// tell the monitoring page who last toggled the flag and when. Ties are
// broken by row id so two toggles in the same microsecond still resolve to
// the later one.
func LatestExamAuditLog(ctx context.Context, pool *pgxpool.Pool, examID int) (*AdminAuditLog, error) {
	row := pool.QueryRow(ctx, `
		SELECT id, user_id, username, action, exam_id, detail, created_at
		FROM admin_audit_logs
		WHERE exam_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, examID)

	var l AdminAuditLog
	if err := row.Scan(&l.ID, &l.UserID, &l.Username, &l.Action, &l.ExamID, &l.Detail, &l.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("latest exam audit log: %w", err)
	}
	return &l, nil
}

// ListExamAuditLogs returns the full append-only admin audit trail for an
// exam, newest first — the complete history behind the single "last changed"
// hint so the monitoring page can render a full accountability panel.
// limit caps the payload (the UI shows a bounded window); ties are broken by
// row id so two toggles in the same microsecond still resolve to a stable
// order. An exam with no trail yet yields an empty, non-nil slice.
func ListExamAuditLogs(ctx context.Context, pool *pgxpool.Pool, examID, limit int) ([]AdminAuditLog, error) {
	if limit < 1 {
		limit = 100
	}
	rows, err := pool.Query(ctx, `
		SELECT id, user_id, username, action, exam_id, detail, created_at
		FROM admin_audit_logs
		WHERE exam_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2`, examID, limit)
	if err != nil {
		return nil, fmt.Errorf("list exam audit logs: %w", err)
	}
	defer rows.Close()

	logs := make([]AdminAuditLog, 0, 16)
	for rows.Next() {
		var l AdminAuditLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.Username, &l.Action, &l.ExamID, &l.Detail, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan exam audit log: %w", err)
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate exam audit logs: %w", err)
	}
	return logs, nil
}

// ListVoucherAuditLogsOpts carries the pagination/search window for the
// SuperAdmin voucher-claim audit trail (ListVoucherAuditLogs). Search matches
// the actor username or the detail snapshot (e.g. a voucher code).
type ListVoucherAuditLogsOpts struct {
	Page    int
	PerPage int
	Search  string
}

// ListVoucherAuditLogsResult is the paginated result of the voucher-claim
// audit trail query, mirroring the ListVouchers result shape so the admin API
// and the page can reuse the same rendering conventions.
type ListVoucherAuditLogsResult struct {
	Logs       []AdminAuditLog
	Total      int
	Page       int
	PerPage    int
	TotalPages int
}

// ListVoucherAuditLogs returns the global append-only trail of voucher
// claims and activations (actions voucher_redeemed / voucher_activated),
// newest first — the SuperAdmin accountability view for "who claimed which
// voucher and when" across all accounts. Unlike ListExamAuditLogs this is a
// global, paginated, searchable query (the SuperAdmin page shows a bounded
// window with paging rather than one capped dump). Rows with exam_id are
// excluded by construction: voucher actions are never exam-scoped. Ties are
// broken by row id so two writes in the same microsecond resolve to a stable
// order. An empty trail yields an empty, non-nil Logs slice.
func ListVoucherAuditLogs(ctx context.Context, pool *pgxpool.Pool, opts ListVoucherAuditLogsOpts) (*ListVoucherAuditLogsResult, error) {
	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.PerPage < 1 {
		opts.PerPage = 20
	}
	offset := (opts.Page - 1) * opts.PerPage

	whereClause := ` WHERE action IN ('voucher_redeemed', 'voucher_activated', 'voucher_deactivated')`
	var args []interface{}
	argIdx := 1

	if opts.Search != "" {
		whereClause += fmt.Sprintf(" AND (username ILIKE $%d OR detail ILIKE $%d)", argIdx, argIdx)
		args = append(args, "%"+opts.Search+"%")
		argIdx++
	}

	countSQL := `SELECT COUNT(*) FROM admin_audit_logs` + whereClause
	var total int
	if err := pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count voucher audit logs: %w", err)
	}

	totalPages := (total + opts.PerPage - 1) / opts.PerPage
	if totalPages == 0 {
		totalPages = 1
	}

	querySQL := fmt.Sprintf(`
		SELECT id, user_id, username, action, exam_id, detail, created_at
		FROM admin_audit_logs
		%s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d OFFSET $%d`, whereClause, argIdx, argIdx+1)

	args = append(args, opts.PerPage, offset)

	rows, err := pool.Query(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("query voucher audit logs: %w", err)
	}
	defer rows.Close()

	logs := make([]AdminAuditLog, 0, 16)
	for rows.Next() {
		var l AdminAuditLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.Username, &l.Action, &l.ExamID, &l.Detail, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan voucher audit log: %w", err)
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate voucher audit logs: %w", err)
	}
	if logs == nil {
		logs = []AdminAuditLog{}
	}

	return &ListVoucherAuditLogsResult{
		Logs:       logs,
		Total:      total,
		Page:       opts.Page,
		PerPage:    opts.PerPage,
		TotalPages: totalPages,
	}, nil
}
