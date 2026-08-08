// Package models provides data structures and database access methods
// mirroring the EXAMVAN server database schema.
package models

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Exam represents a row from the exams table.
type Exam struct {
	ID                 int        `json:"id"`
	Name               string     `json:"name"`
	FilePath           string     `json:"file_path"`
	SizeBytes          int64      `json:"size_bytes"`
	Token              string     `json:"token"`
	ActiveToken        string     `json:"active_token"`
	QuestionsJSON      *string    `json:"questions_json,omitempty"`
	Status             string     `json:"status"`
	SecurityLevel      string     `json:"security_level"`
	StrictMode         int        `json:"strict_mode"`    // 0/1 integer stored in DB
	PublicResults      int        `json:"public_results"` // 0/1 integer stored in DB
	ShowAnswers        int        `json:"show_answers"`   // 0/1 integer stored in DB
	CreatedBy          int        `json:"created_by"`
	CreatedAt          time.Time  `json:"created_at"`
	IdentityFields     *string    `json:"identity_fields,omitempty"`
	PanelColor         *string    `json:"panel_color,omitempty"`
	// CongratsMessage is the customizable message shown on the Android
	// congratulations page after a student submits. Free text (not HTML),
	// rendered plain on the client; nil/empty means the app falls back to its
	// default wording.
	CongratsMessage *string `json:"congrats_message,omitempty"`
	StartTime          *time.Time `json:"start_time,omitempty"`
	EndTime            *time.Time `json:"end_time,omitempty"`
	DelegatedTo        *int       `json:"delegated_to,omitempty"`
	TokenMode          *string    `json:"token_mode,omitempty"`
	TokenResetInterval *int       `json:"token_reset_interval,omitempty"`
	TokenLastResetAt   *time.Time `json:"token_last_reset_at,omitempty"`
	ExamStartedAt      *time.Time `json:"exam_started_at,omitempty"`
	// TombstonedAt is set when this active-but-unstarted exam is
	// auto-inactivated (policy B): the school's operator is cut off (voucher
	// switch or manual suspension) or the creating account's active period
	// expired (trial/personal accounts — see tombstoneExpiredUsersExamsPass).
	// It lets the admin UI distinguish an auto-tombstoned exam from a manually
	// inactivated one. Cleared whenever the exam is (re)activated.
	TombstonedAt *time.Time `json:"tombstoned_at,omitempty"`
}

// IsActive returns true when the exam status is "active".
func (e *Exam) IsActive() bool { return e.Status == "active" }

// IsStrict returns true when strict_mode is non-zero.
func (e *Exam) IsStrict() bool { return e.StrictMode != 0 }

// AreResultsPublic returns true when public_results is non-zero.
func (e *Exam) AreResultsPublic() bool { return e.PublicResults != 0 }

// AreAnswersShown returns true when show_answers is non-zero.
func (e *Exam) AreAnswersShown() bool { return e.ShowAnswers != 0 }

// GetTokenMode returns the token mode as a string, defaulting to "dynamic".
func (e Exam) GetTokenMode() string {
	if e.TokenMode == nil || *e.TokenMode == "" {
		return "dynamic"
	}
	return *e.TokenMode
}

// DefaultExamColumns is the column list used in SELECT queries for the exams table.
const DefaultExamColumns = `id, name, file_path, size_bytes, token, active_token, questions_json,
status, security_level, strict_mode, public_results, show_answers,
created_by, created_at, identity_fields, panel_color,
start_time, end_time, delegated_to, token_mode, token_reset_interval, token_last_reset_at, exam_started_at, tombstoned_at, congrats_message`

// DefaultExamColumnsWithAlias for JOIN queries with e. prefix.
const DefaultExamColumnsWithAlias = `e.id, e.name, e.file_path, e.size_bytes, e.token, e.active_token, e.questions_json,
e.status, e.security_level, e.strict_mode, e.public_results, e.show_answers,
e.created_by, e.created_at, e.identity_fields, e.panel_color,
e.start_time, e.end_time, e.delegated_to, e.token_mode, e.token_reset_interval, e.token_last_reset_at, e.exam_started_at, e.tombstoned_at, e.congrats_message`

