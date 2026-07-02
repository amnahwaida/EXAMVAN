package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/examvan/webui/internal/config"
	r2client "github.com/examvan/webui/internal/handlers/r2"
	"github.com/examvan/webui/internal/models"
	"github.com/examvan/webui/internal/queue"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	requiredAndroidVersion = "2.2.0"

	// cacheTTL is how long the active exam list lives in Redis (seconds).
	cacheTTL = 30 * time.Second

	// Rate-limit parameters for the submit endpoint.
	submitRateLimitMax    = 10
	submitRateLimitWindow = 60 * time.Second

	// Redis key prefixes.
	cacheKeyPrefix       = "api:exams:list:"       // + page:per_page

	rateLimitKeyPrefix   = "ratelimit:submit:"     // + exam_id
	heartbeatKeyPrefix   = "heartbeat:"            // + exam_id:mac_address
	heartbeatTTL         = 5 * time.Minute
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

// getStoragePath returns the configured storage directory, falling back to
// the compiled-in default when no config is available in context.
func getStoragePath(c *gin.Context) string {
	if cfg, exists := c.Get("cfg"); exists {
		return cfg.(*config.Config).StoragePath
	}
	return config.DefaultStoragePath
}

// ---------------------------------------------------------------------------
// Generic response helpers
// ---------------------------------------------------------------------------

func errorResponse(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"success": false, "message": message})
	_ = c.AbortWithError(status, fmt.Errorf("%s", message))
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

// ---------------------------------------------------------------------------
// Identity-fields parsing
// ---------------------------------------------------------------------------

// parseIdentityFields unmarshals the identity_fields JSON column.  Falls
// back to the three-field default (student_name, exam_number, student_class).
func parseIdentityFields(raw *string) []map[string]interface{} {
	if raw == nil || *raw == "" {
		return defaultIdentityFields
	}
	var fields []map[string]interface{}
	if err := json.Unmarshal([]byte(*raw), &fields); err != nil || len(fields) == 0 {
		return defaultIdentityFields
	}
	return fields
}

// ---------------------------------------------------------------------------
// Safe storage-path resolution
// ---------------------------------------------------------------------------

// safeStoragePath resolves a relative file path against the storage
// directory and verifies the result does not escape via directory traversal.
func safeStoragePath(baseDir, relPath string) (string, error) {
	cleanBase := filepath.Clean(baseDir)
	full := filepath.Join(cleanBase, filepath.Clean(relPath))
	if !strings.HasPrefix(full, cleanBase) {
		return "", fmt.Errorf("path traversal detected: %s", relPath)
	}
	return full, nil
}

