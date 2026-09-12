package models

import (
	"context"
	"fmt"
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
