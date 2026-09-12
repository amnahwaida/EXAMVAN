package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	r2client "github.com/examvan/webui/internal/handlers/r2"
	"github.com/examvan/webui/internal/helpers"
	"github.com/examvan/webui/internal/models"
	"github.com/examvan/webui/internal/queue"
	"github.com/examvan/webui/internal/services/examtoken"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// cacheTTL is how long the active exam list lives in Redis (seconds).
	cacheTTL = 30 * time.Second

	// Rate-limit parameters for the submit endpoint.
	submitRateLimitMax    = 10
	submitRateLimitWindow = 60 * time.Second

	// Rate-limit parameters for the presence endpoints (access-log & complete),
	// enforced per exam+MAC in the handler (bucket per-IP middleware saja tidak
	// cukup: satu ruangan di belakang NAT sekolah berbagi satu IP). Satu
	// perangkat mengirim login + ~1 heartbeat/menit + logout + complete, jadi
	// 10/menit memberi headroom besar untuk retry.
	presenceRateLimitMax    = 10
	presenceRateLimitWindow = 60 * time.Second

	// Rate-limit parameters for GET /result (post-submit polling). Per-device
	// bucket (exam+MAC) 60/menit: app mem-poll tiap 2,5 dtk ≈ 24/menit saat
	// submit ter-antri (SubmissionManager.QUEUED_POLL_INTERVAL_MS), jadi 60
	// memberi headroom untuk retry. Aggregate per-exam 12000/menit: polling
	// murah (Redis GET / lookup DB) dan seluruh ruangan mem-poll bersamaan di
	// deadline — 500 perangkat × 24/menit, pola yang sama dengan bucket
	// anti-spam request-approval.
	resultRateLimitMax        = 60
	resultRateLimitWindow     = 60 * time.Second
	resultExamRateLimitMax    = 12000
	resultExamRateLimitWindow = 60 * time.Second

	// Anti-spam flood brake for POST /api/exams/request-approval, enforced as
	// a GLOBAL per-exam bucket in Redis on top of the per-IP middleware limit.
	// Sized with 2× headroom over a full room: defaultMaxApprovalsPerExam
	// devices polling every 5s ≈ 500 × 12 = 6000 req/min sustained, so bursts
	// (a join wave, retries) and the tail of the queue stay under the cap. A
	// distributed attacker that rotates IPs still hits this shared bucket and
	// cannot flood one exam.
	approvalExamRateLimitMax    = 12000
	approvalExamRateLimitWindow = 60 * time.Second

	// Per-device request-approval cap: app mem-poll tiap 5 dtk (≈12/menit per
	// perangkat), jadi 30/menit memberi headroom 2,5× untuk retry. Bucket per
	// exam+MAC membuat satu ruangan di belakang NAT sekolah tidak saling
	// memblokir (pola yang sama dengan presence & polling hasil).
	reqAppRateLimitMax    = 30
	reqAppRateLimitWindow = 60 * time.Second

	// Rate-limit untuk GET /api/exams/token/:token (join wave). Join satu kali
	// per perangkat, tapi seluruh ruangan mengetik token bersamaan di awal
	// ujian dari satu NAT sekolah. Bucket per-token 600/menit (ruangan maks
	// 500 perangkat + headroom) menahan flood join pada satu token (mis.
	// brute-force token dari banyak IP); middleware per-IP dinaikkan agar
	// ruangan di belakang NAT tidak saling memblokir.
	joinRateLimitMax    = 600
	joinRateLimitWindow = 60 * time.Second

	// Rate-limit untuk GET /api/exams/:exam_id/pdf (download wave). Bucket per
	// exam+MAC 10/menit — unduhan satu kali + retry; MAC tak dikenal → per
	// exam+IP. Sama seperti endpoint siswa lain.
	pdfRateLimitMax    = 10
	pdfRateLimitWindow = 60 * time.Second

	// defaultMaxApprovalsPerExam caps how many devices may hold an APPROVED
	// approval row per exam when auto-approve is on (tunable via the
	// max_approvals_per_exam saas setting; 0 = unlimited).
	defaultMaxApprovalsPerExam = 500
	// Redis key prefixes.
	cacheKeyPrefix          = "api:exams:list:"          // + page:per_page
	rateLimitKeyPrefix      = "ratelimit:submit:"        // + exam_id:mac_address
	presenceRateKeyPrefix   = "ratelimit:presence:"      // + exam_id:mac_address (access-log & complete)
	resultRateKeyPrefix     = "ratelimit:result:"        // + exam_id:mac_address (polling hasil)
	resultExamRateKeyPrefix = "ratelimit:result-exam:"   // + exam_id (aggregate polling)
	reqAppDeviceKeyPrefix   = "ratelimit:reqapp-device:" // + exam_id:mac_address
	joinRateKeyPrefix       = "ratelimit:join:"          // + token (join wave)
	pdfRateKeyPrefix        = "ratelimit:pdf:"           // + exam_id:mac_address (download)
	heartbeatKeyPrefix      = "heartbeat:"               // + exam_id:mac_address
	heartbeatTTL            = 5 * time.Minute
)

var defaultIdentityFields []map[string]interface{}

func init() {
	_ = json.Unmarshal([]byte(`[
		{"key":"student_name","label":"Nama","required":true},
		{"key":"exam_number","label":"Nomor Ujian","required":true},
		{"key":"student_class","label":"Kelas","required":true}
	]`), &defaultIdentityFields)
}

// ---------------------------------------------------------------------------
// Context helpers
// ---------------------------------------------------------------------------

// getPool extracts the PostgreSQL connection pool from the gin context.
// Panics if the middleware did not set "db" — this is a required dependency.
func getPool(c *gin.Context) *pgxpool.Pool {
	return c.MustGet("db").(*pgxpool.Pool)
}

// getRedis extracts the optional Redis client from the gin context.
// Returns nil when Redis is not configured.
func getRedis(c *gin.Context) *redis.Client {
	if r, exists := c.Get("redis"); exists {
		if rc, ok := r.(*redis.Client); ok {
			return rc
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Generic response helpers
// ---------------------------------------------------------------------------

func errorResponse(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"success": false, "message": message})
	_ = c.AbortWithError(status, fmt.Errorf("%s", message))
}

// errorResponseWithCode is like errorResponse but also exposes a stable
// machine-readable error_code (e.g. r2client.ErrCodeNotConfigured) so clients
// can branch on the code instead of matching the human-readable message text.
func errorResponseWithCode(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"success": false, "error_code": code, "message": message})
	_ = c.AbortWithError(status, fmt.Errorf("%s [%s]", message, code))
}

func successData(c *gin.Context, data gin.H) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

// ---------------------------------------------------------------------------
// Input sanitisation (mirrors Python sanitize_student_input)
// ---------------------------------------------------------------------------

func sanitize(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 200 {
		v = v[:200]
	}
	return v // html/template auto-escapes on render; storing escaped causes double-escape
}