// scanExam scans a row into an Exam struct. The columns must match DefaultExamColumns order.
func scanExam(row pgx.Row) (Exam, error) {
	var e Exam
	err := row.Scan(
		&e.ID, &e.Name, &e.FilePath, &e.SizeBytes, &e.Token, &e.ActiveToken, &e.QuestionsJSON,
		&e.Status, &e.SecurityLevel, &e.StrictMode, &e.PublicResults, &e.ShowAnswers,
		&e.CreatedBy, &e.CreatedAt, &e.IdentityFields, &e.PanelColor,
		&e.StartTime, &e.EndTime, &e.DelegatedTo,
		&e.TokenMode, &e.TokenResetInterval, &e.TokenLastResetAt, &e.ExamStartedAt,
		&e.TombstonedAt, &e.CongratsMessage,
	)
	return e, err
}

// scanExamFromRows scans the next row from Rows into an Exam.
func scanExamFromRows(rows pgx.Rows) (Exam, error) {
	return scanExam(rows)
}

// GetByID retrieves a single exam by primary key.
func GetExamByID(ctx context.Context, pool *pgxpool.Pool, id int) (Exam, error) {
	sql := `SELECT ` + DefaultExamColumns + ` FROM exams e WHERE e.id = $1`
	return scanExam(pool.QueryRow(ctx, sql, id))
}

// GetExamByToken retrieves an exam by its unique 8-character token.
func GetExamByToken(ctx context.Context, pool *pgxpool.Pool, token string) (Exam, error) {
	sql := `SELECT ` + DefaultExamColumns + ` FROM exams e WHERE e.token = $1 OR e.active_token = $1`
	return scanExam(pool.QueryRow(ctx, sql, token))
}

// ListExamsOpts holds optional filters for listing exams.
type ListExamsOpts struct {
	Page       int
	PerPage    int
	Search     string
	Status     string // optional: "active" or "inactive"
	CreatedBy  *int   // optional: filter by creator
	Instansi   string // optional: filter by creator's instansi (for operator view)
	UserID     *int   // optional: include exams delegated to or assigned as pengawas
	IsPengawas bool   // when true, only show exams where user is assigned as pengawas
	IsGuru     bool   // when true, also include own exams
}

// ListExamsResult holds the paginated exam list and total count.
type ListExamsResult struct {
	Exams      []Exam
	Total      int
	TotalPages int
	Page       int
	PerPage    int
}

const maxPerPage = 100
const minPerPage = 5

func clampPerPage(n int) int {
	if n < minPerPage {
		return minPerPage
	}
	if n > maxPerPage {
		return maxPerPage
	}
	return n
}

func calcOffset(page, perPage int) int {
	return (page - 1) * perPage
}

func calcTotalPages(total, perPage int) int {
	return int(math.Max(1, float64((total+perPage-1)/perPage)))
}

