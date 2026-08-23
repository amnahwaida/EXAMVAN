package models

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StudentAccessLog represents a row from the student_access_logs table.
type StudentAccessLog struct {
	ID                int       `json:"id"`
	ExamID            int       `json:"exam_id"`
	SubmissionID      *int      `json:"submission_id,omitempty"`
	StudentIdentifier string    `json:"student_identifier"`
	StudentName       *string   `json:"student_name,omitempty"`
	ExamNumber        *string   `json:"exam_number,omitempty"`
	StudentClass      *string   `json:"student_class,omitempty"`
	Event             string    `json:"event"` // login, heartbeat, logout
	IPAddress         string    `json:"ip_address"`
	DeviceInfo        string    `json:"device_info"`
	CreatedAt         time.Time `json:"created_at"`
	IdentityData      *string   `json:"identity_data,omitempty"`
}

// AccessLogEvent constants.
const (
	AccessEventLogin     = "login"
	AccessEventHeartbeat = "heartbeat"
	AccessEventLogout    = "logout"
)

const defaultAccessLogColumns = `id, exam_id, submission_id, student_identifier,
student_name, exam_number, student_class, event, ip_address, device_info, created_at, identity_data`

func scanAccessLog(row pgx.Row) (StudentAccessLog, error) {
	var l StudentAccessLog
	err := row.Scan(
		&l.ID, &l.ExamID, &l.SubmissionID, &l.StudentIdentifier,
		&l.StudentName, &l.ExamNumber, &l.StudentClass, &l.Event,
		&l.IPAddress, &l.DeviceInfo, &l.CreatedAt, &l.IdentityData,
	)
	return l, err
}

// CreateAccessLog inserts a new student access log entry.
func CreateAccessLog(ctx context.Context, pool *pgxpool.Pool, l *StudentAccessLog) (*StudentAccessLog, error) {
	sql := `INSERT INTO student_access_logs
(exam_id, submission_id, student_identifier, student_name, exam_number, student_class,
 event, ip_address, device_info, identity_data)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING ` + defaultAccessLogColumns

	created, err := scanAccessLog(pool.QueryRow(ctx, sql,
		l.ExamID, l.SubmissionID, l.StudentIdentifier,
		l.StudentName, l.ExamNumber, l.StudentClass,
		l.Event, l.IPAddress, l.DeviceInfo, l.IdentityData,
	))
	if err != nil {
		return nil, fmt.Errorf("create access log: %w", err)
	}
	return &created, nil
}

// PurgeOldStudentAccessLogs deletes access-log rows older than `days` days
// and returns how many rows were removed. It backs
// admin.StartAccessLogRetentionJob: student_access_logs grows with
// (#devices × exam duration) — a full room emits ~1 heartbeat/minute per
// device into this table via the heartbeat flusher — so on a small-disk
// server (thin-client deployments ship 13 GB) the table must be swept on a
// schedule instead of growing forever.
//
// SAFETY: days <= 0 DISABLES retention and deletes nothing ("0" must never
// mean "delete everything"); the caller treats it as an off switch.
func PurgeOldStudentAccessLogs(ctx context.Context, pool *pgxpool.Pool, days int) (int64, error) {
	if days <= 0 {
		return 0, nil
	}
	tag, err := pool.Exec(ctx,
		`DELETE FROM student_access_logs WHERE created_at < now() - make_interval(days => $1::int)`,
		days)
	if err != nil {
		return 0, fmt.Errorf("purge access logs older than %d days: %w", days, err)
	}
	return tag.RowsAffected(), nil
}

// ListAccessLogsOpts holds filters for listing access logs.
type ListAccessLogsOpts struct {
	ExamID            int
	StudentIdentifier string
	Event             string // optional filter: login, heartbeat, logout
	Page              int
	PerPage           int
}

// ListAccessLogsByExamAndIdentifier returns access logs for a specific exam and student identifier,
// ordered by created_at ascending.
func ListAccessLogsByExamAndIdentifier(ctx context.Context, pool *pgxpool.Pool, examID int, identifier string) ([]StudentAccessLog, error) {
	sql := `SELECT ` + defaultAccessLogColumns +
		` FROM student_access_logs WHERE exam_id = $1 AND student_identifier = $2 ORDER BY created_at ASC`

	rows, err := pool.Query(ctx, sql, examID, identifier)
	if err != nil {
		return nil, fmt.Errorf("list access logs: %w", err)
	}
	defer rows.Close()

	var logs []StudentAccessLog
	for rows.Next() {
		l, err := scanAccessLog(rows)
		if err != nil {
			return nil, fmt.Errorf("scan access log: %w", err)
		}
		logs = append(logs, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}

	if logs == nil {
		logs = []StudentAccessLog{}
	}
	return logs, nil
}

// ListAccessLogsByExam returns all access logs for a given exam, with optional event filter.
func ListAccessLogsByExam(ctx context.Context, pool *pgxpool.Pool, examID int, event string) ([]StudentAccessLog, error) {
	var sql string
	var args []interface{}

	if event != "" {
		sql = `SELECT ` + defaultAccessLogColumns +
			` FROM student_access_logs WHERE exam_id = $1 AND event = $2 ORDER BY created_at ASC`
		args = append(args, examID, event)
	} else {
		sql = `SELECT ` + defaultAccessLogColumns +
			` FROM student_access_logs WHERE exam_id = $1 ORDER BY created_at ASC`
		args = append(args, examID)
	}

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list access logs by exam: %w", err)
	}
	defer rows.Close()

	var logs []StudentAccessLog
	for rows.Next() {
		l, err := scanAccessLog(rows)
		if err != nil {
			return nil, fmt.Errorf("scan access log: %w", err)
		}
		logs = append(logs, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}

	if logs == nil {
		logs = []StudentAccessLog{}
	}
	return logs, nil
}

// GetAccessLogsForSubmission retrieves limited access logs associated with a submission
// (matched by exam_id and mac_address/student_identifier).
func GetAccessLogsForSubmission(ctx context.Context, pool *pgxpool.Pool, examID int, studentIdentifier string) ([]StudentAccessLog, error) {
	return ListAccessLogsByExamAndIdentifier(ctx, pool, examID, studentIdentifier)
}

// GetLatestAccessLogForStudent returns the most recent access log entry for a student
// in a given exam.
func GetLatestAccessLogForStudent(ctx context.Context, pool *pgxpool.Pool, examID int, identifier string) (*StudentAccessLog, error) {
	sql := `SELECT ` + defaultAccessLogColumns +
		` FROM student_access_logs WHERE exam_id = $1 AND student_identifier = $2 ORDER BY created_at DESC LIMIT 1`

	l, err := scanAccessLog(pool.QueryRow(ctx, sql, examID, identifier))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest access log: %w", err)
	}
	return &l, nil
}
