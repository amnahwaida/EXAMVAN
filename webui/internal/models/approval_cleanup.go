package models

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ApprovalCleanupStats reports how many stale exam_approvals rows each purge
// rule removed in one pass (used for job logging).
type ApprovalCleanupStats struct {
	PendingEnded     int64
	ApprovedEnded    int64
	PendingInactive  int64
	ApprovedInactive int64
}

// Total returns the number of rows purged across all rules.
func (s ApprovalCleanupStats) Total() int64 {
	return s.PendingEnded + s.ApprovedEnded + s.PendingInactive + s.ApprovedInactive
}

// PurgeStaleExamApprovals deletes stale pending/approved approval rows so a
// spam-flooded queue or a reused exam cannot grow without bound. Two
// staleness rules, both deliberately conservative:
//
//  1. Rows on exams whose end_time has passed by endedGraceHours — after the
//     schedule ends, no device can join, poll, or submit anymore (the API
//     rejects past end_time with a short grace window), so a pending row is a
//     dead request and an approved row is a device that was approved but never
//     submitted. Removing the approved rows also frees their slots in the
//     per-exam approved-device cap (max_approvals_per_exam) when the exam is
//     reused for the next session.
//  2. Rows on exams that are INACTIVE (stopped or never started) and older
//     than inactiveTTLHours — the teacher had time to restart a stopped exam
//     (devices still queueing keep their rows meanwhile); a row this old on a
//     dormant exam is garbage and only grows the table.
//
// Rejected rows are NEVER touched — they are explicit pengawas decisions, and
// deleting one would silently re-queue a rejected device on its next poll.
// Submissions and access logs are never touched either: they are the durable
// student record behind the monitoring page and the public results page.
//
// The three DELETEs are independent (no transaction): a crash mid-pass simply
// means the next pass finishes the job.
//
// Access strategy: each DELETE joins from exams into exam_approvals by
// exam_id (UNIQUE(exam_id, mac_address) leading column), so the work is
// proportional to the rows actually removed. The ended-exam rules scan exams
// by end_time < now() - grace; the partial index idx_exams_end_time
// (exams(end_time) WHERE end_time IS NOT NULL, schema.sql) keeps that scan
// bounded once exams accumulate.
func PurgeStaleExamApprovals(ctx context.Context, pool *pgxpool.Pool, endedGraceHours, inactiveTTLHours int) (ApprovalCleanupStats, error) {
	var stats ApprovalCleanupStats

	// Rule 1 — pending rows on exams that have ended. A pending row on a dead
	// exam can never be decided (nobody monitors it), so it is pure garbage.
	if tag, err := pool.Exec(ctx, `
		DELETE FROM exam_approvals a
		USING exams e
		WHERE a.exam_id = e.id
		  AND a.status = 'pending'
		  AND e.end_time IS NOT NULL
		  AND e.end_time < now() - ($1 * interval '1 hour')`,
		endedGraceHours); err != nil {
		return stats, fmt.Errorf("purge pending-on-ended approvals: %w", err)
	} else {
		stats.PendingEnded = tag.RowsAffected()
	}

	// Rule 2 — approved rows on exams that have ended. Devices approved but
	// never submitted hold a cap slot (and their polling is impossible once
	// the schedule ended), so freeing them keeps a reused exam's cap honest.
	if tag, err := pool.Exec(ctx, `
		DELETE FROM exam_approvals a
		USING exams e
		WHERE a.exam_id = e.id
		  AND a.status = 'approved'
		  AND e.end_time IS NOT NULL
		  AND e.end_time < now() - ($1 * interval '1 hour')`,
		endedGraceHours); err != nil {
		return stats, fmt.Errorf("purge approved-on-ended approvals: %w", err)
	} else {
		stats.ApprovedEnded = tag.RowsAffected()
	}

	// Rule 3 — pending rows on exams that have been INACTIVE for a while.
	// Covers exams stopped manually and never restarted (and any other dormant
	// exam); rejected rows are excluded, see the doc comment above.
	if tag, err := pool.Exec(ctx, `
		DELETE FROM exam_approvals a
		USING exams e
		WHERE a.exam_id = e.id
		  AND a.status = 'pending'
		  AND e.status = 'inactive'
		  AND a.created_at < now() - ($1 * interval '1 hour')`,
		inactiveTTLHours); err != nil {
		return stats, fmt.Errorf("purge pending-on-inactive approvals: %w", err)
	} else {
		stats.PendingInactive = tag.RowsAffected()
	}

	// Rule 3b — approved rows on exams that have been INACTIVE for a while.
	if tag, err := pool.Exec(ctx, `
		DELETE FROM exam_approvals a
		USING exams e
		WHERE a.exam_id = e.id
		  AND a.status = 'approved'
		  AND e.status = 'inactive'
		  AND a.created_at < now() - ($1 * interval '1 hour')`,
		inactiveTTLHours); err != nil {
		return stats, fmt.Errorf("purge approved-on-inactive approvals: %w", err)
	} else {
		stats.ApprovedInactive = tag.RowsAffected()
	}

	return stats, nil
}