// ListExams returns paginated exams. It builds dynamic WHERE conditions based on opts.
func ListExams(ctx context.Context, pool *pgxpool.Pool, opts ListExamsOpts) (ListExamsResult, error) {
	page := opts.Page
	if page < 1 {
		page = 1
	}
	perPage := clampPerPage(opts.PerPage)

	// Build WHERE clause and parameters.
	var conditions []string
	var args []interface{}
	argIdx := 1

	// Status filter. "tombstoned" is a virtual status: exams auto-inactivated
	// by policy B (voucher switch / manual suspension) — inactive with the
	// tombstoned_at marker set — so the dashboard can list them apart from a
	// plain manual deactivation.
	if opts.Status != "" {
		if opts.Status == "tombstoned" {
			conditions = append(conditions, "e.status = 'inactive' AND e.tombstoned_at IS NOT NULL")
		} else {
			conditions = append(conditions, fmt.Sprintf("e.status = $%d", argIdx))
			args = append(args, opts.Status)
			argIdx++
		}
	}

	// Search filter.
	if opts.Search != "" {
		searchPattern := "%" + opts.Search + "%"
		conditions = append(conditions,
			fmt.Sprintf("(e.name ILIKE $%d OR e.token ILIKE $%d OR u.username ILIKE $%d)", argIdx, argIdx+1, argIdx+2))
		args = append(args, searchPattern, searchPattern, searchPattern)
		argIdx += 3
	}

	// CreatedBy filter.
	if opts.CreatedBy != nil {
		conditions = append(conditions, fmt.Sprintf("e.created_by = $%d", argIdx))
		args = append(args, *opts.CreatedBy)
		argIdx++
	}

	// Instansi filter (for operator view): all users in the operator's instansi.
	if opts.Instansi != "" {
		conditions = append(conditions, fmt.Sprintf(
			`e.created_by IN (SELECT id FROM admin_users WHERE instansi = $%d)`, argIdx))
		args = append(args, opts.Instansi)
		argIdx++
	}

	// User-specific visibility: include created_by, delegated_to, and/or exam_pengawas.
	if opts.UserID != nil {
		if opts.IsPengawas && opts.IsGuru {
			conditions = append(conditions, fmt.Sprintf(
				`(e.created_by = $%d OR e.delegated_to = $%d OR e.id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = $%d))`, argIdx, argIdx, argIdx))
			args = append(args, *opts.UserID)
			argIdx++
		} else if opts.IsPengawas {
			conditions = append(conditions, fmt.Sprintf(
				`e.id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = $%d)`, argIdx))
			args = append(args, *opts.UserID)
			argIdx++
		} else {
			// Guru-only: own exams + delegated exams
			conditions = append(conditions, fmt.Sprintf(
				`(e.created_by = $%d OR e.delegated_to = $%d)`, argIdx, argIdx))
			args = append(args, *opts.UserID)
			argIdx++
		}
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " WHERE " + joinConditions(conditions, " AND ")
	}

	// Count total matching rows.
	countSQL := `SELECT COUNT(*) as cnt FROM exams e LEFT JOIN admin_users u ON e.created_by = u.id` + whereClause
	var total int
	err := pool.QueryRow(ctx, countSQL, args...).Scan(&total)
	if err != nil {
		return ListExamsResult{}, fmt.Errorf("count exams: %w", err)
	}

	totalPages := calcTotalPages(total, perPage)
	if page > totalPages && total > 0 {
		page = totalPages
	}
	offset := calcOffset(page, perPage)

	// Fetch page.
	query := `SELECT ` + DefaultExamColumnsWithAlias + ` FROM exams e
LEFT JOIN admin_users u ON e.created_by = u.id` + whereClause +
		` ORDER BY e.created_at DESC LIMIT $` + fmt.Sprintf("%d", argIdx) +
		` OFFSET $` + fmt.Sprintf("%d", argIdx+1)
	args = append(args, perPage, offset)

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return ListExamsResult{}, fmt.Errorf("list exams: %w", err)
	}
	defer rows.Close()

	var exams []Exam
	for rows.Next() {
		e, err := scanExamFromRows(rows)
		if err != nil {
			return ListExamsResult{}, fmt.Errorf("scan exam row: %w", err)
		}
		exams = append(exams, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}

	if exams == nil {
		exams = []Exam{}
	}

	return ListExamsResult{
		Exams:      exams,
		Total:      total,
		TotalPages: totalPages,
		Page:       page,
		PerPage:    perPage,
	}, nil
}

// (ListActiveExams was removed: a global, unscoped list of every tenant's active
// exams is a cross-tenant leak on the shared SaaS API. The public exam-list
// endpoint now uses ListActiveExamsByInstansi to scope results to one school.)

// queryRower abstracts the query interface shared by *pgxpool.Pool and pgx.Tx
// so CreateExam can run both standalone and inside a transaction.
type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// createExam inserts a new exam row using the given queryer (pool or tx) and
// returns the created Exam with its generated ID.
func createExam(ctx context.Context, q queryRower, e *Exam) (*Exam, error) {
	// Set active_token = token on creation
	if e.ActiveToken == "" {
		e.ActiveToken = e.Token
	}
	sql := `INSERT INTO exams
(name, file_path, size_bytes, token, active_token, questions_json, status, security_level,
 strict_mode, public_results, show_answers, created_by, identity_fields,
 panel_color, start_time, end_time, delegated_to, token_mode, token_reset_interval, token_last_reset_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
RETURNING ` + DefaultExamColumns

	created, err := scanExam(q.QueryRow(ctx, sql,
		e.Name, e.FilePath, e.SizeBytes, e.Token, e.ActiveToken, e.QuestionsJSON,
		e.Status, e.SecurityLevel, e.StrictMode, e.PublicResults, e.ShowAnswers,
		e.CreatedBy, e.IdentityFields, e.PanelColor, e.StartTime, e.EndTime,
		e.DelegatedTo, e.TokenMode, e.TokenResetInterval, e.TokenLastResetAt,
	))
	if err != nil {
		return nil, fmt.Errorf("create exam: %w", err)
	}
	return &created, nil
}

