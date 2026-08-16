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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/jackc/pgx/v5"
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
	rdb       *goredis.Client
	pool      *pgxpool.Pool
	quit      chan struct{}
	batchChan chan SubmissionResult
	wg        sync.WaitGroup
}

// SubmissionResult wraps a job result for the batch inserter.
type SubmissionResult struct {
	Job   SubmissionJob
	Score *float64
	Error error
}

// StartWorker launches a pool of background goroutines that concurrently
// process submission jobs from the Redis queue, and flushes results in batches.
func StartWorker(rdb *goredis.Client, pool *pgxpool.Pool) *Worker {
	const workerCount = 8
	const batchSize = 50

	w := &Worker{
		rdb:       rdb,
		pool:      pool,
		quit:      make(chan struct{}),
		batchChan: make(chan SubmissionResult, batchSize*2),
	}

	log.Printf("queue: starting submission worker pool (%d workers, batch size %d)", workerCount, batchSize)

	// Spawn workers to read and process jobs
	for i := 1; i <= workerCount; i++ {
		w.wg.Add(1)
		go w.runWorker(i)
	}

	// Spawn batch inserter
	w.wg.Add(1)
	go w.runBatchInserter(batchSize)

	return w
}

// runWorker is the main loop for a single worker thread.
func (w *Worker) runWorker(id int) {
	defer w.wg.Done()

	if isRedisUnavailable(w.rdb) {
		log.Printf("queue: Redis unavailable — worker %d not started", id)
		return
	}

	log.Printf("queue: worker %d started (polling %s)", id, QueueKey)

	for {
		select {
		case <-w.quit:
			log.Printf("queue: worker %d stopped", id)
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
				log.Printf("queue: worker %d BRPOP error: %v", id, err)
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
				log.Printf("queue: worker %d unmarshal job error: %v", id, err)
				continue
			}

			// Process and score submission
			jobCtx, jobCancel := context.WithTimeout(context.Background(), 30*time.Second)
			score, err := w.processSubmission(jobCtx, &job)
			jobCancel()

			// Send to batch channel
			select {
			case w.batchChan <- SubmissionResult{Job: job, Score: score, Error: err}:
			case <-w.quit:
				// The batch inserter only drains what already reached the channel,
				// so an in-flight job held HERE would otherwise be LOST on
				// shutdown — the student already received a 202. Re-push it to
				// the queue (best-effort) so a fresh worker on the next boot
				// finishes it; only an unavailable Redis (which already failed
				// every enqueue) can still drop it.
				if reqErr := EnqueueSubmissionWithJob(w.rdb, &job); reqErr != nil {
					log.Printf("queue: worker %d shutdown, requeue in-flight job %s failed: %v", id, job.JobID, reqErr)
				} else {
					log.Printf("queue: worker %d shutdown, requeued in-flight job %s", id, job.JobID)
				}
				return
			}
		}
	}
}

// Stop signals the worker to shut down after the current job finishes.
func (w *Worker) Stop() {
	close(w.quit)
	w.wg.Wait()
	log.Println("queue: worker pool stopped")
}

// processSubmission fetches the exam questions and calculates the score.
func (w *Worker) processSubmission(ctx context.Context, job *SubmissionJob) (*float64, error) {
	if w.pool == nil {
		return nil, errors.New("database pool is nil")
	}

	var questionsJSON *string
	err := w.pool.QueryRow(ctx,
		`SELECT questions_json FROM exams WHERE id = $1`, job.ExamID).Scan(&questionsJSON)
	if err != nil {
		return nil, fmt.Errorf("fetch exam: %w", err)
	}

	questions, parseErr := models.ParseQuestionsJSON(questionsJSON)
	if parseErr != nil {
		return nil, fmt.Errorf("parse questions: %w", parseErr)
	}

	var score *float64
	if len(questions) > 0 {
		score = models.CalculateSubmissionScore(job.Answers, questions)
	}

	return score, nil
}