// sanitizeMap sanitises all string values in a map and returns the result.
func sanitizeMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = sanitize(s)
		} else {
			out[k] = v
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// JSON / time formatting
// ---------------------------------------------------------------------------

// formatISOUTC formats a time.Time as an ISO 8601 UTC string.
func formatISOUTC(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

// formatNullableISOUTC returns an ISO string for a non-nil time, or nil.
func formatNullableISOUTC(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return formatISOUTC(*t)
}

// roundTo rounds a float64 to the given number of decimal places.
func roundTo(val float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(val*pow) / pow
}

// Redis helpers
// ---------------------------------------------------------------------------

// checkRateLimit uses Redis INCR + EXPIRE to enforce a per-key rate limit.
// When Redis is unavailable the check is skipped (open access).
func checkRateLimit(rdb *redis.Client, key string, max int64, window time.Duration) bool {
	if rdb == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	count, err := rdb.Incr(ctx, key).Result()
	if err != nil {
		return true
	}
	if count == 1 {
		rdb.Expire(ctx, key, window)
	}
	return count <= max
}

// generateJobID returns a 16-character random hex string used as a
// submission queue job identifier.
func generateJobID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback: time-based ID.
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// enqueueSubmission pushes a JSON-encoded submission job onto the
// submission:queue list in Redis.  Returns the generated job ID.
func enqueueSubmission(rdb *redis.Client, job map[string]interface{}) (string, error) {
	jobID := generateJobID()
	job["job_id"] = jobID

	data, err := json.Marshal(job)
	if err != nil {
		return "", fmt.Errorf("marshal job: %w", err)
	}

	pushCtx, pushCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pushCancel()
	if err := rdb.LPush(pushCtx, queue.QueueKey, data).Err(); err != nil {
		return "", fmt.Errorf("lpush: %w", err)
	}
	return jobID, nil
}

// setStudentHeartbeat writes student activity data to Redis with a TTL so
// the admin realtime dashboard can consume it.
func setStudentHeartbeat(rdb *redis.Client, examID int, macAddress string, data map[string]interface{}) {
	if rdb == nil {
		return
	}
	key := fmt.Sprintf("%s%d:%s", heartbeatKeyPrefix, examID, macAddress)
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	setCtx, setCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer setCancel()
	if err := rdb.Set(setCtx, key, payload, heartbeatTTL).Err(); err != nil {
		log.Printf("heartbeat redis set error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 1. GET /api/exams — List active exams (with optional Redis cache)
// ---------------------------------------------------------------------------

// ListExams returns a gin.HandlerFunc that lists active exams with
// pagination.  Results are cached in Redis for cacheTTL seconds when Redis
// is available.
func ListExams() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		rdb := getRedis(c)
		ctx := c.Request.Context()

		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		if page < 1 {
			page = 1
		}
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "50"))
		if perPage < 1 {
			perPage = 1
		} else if perPage > 200 {
			perPage = 200
		}

		// --- School scope ---
		// This endpoint is a shared multi-tenant SaaS API, so the exam list is
		// scoped to a single school by its unique instansi CODE. Without a code
		// we return an EMPTY list rather than every school's active exams (which
		// would be a cross-tenant leak). Names are intentionally NOT accepted
		// (non-unique -> could bleed across same-named schools).
		instansi := strings.TrimSpace(c.Query("instansi"))
		if instansi == "" {
			instansi = strings.TrimSpace(c.Query("kode"))
		}
		if instansi == "" {
			instansi = strings.TrimSpace(c.Query("code"))
		}
		if instansi == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"data":    []interface{}{},
				"pagination": gin.H{
					"page": page, "per_page": perPage, "total": 0, "total_pages": 0,
				},
			})
			return
		}

		// --- Redis cache lookup (keyed per school to avoid cross-school mixing) ---
		cacheKey := fmt.Sprintf("%s%s:%d:%d", cacheKeyPrefix, strings.ToLower(instansi), page, perPage)
		if rdb != nil {
			cached, err := rdb.Get(ctx, cacheKey).Result()
			if err == nil {
				c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(cached))
				return
			}
		}

		// --- Database query (scoped to the requested school) ---
		result, err := models.ListActiveExamsByInstansi(ctx, pool, instansi, page, perPage)
		if err != nil {
			log.Printf("list active exams error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat daftar ujian")
			return
		}

		type item struct {
			ID        int         `json:"id"`
			Name      string      `json:"name"`
			Status    string      `json:"status"`
			SizeMB    float64     `json:"size_mb"`
			StartTime interface{} `json:"start_time"`
			EndTime   interface{} `json:"end_time"`
			CreatedAt string      `json:"created_at"`
		}

		items := make([]item, 0, len(result.Exams))
		for _, e := range result.Exams {
			items = append(items, item{
				ID:        e.ID,
				Name:      e.Name,
				Status:    e.Status,
				SizeMB:    roundTo(float64(e.SizeBytes)/(1024*1024), 2),
				StartTime: formatNullableISOUTC(e.StartTime),
				EndTime:   formatNullableISOUTC(e.EndTime),
				CreatedAt: formatISOUTC(e.CreatedAt),
			})
		}

		resp := gin.H{
			"success": true,
			"data":    items,
			"pagination": gin.H{
				"page":        result.Page,
				"per_page":    result.PerPage,
				"total":       result.Total,
				"total_pages": result.TotalPages,
			},
		}

		// --- Store in Redis cache (same per-school key as the lookup) ---
		if rdb != nil {
			if jsonBytes, err := json.Marshal(resp); err == nil {
				if err := rdb.Set(ctx, cacheKey, jsonBytes, cacheTTL).Err(); err != nil {
					log.Printf("redis cache set error: %v", err)
				}
			}
		}

		c.JSON(http.StatusOK, resp)
	}
}