// CreateExam inserts a new exam row and returns the created Exam with its generated ID.
func CreateExam(ctx context.Context, pool *pgxpool.Pool, e *Exam) (*Exam, error) {
	return createExam(ctx, pool, e)
}

// CreateExamTx inserts a new exam row inside an existing transaction.
func CreateExamTx(ctx context.Context, tx pgx.Tx, e *Exam) (*Exam, error) {
	return createExam(ctx, tx, e)
}

// CountRunningExams returns the number of exams created by createdBy that are
// currently RUNNING (status='active' AND exam_started_at IS NOT NULL), i.e.
// exams students can actually work on right now. excludeID is not counted
// (used when the caller is about to start/activate that exam itself).
func CountRunningExams(ctx context.Context, pool *pgxpool.Pool, createdBy, excludeID int) (int, error) {
	var n int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM exams
		WHERE created_by = $1 AND status = 'active' AND exam_started_at IS NOT NULL AND id <> $2`,
		createdBy, excludeID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count running exams: %w", err)
	}
	return n, nil
}

// RunningExamCountsAfterActivation returns, per distinct created_by owner of
// the given exam ids, the number of running exams that owner would have if
// every selected exam were activated. Only exams with exam_started_at already
// set become "running" on activation (activating a never-started exam is not
// enough for students to join it). Used to enforce the concurrent-exam quota
// on bulk activation.
func RunningExamCountsAfterActivation(ctx context.Context, pool *pgxpool.Pool, ids []int) (map[int]int, error) {
	out := make(map[int]int)
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT e.created_by,
		       (SELECT COUNT(*) FROM exams x
		         WHERE x.created_by = e.created_by
		           AND x.status = 'active' AND x.exam_started_at IS NOT NULL)
		       + COUNT(*) FILTER (WHERE e.status <> 'active' AND e.exam_started_at IS NOT NULL) AS running_after
		FROM exams e
		WHERE e.id = ANY($1) AND e.exam_started_at IS NOT NULL
		GROUP BY e.created_by`, ids)
	if err != nil {
		return nil, fmt.Errorf("running exam counts after activation: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var owner, after int
		if err := rows.Scan(&owner, &after); err != nil {
			return nil, fmt.Errorf("scan running exam count: %w", err)
		}
		out[owner] = after
	}
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}
	return out, nil
}

// UpdateExam updates exam name and optionally file_path + size_bytes.
// Only non-zero/non-nil fields passed via the struct are written.
// A nil pointer in Exam for optional fields means "leave as-is".
func UpdateExam(ctx context.Context, pool *pgxpool.Pool, id int, e *Exam) error {
	sql := `UPDATE exams SET
name = $1, file_path = $2, size_bytes = $3
WHERE id = $4`
	_, err := pool.Exec(ctx, sql, e.Name, e.FilePath, e.SizeBytes, id)
	if err != nil {
		return fmt.Errorf("update exam: %w", err)
	}
	return nil
}

// UpdateExamQuestions updates the questions_json, security_level, strict_mode,
// identity_fields, panel_color, start_time, end_time, and the custom
// congrats_message for an exam.
func UpdateExamQuestions(ctx context.Context, pool *pgxpool.Pool, id int,
	questionsJSON, securityLevel, identityFieldsJSON, panelColor, startTime, endTime, congratsMessage *string, strictMode int) error {
	sql := `UPDATE exams SET
questions_json = $1, security_level = $2, strict_mode = $3,
identity_fields = $4, panel_color = $5, start_time = $6, end_time = $7,
congrats_message = $8
WHERE id = $9`
	_, err := pool.Exec(ctx, sql,
		questionsJSON, securityLevel, strictMode,
		identityFieldsJSON, panelColor, startTime, endTime, congratsMessage, id)
	if err != nil {
		return fmt.Errorf("update exam questions: %w", err)
	}
	return nil
}

