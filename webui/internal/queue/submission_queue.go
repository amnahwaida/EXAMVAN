// Package queue implements an async submission processing queue powered by
// Redis Lists.
//
// Usage:
//
//	rdb := redis.Connect(ctx, cfg.RedisURL)
//	pool := database.Connect(cfg)
//
//	// Start the worker in a background goroutine.
//	go queue.StartWorker(rdb, pool)
//
//	// Enqueue a submission.
//	jobID, err := queue.EnqueueSubmission(rdb, data)
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// QueueKey is the Redis list key for pending submissions.
	QueueKey = "examvan:submissions:pending"

	// ResultKeyPrefix is the prefix for storing job results in Redis.
	ResultKeyPrefix = "examvan:submissions:result:"

	// pubSubChannel is the Redis channel for broadcasting completed submissions.
	pubSubChannel = "examvan:submissions:completed"

	// resultTTL is the TTL for stored job results (5 minutes).
	resultTTL = 5 * time.Minute

	// defaultPollTimeout is how long BRPOP waits before retrying.
	defaultPollTimeout = 5 * time.Second

	// maxRetriesPerJob is the number of times a failed job is retried.
	maxRetriesPerJob = 3
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// SubmissionJob is the payload enqueued in the Redis list.
type SubmissionJob struct {
	JobID       string                 `json:"job_id"`
	ExamID      int                    `json:"exam_id"`
	StudentName string                 `json:"student_name"`
	ExamNumber  string                 `json:"exam_number"`
	StudentClass string                `json:"student_class"`
	Answers     map[string]interface{} `json:"answers"`
	MACAddress  string                 `json:"mac_address"`
	IdentityData map[string]interface{} `json:"identity_data,omitempty"`
	Retries     int                    `json:"retries"`
	EnqueuedAt  string                 `json:"enqueued_at"` // ISO 8601 UTC
}

// JobResult is stored in Redis after a job is processed.
type JobResult struct {
	JobID       string   `json:"job_id"`
	Success     bool     `json:"success"`
	Score       *float64 `json:"score,omitempty"`
	Message     string   `json:"message"`
	ProcessedAt string   `json:"processed_at"`
}

// isRedisUnavailable checks whether the Redis client is nil.
func isRedisUnavailable(rdb *goredis.Client) bool {
	return rdb == nil
}

// ---------------------------------------------------------------------------
// Enqueue
// ---------------------------------------------------------------------------

// EnqueueSubmission pushes a submission job onto the Redis queue.
// Returns a generated jobID and any error. If Redis is unavailable, it
// returns an error immediately.
func EnqueueSubmission(rdb *goredis.Client, data map[string]interface{}) (string, error) {
	if isRedisUnavailable(rdb) {
		return "", errors.New("queue: Redis unavailable")
	}

	jobID := generateJobID()
	now := time.Now().UTC().Format(time.RFC3339)

	job := SubmissionJob{
		JobID:       jobID,
		ExamID:      extractInt(data, "exam_id"),
		StudentName: extractString(data, "student_name"),
		ExamNumber:  extractString(data, "exam_number"),
		StudentClass: extractString(data, "student_class"),
		MACAddress:  extractString(data, "mac_address"),
		EnqueuedAt:  now,
		Answers:     extractMap(data, "answers"),
	}

	payload, err := json.Marshal(job)
	if err != nil {
		return "", fmt.Errorf("queue: marshal job: %w", err)
	}

	ctx := context.Background()
	if err := rdb.LPush(ctx, QueueKey, payload).Err(); err != nil {
		return "", fmt.Errorf("queue: LPUSH: %w", err)
	}

	log.Printf("queue: enqueued submission job %s for exam %d", jobID, job.ExamID)
	return jobID, nil
}

// EnqueueSubmissionWithJob allows callers to control the full job struct.
// This is useful when retrying failed jobs.
func EnqueueSubmissionWithJob(rdb *goredis.Client, job *SubmissionJob) error {
	if isRedisUnavailable(rdb) {
		return errors.New("queue: Redis unavailable")
	}

	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("queue: marshal retry job: %w", err)
	}

	ctx := context.Background()
	if err := rdb.LPush(ctx, QueueKey, payload).Err(); err != nil {
		return fmt.Errorf("queue: LPUSH retry: %w", err)
	}

	log.Printf("queue: re-enqueued job %s (retry %d)", job.JobID, job.Retries)
	return nil
}

