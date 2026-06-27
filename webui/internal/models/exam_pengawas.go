package models

import (
	"context"
	"fmt"
	"log"

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
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}

	if ids == nil {
		ids = []int{}
	}
	return ids, nil
}

// GetPengawasAssignments returns the list of pengawas with user details for an exam.
func GetPengawasAssignments(ctx context.Context, pool *pgxpool.Pool, examID int) ([]PengawasAssignment, error) {
	rows, err := pool.Query(ctx, `
		SELECT ep.user_id, u.username, COALESCE(u.instansi, '') as instansi
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
		if err := rows.Scan(&a.UserID, &a.Username, &a.Instansi); err != nil {
			return nil, fmt.Errorf("scan pengawas assignment: %w", err)
		}
		assignments = append(assignments, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
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

// DeleteExamPengawas removes a single exam_pengawas relationship.
func DeleteExamPengawas(ctx context.Context, pool *pgxpool.Pool, examID, userID int) error {
	_, err := pool.Exec(ctx,
		`DELETE FROM exam_pengawas WHERE exam_id = $1 AND user_id = $2`,
		examID, userID)
	if err != nil {
		return fmt.Errorf("delete exam pengawas: %w", err)
	}
	return nil
}

// DeletePengawasByExam removes all pengawas assignments for a given exam.
func DeletePengawasByExam(ctx context.Context, pool *pgxpool.Pool, examID int) error {
	_, err := pool.Exec(ctx, `DELETE FROM exam_pengawas WHERE exam_id = $1`, examID)
	if err != nil {
		return fmt.Errorf("delete pengawas by exam: %w", err)
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

// GetExamsForUser returns all exam IDs that a user is assigned to as pengawas.
func GetExamsForUser(ctx context.Context, pool *pgxpool.Pool, userID int) ([]int, error) {
	rows, err := pool.Query(ctx,
		`SELECT exam_id FROM exam_pengawas WHERE user_id = $1 ORDER BY exam_id`, userID)
	if err != nil {
		return nil, fmt.Errorf("get exams for user: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan exam id: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}

	if ids == nil {
		ids = []int{}
	}
	return ids, nil
}

// IsUserAssignedAsPengawas checks whether a user is assigned as pengawas for a specific exam.
func IsUserAssignedAsPengawas(ctx context.Context, pool *pgxpool.Pool, examID, userID int) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM exam_pengawas WHERE exam_id = $1 AND user_id = $2)`,
		examID, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check pengawas assignment: %w", err)
	}
	return exists, nil
}

// IsExamAssignedToUser checks if a given user has any pengawas assignment for a specific exam.
// Same as IsUserAssignedAsPengawas but returns the ID if found.
func IsExamAssignedToUser(ctx context.Context, pool *pgxpool.Pool, examID, userID int) (bool, error) {
	return IsUserAssignedAsPengawas(ctx, pool, examID, userID)
}

// GetPengawasCount returns the number of pengawas assigned to an exam.
func GetPengawasCount(ctx context.Context, pool *pgxpool.Pool, examID int) (int, error) {
	var count int
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM exam_pengawas WHERE exam_id = $1`, examID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("get pengawas count: %w", err)
	}
	return count, nil
}