// UpdateExamToken updates both the permanent token and active_token for an exam.
func UpdateExamToken(ctx context.Context, pool *pgxpool.Pool, id int, token string) error {
	_, err := pool.Exec(ctx, `UPDATE exams SET token = $1, active_token = $1, token_last_reset_at = CURRENT_TIMESTAMP WHERE id = $2`, token, id)
	if err != nil {
		return fmt.Errorf("update exam token: %w", err)
	}
	return nil
}

// GetExamByActiveToken retrieves an exam by its active_token (for Android API lookup).
func GetExamByActiveToken(ctx context.Context, pool *pgxpool.Pool, activeToken string) (Exam, error) {
	sql := `SELECT ` + DefaultExamColumns + ` FROM exams e WHERE e.active_token = $1`
	return scanExam(pool.QueryRow(ctx, sql, activeToken))
}

// UpdateExamActiveToken updates the active_token for dynamic rotation, leaving the permanent token unchanged.
func UpdateExamActiveToken(ctx context.Context, pool *pgxpool.Pool, id int, activeToken string) error {
	_, err := pool.Exec(ctx, `UPDATE exams SET active_token = $1, token_last_reset_at = CURRENT_TIMESTAMP WHERE id = $2`, activeToken, id)
	if err != nil {
		return fmt.Errorf("update exam active token: %w", err)
	}
	return nil
}

// StartExam marks an exam as started (sets exam_started_at) and optionally resets the active_token.
// Starting is an activation, so any tombstone marker is cleared.
func StartExam(ctx context.Context, pool *pgxpool.Pool, id int) error {
	_, err := pool.Exec(ctx, `UPDATE exams SET status = 'active', exam_started_at = CURRENT_TIMESTAMP, token_last_reset_at = CURRENT_TIMESTAMP, tombstoned_at = NULL WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("start exam: %w", err)
	}
	return nil
}

// StopExam marks an exam as inactive and clears the exam_started_at flag.
func StopExam(ctx context.Context, pool *pgxpool.Pool, id int) error {
	_, err := pool.Exec(ctx, `UPDATE exams SET status = 'inactive', exam_started_at = NULL WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("stop exam: %w", err)
	}
	return nil
}