// ---------------------------------------------------------------------------
// Request Approval (Android)
// ---------------------------------------------------------------------------
func RequestApproval() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			ExamID       int                    `json:"exam_id"`
			MACAddress   string                 `json:"mac_address"`
			StudentName  string                 `json:"student_name"`
			ExamNumber   string                 `json:"exam_number"`
			StudentClass string                 `json:"student_class"`
			IdentityData map[string]interface{} `json:"identity_data"`
			Reset        bool                   `json:"reset"`
			Token        string                 `json:"token"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			errorResponse(c, http.StatusBadRequest, "Payload tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		// Sanitise all attacker-controlled fields before persisting: this data
		// is rendered by the admin monitoring UI (innerHTML), so restrict the
		// MAC to a safe charset and trim/cap the free-text identity fields.
		macAddress := sanitizeMAC(req.MACAddress)
		if macAddress == "" || macAddress == "unknown" {
			errorResponse(c, http.StatusBadRequest, "MAC address diperlukan")
			return
		}
		studentName := sanitize(req.StudentName)
		examNumber := sanitize(req.ExamNumber)
		studentClass := sanitize(req.StudentClass)

		idDataStr, _ := json.Marshal(sanitizeMap(req.IdentityData))
		if string(idDataStr) == "null" {
			idDataStr = []byte("{}")
		}

		// Server-side auto-approve: when the exam's auto_approve flag is on AND
		// the exam is live (active, started, schedule not ended), request-
		// approval approves the device immediately instead of queueing it for a
		// pengawas. The flag lives in the DB, so this keeps working even when
		// no pengawas monitoring page is open.
		//
		// Status transitions while the flag is ON:
		//   - first request (row INSERT) → approved, regardless of the reset
		//     flag (the Android client sends reset=false on its first poll)
		//   - explicit retry (reset=true) → approved ("Minta Izin Lagi")
		//   - poll (reset=false), row pending → approved — unsticks devices
		//     that queued before the flag was turned on or while the exam was
		//     dormant; pending is not a manual decision, so flipping it is safe
		//   - poll (reset=false), row rejected → stays rejected — auto-approve
		//     never silently overrides an explicit pengawas decision; the
		//     student must actively retry (reset=true)
		//
		// Only effective while the exam is live — a request against a dormant
		// exam still lands in the pending queue.
		exam, examErr := models.GetExamByID(ctx, pool, req.ExamID)
		if examErr != nil {
			if examErr == pgx.ErrNoRows {
				errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
				return
			}
			log.Printf("request approval exam lookup error: %v", examErr)
			errorResponse(c, http.StatusInternalServerError, "Gagal memproses persetujuan")
			return
		}

		// Only live exams accept approval requests. A request against an inactive
		// exam would otherwise sit in the pending queue forever (auto-approve
		// already requires the exam to be live), bloating the queue and the
		// monitoring page with rows nobody can act on — the exam was stopped, so
		// reject it outright. Mirrors the join gate in ExamByToken.
		if !exam.IsActive() {
			errorResponse(c, http.StatusForbidden, "Ujian tidak aktif")
			return
		}
		// Same for an exam whose schedule has ended: a queued request would just
		// sit forever (auto-approve already refuses to fire past end_time+grace).
		// Mirrors the join/submit gate in ExamByToken/SubmitExam.
		if models.ExamScheduleEnded(&exam, time.Now().UTC()) {
			errorResponse(c, http.StatusForbidden, "Waktu ujian telah berakhir")
			return
		}

		// --- Per-exam rate limit (anti-spam) ---
		// A global per-exam bucket in Redis catches distributed floods that
		// defeat the per-IP middleware limit. Placed BEFORE the token check so
		// a wrong-token brute-force flood also consumes this shared bucket
		// (not just the per-IP middleware limit). Skipped when Redis is absent
		// (the per-IP memory limiter still applies).
		if rdb := getRedis(c); rdb != nil {
			if !checkRateLimit(rdb, fmt.Sprintf("ratelimit:reqapp-exam:%d", req.ExamID),
				approvalExamRateLimitMax, approvalExamRateLimitWindow) {
				errorResponse(c, http.StatusTooManyRequests,
					"Terlalu banyak permintaan izin untuk ujian ini. Silakan coba lagi nanti.")
				return
			}
			// Per-device bucket (exam+MAC): seluruh ruangan mem-poll status
			// approval tiap 5 dtk dari satu NAT sekolah — bucket per-IP middleware
			// tidak cukup. MAC wajib di endpoint ini (ditolak 400 bila kosong),
			// jadi tidak ada fallback per-IP seperti endpoint lain.
			if !checkRateLimit(rdb, fmt.Sprintf("%s%d:%s", reqAppDeviceKeyPrefix, req.ExamID, macAddress),
				reqAppRateLimitMax, reqAppRateLimitWindow) {
				errorResponse(c, http.StatusTooManyRequests,
					"Terlalu banyak permintaan izin. Silakan coba lagi nanti.")
				return
			}
		}

		// --- Token check (anti-spam) ---
		// Request-approval is the gate that (with auto-approve on) grants a
		// device submit access, so it must be as hard to call as the exam
		// itself: the device already holds the exam token (it fetched the exam
		// by token first), and a caller without it must be able to neither
		// spam the pending queue nor self-approve.
		//
		// A device that ALREADY has an approval row is tolerated with a stale
		// token — the same tolerance SubmitExam/AccessLog apply to known
		// devices — so a dynamic-token rotation mid-wait cannot strand a
		// device that is already queued (it only needs the token once, at
		// join time).
		if !examtoken.Matches(exam, req.Token) {
			var known bool
			if err := pool.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM exam_approvals WHERE exam_id = $1 AND mac_address = $2)`,
				req.ExamID, macAddress).Scan(&known); err != nil || !known {
				errorResponse(c, http.StatusUnauthorized, "Token tidak valid")
				return
			}
		}

		// --- Auto-approve decision ---
		// The server-side auto-approve flag approves the device immediately
		// while the exam is live, UNLESS the per-exam approved-device cap is
		// already reached — then the request falls back to the pending queue
		// so a leaked token cannot mint unlimited approved devices.
		//
		// ATOMIC when auto-approve is live: the approved-count check and the
		// INSERT run under one per-exam advisory lock (approval-cap:<exam_id>),
		// so two devices auto-approving CONCURRENTLY can never BOTH count the
		// same free slot and overshoot the cap — the same count-then-insert
		// race the quota gates close elsewhere. Without the lock every racing
		// request reads the pre-commit snapshot and the cap is exceeded by
		// however many requests raced. Pending-queue INSERTs (flag off) need
		// no lock: the cap is not in play.
		autoApproveLive := exam.AutoApprove && exam.IsActive() &&
			exam.ExamStartedAt != nil && !models.ExamScheduleEnded(&exam, time.Now().UTC())

		// Single INSERT core shared by both paths (see the status CASE below).
		insertApprovalSQL := `INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, student_class, identity_data, status)
			 VALUES ($1, $2, $3, $4, $5, $6,
			         CASE WHEN $8::boolean THEN 'approved' ELSE 'pending' END)
			 ON CONFLICT (exam_id, mac_address) DO UPDATE
			 SET student_name = EXCLUDED.student_name,
			     exam_number = EXCLUDED.exam_number,
			     student_class = EXCLUDED.student_class,
			     identity_data = EXCLUDED.identity_data,
		     status = CASE
		         -- Explicit retry ("Minta Izin Lagi"): fresh decision. An
		         -- already-approved device is never demoted, even when the
		         -- per-exam cap blocks further auto-approvals.
		         WHEN $7::boolean THEN CASE WHEN $8::boolean THEN 'approved'
		                                  WHEN exam_approvals.status = 'approved' THEN 'approved'
		                                  ELSE 'pending' END
		         -- Poll (reset=false): auto-approve flips a leftover pending
		         -- row to approved (it was never decided manually), but
		         -- never overrides an explicit rejection.
		         WHEN $8::boolean AND exam_approvals.status = 'pending' THEN 'approved'
		         ELSE exam_approvals.status
		     END,
			     updated_at = CURRENT_TIMESTAMP
			 RETURNING status`

		var status string
		if autoApproveLive {
			tx, bErr := pool.Begin(ctx)
			if bErr != nil {
				log.Printf("request approval begin tx error: %v", bErr)
				errorResponse(c, http.StatusInternalServerError, "Gagal memproses persetujuan")
				return
			}
			defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit

			if _, aErr := tx.Exec(ctx,
				`SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`,
				fmt.Sprintf("approval-cap:%d", req.ExamID)); aErr != nil {
				log.Printf("request approval: lock approval cap (%d): %v", req.ExamID, aErr)
				errorResponse(c, http.StatusInternalServerError, "Gagal memproses persetujuan")
				return
			}

			// Re-count the approved devices UNDER the lock — the count is what
			// the cap decision is based on, so it must observe every committed
			// approval before this one (a concurrent approver's commit is
			// visible once we hold the lock and it has released it).
			approvalCap := models.GetSaasSettingInt(ctx, pool,
				models.SettingMaxApprovalsPerExam, defaultMaxApprovalsPerExam)
			autoApprove := true
			if approvalCap > 0 {
				var approvedCount int
				if err := tx.QueryRow(ctx,
					`SELECT COUNT(*) FROM exam_approvals WHERE exam_id = $1 AND status = 'approved'`,
					req.ExamID).Scan(&approvedCount); err == nil && approvedCount >= approvalCap {
					autoApprove = false
				}
			}

			if err := tx.QueryRow(ctx, insertApprovalSQL,
				req.ExamID, macAddress, studentName, examNumber, studentClass, string(idDataStr), req.Reset, autoApprove).Scan(&status); err != nil {
				log.Printf("request approval error: %v", err)
				errorResponse(c, http.StatusInternalServerError, "Gagal memproses persetujuan")
				return
			}
			if err := tx.Commit(ctx); err != nil {
				log.Printf("request approval commit error: %v", err)
				errorResponse(c, http.StatusInternalServerError, "Gagal memproses persetujuan")
				return
			}
		} else {
			autoApprove := false
			if err := pool.QueryRow(ctx, insertApprovalSQL,
				req.ExamID, macAddress, studentName, examNumber, studentClass, string(idDataStr), req.Reset, autoApprove).Scan(&status); err != nil {
				log.Printf("request approval error: %v", err)
				errorResponse(c, http.StatusInternalServerError, "Gagal memproses persetujuan")
				return
			}
		}

		// An approved device needs its monitoring-row bookkeeping (same as the
		// manual approval endpoint). Idempotent — safe to call on every poll.
		if status == "approved" {
			if err := models.EnsureFreshSubmissionOnApproval(ctx, pool, req.ExamID, macAddress); err != nil {
				log.Printf("request approval: ensure submission: %v", err)
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"status":  status,
		})
	}
}

// ---------------------------------------------------------------------------
// 2. GET /api/exams/token/:token — Start Exam (Android/Web)
// ---------------------------------------------------------------------------