// ---------------------------------------------------------------------------
// Worker
// ---------------------------------------------------------------------------

// Worker processes jobs from the submission queue. It reads exam questions,
// scores the answers, inserts the submission into PostgreSQL, stores the
// result in Redis, and publishes a notification via Redis Pub/Sub.
type Worker struct {
	rdb  *goredis.Client
	pool *pgxpool.Pool
	quit chan struct{}
}

// StartWorker launches a background goroutine that continuously processes
// submission jobs from the Redis queue. It blocks until the queue is
// exhausted or Stop() is called. Call StartWorker in a goroutine.
//
//	pool := database.Connect(cfg)
//	rdb := redis.Connect(ctx, cfg.RedisURL)
//	go queue.StartWorker(rdb, pool)
func StartWorker(rdb *goredis.Client, pool *pgxpool.Pool) {
	w := &Worker{
		rdb:  rdb,
		pool: pool,
		quit: make(chan struct{}),
	}
	w.run()
}

// run is the main loop. It blocks on BRPOP and processes each job.
func (w *Worker) run() {
	if isRedisUnavailable(w.rdb) {
		log.Printf("queue: Redis unavailable — worker not started")
		return
	}

	log.Printf("queue: worker started (polling %s)", QueueKey)

	ctx := context.Background()

	for {
		select {
		case <-w.quit:
			log.Printf("queue: worker stopped")
			return
		default:
			// BRPOP with a timeout blocks until a job arrives.
			result, err := w.rdb.BRPop(ctx, defaultPollTimeout, QueueKey).Result()
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					continue
				}
				if errors.Is(err, goredis.Nil) {
					continue
				}
				log.Printf("queue: BRPOP error: %v", err)
				time.Sleep(time.Second)
				continue
			}

			// result[0] = key name, result[1] = value.
			if len(result) < 2 {
				continue
			}
			payload := []byte(result[1])

			var job SubmissionJob
			if err := json.Unmarshal(payload, &job); err != nil {
				log.Printf("queue: unmarshal job error: %v", err)
				continue
			}

			w.processJob(ctx, job)
		}
	}
}

// Stop signals the worker to shut down after the current job finishes.
func (w *Worker) Stop() {
	close(w.quit)
}

// processJob handles a single submission job.
func (w *Worker) processJob(ctx context.Context, job SubmissionJob) {
	start := time.Now()
	log.Printf("queue: processing job %s (exam %d, student %s)", job.JobID, job.ExamID, job.StudentName)

	// Set worker heartbeat (30s TTL)
	if !isRedisUnavailable(w.rdb) {
		w.rdb.Set(ctx, "examvan:submissions:worker_heartbeat", time.Now().UTC().Format(time.RFC3339), 30*time.Second)
	}

	result := w.doProcess(ctx, &job)
	elapsed := time.Since(start)

	// Store result in Redis.
	if isRedisUnavailable(w.rdb) {
		log.Printf("queue: Redis unavailable — cannot store result for %s", job.JobID)
		return
	}

	resultPayload, err := json.Marshal(result)
	if err != nil {
		log.Printf("queue: marshal result error: %v", err)
		return
	}

	resultKey := ResultKeyPrefix + job.JobID
	if err := w.rdb.Set(ctx, resultKey, resultPayload, resultTTL).Err(); err != nil {
		log.Printf("queue: store result error: %v", err)
	}

	// Publish to Pub/Sub channel.
	if err := w.rdb.Publish(ctx, pubSubChannel, resultPayload).Err(); err != nil {
		log.Printf("queue: publish error: %v", err)
	}

	// Update stats counters.
	statsKey := "examvan:submissions:stats"
	if result.Success {
		w.rdb.HIncrBy(ctx, statsKey, "processed", 1)
	} else {
		w.rdb.HIncrBy(ctx, statsKey, "errored", 1)
	}

	log.Printf("queue: completed job %s in %v (success=%v, score=%v)", job.JobID, elapsed, result.Success, result.Score)
}