// GetStartedExamsByPengawas returns exams that have been started and are assigned to the given user as pengawas.
func GetStartedExamsByPengawas(ctx context.Context, pool *pgxpool.Pool, userID int) ([]Exam, error) {
	rows, err := pool.Query(ctx,
		`SELECT `+DefaultExamColumns+` FROM exams e
WHERE e.exam_started_at IS NOT NULL
AND e.id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = $1)
ORDER BY e.created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("get started exams: %w", err)
	}
	defer rows.Close()

	var exams []Exam
	for rows.Next() {
		e, err := scanExamFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan started exam: %w", err)
		}
		exams = append(exams, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}
	if exams == nil {
		exams = []Exam{}
	}
	return exams, nil
}

// UpdateExamTokenMode updates the token mode (static/dynamic) and reset interval.
func UpdateExamTokenMode(ctx context.Context, pool *pgxpool.Pool, id int, tokenMode string, resetInterval *int) error {
	_, err := pool.Exec(ctx,
		`UPDATE exams SET token_mode = $1, token_reset_interval = $2 WHERE id = $3`,
		tokenMode, resetInterval, id)
	if err != nil {
		return fmt.Errorf("update exam token mode: %w", err)
	}
	return nil
}

// ToggleExamStatus switches the exam status between 'active' and 'inactive'.
// Returns the new status string.
func ToggleExamStatus(ctx context.Context, pool *pgxpool.Pool, id int) (string, error) {
	exam, err := GetExamByID(ctx, pool, id)
	if err != nil {
		return "", fmt.Errorf("toggle status: get exam: %w", err)
	}
	newStatus := "inactive"
	if exam.Status == "inactive" {
		newStatus = "active"
	}
	// (Re)activating clears the tombstone marker: the exam is back under the
	// owner's control, so it is no longer "auto-inactivated" (policy B).
	_, err = pool.Exec(ctx, `UPDATE exams SET status = $1,
		tombstoned_at = CASE WHEN $1 = 'active' THEN NULL ELSE tombstoned_at END
		WHERE id = $2`, newStatus, id)
	if err != nil {
		return "", fmt.Errorf("toggle status: %w", err)
	}
	return newStatus, nil
}

// DeleteExam deletes an exam by ID and returns the deleted exam's file_path
// so the caller can clean up the file from storage. Also cleans up related data.
func DeleteExam(ctx context.Context, pool *pgxpool.Pool, id int) (*Exam, error) {
	exam, err := GetExamByID(ctx, pool, id)
	if err != nil {
		return nil, fmt.Errorf("delete exam: get exam: %w", err)
	}

	// Clean up related data in a transaction
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("delete exam: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM exam_pengawas WHERE exam_id = $1`, id); err != nil {
		return nil, fmt.Errorf("delete exam: delete pengawas: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM student_access_logs WHERE exam_id = $1`, id); err != nil {
		return nil, fmt.Errorf("delete exam: delete access logs: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM submissions WHERE exam_id = $1`, id); err != nil {
		return nil, fmt.Errorf("delete exam: delete submissions: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM exams WHERE id = $1`, id); err != nil {
		return nil, fmt.Errorf("delete exam: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("delete exam: commit: %w", err)
	}

	return &exam, nil
}

// BulkDeleteExams deletes multiple exams and returns their file paths for cleanup.
func BulkDeleteExams(ctx context.Context, pool *pgxpool.Pool, ids []int) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `SELECT file_path FROM exams WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("bulk delete: query file_paths: %w", err)
	}
	var paths []string
	for rows.Next() {
		var fp string
		if err := rows.Scan(&fp); err != nil {
			return nil, fmt.Errorf("bulk delete: scan: %w", err)
		}
		paths = append(paths, fp)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}

	_, err = pool.Exec(ctx, `DELETE FROM exams WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("bulk delete: exec: %w", err)
	}
	return paths, nil
}

// ListActiveExamsByInstansi lists active, started exams whose creator belongs to
// the given school, identified by its unique instansi_code (case-insensitive).
// Used by the public mobile exam-list endpoint so a client only ever sees ONE
// school's exams — never a cross-tenant global list.
//
// Matching is by CODE only (not the free-text instansi name): names are not
// unique (the unique-name constraint was dropped and names are not deduplicated
// on creation), so a name match could bleed exams across two schools that
// happen to share a name. An empty/unknown code returns no exams.
func ListActiveExamsByInstansi(ctx context.Context, pool *pgxpool.Pool, code string, page, perPage int) (ListExamsResult, error) {
	if page < 1 {
		page = 1
	}
	perPage = clampPerPage(perPage)

	empty := ListExamsResult{Exams: []Exam{}, Total: 0, TotalPages: 1, Page: page, PerPage: perPage}
	if strings.TrimSpace(code) == "" {
		return empty, nil
	}

	const scope = ` FROM exams e JOIN admin_users u ON e.created_by = u.id
		WHERE e.status = 'active' AND e.exam_started_at IS NOT NULL
		  AND u.instansi_code IS NOT NULL AND u.instansi_code <> ''
		  AND LOWER(u.instansi_code) = LOWER($1)`

	var total int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*)`+scope, code).Scan(&total); err != nil {
		return ListExamsResult{}, fmt.Errorf("count active exams by instansi: %w", err)
	}

	totalPages := calcTotalPages(total, perPage)
	if page > totalPages && total > 0 {
		page = totalPages
	}
	offset := calcOffset(page, perPage)

	rows, err := pool.Query(ctx,
		`SELECT `+DefaultExamColumnsWithAlias+scope+` ORDER BY e.created_at DESC LIMIT $2 OFFSET $3`,
		code, perPage, offset)
	if err != nil {
		return ListExamsResult{}, fmt.Errorf("list active exams by instansi: %w", err)
	}
	defer rows.Close()

	var exams []Exam
	for rows.Next() {
		e, err := scanExamFromRows(rows)
		if err != nil {
			return ListExamsResult{}, fmt.Errorf("scan active exam by instansi: %w", err)
		}
		exams = append(exams, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}
	if exams == nil {
		exams = []Exam{}
	}

	return ListExamsResult{
		Exams:      exams,
		Total:      total,
		TotalPages: totalPages,
		Page:       page,
		PerPage:    perPage,
	}, nil
}

// BulkToggleExamStatus changes the status of multiple exams at once.
// Bulk activation clears any tombstone markers (policy B).
func BulkToggleExamStatus(ctx context.Context, pool *pgxpool.Pool, ids []int, status string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := pool.Exec(ctx, `UPDATE exams SET status = $1,
		tombstoned_at = CASE WHEN $1 = 'active' THEN NULL ELSE tombstoned_at END
		WHERE id = ANY($2)`, status, ids)
	if err != nil {
		return fmt.Errorf("bulk toggle: %w", err)
	}
	return nil
}

// UserCanAccessExam reports whether a user may monitor an exam (e.g. join its
// WebSocket room / view live data). SuperAdmin always may. Otherwise access
// requires ownership (created_by), delegation (delegated_to), a pengawas
// assignment, or being an operator in the exam creator's (non-empty) instansi.
// An empty instansi never matches, so mis-provisioned/empty-instansi operators
// cannot reach other tenants' exams.
func UserCanAccessExam(ctx context.Context, pool *pgxpool.Pool, userID int, isSuper bool, examID int) bool {
	if isSuper {
		return true
	}
	var cnt int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM exams e
		LEFT JOIN admin_users owner ON owner.id = e.created_by
		WHERE e.id = $1 AND (
			e.created_by = $2
			OR e.delegated_to = $2
			OR e.id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = $2)
			OR EXISTS (
				SELECT 1 FROM admin_users me
				WHERE me.id = $2 AND me.role ILIKE '%"operator"%'
				  AND me.instansi NOT IN ('', 'personal') AND me.instansi = owner.instansi
			)
		)`, examID, userID).Scan(&cnt)
	if err != nil {
		return false
	}
	return cnt > 0
}

