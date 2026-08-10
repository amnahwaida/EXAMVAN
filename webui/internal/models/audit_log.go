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
)

// CreateAdminAuditLog appends one audit row. username/detail are snapshotted
// at write time, so the row survives later account renames or deletion.
// Failures are non-fatal to the caller's main flow (the handler logs them),
// which is why this returns only an error and no context of its own.
func CreateAdminAuditLog(ctx context.Context, pool *pgxpool.Pool, userID int, username, action string, examID int, detail string) error {
	if _, err := pool.Exec(ctx, `
		INSERT INTO admin_audit_logs (user_id, username, action, exam_id, detail)
		VALUES ($1, $2, $3, $4, $5)`,
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
