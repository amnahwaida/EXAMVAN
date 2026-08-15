package models

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Submission represents a row from the submissions table.
type Submission struct {
	ID           int        `json:"id"`
	ExamID       int        `json:"exam_id"`
	StudentName  string     `json:"student_name"`
	ExamNumber   string     `json:"exam_number"`
	StudentClass string     `json:"student_class"`
	AnswersJSON  *string    `json:"answers_json,omitempty"`
	Score        *float64   `json:"score,omitempty"`
	StartTime    *string    `json:"start_time,omitempty"`
	MACAddress   string     `json:"mac_address"`
	CreatedAt    time.Time  `json:"created_at"`
	IdentityData *string    `json:"identity_data,omitempty"`
}

// SubmissionWithExam extends Submission with exam-related fields for display.
type SubmissionWithExam struct {
	Submission
	ExamName      string   `json:"exam_name"`
	QuestionsJSON *string  `json:"questions_json,omitempty"`
	MaxScore      *float64 `json:"max_score,omitempty"`
	ScorePct      *float64 `json:"score_pct,omitempty"`
}

// Question represents a single question from the questions_json array.
type Question struct {
	Number         interface{} `json:"number"`
	Key            interface{} `json:"key"`
	Answer         interface{} `json:"answer"` // alternative for key
	Type           string      `json:"type"`
	Weight         float64     `json:"weight"`
	Score          float64     `json:"score"` // alternative for weight
	PartialScoring bool        `json:"partial_scoring"`
	Label          string      `json:"label,omitempty"`
}

// GetKey returns the answer key, checking both Key and Answer fields.
func (q *Question) GetKey() interface{} {
	if q.Key != nil {
		return q.Key
	}
	return q.Answer
}

// GetWeight returns the weight, checking both Weight and Score fields.
func (q *Question) GetWeight() float64 {
	if q.Weight > 0 {
		return q.Weight
	}
	if q.Score > 0 {
		return q.Score
	}
	return 1.0
}

// EvaluationDetail holds the result of evaluating a single question.
type EvaluationDetail struct {
	Earned      float64 `json:"earned"`
	StatusText  string  `json:"statusText"`
	StatusClass string  `json:"statusClass"`
}

const (
	StatusCorrect    = "correct"
	StatusIncorrect  = "incorrect"
	StatusPartial    = "partial"
	StatusUnanswered = "unanswered"
)

// ===== Scoring Engine =====

