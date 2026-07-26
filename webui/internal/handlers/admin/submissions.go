package admin

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
	"github.com/examvan/webui/internal/queue"
)

// ---------------------------------------------------------------------------
// 1. GET /admin/submissions — Render submissions overview page
// ---------------------------------------------------------------------------

func SubmissionsPage() gin.HandlerFunc {
	return func(c *gin.Context) {
		isSuper := isSuperAdmin(c)
		isOp := isOperator(c)

		// Guru-only access gate
		if !isSuper && !isOp && !hasCurrentRole(c, models.RoleGuru) {
			c.Redirect(http.StatusFound, "/admin/pengawas")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		if page < 1 {
			page = 1
		}
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "25"))
		if perPage < 5 {
			perPage = 5
		} else if perPage > 100 {
			perPage = 100
		}

		var examFilter int
		examFilterStr := c.Query("exam_id")
		if examFilterStr != "" {
			examFilter, _ = strconv.Atoi(examFilterStr)
		}

		// Count total submissions within scope
		var total int
		countQuery := `SELECT COUNT(*) FROM submissions s JOIN exams e ON s.exam_id = e.id`
		countArgs, _ := buildScopeConditions(c, pool, &countQuery)

		if examFilter > 0 {
			countQuery += ` AND s.exam_id = $` + strconv.Itoa(len(countArgs)+1)
			countArgs = append(countArgs, examFilter)
		}

		err := pool.QueryRow(ctx, countQuery, countArgs...).Scan(&total)
		if err != nil {
			log.Printf("submissions count error: %v", err)
		}

		totalPages := int(math.Max(1, float64((total+perPage-1)/perPage)))
		if page > totalPages && total > 0 {
			page = totalPages
		}
		offset := (page - 1) * perPage

		// Fetch submissions
		dataQuery := `SELECT s.id, s.exam_id, s.student_name, s.exam_number, s.student_class,
	s.answers_json, s.score, s.start_time, s.mac_address, s.created_at, s.identity_data,
	e.name as exam_name, e.questions_json
	FROM submissions s JOIN exams e ON s.exam_id = e.id`
		dataArgs, hasWhere := buildScopeConditions(c, pool, &dataQuery)

		if examFilter > 0 {
			if hasWhere {
				dataQuery += ` AND s.exam_id = $` + strconv.Itoa(len(dataArgs)+1)
			} else {
				dataQuery += ` WHERE s.exam_id = $` + strconv.Itoa(len(dataArgs)+1)
			}
			dataArgs = append(dataArgs, examFilter)
		}

		dataQuery += ` ORDER BY s.created_at DESC LIMIT $` + strconv.Itoa(len(dataArgs)+1) +
			` OFFSET $` + strconv.Itoa(len(dataArgs)+2)
		dataArgs = append(dataArgs, perPage, offset)

		rows, err := pool.Query(ctx, dataQuery, dataArgs...)
		if err != nil {
			log.Printf("submissions query error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat data")
			return
		}
		defer rows.Close()

		type subItem struct {
			ID           int                    `json:"id"`
			ExamID       int                    `json:"exam_id"`
			ExamName     string                 `json:"exam_name"`
			StudentName  string                 `json:"student_name"`
			ExamNumber   string                 `json:"exam_number"`
			StudentClass string                 `json:"student_class"`
			IdentityData map[string]interface{} `json:"identity_data"`
			Score        *float64               `json:"score"`
			ScoreDisplay string                 `json:"score_display"`
			MaxScore     float64                `json:"max_score"`
			ScorePct     float64                `json:"score_pct"`
			HasScore     bool                   `json:"has_score"`
			StartTime    *string                `json:"start_time"`
			CreatedAt    time.Time              `json:"created_at"`
			MACAddress   string                 `json:"mac_address"`
		}

		subData := make([]subItem, 0)
		for rows.Next() {
			var (
				id, examID                                 int
				studentName, examNumber, studentClass       string
				answersJSON, identityDataRaw                *string
				score                                       *float64
				startTime                                   *string
				macAddress                                  string
				createdAt                                   time.Time
				examName, questionsJSON                     *string
			)
			err := rows.Scan(
				&id, &examID, &studentName, &examNumber, &studentClass,
				&answersJSON, &score, &startTime, &macAddress, &createdAt, &identityDataRaw,
				&examName, &questionsJSON,
			)
			if err != nil {
				log.Printf("scan submission row: %v", err)
				continue
			}

			// Parse identity data
			identityData := make(map[string]interface{})
			if identityDataRaw != nil && *identityDataRaw != "" {
				json.Unmarshal([]byte(*identityDataRaw), &identityData)
			}

			// Compute max score
			var maxScore float64
			var scorePct float64
			var hasScore bool
			if questionsJSON != nil && *questionsJSON != "" {
				questions, parseErr := models.ParseQuestionsJSON(questionsJSON)
				if parseErr == nil {
					maxScore = models.ComputeMaxScore(questions)
					if score != nil && maxScore > 0 {
						scorePct = math.Round(*score/maxScore*100*10) / 10
						hasScore = true
					}
				}
			}

			en := ""
			if examName != nil {
				en = *examName
			}

			subData = append(subData, subItem{
				ID:           id,
				ExamID:       examID,
				ExamName:     en,
				StudentName:  studentName,
				ExamNumber:   examNumber,
				StudentClass: studentClass,
				IdentityData: identityData,
				Score:        score,
				ScoreDisplay: func() string {
					if score != nil {
						return fmt.Sprintf("%.1f", *score)
					}
					return ""
				}(),
				MaxScore:     maxScore,
				ScorePct:     scorePct,
				HasScore:     hasScore,
				StartTime:    startTime,
				CreatedAt:    createdAt,
				MACAddress:   macAddress,
			})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			log.Printf("rows iteration error: %v", err)
		}

		// Fetch exam list for filter dropdown
		examList := fetchFilterExams(c, pool)

		// Exam info for filter
		var examInfo gin.H
		if examFilter > 0 {
			exam, err := models.GetExamByID(ctx, pool, examFilter)
			if err == nil {
				_, _ = models.ParseQuestionsJSON(exam.QuestionsJSON)
				var subCount int
				pool.QueryRow(ctx, `SELECT COUNT(*) FROM submissions WHERE exam_id = $1`, examFilter).Scan(&subCount)

				// Creator name
				var creatorName string
				pool.QueryRow(ctx, `SELECT username FROM admin_users WHERE id = $1`, exam.CreatedBy).Scan(&creatorName)

				// Delegated name
				var delegatedName string
				if exam.DelegatedTo != nil {
					pool.QueryRow(ctx, `SELECT username FROM admin_users WHERE id = $1`, *exam.DelegatedTo).Scan(&delegatedName)
				}

				// Pengawas list
				pengawasList := []string{}
				pRows, err := pool.Query(ctx,
					`SELECT u.username FROM exam_pengawas ep JOIN admin_users u ON ep.user_id = u.id WHERE ep.exam_id = $1 ORDER BY u.username`,
					examFilter)
				if err == nil {
					for pRows.Next() {
						var pName string
						pRows.Scan(&pName)
						pengawasList = append(pengawasList, pName)
					}
					pRows.Close()
					if err := pRows.Err(); err != nil {
						log.Printf("rows iteration error: %v", err)
					}
				}

				// Format timestamps
				startTime := ""
				if exam.StartTime != nil {
					startTime = exam.StartTime.Format("2006-01-02 15:04")
				}
				endTime := ""
				if exam.EndTime != nil {
					endTime = exam.EndTime.Format("2006-01-02 15:04")
				}
				createdAt := exam.CreatedAt.Format("2006-01-02 15:04")

				examInfo = gin.H{
					"name":           exam.Name,
					"token":          exam.Token,
					"creator_name":   creatorName,
					"delegated_name": delegatedName,
					"pengawas":       pengawasList,
					"created_at":     createdAt,
					"size_mb":        roundTo(float64(exam.SizeBytes)/(1024*1024), 2),
					"sub_count":      subCount,
					"start_time":     startTime,
					"end_time":       endTime,
				}
			}
		}

		renderAdminPage(c, "admin/submissions.html", gin.H{
			"submissions":       subData,
			"exams":             examList,
			"active_page":       "submissions",
			"page":              page,
			"per_page":          perPage,
			"total_pages":       totalPages,
			"total_submissions": total,
			"exam_filter_param": examFilter,
			"exam_info":         examInfo,
		})
	}
}