// ExamByToken returns a gin.HandlerFunc for the token-based exam lookup.
// The Android app calls this to fetch exam metadata after scanning a QR code
// or entering a token manually.
func ExamByToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Param("token")

		pool := getPool(c)
		ctx := c.Request.Context()

		// --- Rate limit (per-token aggregate) ---
		// Join satu kali per perangkat, tapi seluruh ruangan bergabung
		// bersamaan di awal ujian dari satu NAT sekolah — bucket per-IP
		// middleware tidak cukup. Bucket per-token menahan flood join pada satu
		// token (mis. brute-force dari banyak IP); skip saat Redis tidak ada
		// (middleware per-IP tetap berlaku).
		if rdb := getRedis(c); rdb != nil {
			if !checkRateLimit(rdb, fmt.Sprintf("%s%s", joinRateKeyPrefix, token),
				joinRateLimitMax, joinRateLimitWindow) {
				errorResponse(c, http.StatusTooManyRequests,
					"Terlalu banyak percobaan. Silakan coba lagi nanti.")
				return
			}
		}

		// Optional version check — if the header is absent the check is
		// skipped (web clients), the SAME policy as AndroidVersionCheck and
		// the other two gate sites (M3). Versi yang diminta adalah versi
		// EFEKTIF (dibatasi ke APK yang terbit, "" bila tak ada APK yang
		// bisa diunduh). The comparator is models.CompareVersions (M4): same
		// padding/semantics as the middleware and the Android client's
		// UpdateManager, so one client verdict can never differ between
		// layers on the same route.
		clientVersion := c.GetHeader("X-App-Version")
		if clientVersion != "" {
			required := models.EffectiveAndroidRequiredVersion(ctx, pool)
			if required != "" && models.CompareVersions(clientVersion, required) < 0 {
				c.JSON(http.StatusUpgradeRequired, gin.H{
					"success": false,
					"error":   "upgrade_required",
					"message": fmt.Sprintf(
						"Versi aplikasi Anda usang (%s). Silakan unduh EXAMVAN v%s terbaru.",
						clientVersion, required),
				})
				return
			}
		}

		exam, err := models.GetExamByActiveToken(ctx, pool, token)
		if err != nil {
			if err == pgx.ErrNoRows {
				errorResponse(c, http.StatusNotFound, "Token tidak valid")
				return
			}
			log.Printf("exam by token error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat data ujian")
			return
		}

		if !exam.IsActive() || exam.ExamStartedAt == nil {
			errorResponse(c, http.StatusForbidden, "Ujian belum dimulai oleh pengawas")
			return
		}
		if models.ExamScheduleEnded(&exam, time.Now().UTC()) {
			errorResponse(c, http.StatusForbidden, "Waktu ujian telah berakhir")
			return
		}

		// Auto-reset active_token if exam has started and token mode is dynamic.
		if err := examtoken.MaybeResetActiveToken(ctx, pool, &exam, time.Now().UTC()); err != nil {
			log.Printf("exam token reset error: %v", err)
		}

		identityFields := helpers.ParseIdentityFields(exam.IdentityFields, defaultIdentityFields)

		panelColor := "#6366f1"
		if exam.PanelColor != nil && *exam.PanelColor != "" {
			panelColor = *exam.PanelColor
		}

		sizeMB := roundTo(float64(exam.SizeBytes)/(1024*1024), 2)

		// Parse questions JSON for clients that need it (Android, Linux)
		var questions interface{}
		if exam.QuestionsJSON != nil && *exam.QuestionsJSON != "" {
			var parsed interface{}
			if err := json.Unmarshal([]byte(*exam.QuestionsJSON), &parsed); err == nil {
				questions = parsed
			}
		}

		examResp := gin.H{
			"id":              exam.ID,
			"name":            exam.Name,
			"status":          exam.Status,
			"security_level":  exam.SecurityLevel,
			"strict_mode":     exam.IsStrict(),
			"public_results":  exam.PublicResults,
			"show_answers":    exam.ShowAnswers,
			"identity_fields": identityFields,
			"panel_color":     panelColor,
			"size_mb":         sizeMB,
			"time_limit":      nil,
			"start_time":      formatNullableISOUTC(exam.StartTime),
			"end_time":        formatNullableISOUTC(exam.EndTime),
		}
		if questions != nil {
			// Strip answer keys before sending to students. The answer key for
			// each question is stored under either "key" (primary) or "answer"
			// (legacy alias), and this endpoint is public — a student must not
			// be able to read the correct answers before/while taking the exam.
			// Mirrors the stripping the public hasil page applies to anonymous
			// viewers.
			stripAnswerKeys(questions)
			examResp["questions"] = questions
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"exam":    examResp,
		})
	}
}

// ---------------------------------------------------------------------------
// 3. GET /api/exams/:exam_id/pdf — Download exam PDF (Android)
// ---------------------------------------------------------------------------

// ExamPDF returns a gin.HandlerFunc that serves the exam PDF file after
// validating the exam token supplied via header or query param.
func ExamPDF() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		token := c.GetHeader("X-Exam-Token")
		if token == "" {
			errorResponse(c, http.StatusUnauthorized, "Token tidak disertakan")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		// Verify exam exists with matching active token.
		exam, err := models.GetExamByID(ctx, pool, examID)
		if err != nil {
			if err == pgx.ErrNoRows {
				errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
				return
			}
			log.Printf("exam pdf lookup error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat data ujian")
			return
		}
		if !exam.IsActive() || exam.ExamStartedAt == nil || !examtoken.Matches(exam, token) {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}
		if models.ExamScheduleEnded(&exam, time.Now().UTC()) {
			errorResponse(c, http.StatusForbidden, "Waktu ujian telah berakhir")
			return
		}

		// --- Server-side approval gate (manual & auto mode) ---
		// The exam content must not be downloadable on the strength of the
		// token alone: in static-token mode the token is shared by the whole
		// class, and the client-side waiting screen was previously the ONLY
		// enforcement of the pengawas' approval — a crafted client (or curl +
		// signed URL) could fetch the PDF without ever being approved. Both
		// first-party clients request approval before opening the viewer
		// (Android WaitingApprovalActivity / desktop WaitingApprovalDialog;
		// auto-approve makes that instant), so every legitimate device holds an
		// 'approved' row here in both modes.
		//
		// The device identity travels in the X-Device-Id header (Android sends
		// "DEVICE:<AndroidId>", desktop sends its MAC — the SAME value it used
		// at request-approval time), with a mac_address query param accepted as
		// a fallback. A device with no match in exam_approvals — including a
		// legitimate student who has never passed the approval gate — is denied.
		deviceID := sanitizeMAC(c.GetHeader("X-Device-Id"))
		if deviceID == "" || deviceID == "unknown" {
			deviceID = sanitizeMAC(c.Query("mac_address"))
		}

		// --- Rate limit (per exam + device) ---
		// Seluruh ruangan mengunduh PDF bersamaan di awal ujian dari satu NAT
		// sekolah. Bucket per exam+MAC memberi tiap perangkat jatah sendiri
		// (unduhan satu kali + retry); perangkat tanpa identitas (sanitizeMAC
		// → "unknown") jatuh ke per exam+IP — pola yang sama dengan endpoint
		// siswa lain.
		pdfRateKey := fmt.Sprintf("%s%d:%s", pdfRateKeyPrefix, examID, deviceID)
		if deviceID == "" || deviceID == "unknown" {
			pdfRateKey = fmt.Sprintf("%s%d:ip:%s", pdfRateKeyPrefix, examID, c.ClientIP())
		}
		if !checkRateLimit(getRedis(c), pdfRateKey, pdfRateLimitMax, pdfRateLimitWindow) {
			errorResponse(c, http.StatusTooManyRequests,
				"Terlalu banyak request. Silakan coba lagi nanti.")
			return
		}

		var approvalStatus string
		err = pool.QueryRow(ctx,
			`SELECT status FROM exam_approvals WHERE exam_id = $1 AND mac_address = $2`,
			examID, deviceID).Scan(&approvalStatus)
		if err != nil || approvalStatus != "approved" {
			errorResponse(c, http.StatusForbidden, "Perangkat belum disetujui pengawas")
			return
		}

		// Serve PDF via Cloudflare R2 signed URL (Mandatory): only an ENABLED
		// backend may sign (mirrors admin ExamPDF / download guards).
		if r2c, exists := c.Get("r2"); exists {
			client := r2client.FromContext(r2c)
			if client != nil && client.Enabled() {
				r2Key := fmt.Sprintf("pdfs/%s", exam.FilePath)
				signedURL, err := client.SignedURL(ctx, r2Key, 1*time.Hour)
				if err == nil {
					c.Redirect(http.StatusFound, signedURL)
					return
				}
				log.Printf("api: R2 signed URL error: %v", err)
				errorResponseWithCode(c, http.StatusInternalServerError, r2client.ErrCodeSignURLFailed, r2client.ErrMsgSignURLFailed)
				return
			}
		}

		errorResponseWithCode(c, http.StatusInternalServerError, r2client.ErrCodeNotConfigured, r2client.ErrMsgNotConfigured)
	}
}