// doProcess performs the actual work: fetch exam, parse questions, score
// answers, insert submission into PostgreSQL.
func (w *Worker) doProcess(ctx context.Context, job *SubmissionJob) JobResult {
	// Fetch exam questions.
	exam, err := w.fetchExam(ctx, job.ExamID)
	if err != nil {
		log.Printf("queue: fetch exam %d: %v", job.ExamID, err)
		return w.fail(job.JobID, fmt.Sprintf("fetch exam: %v", err))
	}

	// Parse questions.
	questions, err := parseQuestionsJSON(exam.QuestionsJSON)
	if err != nil {
		log.Printf("queue: parse questions: %v", err)
		return w.fail(job.JobID, fmt.Sprintf("parse questions: %v", err))
	}

	// Calculate score.
	var score *float64
	if len(questions) > 0 {
		s := calculateScore(job.Answers, questions)
		score = &s
	}

	// Insert submission into PostgreSQL.
	submissionID, err := w.insertSubmission(ctx, job, score)
	if err != nil {
		log.Printf("queue: insert submission: %v", err)
		// Retry logic.
		if job.Retries < maxRetriesPerJob {
			job.Retries++
			log.Printf("queue: retrying job %s (attempt %d/%d)", job.JobID, job.Retries, maxRetriesPerJob)
			if err := EnqueueSubmissionWithJob(w.rdb, job); err != nil {
				return w.fail(job.JobID, fmt.Sprintf("retry failed: %v", err))
			}
			return JobResult{
				JobID:   job.JobID,
				Success: true,
				Message: "retried",
			}
		}
		return w.fail(job.JobID, fmt.Sprintf("insert submission after %d retries: %v", maxRetriesPerJob, err))
	}

	now := time.Now().UTC().Format(time.RFC3339)
	return JobResult{
		JobID:       job.JobID,
		Success:     true,
		Score:       score,
		Message:     fmt.Sprintf("submission %d created", submissionID),
		ProcessedAt: now,
	}
}

// fetchExam retrieves exam data by ID.
func (w *Worker) fetchExam(ctx context.Context, examID int) (*ExamRow, error) {
	if w.pool == nil {
		return nil, errors.New("database pool is nil")
	}

	sql := `SELECT id, questions_json FROM exams WHERE id = $1`
	var exam ExamRow
	err := w.pool.QueryRow(ctx, sql, examID).Scan(&exam.ID, &exam.QuestionsJSON)
	if err != nil {
		return nil, fmt.Errorf("query exam %d: %w", examID, err)
	}
	return &exam, nil
}

// insertSubmission writes the submission record into PostgreSQL and
// returns the new submission ID.
func (w *Worker) insertSubmission(ctx context.Context, job *SubmissionJob, score *float64) (int, error) {
	if w.pool == nil {
		return 0, errors.New("database pool is nil")
	}

	answersJSON, _ := json.Marshal(job.Answers)
	identityJSON, _ := json.Marshal(job.IdentityData)

	var answersPtr, identityPtr *string
	answersStr := string(answersJSON)
	identityStr := string(identityJSON)
	if answersStr != "null" && answersStr != "{}" {
		answersPtr = &answersStr
	}
	if identityStr != "null" && identityStr != "{}" {
		identityPtr = &identityStr
	}

	sql := `INSERT INTO submissions
		(exam_id, student_name, exam_number, student_class, answers_json, score, mac_address, identity_data)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`

	var submissionID int
	err := w.pool.QueryRow(ctx, sql,
		job.ExamID, job.StudentName, job.ExamNumber, job.StudentClass,
		answersPtr, score, job.MACAddress, identityPtr,
	).Scan(&submissionID)
	if err != nil {
		return 0, fmt.Errorf("insert: %w", err)
	}

	return submissionID, nil
}

// fail creates a failed JobResult.
func (w *Worker) fail(jobID, message string) JobResult {
	return JobResult{
		JobID:   jobID,
		Success: false,
		Message: message,
	}
}

// ---------------------------------------------------------------------------
// Queue Stats
// ---------------------------------------------------------------------------

// QueueStats holds queue statistics for monitoring.
type QueueStats struct {
	Pending      int   `json:"pending"`
	Processed    int64 `json:"processed"`
	Errored      int64 `json:"errored"`
	Available    bool  `json:"available"`
	WorkerActive bool  `json:"worker_active"`
}