// buildScopeConditions adds WHERE conditions scoped to the current user and
// modifies the query string in-place (adding WHERE or AND). Returns the args
// and whether a WHERE clause was added (so callers can append correctly).
func buildScopeConditions(c *gin.Context, pool *pgxpool.Pool, query *string) ([]interface{}, bool) {
	userID := getCurrentUserID(c)
	isSuper := isSuperAdmin(c)
	isOp := isOperator(c)
	ctx := c.Request.Context()

	var conditions []string
	var args []interface{}
	argIdx := 1

	if isSuper {
		// No filter
	} else if isOp {
		var instansi string
		pool.QueryRow(ctx, `SELECT instansi FROM admin_users WHERE id = $1`, userID).Scan(&instansi)
		conditions = append(conditions,
			fmt.Sprintf(`e.created_by IN (SELECT id FROM admin_users WHERE instansi = $%d)`,
				argIdx))
		args = append(args, instansi)
		argIdx++
	} else {
		conditions = append(conditions,
			fmt.Sprintf(`(e.created_by = $%d OR e.delegated_to = $%d OR s.exam_id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = $%d))`,
				argIdx, argIdx, argIdx))
		args = append(args, userID)
		argIdx++
	}

	if len(conditions) > 0 {
		*query += " WHERE " + strings.Join(conditions, " AND ")
		return args, true
	}
	return args, false
}

