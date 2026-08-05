package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"github.com/xuri/excelize/v2"

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
				id, examID                            int
				studentName, examNumber, studentClass string
				answersJSON, identityDataRaw          *string
				score                                 *float64
				startTime                             *string
				macAddress                            string
				createdAt                             time.Time
				examName, questionsJSON               *string
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
				MaxScore:   maxScore,
				ScorePct:   scorePct,
				HasScore:   hasScore,
				StartTime:  startTime,
				CreatedAt:  createdAt,
				MACAddress: macAddress,
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

	query := `SELECT e.id, e.name, u.username FROM exams e LEFT JOIN admin_users u ON u.id = e.created_by`
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
			fmt.Sprintf(`(e.created_by = $%d OR e.delegated_to = $%d OR e.id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = $%d))`,
				argIdx, argIdx, argIdx))
		args = append(args, userID)
		argIdx++
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY e.created_at DESC"

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var exams []gin.H
	for rows.Next() {
		var id int
		var name string
		var creator string
		if err := rows.Scan(&id, &name, &creator); err == nil {
			exams = append(exams, gin.H{"id": id, "name": name, "creator": creator})
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
			"success":     true,
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
// 5. GET /admin/api/submissions/export — Excel (.xlsx) export
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
			exportSingleExamXLSX(c, pool, ctx, examFilter)
		} else {
			// All exports
			exportAllXLSX(c, pool, ctx, userID, isSuper, isOp)
		}
	}
}