// runBatchInserter continuously reads from batchChan and flushes to PostgreSQL.
func (w *Worker) runBatchInserter(batchSize int) {
	defer w.wg.Done()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var buf []SubmissionResult

	flush := func() {
		if len(buf) == 0 {
			return
		}
		w.flushBatch(context.Background(), buf)
		buf = buf[:0]
	}

	for {
		select {
		case <-w.quit:
			flush()
			// Drain channel before exiting
			for {
				select {
				case res := <-w.batchChan:
					buf = append(buf, res)
					if len(buf) >= batchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case result := <-w.batchChan:
			buf = append(buf, result)
			if len(buf) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// succeededRow tracks a row that was inserted/updated successfully inside the
// batch transaction, so its success result is only published AFTER commit.
type succeededRow struct {
	job          SubmissionJob
	score        *float64
	submissionID int
}

// flushBatch inserts a batch of submissions into PostgreSQL. Each row is written
// inside its own savepoint so that a single failing row does not abort the whole
// batch, and success results are only published to Redis AFTER the transaction
// commits — otherwise a commit failure would report "success" to students while
// silently discarding their answers.
func (w *Worker) flushBatch(ctx context.Context, results []SubmissionResult) {
	if w.pool == nil {
		log.Printf("queue batch: database pool is nil")
		return
	}

	start := time.Now()
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		log.Printf("queue batch: tx begin error: %v", err)
		// Nothing was persisted — retry/park every job so none are lost.
		for i := range results {
			w.retryOrFail(&results[i], fmt.Sprintf("tx begin error: %v", err))
		}
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// Set worker heartbeat in Redis (best effort)
	if !isRedisUnavailable(w.rdb) {
		w.rdb.Set(ctx, "examvan:submissions:worker_heartbeat", time.Now().UTC().Format(time.RFC3339), 30*time.Second)
	}

	var succeeded []succeededRow
	var failed []*SubmissionResult

	for i := range results {
		r := &results[i]
		if r.Error != nil {
			log.Printf("queue batch: skip job %s due to error: %v", r.Job.JobID, r.Error)
			failed = append(failed, r)
			continue
		}

		// Per-row savepoint (pgx pseudo-nested tx) isolates row failures.
		sp, spErr := tx.Begin(ctx)
		if spErr != nil {
			log.Printf("queue batch: savepoint begin error for job %s: %v", r.Job.JobID, spErr)
			r.Error = spErr
			failed = append(failed, r)
			continue
		}

		submissionID, insErr := upsertSubmissionRow(ctx, sp, &r.Job, r.Score)
		if insErr != nil {
			_ = sp.Rollback(ctx)
			log.Printf("queue batch: insert job %s error: %v", r.Job.JobID, insErr)
			r.Error = insErr
			failed = append(failed, r)
			continue
		}
		if relErr := sp.Commit(ctx); relErr != nil {
			log.Printf("queue batch: savepoint release error for job %s: %v", r.Job.JobID, relErr)
			r.Error = relErr
			failed = append(failed, r)
			continue
		}
		succeeded = append(succeeded, succeededRow{job: r.Job, score: r.Score, submissionID: submissionID})
	}

	if err := tx.Commit(ctx); err != nil {
		log.Printf("queue batch: tx commit error: %v — nothing persisted, re-enqueuing %d rows", err, len(succeeded))
		// The commit failed, so every "succeeded" row was rolled back. Retry
		// them all rather than reporting a false success.
		for i := range succeeded {
			jr := &SubmissionResult{Job: succeeded[i].job, Score: succeeded[i].score}
			w.retryOrFail(jr, fmt.Sprintf("batch commit failed: %v", err))
		}
		for _, r := range failed {
			w.retryOrFail(r, fmt.Sprintf("batch commit failed: %v", err))
		}
		return
	}
	committed = true

	// Data is durable now — safe to publish results / retry failures.
	for i := range succeeded {
		w.storeResult(ctx, succeeded[i].job.JobID, true, succeeded[i].score,
			fmt.Sprintf("submission %d created", succeeded[i].submissionID))

		// Revoke the device's approval ONLY now that its submission is durable
		// — "sesi berikutnya butuh persetujuan lagi". The handler deliberately
		// no longer revokes at enqueue time: if this batch had failed (and the
		// job were re-enqueued for retry), a student whose answers were not yet
		// saved would otherwise lose the re-entry entitlement AND their
		// client-side copy (the app clears its saved answers on submit
		// success) — a permanent, silent answer loss. Best-effort: a leak here
		// only weakens "re-approval for a second attempt", never data.
		if _, err := w.pool.Exec(ctx,
			`DELETE FROM exam_approvals WHERE exam_id = $1 AND mac_address = $2`,
			succeeded[i].job.ExamID, succeeded[i].job.MACAddress); err != nil {
			log.Printf("queue batch: revoke approval for job %s: %v", succeeded[i].job.JobID, err)
		}
	}
	for _, r := range failed {
		w.retryOrFail(r, fmt.Sprintf("submission error: %v", r.Error))
	}

	elapsed := time.Since(start)
	log.Printf("queue batch: flushed batch of %d items in %v (success: %d, failed: %d)",
		len(results), elapsed, len(succeeded), len(failed))
}

// upsertSubmissionRow updates the student's latest row for this device (the
// un-submitted placeholder row if there is one, or — crucially — the already-
// submitted row when this job is a RETRY of a submission whose first attempt
// was persisted but whose response never reached the client) or inserts a new
// submission, returning the row id.
//
// Idempotency contract: the match is "latest row for (exam, device,
// exam_number)" REGARDLESS of whether answers are present. Previously the
// match only targeted the open (answers_json IS NULL) placeholder, so a retry
// of an already-persisted submit found no placeholder and INSERTed a second
// row — the same student appeared twice in the admin monitoring table with
// duplicate scores. Retrying now overwrites the same row (the retried answers
// win), and a genuinely new attempt is still a fresh row because re-approval
// creates a new placeholder via EnsureFreshSubmissionOnApproval, which becomes
// the "latest row" the next submit targets.
func upsertSubmissionRow(ctx context.Context, q pgx.Tx, job *SubmissionJob, score *float64) (int, error) {
	// Serialise per (exam, device) with the SAME advisory lock the sync path
	// (models.CreateSubmission) and approval bookkeeping
	// (EnsureFreshSubmissionOnApproval) take. The queue's own jobs are already
	// serialised by the single batch-inserter goroutine, but a CONCURRENT
	// sync-path resubmit (recovery re-entry "Kirim Lagi" when Redis enqueue
	// fails and the handler falls through to sync) or a second worker instance
	// would otherwise race this UPDATE-then-INSERT check-then-act: both could
	// observe "no row yet" and both INSERT, duplicating the student in the
	// monitoring table / hasil page.
	if _, err := q.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`,
		fmt.Sprintf("approval:%d:%s", job.ExamID, job.MACAddress)); err != nil {
		return 0, fmt.Errorf("upsert submission row: advisory lock: %w", err)
	}

	answersJSON, _ := json.Marshal(job.Answers)
	identityJSON, _ := json.Marshal(job.IdentityData)

	var answersPtr, identityPtr *string
	answersStr := string(answersJSON)
	identityStr := string(identityJSON)
	if answersStr != "null" {
		answersPtr = &answersStr
	}
	if identityStr != "null" && identityStr != "{}" {
		identityPtr = &identityStr
	}

	var submissionID int
	err := q.QueryRow(ctx, `
		UPDATE submissions
		SET answers_json = $1, score = $2, start_time = COALESCE(start_time, NULLIF($3, '')), student_name = $4, exam_number = $5, student_class = $6, identity_data = $7
		WHERE id = (
			SELECT id FROM submissions
			WHERE exam_id = $8 AND mac_address = $9
			  AND ($10 = '' OR exam_number = $10)
			ORDER BY created_at DESC LIMIT 1
		)
		RETURNING id
	`, answersPtr, score, job.StartTime, job.StudentName, job.ExamNumber, job.StudentClass, identityPtr, job.ExamID, job.MACAddress, job.ExamNumber).Scan(&submissionID)

	if err == pgx.ErrNoRows {
		err = q.QueryRow(ctx, `INSERT INTO submissions
			(exam_id, student_name, exam_number, student_class, answers_json, score, start_time, mac_address, identity_data)
			VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9)
			RETURNING id`,
			job.ExamID, job.StudentName, job.ExamNumber, job.StudentClass,
			answersPtr, score, job.StartTime, job.MACAddress, identityPtr,
		).Scan(&submissionID)
	}
	return submissionID, err
}

// retryOrFail re-enqueues a job for another attempt, or records a terminal
// failure result once the retry budget is exhausted.
func (w *Worker) retryOrFail(r *SubmissionResult, msg string) {
	if r.Job.Retries < maxRetriesPerJob {
		r.Job.Retries++
		log.Printf("queue batch: retrying job %s (attempt %d/%d): %s", r.Job.JobID, r.Job.Retries, maxRetriesPerJob, msg)
		_ = EnqueueSubmissionWithJob(w.rdb, &r.Job)
		return
	}
	w.storeResult(context.Background(), r.Job.JobID, false, nil, msg)
}

// storeResult writes job result to Redis and publishes updates.
func (w *Worker) storeResult(ctx context.Context, jobID string, success bool, score *float64, message string) {
	if isRedisUnavailable(w.rdb) {
		return
	}

	result := JobResult{
		JobID:       jobID,
		Success:     success,
		Score:       score,
		Message:     message,
		ProcessedAt: time.Now().UTC().Format(time.RFC3339),
	}

	resultPayload, err := json.Marshal(result)
	if err != nil {
		return
	}

	resultKey := ResultKeyPrefix + jobID
	_ = w.rdb.Set(ctx, resultKey, resultPayload, resultTTL).Err()

	// Publish to Pub/Sub channel.
	_ = w.rdb.Publish(ctx, pubSubChannel, resultPayload).Err()

	// Update stats counters.
	statsKey := "examvan:submissions:stats"
	if success {
		w.rdb.HIncrBy(ctx, statsKey, "processed", 1)
	} else {
		w.rdb.HIncrBy(ctx, statsKey, "errored", 1)
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

// generateJobID creates a unique 16-hex-char identifier using crypto/rand.
func generateJobID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback if rand fails, though extremely rare
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
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