func fetchFilterExams(c *gin.Context, pool *pgxpool.Pool) []gin.H {
	userID := getCurrentUserID(c)
	isSuper := isSuperAdmin(c)
	isOp := isOperator(c)
	ctx := c.Request.Context()

	query := `SELECT id, name FROM exams`
	var conditions []string
	var args []interface{}
	argIdx := 1

	if isSuper {
		// all
	} else if isOp {
		var instansi string
		pool.QueryRow(ctx, `SELECT instansi FROM admin_users WHERE id = $1`, userID).Scan(&instansi)
		conditions = append(conditions,
			fmt.Sprintf(`created_by IN (SELECT id FROM admin_users WHERE instansi = $%d)`,
				argIdx))
		args = append(args, instansi)
		argIdx++
	} else {
		conditions = append(conditions,
			fmt.Sprintf(`(created_by = $%d OR delegated_to = $%d OR id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = $%d))`,
				argIdx, argIdx, argIdx))
		args = append(args, userID)
		argIdx++
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC"

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var exams []gin.H
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err == nil {
			exams = append(exams, gin.H{"id": id, "name": name})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}
	return exams
}

// ---------------------------------------------------------------------------
// 2. GET /admin/api/submissions — JSON list with pagination / exam filter
// ---------------------------------------------------------------------------

func ListSubmissions() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		isSuper := isSuperAdmin(c)
		isOp := isOperator(c)
		ctx := c.Request.Context()

		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		if page < 1 {
			page = 1
		}
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "25"))
		if perPage < 5 {
			perPage = 5
		} else if perPage > 100 {
			perPage = 100
		}

		var examFilter int
		if v := c.Query("exam_id"); v != "" {
			examFilter, _ = strconv.Atoi(v)
		}

		if examFilter == 0 {
			errorResponse(c, http.StatusBadRequest, "Parameter exam_id wajib diisi")
			return
		}

		// Verify access
		if !isSuper && !isOp {
			owned, _ := pool.Query(ctx,
				`SELECT 1 FROM exams WHERE id = $1 AND (created_by = $2 OR delegated_to = $2 OR id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = $2))`,
				examFilter, userID)
			if !owned.Next() {
				errorResponse(c, http.StatusForbidden, "Akses ditolak")
				return
			}
			owned.Close()
		}

		opts := models.ListSubmissionsByExamOpts{
			ExamID:  examFilter,
			Page:    page,
			PerPage: perPage,
			Search:  c.Query("search"),
		}

		result, err := models.ListSubmissionsByExam(ctx, pool, opts)
		if err != nil {
			log.Printf("list submissions error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat data")
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"submissions": result.Submissions,
			"pagination": gin.H{
				"page":        result.Page,
				"per_page":    result.PerPage,
				"total":       result.Total,
				"total_pages": result.TotalPages,
			},
			"stats": result.Stats,
		})
	}
}

// ---------------------------------------------------------------------------
// 3. GET /admin/api/submissions/:id/detail — Answer detail
// ---------------------------------------------------------------------------