func exportSingleExamXLSX(c *gin.Context, pool *pgxpool.Pool, ctx context.Context, examID int) {
	// Excel export for a single exam: a summary sheet of all students plus one
	// detail worksheet per student (per-question answers and scoring).
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

	questions, _ := models.ParseQuestionsJSON(exam.QuestionsJSON)
	if questions == nil {
		questions = []models.Question{}
	}

	f := excelize.NewFile()
	defer f.Close()
	st := newXLSXStyles(f)

	summarySheet := "Rekapitulasi"
	if err := f.SetSheetName("Sheet1", summarySheet); err != nil {
		errorResponse(c, http.StatusInternalServerError, "Gagal mengekspor data")
		return
	}

	headers := []interface{}{"ID", "Nama Ujian", "Nama Siswa", "Nomor Ujian", "Kelas",
		"Nilai", "Waktu Mulai", "Waktu Kumpul", "ID Perangkat"}
	_ = f.SetSheetRow(summarySheet, "A1", &headers)
	if st.headerStyle > 0 {
		_ = f.SetCellStyle(summarySheet, "A1", "I1", st.headerStyle)
	}

	usedSheets := map[string]int{}
	for i, sub := range submissions {
		row := i + 2
		startTimeStr := ""
		if sub.StartTime != nil {
			startTimeStr = *sub.StartTime
		}
		var scoreVal interface{}
		if sub.Score != nil {
			scoreVal = *sub.Score
		}
		_ = f.SetCellInt(summarySheet, cellRef(1, row), int64(sub.ID))
		_ = f.SetCellStr(summarySheet, cellRef(2, row), exam.Name)
		_ = f.SetCellStr(summarySheet, cellRef(3, row), sub.StudentName)
		_ = f.SetCellStr(summarySheet, cellRef(4, row), sub.ExamNumber)
		_ = f.SetCellStr(summarySheet, cellRef(5, row), sub.StudentClass)
		_ = f.SetCellValue(summarySheet, cellRef(6, row), scoreVal)
		_ = f.SetCellStr(summarySheet, cellRef(7, row), startTimeStr)
		_ = f.SetCellStr(summarySheet, cellRef(8, row), sub.CreatedAt.Format("2006-01-02 15:04:05"))
		_ = f.SetCellStr(summarySheet, cellRef(9, row), sub.MACAddress)

		// Per-student detail sheet.
		answers, _ := models.ParseAnswersJSON(sub.AnswersJSON)
		if answers == nil {
			answers = map[string]interface{}{}
		}
		evaluated := models.EvaluateAnswersDetailed(answers, questions)

		base := sanitizeSheetName("Detail - " + sub.StudentName)
		detailSheet := base
		// Excel sheet names are case-insensitive, so key uniqueness on the
		// lowercased name to avoid NewSheet errors for e.g. "Budi" vs "BUDI".
		key := strings.ToLower(base)
		if usedSheets[key] > 0 {
			detailSheet = fmt.Sprintf("%s (%d)", base, usedSheets[key]+1)
			if r := []rune(detailSheet); len(r) > 31 {
				detailSheet = string(r[:31])
			}
		}
		usedSheets[key]++
		if _, err := f.NewSheet(detailSheet); err != nil {
			log.Printf("export: skip detail sheet for %q: %v", sub.StudentName, err)
			continue
		}
		writeStudentDetailSheet(f, detailSheet, exam.Name, sub, answers, evaluated, questions, st)
	}

	setXLSXSummaryLayout(f, summarySheet, 9)

	filename := fmt.Sprintf("Hasil_Ujian_%s_%s.xlsx",
		sanitizeFilename(exam.Name),
		time.Now().UTC().Format("20060102_150405"))
	writeXLSXResponse(c, f, filename)
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

func exportAllXLSX(c *gin.Context, pool *pgxpool.Pool, ctx context.Context,
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
	query += " ORDER BY e.name, s.student_class, s.student_name"

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		log.Printf("export all xlsx error: %v", err)
		errorResponse(c, http.StatusInternalServerError, "Gagal mengekspor data")
		return
	}
	defer rows.Close()

	f := excelize.NewFile()
	defer f.Close()
	sheet := "Semua Hasil"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		errorResponse(c, http.StatusInternalServerError, "Gagal mengekspor data")
		return
	}

	headers := []interface{}{"ID", "Nama Ujian", "Nama Siswa", "Nomor Ujian", "Kelas",
		"Nilai", "Waktu Mulai", "Waktu Kumpul", "ID Perangkat"}
	_ = f.SetSheetRow(sheet, "A1", &headers)
	if style, err := xlsxHeaderStyle(f); err == nil {
		_ = f.SetCellStyle(sheet, "A1", "I1", style)
	}

	row := 2
	for rows.Next() {
		var (
			id, examID                   int
			studentName, examNumber, cls string
			identityDataRaw              *string
			score                        *float64
			startTime, macAddress        *string
			createdAt                    time.Time
			examName                     string
		)
		// NOTE: scan targets must match the 11 selected columns exactly. A
		// previous mismatch (12 targets / identity_data scanned as *float64)
		// made every row fail the scan and produced an empty export.
		if err := rows.Scan(&id, &examID, &studentName, &examNumber, &cls,
			&identityDataRaw, &score, &startTime, &macAddress, &createdAt, &examName); err != nil {
			log.Printf("export all scan error: %v", err)
			continue
		}

		startTimeStr := ""
		if startTime != nil {
			startTimeStr = *startTime
		}
		macStr := ""
		if macAddress != nil {
			macStr = *macAddress
		}
		var scoreVal interface{}
		if score != nil {
			scoreVal = *score
		}

		_ = f.SetCellInt(sheet, cellRef(1, row), int64(id))
		_ = f.SetCellStr(sheet, cellRef(2, row), examName)
		_ = f.SetCellStr(sheet, cellRef(3, row), studentName)
		_ = f.SetCellStr(sheet, cellRef(4, row), examNumber)
		_ = f.SetCellStr(sheet, cellRef(5, row), cls)
		_ = f.SetCellValue(sheet, cellRef(6, row), scoreVal)
		_ = f.SetCellStr(sheet, cellRef(7, row), startTimeStr)
		_ = f.SetCellStr(sheet, cellRef(8, row), createdAt.Format("2006-01-02 15:04:05"))
		_ = f.SetCellStr(sheet, cellRef(9, row), macStr)
		row++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}

	setXLSXSummaryLayout(f, sheet, 9)

	filename := fmt.Sprintf("hasil_ujian_%s.xlsx",
		time.Now().UTC().Format("20060102_150405"))
	writeXLSXResponse(c, f, filename)
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
// 7. GET /admin/api/submissions/:id/export_detail — Export per-student Excel
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

		f := excelize.NewFile()
		defer f.Close()
		st := newXLSXStyles(f)
		sheet := "Detail"
		if err := f.SetSheetName("Sheet1", sheet); err != nil {
			errorResponse(c, http.StatusInternalServerError, "Gagal mengekspor data")
			return
		}
		writeStudentDetailSheet(f, sheet, detail.ExamName, detail.Submission, answers, evaluated, questions, st)

		filename := fmt.Sprintf("Detail_%s_%s.xlsx",
			sanitizeFilename(detail.StudentName),
			time.Now().UTC().Format("20060102_150405"))
		writeXLSXResponse(c, f, filename)
	}
}

// ---------------------------------------------------------------------------
// Excel export helpers
// ---------------------------------------------------------------------------

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// cellRef returns an A1-style cell reference for 1-based column/row numbers.
func cellRef(col, row int) string {
	ref, err := excelize.CoordinatesToCellName(col, row)
	if err != nil {
		return fmt.Sprintf("R%dC%d", row, col)
	}
	return ref
}

// xlsxHeaderStyle returns the shared bold white-on-indigo header style.
func xlsxHeaderStyle(f *excelize.File) (int, error) {
	return f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF", Size: 11},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"4F46E5"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
}

// setXLSXSummaryLayout applies column widths, freezes the header row and adds
// an auto-filter to a summary export sheet.
func setXLSXSummaryLayout(f *excelize.File, sheet string, lastCol int) {
	widths := []float64{8, 32, 26, 14, 12, 10, 20, 20, 20, 14, 14, 14, 14}
	for i := 1; i <= lastCol && i <= len(widths); i++ {
		colName, _ := excelize.ColumnNumberToName(i)
		_ = f.SetColWidth(sheet, colName, colName, widths[i-1])
	}
	lastColName, _ := excelize.ColumnNumberToName(lastCol)
	_ = f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	_ = f.AutoFilter(sheet, "A1:"+lastColName+"1", nil)
}