// GetQueueStats returns current queue statistics.
func GetQueueStats(rdb *goredis.Client) QueueStats {
	stats := QueueStats{Available: false, WorkerActive: false}
	if isRedisUnavailable(rdb) {
		return stats
	}

	ctx := context.Background()
	stats.Available = true

	// Count pending jobs
	pending, err := rdb.LLen(ctx, QueueKey).Result()
	if err == nil {
		stats.Pending = int(pending)
	}

	// Get processed/errored counts from stats hash
	processed, err := rdb.HGet(ctx, "examvan:submissions:stats", "processed").Int64()
	if err == nil {
		stats.Processed = processed
	}
	errored, err := rdb.HGet(ctx, "examvan:submissions:stats", "errored").Int64()
	if err == nil {
		stats.Errored = errored
	}

	// Check if worker is active (heartbeat key exists)
	exists, err := rdb.Exists(ctx, "examvan:submissions:worker_heartbeat").Result()
	if err == nil && exists > 0 {
		stats.WorkerActive = true
	}

	return stats
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// ExamRow is a minimal exam struct for the queue worker.
type ExamRow struct {
	ID            int
	QuestionsJSON *string
}

// questionItem is a lightweight question struct for scoring in the queue
// (no dependency on the models package).
type questionItem struct {
	Number         interface{} `json:"number"`
	Key            interface{} `json:"key"`
	Answer         interface{} `json:"answer"`
	Type           string      `json:"type"`
	Weight         float64     `json:"weight"`
	Score          float64     `json:"score"`
	PartialScoring bool        `json:"partial_scoring"`
}

func (q *questionItem) getWeight() float64 {
	if q.Weight > 0 {
		return q.Weight
	}
	if q.Score > 0 {
		return q.Score
	}
	return 1.0
}

func parseQuestionsJSON(raw *string) ([]questionItem, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	var questions []questionItem
	if err := json.Unmarshal([]byte(*raw), &questions); err != nil {
		return nil, err
	}
	return questions, nil
}

func calculateScore(answers map[string]interface{}, questions []questionItem) float64 {
	var total float64
	for _, q := range questions {
		qNum := normalizeQNum(q.Number)
		studentAns := answers[qNum]
		correctAns := q.Key
		if correctAns == nil {
			correctAns = q.Answer
		}
		earned, _, _ := evaluateSingleQuestion(studentAns, correctAns, q.Type, q.getWeight(), q.PartialScoring)
		total += earned
	}
	return total
}

// generateJobID creates a unique job identifier using timestamp + random.
func generateJobID() string {
	now := time.Now().UnixNano()
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 12)
	for i := 0; i < 8; i++ {
		b[i] = charset[(now>>(i*4))%36]
		now /= 36
	}
	// Add 4 random chars.
	for i := 8; i < 12; i++ {
		b[i] = charset[(now+int64(i)*7)%36]
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// Utility extractors
// ---------------------------------------------------------------------------

func extractInt(data map[string]interface{}, key string) int {
	if v, ok := data[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		case json.Number:
			i, _ := n.Int64()
			return int(i)
		}
	}
	return 0
}

func extractString(data map[string]interface{}, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func extractMap(data map[string]interface{}, key string) map[string]interface{} {
	if v, ok := data[key]; ok {
		if m, ok := v.(map[string]interface{}); ok {
			return m
		}
	}
	return map[string]interface{}{}
}

// ---------------------------------------------------------------------------
// Scoring (minimal copy — no dependency on models or helpers)
// ---------------------------------------------------------------------------

func normalizeQNum(number interface{}) string {
	switch v := number.(type) {
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%.0f", v)
		}
		return fmt.Sprintf("%v", v)
	case string:
		return v
	case int, int64, int32:
		return fmt.Sprintf("%d", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func evaluateSingleQuestion(studentAns, correctAns interface{}, qType string, qWeight float64, partialScoring bool) (float64, string, string) {
	if studentAns == nil || correctAns == nil {
		if studentAns == nil {
			return 0, "unanswered", "unanswered"
		}
		return 0, "incorrect", "incorrect"
	}

	switch qType {
	case "single_choice", "true_false", "short_answer":
		sNorm := joinFields(fmt.Sprintf("%v", studentAns))
		cNorm := joinFields(fmt.Sprintf("%v", correctAns))
		if sNorm == cNorm {
			return qWeight, "correct", "correct"
		}
		return 0, "incorrect", "incorrect"

	case "multiple_choice":
		studentList, sOK := studentAns.([]interface{})
		correctList, cOK := correctAns.([]interface{})
		if !sOK || !cOK {
			// Treat as space-separated string.
			sFields := splitFields(fmt.Sprintf("%v", studentAns))
			cFields := splitFields(fmt.Sprintf("%v", correctAns))
			return evaluateMC(sFields, cFields, qWeight, partialScoring)
		}
		sStr := make([]string, len(studentList))
		cStr := make([]string, len(correctList))
		for i, v := range studentList {
			sStr[i] = stringsToUpper(fmt.Sprintf("%v", v))
		}
		for i, v := range correctList {
			cStr[i] = stringsToUpper(fmt.Sprintf("%v", v))
		}
		return evaluateMC(sStr, cStr, qWeight, partialScoring)

	case "matching":
		studentMap, sOK := studentAns.(map[string]interface{})
		correctMap, cOK := correctAns.(map[string]interface{})
		if !sOK || !cOK {
			return 0, "incorrect", "incorrect"
		}
		return evaluateMatching(studentMap, correctMap, qWeight, partialScoring)
	}

	return 0, "incorrect", "incorrect"
}

func joinFields(s string) string {
	return joinFieldsInternal(s)
}

func joinFieldsInternal(s string) string {
	in := []byte(s)
	out := make([]byte, 0, len(in))
	space := false
	for _, c := range in {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if !space {
				out = append(out, ' ')
				space = true
			}
		} else {
			out = append(out, c)
			space = false
		}
	}
	// Trim leading/trailing.
	result := string(out)
	if len(result) > 0 && result[0] == ' ' {
		result = result[1:]
	}
	if len(result) > 0 && result[len(result)-1] == ' ' {
		result = result[:len(result)-1]
	}
	return stringsToUpper(result)
}

func splitFields(s string) []string {
	return splitFieldsInternal(s)
}

func splitFieldsInternal(s string) []string {
	s = stringsToUpper(s)
	in := []byte(s)
	var result []string
	var cur []byte
	inField := false
	for _, c := range in {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if inField && len(cur) > 0 {
				result = append(result, string(cur))
				cur = cur[:0]
			}
			inField = false
		} else {
			cur = append(cur, c)
			inField = true
		}
	}
	if inField && len(cur) > 0 {
		result = append(result, string(cur))
	}
	return result
}

func evaluateMC(studentSet, correctSet []string, qWeight float64, partialScoring bool) (float64, string, string) {
	if partialScoring {
		if len(correctSet) == 0 {
			return 0, "incorrect", "incorrect"
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
		portion := float64(correctSelected-incorrectSelected) / float64(len(correctSet))
		if portion < 0 {
			portion = 0
		}
		earned := portion * qWeight
		switch {
		case portion >= 1.0:
			return earned, "correct", "correct"
		case portion > 0:
			return earned, "partial", "partial"
		default:
			return 0, "incorrect", "incorrect"
		}
	}

	if len(studentSet) != len(correctSet) {
		return 0, "incorrect", "incorrect"
	}
	sCopy := make([]string, len(studentSet))
	cCopy := make([]string, len(correctSet))
	copy(sCopy, studentSet)
	copy(cCopy, correctSet)
	sortSlice(sCopy)
	sortSlice(cCopy)
	for i := range sCopy {
		if sCopy[i] != cCopy[i] {
			return 0, "incorrect", "incorrect"
		}
	}
	return qWeight, "correct", "correct"
}

func evaluateMatching(studentMap, correctMap map[string]interface{}, qWeight float64, partialScoring bool) (float64, string, string) {
	if partialScoring {
		if len(correctMap) == 0 {
			return 0, "incorrect", "incorrect"
		}
		correctMatches := 0
		for k, v := range correctMap {
			sv, ok := studentMap[k]
			if !ok {
				continue
			}
			if stringsToUpper(trimSpace(fmt.Sprintf("%v", sv))) == stringsToUpper(trimSpace(fmt.Sprintf("%v", v))) {
				correctMatches++
			}
		}
		portion := float64(correctMatches) / float64(len(correctMap))
		earned := portion * qWeight
		switch {
		case portion >= 1.0:
			return earned, "correct", "correct"
		case portion > 0:
			return earned, "partial", "partial"
		default:
			return 0, "incorrect", "incorrect"
		}
	}

	for k, v := range correctMap {
		sv, ok := studentMap[k]
		if !ok {
			return 0, "incorrect", "incorrect"
		}
		if stringsToUpper(trimSpace(fmt.Sprintf("%v", sv))) != stringsToUpper(trimSpace(fmt.Sprintf("%v", v))) {
			return 0, "incorrect", "incorrect"
		}
	}
	return qWeight, "correct", "correct"
}

func sortSlice(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

// No-import string helpers (avoid importing "strings" and "fmt" for portability,
// though fmt is already imported. These just keep the evaluator self-contained).

func stringsToUpper(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 32
		}
		b[i] = c
	}
	return string(b)
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