// UserCanControlExam reports whether a user may perform management/control
// actions on a single exam (settings, start/stop, delegation, approvals):
// SuperAdmin, the owner, a delegate, or an operator in the exam creator's
// (non-empty) instansi. Unlike UserCanAccessExam, a pengawas-only assignment
// does NOT grant control. This is the single-exam analogue of
// FilterAccessibleExamIDs.
func UserCanControlExam(ctx context.Context, pool *pgxpool.Pool, userID int, isSuper bool, examID int) bool {
	if isSuper {
		return true
	}
	var cnt int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM exams e
		LEFT JOIN admin_users owner ON owner.id = e.created_by
		WHERE e.id = $1 AND (
			e.created_by = $2
			OR e.delegated_to = $2
			OR EXISTS (
				SELECT 1 FROM admin_users me
				WHERE me.id = $2 AND me.role ILIKE '%"operator"%'
				  AND me.instansi NOT IN ('', 'personal') AND me.instansi = owner.instansi
			)
		)`, examID, userID).Scan(&cnt)
	if err != nil {
		return false
	}
	return cnt > 0
}

// FilterAccessibleExamIDs returns the subset of ids a non-super user may
// MANAGE (delete / toggle). Access requires ownership or delegation, or — for
// operators — being in the exam creator's (non-empty) instansi. Unlike
// UserCanAccessExam, pengawas-only assignments do NOT grant management rights,
// matching the single-exam authorization in checkExamOwnership. SuperAdmin
// callers should skip this filter entirely.
func FilterAccessibleExamIDs(ctx context.Context, pool *pgxpool.Pool, userID int, ids []int) ([]int, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT e.id FROM exams e
		LEFT JOIN admin_users owner ON owner.id = e.created_by
		WHERE e.id = ANY($1) AND (
			e.created_by = $2
			OR e.delegated_to = $2
			OR EXISTS (
				SELECT 1 FROM admin_users me
				WHERE me.id = $2 AND me.role ILIKE '%"operator"%'
				  AND me.instansi NOT IN ('', 'personal') AND me.instansi = owner.instansi
			)
		)`, ids, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// TogglePublicResults toggles the public_results flag for an exam.
func TogglePublicResults(ctx context.Context, pool *pgxpool.Pool, id int) (int, error) {
	exam, err := GetExamByID(ctx, pool, id)
	if err != nil {
		return 0, fmt.Errorf("toggle public results: %w", err)
	}
	newVal := 0
	if exam.PublicResults == 0 {
		newVal = 1
	}
	_, err = pool.Exec(ctx, `UPDATE exams SET public_results = $1 WHERE id = $2`, newVal, id)
	if err != nil {
		return 0, fmt.Errorf("toggle public results update: %w", err)
	}
	return newVal, nil
}

// ToggleShowAnswers toggles the show_answers flag for an exam.
func ToggleShowAnswers(ctx context.Context, pool *pgxpool.Pool, id int) (int, error) {
	exam, err := GetExamByID(ctx, pool, id)
	if err != nil {
		return 0, fmt.Errorf("toggle show answers: %w", err)
	}
	newVal := 0
	if exam.ShowAnswers == 0 {
		newVal = 1
	}
	_, err = pool.Exec(ctx, `UPDATE exams SET show_answers = $1 WHERE id = $2`, newVal, id)
	if err != nil {
		return 0, fmt.Errorf("toggle show answers update: %w", err)
	}
	return newVal, nil
}

// DelegateExam sets the delegated_to field (transferring ownership to another user).
func DelegateExam(ctx context.Context, pool *pgxpool.Pool, id int, delegatedTo *int) error {
	_, err := pool.Exec(ctx, `UPDATE exams SET delegated_to = $1 WHERE id = $2`, delegatedTo, id)
	if err != nil {
		return fmt.Errorf("delegate exam: %w", err)
	}
	return nil
}

// joinConditions joins non-empty strings with the given separator.
func joinConditions(parts []string, sep string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}

// ListExamsByPengawas returns exams assigned to a specific user as pengawas,
// with pagination and optional search.
type ListPengawasExamsOpts struct {
	Page            int
	PerPage         int
	Search          string
	UserID          int
	HasPengawasRole bool
}

func ListPengawasExams(ctx context.Context, pool *pgxpool.Pool, opts ListPengawasExamsOpts) (ListExamsResult, error) {
	if opts.Page < 1 {
		opts.Page = 1
	}
	perPage := clampPerPage(opts.PerPage)

	var conditions []string
	var args []interface{}
	argIdx := 1

	if opts.HasPengawasRole {
		conditions = append(conditions, fmt.Sprintf(`(e.created_by = $%d OR e.delegated_to = $%d OR e.id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = $%d))`, argIdx, argIdx, argIdx))
	} else {
		conditions = append(conditions, fmt.Sprintf(`(e.created_by = $%d OR e.delegated_to = $%d)`, argIdx, argIdx))
	}
	args = append(args, opts.UserID)
	argIdx++

	if opts.Search != "" {
		pat := "%" + opts.Search + "%"
		conditions = append(conditions,
			fmt.Sprintf(`(e.name ILIKE $%d OR e.token ILIKE $%d OR u.username ILIKE $%d)`, argIdx, argIdx+1, argIdx+2))
		args = append(args, pat, pat, pat)
		argIdx += 3
	}

	where := " WHERE " + joinConditions(conditions, " AND ")

	var total int
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM exams e JOIN admin_users u ON e.created_by = u.id`+where, args...).Scan(&total)
	if err != nil {
		return ListExamsResult{}, fmt.Errorf("count pengawas exams: %w", err)
	}

	totalPages := calcTotalPages(total, perPage)
	if opts.Page > totalPages && total > 0 {
		opts.Page = totalPages
	}
	offset := calcOffset(opts.Page, perPage)

	sql := `SELECT ` + DefaultExamColumnsWithAlias + `
FROM exams e JOIN admin_users u ON e.created_by = u.id` + where +
		` ORDER BY e.created_at DESC LIMIT $` + fmt.Sprintf("%d", argIdx) +
		` OFFSET $` + fmt.Sprintf("%d", argIdx+1)
	args = append(args, perPage, offset)

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return ListExamsResult{}, fmt.Errorf("list pengawas exams: %w", err)
	}
	defer rows.Close()

	var exams []Exam
	for rows.Next() {
		e, err := scanExamFromRows(rows)
		if err != nil {
			return ListExamsResult{}, fmt.Errorf("scan pengawas exam: %w", err)
		}
		exams = append(exams, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}

	if exams == nil {
		exams = []Exam{}
	}

	return ListExamsResult{
		Exams:      exams,
		Total:      total,
		TotalPages: totalPages,
		Page:       opts.Page,
		PerPage:    perPage,
	}, nil
}
