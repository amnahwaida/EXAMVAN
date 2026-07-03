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
	return c.MustGet("db").(*pgxpool.Pool)
}

// ---------------------------------------------------------------------------
// Time formatting
// ---------------------------------------------------------------------------

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

		pool := getPool(c)
		ctx := c.Request.Context()

		exam, err := models.GetExamByToken(ctx, pool, token)
		if err != nil {
			if err == pgx.ErrNoRows {
				c.HTML(http.StatusNotFound, "public/hasil.html", middleware.MergeTemplateData(c, gin.H{
					"exam_name":      "Ujian Tidak Ditemukan",
					"token":          token,
					"total_students": 0,
					"error":          true,
				}))
				return
			}
			log.Printf("hasil page exam lookup error: %v", err)
			c.HTML(http.StatusInternalServerError, "public/hasil.html", middleware.MergeTemplateData(c, gin.H{
				"exam_name":      "Error",
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

		var total int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM submissions WHERE exam_id = $1`, exam.ID).Scan(&total); err != nil {
			log.Printf("hasil page count error: %v", err)
			total = 0
		}

		var creatorName string
		pool.QueryRow(ctx, `SELECT username FROM admin_users WHERE id = $1`, exam.CreatedBy).Scan(&creatorName)

		var delegatedName string
		if exam.DelegatedTo != nil {
			pool.QueryRow(ctx, `SELECT username FROM admin_users WHERE id = $1`, *exam.DelegatedTo).Scan(&delegatedName)
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

		pool := getPool(c)
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

		// ---- Total count ----
		var total int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM submissions WHERE exam_id = $1`, exam.ID).Scan(&total); err != nil {
			log.Printf("hasil api count error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "Gagal memuat data hasil",
			})
			return
		}

		// ---- Fetch submissions ----
		rows, err := pool.Query(ctx,
			`SELECT id, student_name, exam_number, student_class,
			        answers_json, score, start_time, created_at, identity_data
			 FROM submissions
			 WHERE exam_id = $1
			 ORDER BY score DESC NULLS LAST
			 LIMIT $2 OFFSET $3`,
			exam.ID, perPage, offset)
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
		if exam.QuestionsJSON != nil && *exam.QuestionsJSON != "" {
			var parsed []map[string]interface{}
			if err := json.Unmarshal([]byte(*exam.QuestionsJSON), &parsed); err != nil {
				log.Printf("hasil api parse questions error: %v", err)
			} else {
				questions = parsed
				for _, q := range questions {
					weight := 1.0
					if w, ok := q["weight"].(float64); ok {
						weight = w
					} else if s, ok := q["score"].(float64); ok {
						weight = s
					}
					maxScore += weight
				}
			}
		}

		// ---- Build submission list ----
		type submissionItem struct {
			ID           int                    `json:"id"`
			StudentName  string                 `json:"student_name"`
			ExamNumber   string                 `json:"exam_number"`
			StudentClass string                 `json:"student_class"`
			IdentityData map[string]interface{} `json:"identity_data"`
			Score        *float64               `json:"score"`
			MaxScore     *float64               `json:"max_score"`
			StartTime    interface{}            `json:"start_time"`
			CreatedAt    string                 `json:"created_at"`
		}

		subsData := make([]submissionItem, 0, perPage)

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

			subsData = append(subsData, submissionItem{
				ID:           id,
				StudentName:  studentName,
				ExamNumber:   examNumber,
				StudentClass: studentClass,
				IdentityData: idData,
				Score:        score,
				MaxScore:     maxScorePtr,
				StartTime:    formatISOUTCString(ptrString(startTime)),
				CreatedAt:    formatISOUTC(createdAt),
			})
		}

		showAnswersEnabled := exam.AreAnswersShown()

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
