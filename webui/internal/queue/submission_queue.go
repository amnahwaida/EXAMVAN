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

	"github.com/examvan/webui/internal/models"
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
	JobID        string                 `json:"job_id"`
	ExamID       int                    `json:"exam_id"`
	StudentName  string                 `json:"student_name"`
	ExamNumber   string                 `json:"exam_number"`
	StudentClass string                 `json:"student_class"`
	Answers      map[string]interface{} `json:"answers"`
	StartTime    string                 `json:"start_time,omitempty"`
	MACAddress   string                 `json:"mac_address"`
	IdentityData map[string]interface{} `json:"identity_data,omitempty"`
	Retries      int                    `json:"retries"`
	EnqueuedAt   string                 `json:"enqueued_at"` // ISO 8601 UTC
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
		JobID:        jobID,
		ExamID:       extractInt(data, "exam_id"),
		StudentName:  extractString(data, "student_name"),
		ExamNumber:   extractString(data, "exam_number"),
		StudentClass: extractString(data, "student_class"),
		StartTime:    extractString(data, "start_time"),
		MACAddress:   extractString(data, "mac_address"),
		EnqueuedAt:   now,
		Answers:      extractMap(data, "answers"),
		IdentityData: extractMap(data, "identity_data"),
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
// exhausted or Stop() is called. Returns the Worker so callers can Stop() it.
//
//	pool := database.Connect(cfg)
//	rdb := redis.Connect(ctx, cfg.RedisURL)
//	worker := queue.StartWorker(rdb, pool)
//	// later: worker.Stop()
func StartWorker(rdb *goredis.Client, pool *pgxpool.Pool) *Worker {
	w := &Worker{
		rdb:  rdb,
		pool: pool,
		quit: make(chan struct{}),
	}
	go w.run()
	return w
}

// run is the main loop. It blocks on BRPOP and processes each job.
func (w *Worker) run() {
	if isRedisUnavailable(w.rdb) {
		log.Printf("queue: Redis unavailable — worker not started")
		return
	}

	log.Printf("queue: worker started (polling %s)", QueueKey)

	for {
		select {
		case <-w.quit:
			log.Printf("queue: worker stopped")
			return
		default:
			// Use a per-iteration context with timeout so long operations
			// don't block shutdown indefinitely.
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			// BRPOP with a timeout blocks until a job arrives.
			result, err := w.rdb.BRPop(ctx, defaultPollTimeout, QueueKey).Result()
			cancel()
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

			// Use a fresh context for processing — the BRPOP context was
			// already canceled and must not be reused.
			jobCtx, jobCancel := context.WithTimeout(context.Background(), 30*time.Second)
			w.processJob(jobCtx, job)
			jobCancel()
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
	if w.pool == nil {
		return w.fail(job.JobID, "database pool is nil")
	}

	var questionsJSON *string
	err := w.pool.QueryRow(ctx,
		`SELECT questions_json FROM exams WHERE id = $1`, job.ExamID).Scan(&questionsJSON)
	if err != nil {
		log.Printf("queue: fetch exam %d: %v", job.ExamID, err)
		return w.fail(job.JobID, fmt.Sprintf("fetch exam: %v", err))
	}

	// Parse & score using the shared models package.
	questions, parseErr := models.ParseQuestionsJSON(questionsJSON)
	if parseErr != nil {
		log.Printf("queue: parse questions: %v", parseErr)
		return w.fail(job.JobID, fmt.Sprintf("parse questions: %v", parseErr))
	}

	var score *float64
	if len(questions) > 0 {
		score = models.CalculateSubmissionScore(job.Answers, questions)
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
		(exam_id, student_name, exam_number, student_class, answers_json, score, start_time, mac_address, identity_data)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9)
		RETURNING id`

	var submissionID int
	err := w.pool.QueryRow(ctx, sql,
		job.ExamID, job.StudentName, job.ExamNumber, job.StudentClass,
		answersPtr, score, job.StartTime, job.MACAddress, identityPtr,
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
