// Package public provides Gin handler functions for EXAMVAN public (no-login)
// routes: exam results page and download page.
package public

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/helpers"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Default identity fields (mirrors Python DEFAULT_IDENTITY_FIELDS)
// ---------------------------------------------------------------------------

var defaultIdentityFields []map[string]interface{}

func init() {
	_ = json.Unmarshal([]byte(`[
		{"key":"student_name","label":"Nama","required":true},
		{"key":"exam_number","label":"Nomor Ujian","required":true},
		{"key":"student_class","label":"Kelas","required":true}
	]`), &defaultIdentityFields)
}

// ---------------------------------------------------------------------------
// Context helpers (duplicated per-package; no shared package exists yet)
// ---------------------------------------------------------------------------

func getPool(c *gin.Context) *pgxpool.Pool {
	val, exists := c.Get("db")
	if !exists || val == nil {
		return nil
	}
	pool, ok := val.(*pgxpool.Pool)
	if !ok {
		return nil
	}
	return pool
}

// ---------------------------------------------------------------------------
// Time formatting
// ---------------------------------------------------------------------------

// R66: zona sekolah WIB untuk waktu tampilan halaman hasil — selaras
// submissions.go (Batch 10/S49). LoadLocation gagal (kontainer tanpa tzdata)
// → fallback FixedZone dengan offset yang sama (WIB tidak mengenal DST).
var jakartaLoc = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		return time.FixedZone("WIB", 7*60*60)
	}
	return loc
}()

// formatWIBDisplay formats a time as a human-readable WIB display string.
func formatWIBDisplay(t time.Time) string {
	return t.In(jakartaLoc).Format("2006-01-02 15:04")
}