func SubmissionDetail() gin.HandlerFunc {
	return func(c *gin.Context) {
		submissionID, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		if !checkSubmissionOwnership(c, pool, submissionID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak")
			return
		}

		detail, err := models.GetSubmissionDetail(ctx, pool, submissionID)
		if err != nil {
			log.Printf("submission detail error: %v", err)
			errorResponse(c, http.StatusNotFound, "Hasil ujian tidak ditemukan")
			return
		}

		// Parse answers and evaluate
		answers, _ := models.ParseAnswersJSON(detail.AnswersJSON)
		if answers == nil {
			answers = make(map[string]interface{})
		}

		questions, _ := models.ParseQuestionsJSON(detail.QuestionsJSON)
		if questions == nil {
			questions = []models.Question{}
		}

		evaluatedAnswers := models.EvaluateAnswersDetailed(answers, questions)

		// Parse identity data
		identityData := make(map[string]interface{})
		if detail.IdentityData != nil && *detail.IdentityData != "" {
			json.Unmarshal([]byte(*detail.IdentityData), &identityData)
		}

		var startTime *string
		if detail.StartTime != nil {
			t, parseErr := time.Parse("2006-01-02 15:04:05", *detail.StartTime)
			if parseErr == nil {
				s := formatISOUTC(t)
				startTime = &s
			} else {
				startTime = detail.StartTime
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"success":           true,
			"submission_id":     detail.ID,
			"student_name":      detail.StudentName,
			"exam_number":       detail.ExamNumber,
			"student_class":     detail.StudentClass,
			"identity_data":     identityData,
			"exam_name":         detail.ExamName,
			"score":             detail.Score,
			"start_time":        startTime,
			"mac_address":       detail.MACAddress,
			"created_at":        formatISOUTC(detail.CreatedAt),
			"answers":           answers,
			"questions":         questions,
			"evaluated_answers": evaluatedAnswers,
		})
	}
}

// ---------------------------------------------------------------------------
// 4. DELETE /admin/api/submissions/:id — Delete submission
// ---------------------------------------------------------------------------

func DeleteSubmission() gin.HandlerFunc {
	return func(c *gin.Context) {
		submissionID, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		if !checkSubmissionOwnership(c, pool, submissionID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak")
			return
		}

		if err := models.DeleteSubmission(ctx, pool, submissionID); err != nil {
			log.Printf("delete submission error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menghapus hasil ujian")
			return
		}

		successMessage(c, "Hasil ujian berhasil dihapus")
	}
}

// ---------------------------------------------------------------------------
// 5. GET /admin/api/submissions/export — CSV or XLSX export
// ---------------------------------------------------------------------------

func ExportSubmissions() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		isSuper := isSuperAdmin(c)
		isOp := isOperator(c)
		ctx := c.Request.Context()

		examIDStr := c.Query("exam_id")
		var examFilter int
		if examIDStr != "" {
			examFilter, _ = strconv.Atoi(examIDStr)
		}

		if examFilter > 0 {
			// Specific exam export — enforce tenant ownership (mirrors
			// SubmissionDetail/ExportSubmissionDetail) to prevent cross-tenant
			// IDOR that would otherwise leak another tenant's student PII.
			if !checkExamOwnership(c, pool, examFilter) {
				errorResponse(c, http.StatusForbidden, "Akses ditolak")
				return
			}
			exportSingleExamCSV(c, pool, ctx, examFilter)
		} else {
			// All exports
			exportAllCSV(c, pool, ctx, userID, isSuper, isOp)
		}
	}
}