// ---------------------------------------------------------------------------
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

		// --- Redis cache lookup ---
		if rdb != nil {
			cacheKey := fmt.Sprintf("%s%d:%d", cacheKeyPrefix, page, perPage)
			cached, err := rdb.Get(ctx, cacheKey).Result()
			if err == nil {
				c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(cached))
				return
			}
		}

		// --- Database query ---
		result, err := models.ListActiveExams(ctx, pool, page, perPage)
		if err != nil {
			log.Printf("list active exams error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat daftar ujian")
			return
		}

		type item struct {
			ID        int     `json:"id"`
			Name      string  `json:"name"`
			Status    string  `json:"status"`
			SizeMB    float64 `json:"size_mb"`
			CreatedAt string  `json:"created_at"`
		}

		items := make([]item, 0, len(result.Exams))
		for _, e := range result.Exams {
			items = append(items, item{
				ID:        e.ID,
				Name:      e.Name,
				Status:    e.Status,
				SizeMB:    roundTo(float64(e.SizeBytes)/(1024*1024), 2),
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

		// --- Store in Redis cache ---
		if rdb != nil {
			cacheKey := fmt.Sprintf("%s%d:%d", cacheKeyPrefix, page, perPage)
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
// 2. GET /api/exams/token/:token — Get exam details by token (Android)
// ---------------------------------------------------------------------------

// ExamByToken returns a gin.HandlerFunc for the token-based exam lookup.
// The Android app calls this to fetch exam metadata after scanning a QR code
// or entering a token manually.
func ExamByToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Param("token")

		pool := getPool(c)
		ctx := c.Request.Context()

		// Optional version check — if the header is absent the check is
		// skipped, matching the Python behaviour.
		clientVersion := c.GetHeader("X-App-Version")
		if clientVersion != "" {
			required := models.GetSaasSettingWithDefault(ctx, pool,
				models.SettingAndroidVersion, requiredAndroidVersion)
			if !isVersionAtLeast(clientVersion, required) {
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

		// Auto-reset active_token if exam has started and token mode is dynamic
		if exam.TokenMode != nil && *exam.TokenMode == "dynamic" &&
			exam.ExamStartedAt != nil &&
			exam.TokenResetInterval != nil && *exam.TokenResetInterval > 0 {
			shouldReset := true
			if exam.TokenLastResetAt != nil {
				nextReset := exam.TokenLastResetAt.Add(time.Duration(*exam.TokenResetInterval) * time.Minute)
				if time.Now().UTC().Before(nextReset) {
					shouldReset = false
				}
			}
			if shouldReset {
				newToken := generateToken()
				if err := models.UpdateExamActiveToken(ctx, pool, exam.ID, newToken); err == nil {
					exam.ActiveToken = newToken
				}
			}
		}

		identityFields := parseIdentityFields(exam.IdentityFields)

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

		// Verify exam exists with matching token.
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
		if exam.Token != token {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}

		// Priority 1: Serve PDF via Cloudflare R2 signed URL
		if r2c, exists := c.Get("r2"); exists {
			client := r2c.(*r2client.Client)
			if client.Enabled() {
				r2Key := fmt.Sprintf("pdfs/%s", exam.FilePath)
				signedURL, err := client.SignedURL(ctx, r2Key, 1*time.Hour)
				if err == nil {
					c.Redirect(http.StatusFound, signedURL)
					return
				}
				log.Printf("api: R2 signed URL error: %v — fallback to local", err)
			}
		}

		// Priority 2: Fallback — serve from local storage
		storageDir := getStoragePath(c)
		pdfPath, err := safeStoragePath(storageDir, exam.FilePath)
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "Path tidak valid")
			return
		}

		if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
			errorResponse(c, http.StatusNotFound, "File PDF tidak ditemukan")
			return
		}

		c.Header("Content-Type", "application/pdf")
		c.Header("X-Accel-Redirect", "/internal/pdf/"+exam.FilePath)
		c.Status(http.StatusOK)
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

		// --- Rate limit ---
		if !checkRateLimit(rdb, fmt.Sprintf("%s%d", rateLimitKeyPrefix, examID),
			submitRateLimitMax, submitRateLimitWindow) {
			errorResponse(c, http.StatusTooManyRequests,
				"Terlalu banyak percobaan submit. Silakan coba lagi nanti.")
			return
		}

		// --- Required Android version check ---
		required := models.GetSaasSettingWithDefault(ctx, pool,
			models.SettingAndroidVersion, requiredAndroidVersion)

		clientVersion := c.GetHeader("X-App-Version")
		if !isVersionAtLeast(clientVersion, required) {
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

		// --- Start time ---
		startTime := sanitizeStartTime(body.StartTime)

		// --- Validate required fields ---
		// Only student_name is always required. exam_number and student_class
		// depend on the exam's identity_fields configuration — some exams
		// only define 2 fields (name + number) without class.
		if studentName == "" {
			errorResponse(c, http.StatusBadRequest, "Identitas siswa tidak lengkap")
			return
		}

		// --- Verify exam is active ---
		var exists int
		err = pool.QueryRow(ctx,
			`SELECT 1 FROM exams WHERE id = $1 AND status = 'active'`, examID).Scan(&exists)
		if err != nil {
			if err == pgx.ErrNoRows {
				errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
				return
			}
			log.Printf("submit exam lookup error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memproses jawaban")
			return
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
				"exam_id":        examID,
				"student_name":   studentName,
				"exam_number":    examNumber,
				"student_class":  studentClass,
				"identity_data":  body.IdentityData,
				"answers":        body.Answers,
				"start_time":     startTime,
				"mac_address":    macAddress,
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

				c.JSON(http.StatusOK, gin.H{
					"success": true,
					"message": "Jawaban berhasil dikirim",
					"status":  "queued",
					"job_id":  jobID,
					"score":   nil,
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

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Jawaban berhasil dikirim",
			"score":   score,
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

// sanitizeMAC filters printable characters from a MAC address string and
// caps its length to 100 characters.
func sanitizeMAC(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r >= 32 && r <= 126 { // printable ASCII
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
		required := models.GetSaasSettingWithDefault(ctx, pool,
			models.SettingAndroidVersion, requiredAndroidVersion)

		clientVersion := c.GetHeader("X-App-Version")
		if !isVersionAtLeast(clientVersion, required) {
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

		// --- Verify exam exists and is active ---
		var examExists int
		err = pool.QueryRow(ctx,
			`SELECT 1 FROM exams WHERE id = $1 AND status = 'active'`, examID).Scan(&examExists)
		if err != nil {
			if err == pgx.ErrNoRows {
				errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
				return
			}
			log.Printf("access-log exam lookup error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memproses log")
			return
		}

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

		// --- Insert access log (login/logout only, heartbeats are Redis-only for database performance) ---
		if event != "heartbeat" {
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

// ---------------------------------------------------------------------------
// Small utilities
// ---------------------------------------------------------------------------

// generateToken creates an 8-character uppercase alphanumeric token (A-Z, 0-9).
func generateToken() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 8)
	for i := range b {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			return fmt.Sprintf("%08X", time.Now().UnixNano()%99999999)
		}
		b[i] = chars[idx.Int64()]
	}
	return string(b)
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
// isVersionAtLeast compares two version strings (major.minor.patch).
func isVersionAtLeast(client, required string) bool {
	cp := parseVersionParts(client)
	rp := parseVersionParts(required)
	for i := 0; i < len(rp) && i < len(cp); i++ {
		if cp[i] > rp[i] {
			return true
		}
		if cp[i] < rp[i] {
			return false
		}
	}
	return len(cp) >= len(rp)
}

// parseVersionParts splits a version string into integer parts.
func parseVersionParts(v string) []int {
	parts := strings.Split(v, ".")
	var result []int
	for _, p := range parts {
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err == nil {
			result = append(result, n)
		}
	}
	return result
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