// parseLegacyUTCTime parses legacy submission timestamp strings ("2024-01-15
// 10:30:00" naive-UTC, atau ISO "2024-01-15T10:30:00Z") sebagai UTC.
func parseLegacyUTCTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05Z", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// formatISOUTC formats a time.Time as a UTC ISO 8601 string.
func formatISOUTC(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

// formatISOUTCString converts a legacy timestamp string (e.g. "2024-01-15 10:30:00")
// to ISO 8601 UTC ("2024-01-15T10:30:00Z"). Returns nil when s is empty.
func formatISOUTCString(s string) interface{} {
	if s == "" {
		return nil
	}
	if strings.Contains(s, " ") {
		return strings.Replace(s, " ", "T", 1) + "Z"
	}
	if strings.HasSuffix(s, "Z") {
		return s
	}
	return s + "Z"
}



// ---------------------------------------------------------------------------
// 1. GET /hasil/:token — Public exam results page (HTML)
// ---------------------------------------------------------------------------

// HasilPage renders the public hasil.html page for a given exam token.
func HasilPage() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.ToUpper(strings.TrimSpace(c.Param("token")))

		// Results change as students submit and contain student names + scores:
		// never cache the page and block search-engine indexing (X-Robots-Tag is
		// the strongest signal; the template also carries a <meta name=robots>).
		c.Header("X-Robots-Tag", "noindex, nofollow")
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")

		pool := getPool(c)
		if pool == nil {
			// T12: pesan internal JANGAN dipasangkan ke exam_name — dulu field
			// ini dirender sebagai <h1> hero raksasa. Kirim flag error bersih
			// + judul netral; pesan user-friendly ada di kartu error template.
			c.HTML(http.StatusInternalServerError, "public/hasil.html", middleware.MergeTemplateData(c, gin.H{
				"error_state":    true,
				"exam_name":      "",
				"token":          token,
				"total_students": 0,
				"error":          true,
			}))
			return
		}
		ctx := c.Request.Context()

		exam, err := models.GetExamByToken(ctx, pool, token)
		if err != nil {
			if err == pgx.ErrNoRows {
				c.HTML(http.StatusNotFound, "public/hasil.html", middleware.MergeTemplateData(c, gin.H{
					"error_state":    true,
					"exam_name":      "",
					"token":          token,
					"total_students": 0,
					"error":          true,
				}))
				return
			}
			log.Printf("hasil page exam lookup error: %v", err)
			c.HTML(http.StatusInternalServerError, "public/hasil.html", middleware.MergeTemplateData(c, gin.H{
				"error_state":    true,
				"exam_name":      "",
				"token":          token,
				"total_students": 0,
				"error":          true,
			}))
			return
		}

		_, isLoggedIn := c.Get("user_id")
		if !exam.AreResultsPublic() && !isLoggedIn {
			c.HTML(http.StatusForbidden, "public/hasil.html", middleware.MergeTemplateData(c, gin.H{
				"exam_name":   exam.Name,
				"token":       token,
				"is_disabled": true,
			}))
			return
		}

		// Count only students who actually submitted (heartbeat rows with empty
		// answers_json are excluded — matching the admin submissions view).
		var total int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM submissions WHERE exam_id = $1 AND answers_json IS NOT NULL AND answers_json != ''`, exam.ID).Scan(&total); err != nil {
			log.Printf("hasil page count error: %v", err)
			total = 0
		}

		// Display names only: the `name` column (never the login username —
		// that is a credential half, not a display name). Empty name keeps the
		// span hidden in the template, same as a failed lookup did before.
		var creatorName string
		pool.QueryRow(ctx, `SELECT COALESCE(name, '') FROM admin_users WHERE id = $1`, exam.CreatedBy).Scan(&creatorName)

		var delegatedName string
		if exam.DelegatedTo != nil {
			pool.QueryRow(ctx, `SELECT COALESCE(name, '') FROM admin_users WHERE id = $1`, *exam.DelegatedTo).Scan(&delegatedName)
		}

		c.HTML(http.StatusOK, "public/hasil.html", middleware.MergeTemplateData(c, gin.H{
			"exam_name":      exam.Name,
			"token":          exam.Token,
			"total_students": total,
			"is_logged_in":   isLoggedIn,
			"show_answers":   exam.AreAnswersShown(),
			"creator_name":   creatorName,
			"delegated_name": delegatedName,
		}))
	}
}

// ---------------------------------------------------------------------------
// 2. GET /api/hasil/:token — Public exam results JSON API
// ---------------------------------------------------------------------------

// HasilAPI returns a JSON response with paginated exam results for a given token.
func HasilAPI() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.ToUpper(strings.TrimSpace(c.Param("token")))

		// Results change as students submit: never let proxies/browsers cache.
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")

		pool := getPool(c)
		if pool == nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "Database tidak tersedia.",
			})
			return
		}
		ctx := c.Request.Context()

		exam, err := models.GetExamByToken(ctx, pool, token)
		if err != nil {
			if err == pgx.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{
					"success": false,
					"message": "Token ujian tidak valid atau ujian tidak ditemukan.",
				})
				return
			}
			log.Printf("hasil api exam lookup error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "Gagal memuat data ujian",
			})
			return
		}

		_, isLoggedIn := c.Get("user_id")
		if !exam.AreResultsPublic() && !isLoggedIn {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "Akses dinonaktifkan: Halaman hasil ujian untuk siswa dinonaktifkan oleh guru.",
			})
			return
		}

		// ---- Pagination ----
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		if page < 1 {
			page = 1
		}
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "100"))
		if perPage < 1 {
			perPage = 1
		} else if perPage > 500 {
			perPage = 500
		}
		offset := (page - 1) * perPage

		// ---- Search (server-side, on student name) ----
		search := strings.TrimSpace(c.DefaultQuery("search", ""))

		// ---- Stats (aggregated over the FULL submitted set, independent of
		// search/pagination, so the header stays stable while browsing) ----
		var statCount int
		var statAvg, statMax, statMin float64
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*),
			        COALESCE(AVG(score), 0), COALESCE(MAX(score), 0), COALESCE(MIN(score), 0)
			 FROM submissions
			 WHERE exam_id = $1 AND answers_json IS NOT NULL AND answers_json != ''`,
			exam.ID).Scan(&statCount, &statAvg, &statMax, &statMin); err != nil {
			log.Printf("hasil api stats error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "Gagal memuat data hasil",
			})
			return
		}

		// ---- Total count (search-filtered, for pagination) ----
		var total int
		countSQL := `SELECT COUNT(*) FROM submissions WHERE exam_id = $1 AND answers_json IS NOT NULL AND answers_json != ''`
		countArgs := []interface{}{exam.ID}
		if search != "" {
			countSQL += ` AND student_name ILIKE '%' || $2 || '%'`
			countArgs = append(countArgs, search)
		}
		if err := pool.QueryRow(ctx, countSQL, countArgs...).Scan(&total); err != nil {
			log.Printf("hasil api count error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "Gagal memuat data hasil",
			})
			return
		}

		// ---- Fetch submissions (only rows that actually submitted) ----
		querySQL := `SELECT id, student_name, exam_number, student_class,
			        answers_json, score, start_time, created_at, identity_data
			 FROM submissions
			 WHERE exam_id = $1 AND answers_json IS NOT NULL AND answers_json != ''`
		queryArgs := []interface{}{exam.ID}
		if search != "" {
			querySQL += ` AND student_name ILIKE '%' || $2 || '%'`
			queryArgs = append(queryArgs, search)
		}
		querySQL += ` ORDER BY score DESC NULLS LAST
			 LIMIT $` + strconv.Itoa(len(queryArgs)+1) + ` OFFSET $` + strconv.Itoa(len(queryArgs)+2)
		queryArgs = append(queryArgs, perPage, offset)

		rows, err := pool.Query(ctx, querySQL, queryArgs...)
		if err != nil {
			log.Printf("hasil api submissions query error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "Gagal memuat data hasil",
			})
			return
		}
		defer rows.Close()

		// ---- Parse questions & compute max_score ----
		questions := make([]map[string]interface{}, 0)
		maxScore := 0.0
		var typedQuestions []models.Question
		if exam.QuestionsJSON != nil && *exam.QuestionsJSON != "" {
			var parsed []map[string]interface{}
			if err := json.Unmarshal([]byte(*exam.QuestionsJSON), &parsed); err != nil {
				log.Printf("hasil api parse questions error: %v", err)
			} else {
				questions = parsed
			}
			// Typed questions drive BOTH the per-question evaluation engine and
			// max_score. ComputeMaxScore uses the same GetWeight() semantics as
			// EvaluateAnswersDetailed (weight <= 0 falls back to score, then to
			// 1.0), so the detail-modal total (earned / max_score) can never
			// show earned > max for a weight-0 question.
			typedQuestions, _ = models.ParseQuestionsJSON(exam.QuestionsJSON)
			maxScore = models.ComputeMaxScore(typedQuestions)
		}

		// ---- Build submission list ----
		type submissionItem struct {
			ID               int                             `json:"id"`
			StudentName      string                          `json:"student_name"`
			ExamNumber       string                          `json:"exam_number"`
			StudentClass     string                          `json:"student_class"`
			IdentityData     map[string]interface{}          `json:"identity_data"`
			Score            *float64                        `json:"score"`
			MaxScore         *float64                        `json:"max_score"`
			StartTime        interface{}                     `json:"start_time"`
			CreatedAt        string                          `json:"created_at"`
			// R66: waktu tampilan terformat WIB dari server — penonton tidak
			// lagi melihat jam menurut zona perangkatnya (selaras kartu guru).
			StartTimeDisplay string                             `json:"start_time_display,omitempty"`
			CreatedAtDisplay string                             `json:"created_at_display,omitempty"`
			Answers          map[string]interface{}             `json:"answers,omitempty"`
			EvaluatedAnswers map[string]models.EvaluationDetail `json:"evaluated_answers"`
		}

		subsData := make([]submissionItem, 0, perPage)
		showAnswersEnabled := exam.AreAnswersShown()

		for rows.Next() {
			var (
				id           int
				studentName  string
				examNumber   string
				studentClass string
				answersJSON  *string
				score        *float64
				startTime    *string
				createdAt    time.Time
				identityData *string
			)

			if err := rows.Scan(
				&id, &studentName, &examNumber, &studentClass,
				&answersJSON, &score, &startTime, &createdAt, &identityData,
			); err != nil {
				log.Printf("hasil api scan submission error: %v", err)
				continue
			}

			// Parse identity_data JSON
			idData := make(map[string]interface{})
			if identityData != nil && *identityData != "" {
				if err := json.Unmarshal([]byte(*identityData), &idData); err != nil {
					idData = make(map[string]interface{})
				}
			}

			// maxScore pointer (nil when no questions)
			var maxScorePtr *float64
			if maxScore > 0 {
				maxScorePtr = &maxScore
			}

			// Parse answers JSON for the per-question detail modal
			answers := make(map[string]interface{})
			if answersJSON != nil && *answersJSON != "" {
				if err := json.Unmarshal([]byte(*answersJSON), &answers); err != nil {
					answers = make(map[string]interface{})
				}
			}
			evaluated := models.EvaluateAnswersDetailed(answers, typedQuestions)
			if evaluated == nil {
				evaluated = map[string]models.EvaluationDetail{}
			}

		// R66: waktu tampilan WIB dihitung server-side; field ISO mentah tetap
		// dikirim untuk perhitungan durasi sisi klien (getDurationString).
		startTimeDisplay := ""
		if st, ok := parseLegacyUTCTime(ptrString(startTime)); ok {
			startTimeDisplay = formatWIBDisplay(st)
		}

		item := submissionItem{
			ID:               id,
			StudentName:      studentName,
			ExamNumber:       examNumber,
			StudentClass:     studentClass,
			IdentityData:     idData,
			Score:            score,
			MaxScore:         maxScorePtr,
			StartTime:        formatISOUTCString(ptrString(startTime)),
			CreatedAt:        formatISOUTC(createdAt),
			StartTimeDisplay: startTimeDisplay,
			CreatedAtDisplay: formatWIBDisplay(createdAt),
			EvaluatedAnswers: evaluated,
		}
			// Raw student answers are sent only when the visitor is entitled
			// (logged in or the teacher enabled show_answers); otherwise the
			// frontend masks them while still showing per-question status.
			if showAnswersEnabled || isLoggedIn {
				item.Answers = answers
			}
			subsData = append(subsData, item)
		}

		// Strip answer keys when the user is not logged in and show_answers is disabled
		if !isLoggedIn && !showAnswersEnabled {
			for _, q := range questions {
				delete(q, "key")
				delete(q, "answer")
			}
		}

		identityFields := helpers.ParseIdentityFields(exam.IdentityFields, defaultIdentityFields)

		// Pagination metadata
		totalPages := (total + perPage - 1) / perPage
		if totalPages < 1 {
			totalPages = 1
		}

		// Match Python null-vs-number for max_score
		var maxScoreResponse interface{}
		if maxScore > 0 {
			maxScoreResponse = maxScore
		} else {
			maxScoreResponse = nil
		}

		c.JSON(http.StatusOK, gin.H{
			"success":         true,
			"exam_name":       exam.Name,
			"show_answers":    showAnswersEnabled,
			"token":           exam.Token,
			"questions":       questions,
			"identity_fields": identityFields,
			"max_score":       maxScoreResponse,
			"submissions":     subsData,
			"stats": gin.H{
				"count":   statCount,
				"average": statAvg,
				"max":     statMax,
				"min":     statMin,
			},
			"pagination": gin.H{
				"page":        page,
				"per_page":    perPage,
				"total":       total,
				"total_pages": totalPages,
			},
		})
	}
}

// ptrString returns the value of a string pointer, or empty string when nil.
func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