func exportSingleExamCSV(c *gin.Context, pool *pgxpool.Pool, ctx context.Context, examID int) {
	// Simplified CSV export for a single exam
	exam, err := models.GetExamByID(ctx, pool, examID)
	if err != nil {
		errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
		return
	}

	submissions, dbErr := fetchSubmissionsByExam(ctx, pool, examID)
	if dbErr != nil {
		log.Printf("export exam submissions error: %v", dbErr)
		errorResponse(c, http.StatusInternalServerError, "Gagal mengekspor data")
		return
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	// Header row
	headers := []string{"ID", "Nama Ujian", "Nama Siswa", "Nomor Ujian", "Kelas",
		"Nilai", "Waktu Mulai", "Waktu Kumpul", "ID Perangkat"}
	if err := writer.Write(headers); err != nil {
		errorResponse(c, http.StatusInternalServerError, "Gagal menulis CSV")
		return
	}

	for _, sub := range submissions {
		startTimeStr := ""
		if sub.StartTime != nil {
			startTimeStr = *sub.StartTime
		}
		scoreStr := ""
		if sub.Score != nil {
			scoreStr = fmt.Sprintf("%.2f", *sub.Score)
		}
		row := []string{
			strconv.Itoa(sub.ID),
			csvSafe(exam.Name),
			csvSafe(sub.StudentName),
			csvSafe(sub.ExamNumber),
			csvSafe(sub.StudentClass),
			scoreStr,
			startTimeStr,
			sub.CreatedAt.Format("2006-01-02 15:04:05"),
			csvSafe(sub.MACAddress),
		}
		if err := writer.Write(row); err != nil {
			continue
		}
	}
	writer.Flush()

	filename := fmt.Sprintf("Hasil_Ujian_%s_%s.csv",
		strings.ReplaceAll(exam.Name, " ", "_"),
		time.Now().UTC().Format("20060102_150405"))

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
}

func fetchSubmissionsByExam(ctx context.Context, pool *pgxpool.Pool, examID int) ([]models.Submission, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, exam_id, student_name, exam_number, student_class,
		 answers_json, score, start_time, mac_address, created_at, identity_data
		 FROM submissions WHERE exam_id = $1 ORDER BY student_class, student_name`, examID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []models.Submission
	for rows.Next() {
		var s models.Submission
		err := rows.Scan(
			&s.ID, &s.ExamID, &s.StudentName, &s.ExamNumber, &s.StudentClass,
			&s.AnswersJSON, &s.Score, &s.StartTime, &s.MACAddress, &s.CreatedAt, &s.IdentityData,
		)
		if err != nil {
			return nil, fmt.Errorf("scan submission: %w", err)
		}
		subs = append(subs, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}
	return subs, nil
}

func exportAllCSV(c *gin.Context, pool *pgxpool.Pool, ctx context.Context,
	userID int, isSuper, isOp bool) {

	var instansi string
	query := `SELECT s.id, s.exam_id, s.student_name, s.exam_number, s.student_class,
	s.identity_data, s.score, s.start_time, s.mac_address, s.created_at,
	e.name as exam_name
	FROM submissions s JOIN exams e ON s.exam_id = e.id`

	var conditions []string
	var args []interface{}
	argIdx := 1

	if isOp {
		pool.QueryRow(ctx, `SELECT instansi FROM admin_users WHERE id = $1`, userID).Scan(&instansi)
		if instansi != "" {
			conditions = append(conditions,
				fmt.Sprintf(`e.created_by IN (SELECT id FROM admin_users WHERE instansi = $%d)`, argIdx))
			args = append(args, instansi)
			argIdx++
		}
	} else if !isSuper {
		conditions = append(conditions,
			fmt.Sprintf(`(e.created_by = $%d OR e.delegated_to = $%d OR s.exam_id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = $%d))`,
				argIdx, argIdx, argIdx))
		args = append(args, userID)
		argIdx++
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY s.created_at DESC"

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		log.Printf("export all csv error: %v", err)
		errorResponse(c, http.StatusInternalServerError, "Gagal mengekspor data")
		return
	}
	defer rows.Close()

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	headers := []string{"ID", "Nama Ujian", "Nama Siswa", "Nomor Ujian", "Kelas",
		"Nilai", "Waktu Mulai", "Waktu Kumpul", "ID Perangkat"}
	writer.Write(headers)

	for rows.Next() {
		var (
			id, examID                         int
			studentName, examNumber, studentClass string
			identityDataRaw, score              *float64
			startTime, macAddress               *string
			createdAt                           time.Time
			examName                            string
		)
		var answersJSON *string //nolint:revive
		err := rows.Scan(&id, &examID, &studentName, &examNumber, &studentClass,
			&identityDataRaw, &score, &startTime, &macAddress, &createdAt, &examName,
			&answersJSON)
		if err != nil {
			continue
		}

		startTimeStr := ""
		if startTime != nil {
			startTimeStr = *startTime
		}
		scoreStr := ""
		if score != nil {
			scoreStr = fmt.Sprintf("%.2f", *score)
		}
		macStr := ""
		if macAddress != nil {
			macStr = *macAddress
		}

		writer.Write([]string{
			strconv.Itoa(id),
			csvSafe(examName),
			csvSafe(studentName),
			csvSafe(examNumber),
			csvSafe(studentClass),
			scoreStr,
			startTimeStr,
			createdAt.Format("2006-01-02 15:04:05"),
			csvSafe(macStr),
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}
	writer.Flush()

	filename := fmt.Sprintf("hasil_ujian_%s.csv",
		time.Now().UTC().Format("20060102_150405"))

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
}

// ---------------------------------------------------------------------------
// 6. GET /admin/api/queue/status — Queue monitoring
// ---------------------------------------------------------------------------

func QueueStatus() gin.HandlerFunc {
	return func(c *gin.Context) {
		rdb, exists := c.Get("redis")
		if !exists || rdb == nil {
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"data": gin.H{
					"available":     false,
					"pending":       0,
					"worker_active": false,
				},
			})
			return
		}

		redisClient := rdb.(*goredis.Client)
		stats := queue.GetQueueStats(redisClient)

		successData(c, gin.H{
			"available":     stats.Available,
			"pending":       stats.Pending,
			"processed":     stats.Processed,
			"errored":       stats.Errored,
			"worker_active": stats.WorkerActive,
		})
	}
}

// ---------------------------------------------------------------------------
// 7. GET /admin/api/submissions/:id/export_detail — Export per-student CSV
// ---------------------------------------------------------------------------

func ExportSubmissionDetail() gin.HandlerFunc {
	return func(c *gin.Context) {
		submissionID, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		if !checkSubmissionOwnership(c, pool, submissionID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak")
			return
		}

		detail, err := models.GetSubmissionDetail(ctx, pool, submissionID)
		if err != nil {
			log.Printf("export detail error: %v", err)
			errorResponse(c, http.StatusNotFound, "Hasil ujian tidak ditemukan")
			return
		}

		// Parse answers and evaluate
		answers, _ := models.ParseAnswersJSON(detail.AnswersJSON)
		if answers == nil {
			answers = make(map[string]interface{})
		}

		questions, _ := models.ParseQuestionsJSON(detail.QuestionsJSON)
		if questions == nil {
			questions = []models.Question{}
		}

		evaluated := models.EvaluateAnswersDetailed(answers, questions)

		// Parse identity data
		identityData := make(map[string]interface{})
		if detail.IdentityData != nil && *detail.IdentityData != "" {
			json.Unmarshal([]byte(*detail.IdentityData), &identityData)
		}

		startTimeStr := ""
		if detail.StartTime != nil {
			startTimeStr = *detail.StartTime
		}

		var buf bytes.Buffer
		writer := csv.NewWriter(&buf)

		// Metadata header
		writer.Write([]string{"Detail Hasil Ujian Siswa"})
		writer.Write([]string{"Ujian", csvSafe(detail.ExamName)})
		writer.Write([]string{"Nama Siswa", csvSafe(detail.StudentName)})
		writer.Write([]string{"Nomor Ujian", csvSafe(detail.ExamNumber)})
		writer.Write([]string{"Kelas", csvSafe(detail.StudentClass)})
		writer.Write([]string{"Waktu Mulai", startTimeStr})
		writer.Write([]string{"Waktu Kumpul", detail.CreatedAt.Format("2006-01-02 15:04:05")})
		writer.Write([]string{"ID Perangkat", csvSafe(detail.MACAddress)})
		scoreStr := ""
		if detail.Score != nil {
			scoreStr = fmt.Sprintf("%.2f", *detail.Score)
		}
		writer.Write([]string{"Total Nilai", scoreStr})
		writer.Write([]string{}) // Empty row

		// Questions header
		writer.Write([]string{"No", "Tipe", "Bobot", "Jawaban Siswa", "Kunci Jawaban", "Status", "Poin"})

		for _, q := range questions {
			qNum := fmt.Sprintf("%v", q.Number)
			qType := q.Type
			qWeight := fmt.Sprintf("%.1f", q.GetWeight())

			// Get student answer
			studentAns := ""
			if ans, ok := answers[qNum]; ok {
				studentAns = fmt.Sprintf("%v", ans)
			}

			// Get correct answer
			correctAns := ""
			if q.GetKey() != nil {
				correctAns = fmt.Sprintf("%v", q.GetKey())
			}

			// Get evaluation
			eval, hasEval := evaluated[qNum]
			statusText := "-"
			poin := "0"
			if hasEval {
				statusText = eval.StatusText
				poin = fmt.Sprintf("%.2f", eval.Earned)
			}

			writer.Write([]string{
				qNum,
				qType,
				qWeight,
				csvSafe(studentAns),
				csvSafe(correctAns),
				statusText,
				poin,
			})
		}

		writer.Flush()

		filename := fmt.Sprintf("Detail_%s_%s.csv",
			strings.ReplaceAll(detail.StudentName, " ", "_"),
			time.Now().UTC().Format("20060102_150405"))

		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
		c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
	}
}

// csvSafe prevents CSV formula injection.
func csvSafe(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "\t" + value
	}
	return value
}