// writeXLSXResponse streams the workbook to the client as an Excel attachment.
func writeXLSXResponse(c *gin.Context, f *excelize.File, filename string) {
	buf, err := f.WriteToBuffer()
	if err != nil {
		errorResponse(c, http.StatusInternalServerError, "Gagal menulis file Excel")
		return
	}
	c.Header("Content-Type", xlsxContentType)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
	c.Data(http.StatusOK, xlsxContentType, buf.Bytes())
}

// xlsxStyles groups the reusable style IDs shared by export sheets.
type xlsxStyles struct {
	titleStyle  int
	labelStyle  int
	headerStyle int
}

// newXLSXStyles creates the shared styles used across an export workbook.
func newXLSXStyles(f *excelize.File) xlsxStyles {
	title, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 14, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"4F46E5"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	label, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	header, _ := xlsxHeaderStyle(f)
	return xlsxStyles{titleStyle: title, labelStyle: label, headerStyle: header}
}

// sanitizeSheetName makes a name safe for an Excel sheet tab: Excel limits
// names to 31 characters, forbids []:*?/\ characters, and disallows names that
// start or end with an apostrophe.
func sanitizeSheetName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '[', ']', ':', '*', '?', '/', '\\':
			return '_'
		}
		if r < 0x20 {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "'")
	if name == "" {
		return "Siswa"
	}
	if r := []rune(name); len(r) > 31 {
		name = string(r[:31])
	}
	return name
}

// writeStudentDetailSheet fills a worksheet with one student's exam detail: a
// metadata block followed by a per-question table (student answer, key,
// status, earned points). Shared by the per-student export and the single-exam
// export (which emits one detail sheet per student).
func writeStudentDetailSheet(f *excelize.File, sheet, examName string, sub models.Submission,
	answers map[string]interface{}, evaluated map[string]models.EvaluationDetail,
	questions []models.Question, st xlsxStyles) {

	startTimeStr := ""
	if sub.StartTime != nil {
		startTimeStr = *sub.StartTime
	}
	scoreStr := ""
	if sub.Score != nil {
		scoreStr = fmt.Sprintf("%.2f", *sub.Score)
	}

	_ = f.SetCellValue(sheet, "A1", "Detail Hasil Ujian Siswa")
	if st.titleStyle > 0 {
		_ = f.SetCellStyle(sheet, "A1", "A1", st.titleStyle)
	}

	meta := [][2]string{
		{"Ujian", examName},
		{"Nama Siswa", sub.StudentName},
		{"Nomor Ujian", sub.ExamNumber},
		{"Kelas", sub.StudentClass},
		{"Waktu Mulai", startTimeStr},
		{"Waktu Kumpul", sub.CreatedAt.Format("2006-01-02 15:04:05")},
		{"ID Perangkat", sub.MACAddress},
		{"Total Nilai", scoreStr},
	}
	for i, m := range meta {
		r := i + 2
		_ = f.SetCellStr(sheet, cellRef(1, r), m[0])
		if st.labelStyle > 0 {
			_ = f.SetCellStyle(sheet, cellRef(1, r), cellRef(1, r), st.labelStyle)
		}
		_ = f.SetCellStr(sheet, cellRef(2, r), m[1])
	}

	// Questions header (after an empty spacer row)
	qHeaderRow := len(meta) + 3
	qHeaders := []interface{}{"No", "Tipe", "Bobot", "Jawaban Siswa", "Kunci Jawaban", "Status", "Poin"}
	_ = f.SetSheetRow(sheet, "A"+strconv.Itoa(qHeaderRow), &qHeaders)
	if st.headerStyle > 0 {
		lastCol, _ := excelize.ColumnNumberToName(len(qHeaders))
		_ = f.SetCellStyle(sheet, "A"+strconv.Itoa(qHeaderRow), lastCol+strconv.Itoa(qHeaderRow), st.headerStyle)
	}

	for i, q := range questions {
		row := qHeaderRow + 1 + i
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

		_ = f.SetCellStr(sheet, cellRef(1, row), qNum)
		_ = f.SetCellStr(sheet, cellRef(2, row), qType)
		_ = f.SetCellStr(sheet, cellRef(3, row), qWeight)
		_ = f.SetCellStr(sheet, cellRef(4, row), studentAns)
		_ = f.SetCellStr(sheet, cellRef(5, row), correctAns)
		_ = f.SetCellStr(sheet, cellRef(6, row), statusText)
		_ = f.SetCellStr(sheet, cellRef(7, row), poin)
	}

	for i, w := range []float64{6, 16, 8, 46, 32, 12, 10} {
		colName, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(sheet, colName, colName, w)
	}
}