// ---------------------------------------------------------------------------
// 4. POST /api/exams/:exam_id/submit — Submit answers (async / sync)
// ---------------------------------------------------------------------------

// SubmitExam returns a gin.HandlerFunc that receives a student submission.
// When Redis is available the submission is enqueued for async processing
// (202 Accepted); otherwise it is processed inline (200 OK).
func SubmitExam() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		pool := getPool(c)
		rdb := getRedis(c)
		ctx := c.Request.Context()

		// --- Required Android version check ---
		// ONE policy across all three gate sites (M3): an ABSENT
		// X-App-Version header skips the check (web clients), exactly like
		// middleware.AndroidVersionCheck and ExamByToken — a client without
		// the header could list and join exams but was 426'd here before.
		// Versi EFEKTIF (dibatasi ke APK yang terbit). "" berarti tidak ada
		// APK yang bisa diunduh → penegakan dilewati, jika tidak client
		// usang terkunci di 426 tanpa cara memperbarui (deadlock).
		// Perbandingan via models.CompareVersions (M4): padding segmen
		// hilang ke 0 dan segmen non-numerik = 0, sama dengan middleware dan
		// UpdateManager di klien Android.
		required := models.EffectiveAndroidRequiredVersion(ctx, pool)

		clientVersion := c.GetHeader("X-App-Version")
		if clientVersion != "" && required != "" && models.CompareVersions(clientVersion, required) < 0 {
			displayVersion := clientVersion
			if displayVersion == "" {
				displayVersion = "v1.x"
			}
			c.JSON(http.StatusUpgradeRequired, gin.H{
				"success": false,
				"error":   "upgrade_required",
				"message": fmt.Sprintf(
					"Versi aplikasi Anda usang (%s). Silakan unduh EXAMVAN v%s terbaru untuk dapat mengumpulkan jawaban.",
					displayVersion, required),
			})
			return
		}

		// --- Parse request body ---
		var body struct {
			IdentityData map[string]interface{} `json:"identity_data"`
			StudentName  string                 `json:"student_name"`
			ExamNumber   string                 `json:"exam_number"`
			StudentClass string                 `json:"student_class"`
			Answers      map[string]interface{} `json:"answers"`
			StartTime    string                 `json:"start_time"`
			MACAddress   string                 `json:"mac_address"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			errorResponse(c, http.StatusBadRequest, "Data tidak valid")
			return
		}

		// --- Sanitise identity data ---
		var identityDataJSON *string
		var studentName, examNumber, studentClass string

		if body.IdentityData != nil {
			sanitized := sanitizeMap(body.IdentityData)
			raw, _ := json.Marshal(sanitized)
			s := string(raw)
			identityDataJSON = &s

			// Read standard keys from identity_data if present
			if v, ok := sanitized["student_name"].(string); ok {
				studentName = v
			}
			if v, ok := sanitized["exam_number"].(string); ok {
				examNumber = v
			}
			if v, ok := sanitized["student_class"].(string); ok {
				studentClass = v
			}
		}

		// Fallback: use top-level fields (covers custom identity keys like "nama")
		if studentName == "" {
			studentName = sanitize(body.StudentName)
		}
		if examNumber == "" {
			examNumber = sanitize(body.ExamNumber)
		}
		if studentClass == "" {
			studentClass = sanitize(body.StudentClass)
		}

		// --- MAC address sanitisation ---
		macAddress := sanitizeMAC(body.MACAddress)

		// --- Rate limit (per exam + device) ---
		// Keyed by exam AND MAC so that a single device cannot spam submissions,
		// while many distinct students in the same exam are NOT throttled by a
		// shared per-exam bucket (which previously 429'd legitimate classmates).
		//
		// Devices without a resolvable identifier (sanitizeMAC → "unknown",
		// e.g. fresh installs or emulated devices) all fall into one "unknown"
		// value — keying them together would let one classroom of MAC-less
		// devices drain a single shared bucket and mutually block each other.
		// For those, the bucket is keyed per client IP instead, so each device
		// still gets its own budget while a rotating-IP attacker remains bound
		// by the per-IP middleware limit on the route.
		rateKey := fmt.Sprintf("%s%d:%s", rateLimitKeyPrefix, examID, macAddress)
		if macAddress == "" || macAddress == "unknown" {
			rateKey = fmt.Sprintf("%s%d:ip:%s", rateLimitKeyPrefix, examID, c.ClientIP())
		}
		if !checkRateLimit(rdb, rateKey, submitRateLimitMax, submitRateLimitWindow) {
			errorResponse(c, http.StatusTooManyRequests,
				"Terlalu banyak percobaan submit. Silakan coba lagi nanti.")
			return
		}

		// --- Start time ---
		startTime := sanitizeStartTime(body.StartTime)

		// --- Verify exam is active and request carries the current token ---
		token := strings.TrimSpace(c.GetHeader("X-Exam-Token"))
		if token == "" {
			token = strings.TrimSpace(c.Query("token"))
		}
		if token == "" {
			errorResponse(c, http.StatusUnauthorized, "Token tidak disertakan")
			return
		}

		exam, err := models.GetExamByID(ctx, pool, examID)
		if err != nil {
			if err == pgx.ErrNoRows {
				errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
				return
			}
			log.Printf("submit exam lookup error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memproses jawaban")
			return
		}
		if !exam.IsActive() || exam.ExamStartedAt == nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}
		if models.ExamScheduleEnded(&exam, time.Now().UTC()) {
			// Longgarkan untuk RECOVERY resubmit (desktop/Android "Kirim Lagi"):
			// device yang MASIH memegang approval row 'approved' boleh mengirim
			// ulang jawaban lewat deadline. Approval hanya dicabut SETELAH
			// submit durable (sync path / worker pasca-commit), jadi approval
			// yang tersisa berarti submit sebelumnya GAGAL — persis skenario
			// yang dibuat fitur recovery (jaringan mati di deadline, siswa
			// re-entry nanti). Tanpa pengecualian ini, resubmit ditolak 403
			// setelah end_time+60s → jawaban siswa yang sudah dikerjakan hilang
			// permanen. Device TANPA approval (belum pernah di-approve / sudah
			// selesai) tetap ditolak — mencegah siswa kerja lewat deadline lalu
			// submit.
			var approvalStatus string
			apErr := pool.QueryRow(ctx,
				"SELECT status FROM exam_approvals WHERE exam_id = $1 AND mac_address = $2",
				examID, macAddress).Scan(&approvalStatus)
			if apErr != nil || approvalStatus != "approved" {
				errorResponse(c, http.StatusForbidden, "Waktu ujian telah berakhir")
				return
			}
		}

		if !examtoken.Matches(exam, token) {
			// Token mismatch (maybe stale/refreshed). Check if device is already approved.
			var approvalStatus string
			err := pool.QueryRow(ctx, "SELECT status FROM exam_approvals WHERE exam_id = $1 AND mac_address = $2", examID, macAddress).Scan(&approvalStatus)
			if err != nil || approvalStatus != "approved" {
				errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
				return
			}
			// Device is approved, so allow submission even with stale token
		}

		// Validate that all identity fields in the exam's config are filled.
		// If identity_fields config is empty, default to student_name, exam_number, student_class.
		var expectedFields []struct {
			Key      string `json:"key"`
			Label    string `json:"label"`
			Required bool   `json:"required"`
		}

		if exam.IdentityFields != nil && *exam.IdentityFields != "" && *exam.IdentityFields != "[]" {
			_ = json.Unmarshal([]byte(*exam.IdentityFields), &expectedFields)
		} else {
			expectedFields = []struct {
				Key      string `json:"key"`
				Label    string `json:"label"`
				Required bool   `json:"required"`
			}{
				{Key: "student_name", Label: "Nama Siswa", Required: true},
				{Key: "exam_number", Label: "Nomor Ujian", Required: true},
				{Key: "student_class", Label: "Kelas", Required: true},
			}
		}

		for _, field := range expectedFields {
			var val string
			if body.IdentityData != nil {
				if v, ok := body.IdentityData[field.Key].(string); ok {
					val = strings.TrimSpace(v)
				}
			}
			if val == "" {
				if field.Key == "student_name" {
					val = strings.TrimSpace(studentName)
				} else if field.Key == "exam_number" {
					val = strings.TrimSpace(examNumber)
				} else if field.Key == "student_class" {
					val = strings.TrimSpace(studentClass)
				}
			}

			if val == "" {
				errorResponse(c, http.StatusBadRequest, fmt.Sprintf("Identitas '%s' wajib diisi", field.Label))
				return
			}
		}

		// --- Answers JSON ---
		answersJSON := "{}"
		if body.Answers != nil {
			raw, _ := json.Marshal(body.Answers)
			answersJSON = string(raw)
		}

		// === ASYNC PATH (Redis available) ===
		if rdb != nil {
			job := map[string]interface{}{
				"exam_id":       examID,
				"student_name":  studentName,
				"exam_number":   examNumber,
				"student_class": studentClass,
				"identity_data": body.IdentityData,
				"answers":       body.Answers,
				"start_time":    startTime,
				"mac_address":   macAddress,
			}

			jobID, err := enqueueSubmission(rdb, job)
			if err != nil {
				log.Printf("async submit enqueue failed, falling back to sync: %v", err)
				// Fall through to sync path.
			} else {
				// Log heartbeat (best effort).
				setStudentHeartbeat(rdb, examID, macAddress, map[string]interface{}{
					"student_name":  studentName,
					"exam_number":   examNumber,
					"student_class": studentClass,
					"event":         "login",
					"last_seen":     time.Now().UTC().Format(time.RFC3339),
				})
				// NOTE: the device's approval row is deliberately NOT revoked
				// here. Revocation now happens in the queue worker, AFTER the
				// submission batch commits to PostgreSQL (see
				// queue.flushBatch). Revoking at enqueue time meant a job that
				// failed permanently (worker down / DB outage past retries)
				// left the student with a reported success, a cleared answer
				// sheet, and no way back in — the approval row is the re-entry
				// entitlement that lets the device retry the submit.
				c.JSON(http.StatusAccepted, gin.H{
					"success":          true,
					"message":          "Jawaban berhasil dikirim",
					"status":           "queued",
					"job_id":           jobID,
					"score":            nil,
					"congrats_message": exam.CongratsMessage,
				})
				return
			}
		}

		// === SYNC PATH (fallback / no Redis) ===
		score := calculateScoreSync(ctx, pool, examID, answersJSON)

		sub := &models.Submission{
			ExamID:       examID,
			StudentName:  studentName,
			ExamNumber:   examNumber,
			StudentClass: studentClass,
			AnswersJSON:  &answersJSON,
			Score:        score,
			StartTime:    &startTime,
			MACAddress:   macAddress,
			IdentityData: identityDataJSON,
		}

		if _, err := models.CreateSubmission(ctx, pool, sub); err != nil {
			log.Printf("sync submit insert error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan jawaban")
			return
		}
		// Cabut izin agar sesi berikutnya butuh persetujuan lagi
		_, _ = pool.Exec(ctx, "DELETE FROM exam_approvals WHERE exam_id = $1 AND mac_address = $2", examID, macAddress)

		c.JSON(http.StatusOK, gin.H{
			"success":          true,
			"message":          "Jawaban berhasil dikirim",
			"score":            score,
			"congrats_message": exam.CongratsMessage,
		})
	}
}

// calculateScoreSync parses questions JSON, evaluates answers, and returns
// the calculated score.  Returns nil when questions are absent.
func calculateScoreSync(ctx context.Context, pool *pgxpool.Pool, examID int, answersJSON string) *float64 {
	var questionsRaw string
	err := pool.QueryRow(ctx,
		`SELECT COALESCE(questions_json, '') FROM exams WHERE id = $1`, examID).Scan(&questionsRaw)
	if err != nil || questionsRaw == "" {
		return nil
	}

	questions, err := models.ParseQuestionsJSON(&questionsRaw)
	if err != nil || len(questions) == 0 {
		return nil
	}

	var answers map[string]interface{}
	if err := json.Unmarshal([]byte(answersJSON), &answers); err != nil {
		return nil
	}
	if answers == nil {
		return nil
	}

	return models.CalculateSubmissionScore(answers, questions)
}

// sanitizeMAC restricts a MAC address / device identifier to a safe character
// set and caps its length to 100 characters.
//
// Only characters valid in MAC addresses and device IDs are kept
// ([A-Za-z0-9:._-]); HTML/JS metacharacters (" ' < > & space, etc.) are
// stripped. This is a security boundary: the value is later rendered by the
// admin monitoring UI into HTML and JS-attribute contexts (via innerHTML),
// which are NOT protected by Go's html/template auto-escaping.
func sanitizeMAC(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') ||
			r == ':' || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	s := strings.TrimSpace(b.String())
	if len(s) > 100 {
		s = s[:100]
	}
	if s == "" {
		return "unknown"
	}
	return s
}

// sanitizeStartTime normalises a start-time string by replacing 'T' with a
// space and stripping trailing 'Z', matching the Python behaviour.
func sanitizeStartTime(raw string) string {
	if raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, "T", " ")
	raw = strings.TrimSuffix(raw, "Z")
	return raw
}

// ---------------------------------------------------------------------------
// 4b. GET /api/exams/:exam_id/result — Poll async submission result
// ---------------------------------------------------------------------------

// ExamResult returns a gin.HandlerFunc that lets a client poll the outcome of
// an async submission. When Redis is available, SubmitExam enqueues the job
// and answers status:"queued" with a job_id — there was previously no way for
// the student to learn the score (or even whether the submission was
// persisted). This endpoint reads the job result Redis key written by the
// queue worker; if that key has expired (5-minute TTL) it falls back to the
// database, matching the latest submission for the same exam + device identity
// so the score survives even a delayed poll.
//
// The client supplies either the job_id from the submit response, or the same
// mac_address + identity_data used on submit (used to match the DB row when
// the Redis result is gone). The response mirrors the sync submit shape so a
// client can treat both paths uniformly:
//
//	{"success":true, "status":"done", "score":87.5, "message":"..."}
//	{"success":true, "status":"pending"}          // queued but not yet processed
//	{"success":false, "message":"..."}            // job failed after retries
func ExamResult() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		pool := getPool(c)
		rdb := getRedis(c)
		ctx := c.Request.Context()

		jobID := strings.TrimSpace(c.Query("job_id"))
		macAddress := sanitizeMAC(c.Query("mac_address"))
		identityData := strings.TrimSpace(c.Query("identity_data"))

		// --- Rate limit (per exam + device, lalu aggregate per exam) ---
		// App mem-poll /result tiap 2,5 dtk saat submit ter-antri (≈24/menit per
		// perangkat) dan seluruh ruangan mem-poll bersamaan di deadline. Bucket
		// per-IP middleware tidak cukup: satu ruangan di belakang NAT sekolah
		// berbagi satu IP. Bucket per exam+MAC memberi tiap perangkat jatah
		// sendiri (NAT-independent); perangkat tanpa MAC (sanitizeMAC →
		// "unknown") jatuh ke per exam+IP. Bucket per-exam global menahan
		// aggregate/brute-force pada kapasitas ruangan terbesar (pola yang sama
		// dengan request-approval).
		rateKey := fmt.Sprintf("%s%d:%s", resultRateKeyPrefix, examID, macAddress)
		if macAddress == "" || macAddress == "unknown" {
			rateKey = fmt.Sprintf("%s%d:ip:%s", resultRateKeyPrefix, examID, c.ClientIP())
		}
		if !checkRateLimit(rdb, rateKey, resultRateLimitMax, resultRateLimitWindow) {
			errorResponse(c, http.StatusTooManyRequests,
				"Terlalu banyak request. Silakan coba lagi nanti.")
			return
		}
		if !checkRateLimit(rdb, fmt.Sprintf("%s%d", resultExamRateKeyPrefix, examID),
			resultExamRateLimitMax, resultExamRateLimitWindow) {
			errorResponse(c, http.StatusTooManyRequests,
				"Terlalu banyak request. Silakan coba lagi nanti.")
			return
		}

		// --- Access gate (mirrors AccessLog / SubmitExam tolerance) ---
		// The result endpoint must NOT be public: without a credential it leaks
		// a student's score (and their identity linkage) to anyone who knows a
		// device id + identity payload, before the teacher publishes results.
		// The poller is admitted when it holds the exam's current token or when
		// its device still carries an approved approval row (covers a poll that
		// lands between submit and the worker's post-commit revocation, and
		// token rotation in dynamic-token mode).
		token := strings.TrimSpace(c.GetHeader("X-Exam-Token"))
		if token == "" {
			token = strings.TrimSpace(c.Query("token"))
		}
		exam, err := models.GetExamByID(ctx, pool, examID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}
		resultAccessOK := false
		if token != "" && examtoken.Matches(exam, token) {
			resultAccessOK = true
		} else if macAddress != "" && macAddress != "unknown" {
			var approvalStatus string
			e := pool.QueryRow(ctx,
				"SELECT status FROM exam_approvals WHERE exam_id = $1 AND mac_address = $2",
				examID, macAddress).Scan(&approvalStatus)
			if e == nil && approvalStatus == "approved" {
				resultAccessOK = true
			}
		}
		if !resultAccessOK {
			errorResponse(c, http.StatusUnauthorized, "Akses ditolak")
			return
		}

		// Redis result first — the authoritative worker outcome.
		if jobID != "" && rdb != nil {
			if val, err := rdb.Get(ctx, queue.ResultKeyPrefix+jobID).Bytes(); err == nil {
				var res queue.JobResult
				if json.Unmarshal(val, &res) == nil {
					if res.Success {
						c.JSON(http.StatusOK, gin.H{
							"success": true,
							"status":  "done",
							"score":   res.Score,
							"message": "Jawaban berhasil disimpan",
						})
					} else {
						c.JSON(http.StatusOK, gin.H{
							"success": false,
							"status":  "failed",
							"message": res.Message,
						})
					}
					return
				}
			}
		}

		// Fallback: query the DB for the submission the async job would have
		// written, matching by exam + device identity (Redis result may have
		// expired). Requires the per-submission job_id secret: the identity
		// match alone is guessable — in static-token mode the exam token is
		// SHARED by the whole class, so a token alone must not unlock another
		// device's score before the teacher publishes results. Only the
		// submitting device ever received its job_id (the submit response),
		// which is also what the Redis lookup above already requires.
		if jobID != "" && macAddress != "" {
			sub, err := models.GetLatestSubmissionByIdentity(ctx, pool, examID, macAddress, identityData)
			if err == nil && sub.AnswersJSON != nil && *sub.AnswersJSON != "" {
				c.JSON(http.StatusOK, gin.H{
					"success": true,
					"status":  "done",
					"score":   sub.Score,
					"message": "Jawaban berhasil disimpan",
				})
				return
			}
		}

		// Still nothing durable — either the job is queued/processing or its
		// Redis result expired before the worker finished. Report pending.
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"status":  "pending",
			"score":   nil,
			"message": "Jawaban masih diproses",
		})
	}
}

// ---------------------------------------------------------------------------
// 5. POST /api/exams/:exam_id/access-log — Log student access events
// ---------------------------------------------------------------------------

// AccessLog returns a gin.HandlerFunc that records login / heartbeat /
// logout events from the Android app.
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		pool := getPool(c)
		rdb := getRedis(c)
		ctx := c.Request.Context()

		// --- Required Android version check ---
		// ONE policy across all three gate sites (M3): an ABSENT
		// X-App-Version header skips the check (web clients), exactly like
		// middleware.AndroidVersionCheck and ExamByToken. Versi EFEKTIF
		// (dibatasi ke APK yang terbit). "" berarti tidak ada APK yang bisa
		// diunduh → penegakan dilewati (deadlock bila di-enforce).
		// Perbandingan via models.CompareVersions (M4): padding segmen
		// hilang ke 0 dan segmen non-numerik = 0, sama dengan middleware dan
		// UpdateManager di klien Android.
		required := models.EffectiveAndroidRequiredVersion(ctx, pool)

		clientVersion := c.GetHeader("X-App-Version")
		if clientVersion != "" && required != "" && models.CompareVersions(clientVersion, required) < 0 {
			displayVersion := clientVersion
			if displayVersion == "" {
				displayVersion = "v1.x"
			}
			c.JSON(http.StatusUpgradeRequired, gin.H{
				"success": false,
				"error":   "upgrade_required",
				"message": fmt.Sprintf(
					"Versi aplikasi Anda usang (%s). Silakan unduh EXAMVAN v%s terbaru.",
					displayVersion, required),
			})
			return
		}

		// --- Parse request body ---
		var body struct {
			Event        string                 `json:"event"`
			MACAddress   string                 `json:"mac_address"`
			StudentName  string                 `json:"student_name"`
			ExamNumber   string                 `json:"exam_number"`
			StudentClass string                 `json:"student_class"`
			DeviceInfo   string                 `json:"device_info"`
			IdentityData map[string]interface{} `json:"identity_data"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			errorResponse(c, http.StatusBadRequest, "Data tidak valid")
			return
		}

		// --- Validate event type ---
		event := body.Event
		if event == "" {
			event = "heartbeat"
		}
		switch event {
		case "login", "heartbeat", "logout":
			// valid
		default:
			errorResponse(c, http.StatusBadRequest, "Event tidak valid")
			return
		}

		// --- MAC address ---
		macAddress := sanitizeMAC(body.MACAddress)

		// --- Rate limit (per exam + device) ---
		// Pola yang sama dengan SubmitExam: satu ruangan di belakang NAT sekolah
		// berbagi satu IP, dan tiap perangkat mengirim login + heartbeat tiap
		// menit + logout. Bucket per-IP middleware (120/menit) tidak cukup untuk
		// ruangan besar (30 perangkat = 30+/menit) — bucket per exam+MAC memberi
		// tiap perangkat jatah sendiri. Perangkat tanpa MAC (sanitizeMAC →
		// "unknown") jatuh ke bucket per exam+IP agar satu kelas perangkat tanpa
		// MAC tidak menguras satu bucket bersama dan saling memblokir.
		rateKey := fmt.Sprintf("%s%d:%s", presenceRateKeyPrefix, examID, macAddress)
		if macAddress == "" || macAddress == "unknown" {
			rateKey = fmt.Sprintf("%s%d:ip:%s", presenceRateKeyPrefix, examID, c.ClientIP())
		}
		if !checkRateLimit(rdb, rateKey, presenceRateLimitMax, presenceRateLimitWindow) {
			errorResponse(c, http.StatusTooManyRequests,
				"Terlalu banyak request. Silakan coba lagi nanti.")
			return
		}

		// --- Truncate string fields ---
		studentName := truncate(body.StudentName, 200)
		examNumber := truncate(body.ExamNumber, 100)
		studentClass := truncate(body.StudentClass, 100)
		deviceInfo := truncate(body.DeviceInfo, 200)
		ipAddress := c.ClientIP()

		var identityDataJSON *string
		if body.IdentityData != nil {
			sanitized := sanitizeMap(body.IdentityData)
			b, err := json.Marshal(sanitized)
			if err == nil {
				s := string(b)
				identityDataJSON = &s
			}
		}

		// --- Verify exam is active and the request carries a valid token ---
		// Same contract as SubmitExam/CompleteExam: a valid current token, or an
		// already-approved device (tolerates token rotation). Without this,
		// anyone could inject login/logout logs and heartbeat presence for any
		// active exam ID and pollute the monitoring dashboard.
		token := strings.TrimSpace(c.GetHeader("X-Exam-Token"))
		if token == "" {
			token = strings.TrimSpace(c.Query("token"))
		}
		exam, err := models.GetExamByID(ctx, pool, examID)
		if err != nil || !exam.IsActive() || exam.ExamStartedAt == nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}
		if models.ExamScheduleEnded(&exam, time.Now().UTC()) {
			errorResponse(c, http.StatusForbidden, "Waktu ujian telah berakhir")
			return
		}
		if !examtoken.Matches(exam, token) {
			var approvalStatus string
			e := pool.QueryRow(ctx,
				"SELECT status FROM exam_approvals WHERE exam_id = $1 AND mac_address = $2", examID, macAddress).Scan(&approvalStatus)
			if e != nil || approvalStatus != "approved" {
				errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
				return
			}
		}

		// --- Insert access log (login/logout only, heartbeats are Redis-only for database performance) ---
		// The submissions lookup below only ever feeds CreateAccessLog, so the
		// WHOLE block is skipped for heartbeats: at wave scale heartbeats are
		// ~90% of access-log traffic and this used to cost one extra indexed
		// SELECT per heartbeat whose result was immediately discarded.
		if event != "heartbeat" {
			// --- Find matching submission by MAC address ---
			var submissionID *int
			if macAddress != "" && macAddress != "unknown" {
				var sid int
				err := pool.QueryRow(ctx,
					`SELECT id FROM submissions WHERE exam_id = $1 AND mac_address = $2 ORDER BY id DESC LIMIT 1`,
					examID, macAddress).Scan(&sid)
				if err == nil {
					submissionID = &sid
				}
			}

			accessLog := &models.StudentAccessLog{
				ExamID:            examID,
				SubmissionID:      submissionID,
				StudentIdentifier: macAddress,
				StudentName:       strPtr(studentName),
				ExamNumber:        strPtr(examNumber),
				StudentClass:      strPtr(studentClass),
				Event:             event,
				IPAddress:         ipAddress,
				DeviceInfo:        deviceInfo,
				IdentityData:      identityDataJSON,
			}

			if _, err := models.CreateAccessLog(ctx, pool, accessLog); err != nil {
				log.Printf("access-log insert error: %v", err)
				errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan log")
				return
			}
		}

		// --- Write heartbeat to Redis (login / heartbeat only) ---
		if event == "login" || event == "heartbeat" {
			heartbeatData := map[string]interface{}{
				"student_name":  studentName,
				"exam_number":   examNumber,
				"student_class": studentClass,
				"device_info":   deviceInfo,
				"ip_address":    ipAddress,
				"event":         event,
				"last_seen":     time.Now().UTC().Format(time.RFC3339),
			}
			setStudentHeartbeat(rdb, examID, macAddress, heartbeatData)

			// Push heartbeat to Redis queue to be flushed to DB asynchronously
			if event == "heartbeat" && rdb != nil {
				heartbeatData["exam_id"] = examID
				heartbeatData["mac_address"] = macAddress
				if payload, err := json.Marshal(heartbeatData); err == nil {
					_ = rdb.LPush(ctx, "examvan:heartbeats:pending", payload).Err()
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Access logged",
		})
	}
}

