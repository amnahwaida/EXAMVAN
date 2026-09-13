package models

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ExamPengawas represents a junction table row linking exams to pengawas users.
type ExamPengawas struct {
	ID     int `json:"id"`
	ExamID int `json:"exam_id"`
	UserID int `json:"user_id"`
}

// PengawasAssignment represents a pengawas assignment with user details.
type PengawasAssignment struct {
	UserID   int    `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Instansi string `json:"instansi"`
}

// GetPengawasIDs returns the list of user IDs assigned as pengawas for an exam.
func GetPengawasIDs(ctx context.Context, pool *pgxpool.Pool, examID int) ([]int, error) {
	rows, err := pool.Query(ctx, `SELECT user_id FROM exam_pengawas WHERE exam_id = $1`, examID)
	if err != nil {
		return nil, fmt.Errorf("get pengawas ids: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan pengawas id: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	// Propagate, don't swallow: an interrupted iteration must not come back
	// as a PARTIAL list indistinguishable from "no pengawas" — callers use
	// this to populate the roster picker that REPLACES the whole roster on
	// save, so a silently partial list would drop existing pengawas on the
	// next save.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pengawas ids: %w", err)
	}

	if ids == nil {
		ids = []int{}
	}
	return ids, nil
}

// GetPengawasAssignments returns the list of pengawas with user details for an exam.
func GetPengawasAssignments(ctx context.Context, pool *pgxpool.Pool, examID int) ([]PengawasAssignment, error) {
	rows, err := pool.Query(ctx, `
		SELECT ep.user_id, u.username, u.name, COALESCE(u.instansi, '') as instansi
		FROM exam_pengawas ep
		JOIN admin_users u ON ep.user_id = u.id
		WHERE ep.exam_id = $1
		ORDER BY u.username`, examID)
	if err != nil {
		return nil, fmt.Errorf("get pengawas assignments: %w", err)
	}
	defer rows.Close()

	var assignments []PengawasAssignment
	for rows.Next() {
		var a PengawasAssignment
		if err := rows.Scan(&a.UserID, &a.Username, &a.Name, &a.Instansi); err != nil {
			return nil, fmt.Errorf("scan pengawas assignment: %w", err)
		}
		assignments = append(assignments, a)
	}
	rows.Close()
	// Propagate, don't swallow (same reason as GetPengawasIDs): a partial
	// list renders as "no assignment" instead of an error.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pengawas assignments: %w", err)
	}

	if assignments == nil {
		assignments = []PengawasAssignment{}
	}
	return assignments, nil
}

// CreateExamPengawas inserts a single exam_pengawas relationship.
// If the row already exists (UNIQUE constraint), it is silently ignored.
func CreateExamPengawas(ctx context.Context, pool *pgxpool.Pool, examID, userID int) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO exam_pengawas (exam_id, user_id) VALUES ($1, $2)
		 ON CONFLICT (exam_id, user_id) DO NOTHING`,
		examID, userID)
	if err != nil {
		return fmt.Errorf("create exam pengawas: %w", err)
	}
	return nil
}

// SetPengawasForExam replaces all pengawas assignments for an exam with the given user IDs.
// It deletes existing assignments and inserts the new ones in a single transaction.
func SetPengawasForExam(ctx context.Context, pool *pgxpool.Pool, examID int, userIDs []int) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("set pengawas: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `DELETE FROM exam_pengawas WHERE exam_id = $1`, examID)
	if err != nil {
		return fmt.Errorf("set pengawas: delete existing: %w", err)
	}

	for _, uid := range userIDs {
		_, err = tx.Exec(ctx,
			`INSERT INTO exam_pengawas (exam_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			examID, uid)
		if err != nil {
			return fmt.Errorf("set pengawas: insert user %d: %w", uid, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("set pengawas: commit: %w", err)
	}
	return nil
}