// normalizeQNum normalizes a question number, handling float representations.
// e.g. 1.0 -> "1", "2.5" -> "2.5"
func normalizeQNum(number interface{}) string {
	switch v := number.(type) {
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case string:
		// Try parsing as number first.
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			if f == float64(int64(f)) {
				return strconv.FormatInt(int64(f), 10)
			}
			return strconv.FormatFloat(f, 'f', -1, 64)
		}
		return v
	case int, int64, int32:
		return fmt.Sprintf("%d", v)
	case json.Number:
		f, _ := v.Float64()
		if f == float64(int64(f)) {
			return strconv.FormatInt(int64(f), 10)
		}
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// evaluateSingleQuestion evaluates a single student answer against the correct answer.
// Returns the earned score and status details.
func evaluateSingleQuestion(studentAns, correctAns interface{}, qType string, qWeight float64, partialScoring bool) EvaluationDetail {
	if studentAns == nil || correctAns == nil {
		if studentAns == nil {
			return EvaluationDetail{Earned: 0, StatusText: StatusUnanswered, StatusClass: StatusUnanswered}
		}
		return EvaluationDetail{Earned: 0, StatusText: StatusIncorrect, StatusClass: StatusIncorrect}
	}

	switch qType {
	case "single_choice", "true_false", "short_answer":
		sNorm := strings.ToUpper(strings.Join(strings.Fields(fmt.Sprintf("%v", studentAns)), " "))
		cNorm := strings.ToUpper(strings.Join(strings.Fields(fmt.Sprintf("%v", correctAns)), " "))
		if sNorm == cNorm {
			return EvaluationDetail{Earned: qWeight, StatusText: StatusCorrect, StatusClass: StatusCorrect}
		}
		return EvaluationDetail{Earned: 0, StatusText: StatusIncorrect, StatusClass: StatusIncorrect}

	case "multiple_choice":
		studentList, sOK := studentAns.([]interface{})
		correctList, cOK := correctAns.([]interface{})
		if !sOK || !cOK {
			// Try string representations.
			sStr := strings.Fields(fmt.Sprintf("%v", studentAns))
			cStr := strings.Fields(fmt.Sprintf("%v", correctAns))
			return evaluateMC(sStr, cStr, qWeight, partialScoring)
		}
		return evaluateMC(stripSlice(studentList), stripSlice(correctList), qWeight, partialScoring)

	case "matching":
		studentMap, sOK := studentAns.(map[string]interface{})
		correctMap, cOK := correctAns.(map[string]interface{})
		if !sOK || !cOK {
			// Try map from JSON object.
			sMap := toMapStringInterface(studentAns)
			cMap := toMapStringInterface(correctAns)
			if sMap == nil || cMap == nil {
				return EvaluationDetail{Earned: 0, StatusText: StatusIncorrect, StatusClass: StatusIncorrect}
			}
			return evaluateMatching(sMap, cMap, qWeight, partialScoring)
		}
		return evaluateMatching(studentMap, correctMap, qWeight, partialScoring)
	}

	return EvaluationDetail{Earned: 0, StatusText: StatusIncorrect, StatusClass: StatusIncorrect}
}

func stripSlice(items []interface{}) []string {
	result := make([]string, len(items))
	for i, v := range items {
		result[i] = strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", v)))
	}
	return result
}

func toMapStringInterface(v interface{}) map[string]interface{} {
	switch m := v.(type) {
	case map[string]interface{}:
		return m
	default:
		return nil
	}
}

func evaluateMC(studentSet, correctSet []string, qWeight float64, partialScoring bool) EvaluationDetail {
	if partialScoring {
		if len(correctSet) == 0 {
			return EvaluationDetail{Earned: 0, StatusText: StatusIncorrect, StatusClass: StatusIncorrect}
		}
		correctMap := make(map[string]bool, len(correctSet))
		for _, c := range correctSet {
			correctMap[c] = true
		}

		correctSelected := 0
		incorrectSelected := 0
		for _, s := range studentSet {
			if correctMap[s] {
				correctSelected++
			} else {
				incorrectSelected++
			}
		}

		portion := math.Max(0, float64(correctSelected-incorrectSelected))/float64(len(correctSet))
		earned := portion * qWeight

		switch {
		case portion >= 1.0:
			return EvaluationDetail{Earned: earned, StatusText: StatusCorrect, StatusClass: StatusCorrect}
		case portion > 0:
			return EvaluationDetail{Earned: earned, StatusText: StatusPartial, StatusClass: StatusPartial}
		default:
			return EvaluationDetail{Earned: 0, StatusText: StatusIncorrect, StatusClass: StatusIncorrect}
		}
	}

	// Exact match scoring.
	if len(studentSet) != len(correctSet) {
		return EvaluationDetail{Earned: 0, StatusText: StatusIncorrect, StatusClass: StatusIncorrect}
	}
	sCopy := make([]string, len(studentSet))
	cCopy := make([]string, len(correctSet))
	copy(sCopy, studentSet)
	copy(cCopy, correctSet)
	sortStrings(sCopy)
	sortStrings(cCopy)

	for i := range sCopy {
		if sCopy[i] != cCopy[i] {
			return EvaluationDetail{Earned: 0, StatusText: StatusIncorrect, StatusClass: StatusIncorrect}
		}
	}
	return EvaluationDetail{Earned: qWeight, StatusText: StatusCorrect, StatusClass: StatusCorrect}
}

func evaluateMatching(studentMap, correctMap map[string]interface{}, qWeight float64, partialScoring bool) EvaluationDetail {
	if partialScoring {
		if len(correctMap) == 0 {
			return EvaluationDetail{Earned: 0, StatusText: StatusIncorrect, StatusClass: StatusIncorrect}
		}
		correctMatches := 0
		for k, v := range correctMap {
			sv, ok := studentMap[k]
			if !ok {
				continue
			}
			sNorm := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", sv)))
			cNorm := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", v)))
			if sNorm == cNorm {
				correctMatches++
			}
		}
		portion := float64(correctMatches) / float64(len(correctMap))
		earned := portion * qWeight

		switch {
		case portion >= 1.0:
			return EvaluationDetail{Earned: earned, StatusText: StatusCorrect, StatusClass: StatusCorrect}
		case portion > 0:
			return EvaluationDetail{Earned: earned, StatusText: StatusPartial, StatusClass: StatusPartial}
		default:
			return EvaluationDetail{Earned: 0, StatusText: StatusIncorrect, StatusClass: StatusIncorrect}
		}
	}

	// Exact match: all key-value pairs must match.
	for k, v := range correctMap {
		sv, ok := studentMap[k]
		if !ok {
			return EvaluationDetail{Earned: 0, StatusText: StatusIncorrect, StatusClass: StatusIncorrect}
		}
		sNorm := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", sv)))
		cNorm := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", v)))
		if sNorm != cNorm {
			return EvaluationDetail{Earned: 0, StatusText: StatusIncorrect, StatusClass: StatusIncorrect}
		}
	}
	return EvaluationDetail{Earned: qWeight, StatusText: StatusCorrect, StatusClass: StatusCorrect}
}

// sortStrings sorts a string slice in place.
func sortStrings(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

// EvaluateAnswersDetailed evaluates all student answers against questions.
// Returns a map keyed by question number.
func EvaluateAnswersDetailed(answers map[string]interface{}, questions []Question) map[string]EvaluationDetail {
	result := make(map[string]EvaluationDetail)
	if len(questions) == 0 {
		return result
	}

	for _, q := range questions {
		qNum := normalizeQNum(q.Number)
		studentAns := answers[qNum]
		correctAns := q.GetKey()
		qWeight := q.GetWeight()

		detail := evaluateSingleQuestion(studentAns, correctAns, q.Type, qWeight, q.PartialScoring)
		result[qNum] = detail
	}
	return result
}

// CalculateSubmissionScore computes the total score for a submission.
// Returns nil if questions are empty.
func CalculateSubmissionScore(answers map[string]interface{}, questions []Question) *float64 {
	if len(questions) == 0 {
		return nil
	}
	evaluation := EvaluateAnswersDetailed(answers, questions)
	var total float64
	for _, ev := range evaluation {
		total += ev.Earned
	}
	total = math.Round(total*100) / 100
	return &total
}

// ComputeMaxScore calculates the maximum possible score for a set of questions.
func ComputeMaxScore(questions []Question) float64 {
	var total float64
	for _, q := range questions {
		total += q.GetWeight()
	}
	return total
}

// ParseQuestionsJSON parses the questions JSON string into a slice of Question.
func ParseQuestionsJSON(raw *string) ([]Question, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	var questions []Question
	if err := json.Unmarshal([]byte(*raw), &questions); err != nil {
		return nil, fmt.Errorf("parse questions json: %w", err)
	}
	return questions, nil
}

// ParseAnswersJSON parses the answers JSON string into a map.
func ParseAnswersJSON(raw *string) (map[string]interface{}, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	var answers map[string]interface{}
	if err := json.Unmarshal([]byte(*raw), &answers); err != nil {
		return nil, fmt.Errorf("parse answers json: %w", err)
	}
	return answers, nil
}

// ParseIdentityDataJSON parses the identity_data JSON string into a map.
func ParseIdentityDataJSON(raw *string) (map[string]interface{}, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(*raw), &data); err != nil {
		return nil, fmt.Errorf("parse identity data json: %w", err)
	}
	return data, nil
}

// ===== DB Operations =====

const defaultSubmissionColumns = `id, exam_id, student_name, exam_number, student_class,
answers_json, score, start_time, mac_address, created_at, identity_data`

func scanSubmission(row pgx.Row) (Submission, error) {
	var s Submission
	err := row.Scan(
		&s.ID, &s.ExamID, &s.StudentName, &s.ExamNumber, &s.StudentClass,
		&s.AnswersJSON, &s.Score, &s.StartTime, &s.MACAddress, &s.CreatedAt, &s.IdentityData,
	)
	return s, err
}

// EnsureFreshSubmissionOnApproval grants an approved device its entry
// bookkeeping row: it creates a fresh submissions row when none exists for
// (exam, device) or when the latest one is already completed (answered). This
// is shared by the manual approval endpoint and the server-side auto-approve
// so an approved device shows up in the pengawas monitoring table. It is
// idempotent — an in-progress open row is left untouched, and repeated calls
// (e.g. the client's 5s approval poll) never create duplicates.
//
// A per-device advisory lock serialises concurrent calls, so two racing
// requests (e.g. an overlapping poll and retry) cannot both observe "no open
// row yet" and insert duplicate rows.
func EnsureFreshSubmissionOnApproval(ctx context.Context, pool *pgxpool.Pool, examID int, macAddress string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialise by (exam, device): all approval bookkeeping for the same
	// device runs one at a time, so the check-then-insert below is race-free.
	// The lock is released automatically when the transaction ends.
	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`,
		fmt.Sprintf("approval:%d:%s", examID, macAddress)); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}

	var latestAnswers *string
	err = tx.QueryRow(ctx,
		`SELECT answers_json FROM submissions
		 WHERE exam_id = $1 AND mac_address = $2
		 ORDER BY created_at DESC LIMIT 1`, examID, macAddress).Scan(&latestAnswers)

	switch {
	case err == pgx.ErrNoRows:
		// No row yet — create the fresh entry.
	case err == nil && latestAnswers != nil && *latestAnswers != "":
		// Latest attempt is completed — a new entry starts a new attempt row.
	default:
		if err != nil {
			return fmt.Errorf("lookup latest submission: %w", err)
		}
		// An open (in-progress) row already exists — nothing to do.
		return tx.Commit(ctx)
	}

	// Copy identity fields from the approval row so the monitoring table shows
	// the same student data the device supplied.
	_, err = tx.Exec(ctx, `
		INSERT INTO submissions (exam_id, mac_address, student_name, exam_number, student_class, identity_data, start_time, created_at)
		SELECT exam_id, mac_address, student_name, exam_number, student_class, identity_data, $3, CURRENT_TIMESTAMP
		FROM exam_approvals WHERE exam_id = $1 AND mac_address = $2`,
		examID, macAddress, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("insert submission on approval: %w", err)
	}
	return tx.Commit(ctx)
}

// CreateSubmission inserts a new submission. If the exam has questions_json,
// auto-scoring is performed and the score is saved.
func CreateSubmission(ctx context.Context, pool *pgxpool.Pool, s *Submission) (*Submission, error) {
	// Try to auto-score.
	if s.Score == nil {
		questionsRow, err := GetExamByID(ctx, pool, s.ExamID)
		if err == nil && questionsRow.QuestionsJSON != nil && *questionsRow.QuestionsJSON != "" {
			questions, parseErr := ParseQuestionsJSON(questionsRow.QuestionsJSON)
			if parseErr == nil && len(questions) > 0 {
				answers, parseErr := ParseAnswersJSON(s.AnswersJSON)
				if parseErr == nil {
					score := CalculateSubmissionScore(answers, questions)
					s.Score = score
				}
			}
		}
	}

	// The select-latest-then-update/insert below is a CHECK-THEN-ACT: two
	// concurrent submits from the same device (a double-tap, or a retry racing
	// the first attempt) could BOTH find no latest row and both INSERT,
	// duplicating the student in the admin monitoring table and the hasil
	// page. Serialize per (exam, device) with the SAME advisory lock the
	// approval bookkeeping uses (EnsureFreshSubmissionOnApproval) — this also
	// closes the cross-path race where an approval's placeholder INSERT and a
	// submit's INSERT/UPDATE for the same device overlap.
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("create/update submission: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit

	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`,
		fmt.Sprintf("approval:%d:%s", s.ExamID, s.MACAddress)); err != nil {
		return nil, fmt.Errorf("create/update submission: advisory lock: %w", err)
	}

	// Try to update the student's LATEST row for this device — the open
	// (placeholder) row when a fresh attempt is in progress, or the already-
	// submitted row when this call is a RETRY of a submission whose first
	// attempt was persisted but whose response never reached the client.
	//
	// Idempotency contract: the match is scoped to (exam, device, exam_number)
	// WITHOUT an answers_json filter. The old code only matched the open row,
	// so a retry of an already-persisted submit found nothing and INSERTed a
	// second completed row — the same student showed up twice in the admin
	// monitoring table with duplicate scores. When the student has an exam
	// number, the match is scoped to a row carrying that same number
	// (placeholders store it), so two students who share one device still get
	// two separate rows. When the student has no exam number, any latest row on
	// the device is targeted, preserving the one-device-one-row behaviour for
	// exams that don't assign numbers.
	var existingID int
	err = tx.QueryRow(ctx, `SELECT id FROM submissions
		WHERE exam_id = $1 AND mac_address = $2
		  AND ($3 = '' OR exam_number = $3)
		ORDER BY created_at DESC LIMIT 1`, s.ExamID, s.MACAddress, s.ExamNumber).Scan(&existingID)

	var created Submission
	if err == nil {
		// Update existing row
		created, err = scanSubmission(tx.QueryRow(ctx, `UPDATE submissions
		SET answers_json = $1, score = $2, start_time = COALESCE(start_time, $3), student_name = $4, exam_number = $5, student_class = $6, identity_data = $7
		WHERE id = $8
		RETURNING `+defaultSubmissionColumns,
			s.AnswersJSON, s.Score, s.StartTime, s.StudentName, s.ExamNumber, s.StudentClass, s.IdentityData,
			existingID,
		))
	} else {
		// Insert new row
		created, err = scanSubmission(tx.QueryRow(ctx, `INSERT INTO submissions
		(exam_id, student_name, exam_number, student_class, answers_json, score, start_time, mac_address, identity_data)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING `+defaultSubmissionColumns,
			s.ExamID, s.StudentName, s.ExamNumber, s.StudentClass,
			s.AnswersJSON, s.Score, s.StartTime, s.MACAddress, s.IdentityData,
		))
	}
	if err != nil {
		return nil, fmt.Errorf("create/update submission: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("create/update submission: commit: %w", err)
	}
	return &created, nil
}

// GetSubmissionByID retrieves a single submission by primary key.
func GetSubmissionByID(ctx context.Context, pool *pgxpool.Pool, id int) (Submission, error) {
	sql := `SELECT ` + defaultSubmissionColumns + ` FROM submissions WHERE id = $1`
	return scanSubmission(pool.QueryRow(ctx, sql, id))
}

// GetLatestSubmissionByIdentity returns the most recent submission for a
// student in an exam, matched by their device identifier and, when present,
// their identity payload. It is the DB-side fallback for an async submission
// job whose Redis result key has already expired (TTL): the submit call wrote
// mac_address + identity_data, so an arriving student can poll the result by
// supplying those same values. The identity_data match is deliberately exact
// JSON-equality on the serialised bytes — the client reproduces the same map,
// and the queue worker stored it verbatim, so the round trip is stable.
//
// Returns pgx.ErrNoRows when no submission matches.
func GetLatestSubmissionByIdentity(ctx context.Context, pool *pgxpool.Pool, examID int, macAddress, identityData string) (Submission, error) {
	sql := `SELECT ` + defaultSubmissionColumns + ` FROM submissions
	WHERE exam_id = $1 AND mac_address = $2
	  AND ($3 = '' OR identity_data = $3)
	ORDER BY created_at DESC LIMIT 1`
	sub, err := scanSubmission(pool.QueryRow(ctx, sql, examID, macAddress, identityData))
	if err != nil {
		return Submission{}, err
	}
	return sub, nil
}

// GetSubmissionDetail retrieves a submission joined with exam data.
func GetSubmissionDetail(ctx context.Context, pool *pgxpool.Pool, id int) (SubmissionWithExam, error) {
	var s SubmissionWithExam
	err := pool.QueryRow(ctx, `SELECT s.id, s.exam_id, s.student_name, s.exam_number, s.student_class,
s.answers_json, s.score, s.start_time, s.mac_address, s.created_at, s.identity_data,
e.name as exam_name, e.questions_json
FROM submissions s JOIN exams e ON s.exam_id = e.id
WHERE s.id = $1`, id).Scan(
		&s.ID, &s.ExamID, &s.StudentName, &s.ExamNumber, &s.StudentClass,
		&s.AnswersJSON, &s.Score, &s.StartTime, &s.MACAddress, &s.CreatedAt, &s.IdentityData,
		&s.ExamName, &s.QuestionsJSON,
	)
	if err != nil {
		return s, fmt.Errorf("get submission detail: %w", err)
	}

	// Compute max_score and percentage.
	if s.QuestionsJSON != nil && *s.QuestionsJSON != "" {
		questions, parseErr := ParseQuestionsJSON(s.QuestionsJSON)
		if parseErr == nil {
			maxScore := ComputeMaxScore(questions)
			s.MaxScore = &maxScore
			if s.Score != nil && maxScore > 0 {
				pct := math.Round(*s.Score/maxScore*100*10) / 10
				s.ScorePct = &pct
			}
		}
	}
	return s, nil
}

// ListSubmissionsByExamOpts holds options for listing submissions for an exam.
type ListSubmissionsByExamOpts struct {
	ExamID   int
	Page     int
	PerPage  int
	Search   string
	Status   string
}

// ListSubmissionsResult holds paginated submissions and stats.
type ListSubmissionsResult struct {
	Submissions []Submission
	Total       int
	TotalPages  int
	Page        int
	PerPage     int
	Stats       SubmissionStats
}

// SubmissionStats aggregates counts for a given exam.
type SubmissionStats struct {
	Total       int `json:"total"`
	Submitted   int `json:"submitted"`
	InProgress  int `json:"active"`
	NotStarted  int `json:"not_started"`
}

// ListSubmissionsByExam returns paginated submissions for a specific exam.
func ListSubmissionsByExam(ctx context.Context, pool *pgxpool.Pool, opts ListSubmissionsByExamOpts) (ListSubmissionsResult, error) {
	if opts.Page < 1 {
		opts.Page = 1
	}
	perPage := clampPerPage(opts.PerPage)

	var conditions []string
	var args []interface{}
	argIdx := 1

	conditions = append(conditions, fmt.Sprintf(`exam_id = $%d`, argIdx))
	args = append(args, opts.ExamID)
	argIdx++

	if opts.Search != "" {
		pat := "%" + opts.Search + "%"
		conditions = append(conditions,
			fmt.Sprintf(`(student_name ILIKE $%d OR mac_address ILIKE $%d OR student_class ILIKE $%d OR exam_number ILIKE $%d)`,
				argIdx, argIdx+1, argIdx+2, argIdx+3))
		args = append(args, pat, pat, pat, pat)
		argIdx += 4
	}

	if opts.Status != "" {
		if opts.Status == "submitted" {
			conditions = append(conditions, "answers_json IS NOT NULL AND answers_json != ''")
		} else if opts.Status == "in_progress" {
			conditions = append(conditions, "(answers_json IS NULL OR answers_json = '') AND start_time IS NOT NULL")
		} else if opts.Status == "not_started" {
			conditions = append(conditions, "(answers_json IS NULL OR answers_json = '') AND start_time IS NULL")
		}
	}

	where := " WHERE " + joinConditions(conditions, " AND ")

	// Count.
	countSQL := `
		SELECT COUNT(*) FROM (
			SELECT DISTINCT ON (mac_address) id 
			FROM submissions ` + where + `
		) AS sub`
	var total int
	err := pool.QueryRow(ctx, countSQL, args...).Scan(&total)
	if err != nil {
		return ListSubmissionsResult{}, fmt.Errorf("count submissions: %w", err)
	}

	totalPages := calcTotalPages(total, perPage)
	if opts.Page > totalPages && total > 0 {
		opts.Page = totalPages
	}
	offset := calcOffset(opts.Page, perPage)

	// Fetch data.
	sql := `
		SELECT ` + defaultSubmissionColumns + ` 
		FROM (
			SELECT DISTINCT ON (mac_address) ` + defaultSubmissionColumns + `
			FROM submissions
			` + where + `
			ORDER BY mac_address, created_at DESC
		) AS latest_subs
		ORDER BY student_name ASC LIMIT $` + fmt.Sprintf("%d", argIdx) + ` OFFSET $` + fmt.Sprintf("%d", argIdx+1)
	args = append(args, perPage, offset)

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return ListSubmissionsResult{}, fmt.Errorf("list submissions: %w", err)
	}
	defer rows.Close()

	var subs []Submission
	for rows.Next() {
		s, err := scanSubmission(rows)
		if err != nil {
			return ListSubmissionsResult{}, fmt.Errorf("scan submission: %w", err)
		}
		subs = append(subs, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}

	if subs == nil {
		subs = []Submission{}
	}

	// Compute stats across all submissions (ignoring search/pagination).
	stats, err := GetSubmissionStats(ctx, pool, opts.ExamID)
	if err != nil {
		stats = SubmissionStats{}
	}

	return ListSubmissionsResult{
		Submissions: subs,
		Total:       total,
		TotalPages:  totalPages,
		Page:        opts.Page,
		PerPage:     perPage,
		Stats:       stats,
	}, nil
}

// GetSubmissionStats returns aggregate counts for a specific exam.
func GetSubmissionStats(ctx context.Context, pool *pgxpool.Pool, examID int) (SubmissionStats, error) {
	var stats SubmissionStats

	// Deduplicate by device (DISTINCT ON mac_address, latest row) so the stat
	// cards match the deduplicated submissions list, and COALESCE the SUMs so
	// an exam with zero submissions does not fail the scan (SUM -> NULL).
	err := pool.QueryRow(ctx,
		`WITH latest AS (
			SELECT DISTINCT ON (mac_address) answers_json, start_time
			FROM submissions
			WHERE exam_id = $1
			ORDER BY mac_address, created_at DESC
		)
		SELECT
			COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN answers_json IS NOT NULL AND answers_json != '' THEN 1 ELSE 0 END), 0) AS submitted,
			COALESCE(SUM(CASE WHEN (answers_json IS NULL OR answers_json = '') AND start_time IS NOT NULL THEN 1 ELSE 0 END), 0) AS in_progress
		 FROM latest`, examID).Scan(&stats.Total, &stats.Submitted, &stats.InProgress)
	if err != nil {
		return stats, fmt.Errorf("stats: %w", err)
	}

	stats.NotStarted = stats.Total - stats.Submitted - stats.InProgress
	return stats, nil
}

// UpdateSubmissionScore recalculates and updates the score for a specific submission.
func UpdateSubmissionScore(ctx context.Context, pool *pgxpool.Pool, submissionID int) error {
	sub, err := GetSubmissionByID(ctx, pool, submissionID)
	if err != nil {
		return fmt.Errorf("update score: get submission: %w", err)
	}

	exam, err := GetExamByID(ctx, pool, sub.ExamID)
	if err != nil {
		return fmt.Errorf("update score: get exam: %w", err)
	}

	if exam.QuestionsJSON == nil || *exam.QuestionsJSON == "" {
		return nil
	}

	questions, err := ParseQuestionsJSON(exam.QuestionsJSON)
	if err != nil {
		return fmt.Errorf("update score: parse questions: %w", err)
	}

	answers, err := ParseAnswersJSON(sub.AnswersJSON)
	if err != nil {
		return nil // no answers to score
	}

	// Only recalculate if there are questions and answers.
	if len(questions) > 0 && answers != nil {
		score := CalculateSubmissionScore(answers, questions)
		_, err = pool.Exec(ctx, `UPDATE submissions SET score = $1 WHERE id = $2`, score, submissionID)
		if err != nil {
			return fmt.Errorf("update score: exec: %w", err)
		}
	}
	return nil
}

// RecalculateAllScoresForExam recalculates scores for all submissions of an exam.
func RecalculateAllScoresForExam(ctx context.Context, pool *pgxpool.Pool, examID int) error {
	exam, err := GetExamByID(ctx, pool, examID)
	if err != nil {
		return fmt.Errorf("recalculate scores: get exam: %w", err)
	}

	questions, err := ParseQuestionsJSON(exam.QuestionsJSON)
	if err != nil {
		return fmt.Errorf("recalculate scores: parse questions: %w", err)
	}

	if len(questions) == 0 {
		return nil
	}

	// Fetch all submissions for this exam.
	rows, err := pool.Query(ctx,
		`SELECT id, answers_json FROM submissions WHERE exam_id = $1 ORDER BY id`, examID)
	if err != nil {
		return fmt.Errorf("recalculate scores: query submissions: %w", err)
	}
	defer rows.Close()

	type scoreUpdate struct {
		ID    int
		Score *float64
	}
	var updates []scoreUpdate

	for rows.Next() {
		var id int
		var answersRaw *string
		if err := rows.Scan(&id, &answersRaw); err != nil {
			return fmt.Errorf("recalculate scores: scan: %w", err)
		}
		answers, _ := ParseAnswersJSON(answersRaw)
		if answers == nil {
			answers = map[string]interface{}{}
		}
		score := CalculateSubmissionScore(answers, questions)
		updates = append(updates, scoreUpdate{ID: id, Score: score})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}

	// Batch update via VALUES.
	if len(updates) > 0 {
		valueClauses := make([]string, 0, len(updates))
		batchArgs := make([]interface{}, 0, len(updates)*2)
		for i, u := range updates {
			valueClauses = append(valueClauses,
				fmt.Sprintf("($%d, $%d)", i*2+1, i*2+2))
			batchArgs = append(batchArgs, u.Score, u.ID)
		}
		sql := `UPDATE submissions SET score = v.score FROM (VALUES ` +
			strings.Join(valueClauses, ", ") +
			`) AS v(score, id) WHERE submissions.id = v.id`
		if _, err := pool.Exec(ctx, sql, batchArgs...); err != nil {
			return fmt.Errorf("recalculate scores: batch update: %w", err)
		}
	}

	return nil
}

// DeleteSubmission removes a submission by ID.
func DeleteSubmission(ctx context.Context, pool *pgxpool.Pool, id int) error {
	_, err := pool.Exec(ctx, `DELETE FROM submissions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete submission: %w", err)
	}
	return nil
}

// DeleteSubmissionsByExam removes all submissions for a given exam.
func DeleteSubmissionsByExam(ctx context.Context, pool *pgxpool.Pool, examID int) error {
	_, err := pool.Exec(ctx, `DELETE FROM submissions WHERE exam_id = $1`, examID)
	if err != nil {
		return fmt.Errorf("delete submissions by exam: %w", err)
	}
	return nil
}