// CompleteExam records the completion of an exam by deleting the student's heartbeat.
// POST /api/exams/:exam_id/complete
func CompleteExam() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		var body struct {
			MACAddress string `json:"mac_address"`
			Token      string `json:"token"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			errorResponse(c, http.StatusBadRequest, "Payload tidak valid")
			return
		}

		if body.MACAddress == "" {
			errorResponse(c, http.StatusBadRequest, "MAC address diperlukan")
			return
		}

		// --- Verify token ---
		token := strings.TrimSpace(c.GetHeader("X-Exam-Token"))
		if token == "" {
			token = strings.TrimSpace(body.Token)
		}
		if token == "" {
			token = strings.TrimSpace(c.Query("token"))
		}
		if token == "" {
			errorResponse(c, http.StatusUnauthorized, "Token tidak disertakan")
			return
		}

		pool := getPool(c)
		rdb := getRedis(c)
		ctx := c.Request.Context()

		// --- Sanitise MAC ---
		macAddress := sanitizeMAC(body.MACAddress)

		// --- Rate limit (per exam + device) ---
		// Semua perangkat menyelesaikan ujian bersamaan (deadline burst) dari
		// satu NAT sekolah; bucket per-IP middleware tidak cukup untuk ruangan
		// besar. Bucket per exam+MAC (pola SubmitExam) memberi tiap perangkat
		// jatah sendiri; MAC tidak dikenal → per exam+IP.
		rateKey := fmt.Sprintf("%s%d:%s", presenceRateKeyPrefix, examID, macAddress)
		if macAddress == "" || macAddress == "unknown" {
			rateKey = fmt.Sprintf("%s%d:ip:%s", presenceRateKeyPrefix, examID, c.ClientIP())
		}
		if !checkRateLimit(rdb, rateKey, presenceRateLimitMax, presenceRateLimitWindow) {
			errorResponse(c, http.StatusTooManyRequests,
				"Terlalu banyak request. Silakan coba lagi nanti.")
			return
		}

		exam, err := models.GetExamByID(ctx, pool, examID)
		if err != nil {
			if err == pgx.ErrNoRows {
				errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
				return
			}
			log.Printf("complete exam lookup error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memproses permintaan")
			return
		}
		if !exam.IsActive() || !examtoken.Matches(exam, token) {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}
		// NOTE: CompleteExam is deliberately NOT gated on end_time — the client
		// calls it right after a (valid) submit at the deadline to clear its
		// heartbeat; blocking it there would strand the offline indicator.

		// Delete heartbeat from Redis so student shows offline immediately
		if rdb != nil {
			key := fmt.Sprintf("heartbeat:%d:%s", examID, macAddress)
			_ = rdb.Del(ctx, key).Err()
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Exam completed status recorded",
		})
	}
}

// ---------------------------------------------------------------------------
// Small utilities
// ---------------------------------------------------------------------------

// stripAnswerKeys removes the answer-key fields ("key" and its legacy alias
// "answer") from every question in a questions payload. The payload is decoded
// from JSON as interface{} (json.Unmarshal), so questions arrive as []interface{}
// of map[string]interface{} — the helper walks that generic shape and deletes
// the key/answer entries in place. It also accepts the typed
// []map[string]interface{} shape used by other callers. Nil, non-array, and
// non-map payloads are no-ops. It mirrors the stripping the public hasil page
// applies to anonymous viewers (see public/hasil.go).
func stripAnswerKeys(questions interface{}) {
	switch v := questions.(type) {
	case []interface{}:
		for _, item := range v {
			if q, ok := item.(map[string]interface{}); ok {
				delete(q, "key")
				delete(q, "answer")
			}
		}
	case []map[string]interface{}:
		for _, q := range v {
			delete(q, "key")
			delete(q, "answer")
		}
	}
}

// truncate returns the first n runes of s.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// strPtr returns a pointer to s, or nil when s is empty.
// (M4: isVersionAtLeast/parseVersionParts were removed — every version gate
// now compares via models.CompareVersions, whose padding and non-numeric
// semantics match middleware.isVersionCompatible and the Android client's
// UpdateManager.)

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
