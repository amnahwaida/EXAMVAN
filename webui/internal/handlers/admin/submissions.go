package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xuri/excelize/v2"

	"github.com/examvan/webui/internal/helpers"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// 1. GET /admin/submissions — Render submissions overview page
// ---------------------------------------------------------------------------

// Batch 10 (S49): zona sekolah WIB untuk kartu info ujian — selaras
// formatExamTime di main.go. LoadLocation gagal (tanpa tzdata) → fallback
// FixedZone dengan offset yang sama (WIB tidak mengenal DST).
var jakartaLoc = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		return time.FixedZone("WIB", 7*60*60)
	}
	return loc
}()

func formatExamTimeWIB(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(jakartaLoc).Format("2006-01-02 15:04")
}

func formatCreatedTimeWIB(t time.Time) string {
	return t.In(jakartaLoc).Format("2006-01-02 15:04")
}

// submissionEnd picks the timestamp the work duration is measured against:
// submitted_at (the moment answers were persisted) when present, else created_at
// (row creation — the approval moment for placeholders, and the only stamp
// legacy rows have). Falling back keeps pre-migration results rendering a real
// duration instead of collapsing to 0.
func submissionEnd(createdAt time.Time, submittedAt *time.Time) time.Time {
	if submittedAt != nil && !submittedAt.IsZero() {
		return *submittedAt
	}
	return createdAt
}

// submissionEndOf is submissionEnd for a scanned models.Submission (SubmittedAt
// is nil when the column is NULL).
func submissionEndOf(s models.Submission) time.Time {
	return submissionEnd(s.CreatedAt, s.SubmittedAt)
}

// submittedOnlyCondition keeps heartbeat/monitoring placeholder rows out of
// Hasil Ujian surfaces. The heartbeat flusher (and approval bookkeeping)
// inserts placeholder rows into `submissions` with NULL/empty answers_json so
// the device shows up on the Monitoring Perangkat page — those rows are
// presence tracking, NOT submitted answers, so every "hasil" query (page,
// count, export) must exclude them. Same predicate as the public hasil page.
const submittedOnlyCondition = `s.answers_json IS NOT NULL AND s.answers_json != ''`

// appendSQLCondition appends a raw SQL condition to *query, using WHERE when
// no WHERE clause exists yet and AND otherwise. Returns true (a WHERE clause
// now exists) so callers can chain further conditions.
func appendSQLCondition(query *string, hasWhere bool, cond string) bool {
	if hasWhere {
		*query += " AND " + cond
	} else {
		*query += " WHERE " + cond
	}
	return true
}

func SubmissionsPage() gin.HandlerFunc {
	return func(c *gin.Context) {
		isSuper := isSuperAdmin(c)
		isOp := isOperator(c)

		// Guru-only access gate — pengawas-only accounts are redirected to
		// their pengawas hub, everyone else passes (the per-exam visibility is
		// enforced by the scoped list + examInfo gate below).
		if !isSuper && !isOp && !hasCurrentRole(c, models.RoleGuru) && !hasCurrentRole(c, models.RolePengawas) {
			c.Redirect(http.StatusFound, "/admin/pengawas")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()
		userID := getCurrentUserID(c)

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

		// Tenant gate FIRST — before any data is read. Without it, a forged
		// ?exam_id=N forces a full count + row scan of another tenant's
		// submissions (and questions_json) before the authorisation decision.
		// The exam-info card below re-checks the same predicate before
		// rendering, so this early gate changes no observable behaviour.
		if examFilter > 0 && !models.UserCanAccessExam(ctx, pool, userID, isSuper, examFilter) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak")
			return
		}

		// Count total submissions within scope (only rows that actually
		// submitted answers — heartbeat placeholders excluded).
		var total int
		countQuery := `SELECT COUNT(*) FROM submissions s JOIN exams e ON s.exam_id = e.id`
		countArgs, countHasWhere, err := buildScopeConditions(c, pool, &countQuery)
		if err != nil {
			log.Printf("submissions scope error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat data")
			return
		}

		if examFilter > 0 {
			if countHasWhere {
				countQuery += ` AND s.exam_id = $` + strconv.Itoa(len(countArgs)+1)
			} else {
				countQuery += ` WHERE s.exam_id = $` + strconv.Itoa(len(countArgs)+1)
			}
			countArgs = append(countArgs, examFilter)
			countHasWhere = true
		}
		appendSQLCondition(&countQuery, countHasWhere, submittedOnlyCondition)

		err = pool.QueryRow(ctx, countQuery, countArgs...).Scan(&total)
		if err != nil {
			// Fail closed: rendering rows with total=0 breaks pagination
			// ("0 dari 0 hasil" with rows shown) and hides the failure.
			log.Printf("submissions count error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat data")
			return
		}

		totalPages := int(math.Max(1, float64((total+perPage-1)/perPage)))
		if page > totalPages && total > 0 {
			page = totalPages
		}
		offset := (page - 1) * perPage

		// Fetch submissions
		dataQuery := `SELECT s.id, s.exam_id, s.student_name, s.exam_number, s.student_class,
	s.answers_json, s.score, s.start_time, s.mac_address, s.created_at, s.submitted_at, s.identity_data,
	e.name as exam_name, e.questions_json
	FROM submissions s JOIN exams e ON s.exam_id = e.id`
		dataArgs, hasWhere, err := buildScopeConditions(c, pool, &dataQuery)
		if err != nil {
			log.Printf("submissions scope error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat data")
			return
		}

		if examFilter > 0 {
			if hasWhere {
				dataQuery += ` AND s.exam_id = $` + strconv.Itoa(len(dataArgs)+1)
			} else {
				dataQuery += ` WHERE s.exam_id = $` + strconv.Itoa(len(dataArgs)+1)
			}
			dataArgs = append(dataArgs, examFilter)
			hasWhere = true
		}
		// Only rows that actually submitted answers (heartbeat placeholders
		// excluded — same predicate as the count query above).
		appendSQLCondition(&dataQuery, hasWhere, submittedOnlyCondition)

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
			// EndAt is the timestamp the work duration is measured against:
			// submitted_at (the submit moment) with created_at as the fallback
			// for approval placeholders and legacy rows that predate the column.
			EndAt      string `json:"end_at"`
			MACAddress string `json:"mac_address"`
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
				submittedAt                           *time.Time
				examName, questionsJSON               *string
			)
			err := rows.Scan(
				&id, &examID, &studentName, &examNumber, &studentClass,
				&answersJSON, &score, &startTime, &macAddress, &createdAt, &submittedAt,
				&identityDataRaw, &examName, &questionsJSON,
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
			StartTime: startTime,
			CreatedAt: createdAt,
			EndAt:     formatISOUTC(submissionEnd(createdAt, submittedAt)),
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
			// Tenant gate (re-check of the early gate above, kept next to the
			// data it guards): without it, any operator/guru could enumerate
			// ?exam_id=N to read another tenant's exam name, exam token,
			// creator/delegate/pengawas usernames, submission count and
			// schedule from the info card even though the submission list
			// itself is scoped below.
			if !models.UserCanAccessExam(ctx, pool, userID, isSuper, examFilter) {
				errorResponse(c, http.StatusForbidden, "Akses ditolak")
				return
			}
			exam, err := models.GetExamByID(ctx, pool, examFilter)
			if err == nil {
				_, _ = models.ParseQuestionsJSON(exam.QuestionsJSON)
			var subCount int
			pool.QueryRow(ctx, `SELECT COUNT(*) FROM submissions WHERE exam_id = $1 AND answers_json IS NOT NULL AND answers_json != ''`, examFilter).Scan(&subCount)

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

				// Format timestamps — Batch 10 (S49): tampilkan dalam zona
				// sekolah (WIB), bukan UTC mentah; selaras formatExamTime
				// (main.go) yang dipakai badge halaman yang sama.
				startTime := ""
				if exam.StartTime != nil {
					startTime = formatExamTimeWIB(exam.StartTime)
				}
				endTime := ""
				if exam.EndTime != nil {
					endTime = formatExamTimeWIB(exam.EndTime)
				}
				createdAt := formatCreatedTimeWIB(exam.CreatedAt)

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
					// Status + tombstone marker so the hasil page can show the
					// same three-state badge (Aktif/Nonaktif/Ditombstone) as the
					// dashboard and pengawas pages.
					"status":        exam.Status,
					"tombstoned":    exam.TombstonedAt != nil,
					"tombstoned_at": exam.TombstonedAt,
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
func buildScopeConditions(c *gin.Context, pool *pgxpool.Pool, query *string) ([]interface{}, bool, error) {
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
		// Fail-closed, canonical tenant identity (instansi_id-first): an
		// operator whose scope cannot be resolved aborts the query; a bucket
		// scope (""/"personal"/"owner", any casing) is NOT a tenant, so it
		// must never widen the scope to other tenants — fall back to
		// own-created exams only. Name-only matching is wrong here because
		// instansi.name is not unique: two schools may share a name with
		// different instansi_ids, and LOWER() would bleed one's students
		// into the other's Hasil Ujian.
		scope, err := getInstansiScopeForOperator(ctx, pool, userID)
		if err != nil {
			return nil, false, err
		}
		if scope.IsBucket() {
			conditions = append(conditions, fmt.Sprintf(`e.created_by = $%d`, argIdx))
			args = append(args, userID)
			argIdx++
		} else {
			frag, fargs := models.InstansiMatchSQL("", argIdx, scope)
			conditions = append(conditions,
				fmt.Sprintf(`e.created_by IN (SELECT id FROM admin_users WHERE %s)`, frag))
			args = append(args, fargs...)
			argIdx += len(fargs)
		}
	} else {
		conditions = append(conditions,
			fmt.Sprintf(`(e.created_by = $%d OR e.delegated_to = $%d OR s.exam_id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = $%d))`,
				argIdx, argIdx, argIdx))
		args = append(args, userID)
		argIdx++
	}

	if len(conditions) > 0 {
		*query += " WHERE " + strings.Join(conditions, " AND ")
		return args, true, nil
	}
	return args, false, nil
}

func fetchFilterExams(c *gin.Context, pool *pgxpool.Pool) []gin.H {
	userID := getCurrentUserID(c)
	isSuper := isSuperAdmin(c)
	isOp := isOperator(c)
	ctx := c.Request.Context()

	query := `SELECT e.id, e.name, u.username, e.tombstoned_at FROM exams e LEFT JOIN admin_users u ON u.id = e.created_by`
	var conditions []string
	var args []interface{}
	argIdx := 1

	if isSuper {
		// all
	} else if isOp {
		// Fail-closed, canonical tenant identity (instansi_id-first) — same
		// contract as buildScopeConditions: unresolved scope → empty
		// dropdown; bucket scope (""/"personal"/"owner", any casing) →
		// own-created exams only, so an empty instansi never widens the
		// filter. The created_by reference MUST be qualified (e.created_by):
		// both exams and admin_users expose the column, and the unqualified
		// form errors with 42702 "ambiguous", silently emptying the dropdown
		// for every operator.
		scope, err := getInstansiScopeForOperator(ctx, pool, userID)
		if err != nil {
			log.Printf("fetch filter exams scope error: %v", err)
			return nil
		}
		if scope.IsBucket() {
			conditions = append(conditions, fmt.Sprintf(`e.created_by = $%d`, argIdx))
			args = append(args, userID)
			argIdx++
		} else {
			frag, fargs := models.InstansiMatchSQL("", argIdx, scope)
			conditions = append(conditions,
				fmt.Sprintf(`e.created_by IN (SELECT id FROM admin_users WHERE %s)`, frag))
			args = append(args, fargs...)
			argIdx += len(fargs)
		}
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
		var tombstonedAt *time.Time
		if err := rows.Scan(&id, &name, &creator, &tombstonedAt); err == nil {
			exams = append(exams, gin.H{
				"id":         id,
				"name":       name,
				"creator":    creator,
				"tombstoned": tombstonedAt != nil,
			})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}
	return exams
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

		tzOffset := parseTZOffset(c)

		examIDStr := c.Query("exam_id")
		var examFilter int
		if examIDStr != "" {
			examFilter, _ = strconv.Atoi(examIDStr)
		}

		if examFilter > 0 {
			// Specific exam export — gate the download with the SAME access
			// predicate the submissions page/list uses (UserCanAccessExam), so
			// "page is visible" always implies "export is allowed". Using
			// checkExamOwnership here was stricter than the list/dropdown and
			// the all-exams export: its operator branch compares the raw
			// free-text `instansi` byte-for-byte (case-sensitive, untrimmed, no
			// instansi_id), while the rest of the app matches tenants via
			// InstansiMatchSelfSQL (canonical instansi_id, case-insensitive name
			// fallback). That mismatch made a legitimate same-instansi operator
			// (or an assigned pengawas) get 403 for a specific exam even though
			// the page listed it and "Semua Ujian" exported fine.
			if !models.UserCanAccessExam(ctx, pool, userID, isSuper, examFilter) {
				errorResponse(c, http.StatusForbidden, "Akses ditolak")
				return
			}
			exportSingleExamXLSX(c, pool, ctx, examFilter, tzOffset)
		} else {
			// All exports
			exportAllXLSX(c, pool, ctx, userID, isSuper, isOp, tzOffset)
		}
	}
}

// parseTZOffset reads the optional tz_offset query parameter (the browser's
// getTimezoneOffset() in minutes) and returns it as an *int. Returns nil when
// absent or invalid (exports stay in UTC).
func parseTZOffset(c *gin.Context) *int {
	v := c.Query("tz_offset")
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil
	}
	return &n
}

func exportSingleExamXLSX(c *gin.Context, pool *pgxpool.Pool, ctx context.Context, examID int, tzOffset *int) {
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
	maxScore := models.ComputeMaxScore(questions)

	// Custom identity fields configured for this exam (NIS, NISN, etc.).
	identityFields := helpers.ParseIdentityFields(exam.IdentityFields, defaultIdentityFields)
	identityCols := exportIdentityColumns(identityFields)

	f := excelize.NewFile()
	defer f.Close()
	st := newXLSXStyles(f)

	summarySheet := "Rekapitulasi"
	if err := f.SetSheetName("Sheet1", summarySheet); err != nil {
		errorResponse(c, http.StatusInternalServerError, "Gagal mengekspor data")
		return
	}

	headers := append([]interface{}{"ID", "Nama Ujian", "Nama Siswa", "Nomor Ujian", "Kelas"},
		identityColLabels(identityCols)...)
	headers = append(headers, "Nilai", "Skor Maks", "Persentase", "Waktu Mulai", "Waktu Kumpul", "ID Perangkat")
	_ = f.SetSheetRow(summarySheet, "A1", &headers)
	if st.headerStyle > 0 {
		lastCol, _ := excelize.ColumnNumberToName(len(headers))
		_ = f.SetCellStyle(summarySheet, "A1", lastCol+"1", st.headerStyle)
	}

	usedSheets := map[string]int{}
	for i, sub := range submissions {
		if err := ctx.Err(); err != nil {
			errorResponse(c, http.StatusRequestTimeout, "Export dibatalkan: batas waktu terlampaui")
			return
		}

		row := i + 2
		startTimeStr := ""
		if sub.StartTime != nil {
			startTimeStr = localizeExportTimeStr(*sub.StartTime, tzOffset)
		}

		// Parse identity data for this student.
		identityData := make(map[string]interface{})
		if sub.IdentityData != nil && *sub.IdentityData != "" {
			_ = json.Unmarshal([]byte(*sub.IdentityData), &identityData)
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
		col := 6
		for _, ic := range identityCols {
			_ = f.SetCellStr(summarySheet, cellRef(col, row), identityValueString(identityData, ic.key))
			col++
		}
		_ = f.SetCellValue(summarySheet, cellRef(col, row), scoreVal)
		col++
		if maxScore > 0 {
			_ = f.SetCellValue(summarySheet, cellRef(col, row), maxScore)
		} else {
			_ = f.SetCellStr(summarySheet, cellRef(col, row), "")
		}
		col++
		if sub.Score != nil && maxScore > 0 {
			pct := math.Round(*sub.Score/maxScore*1000) / 10
			_ = f.SetCellValue(summarySheet, cellRef(col, row), pct)
		} else {
			_ = f.SetCellStr(summarySheet, cellRef(col, row), "")
		}
		col++
		_ = f.SetCellStr(summarySheet, cellRef(col, row), startTimeStr)
		col++
		_ = f.SetCellStr(summarySheet, cellRef(col, row), localizeExportTime(submissionEndOf(sub), tzOffset))
		col++
		_ = f.SetCellStr(summarySheet, cellRef(col, row), sub.MACAddress)

		// Per-student detail sheet — only for students who actually submitted
		// answers. In-progress rows (heartbeat placeholders) have no answers
		// and would produce an empty sheet, so skip them.
		if sub.AnswersJSON == nil || *sub.AnswersJSON == "" {
			continue
		}

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
		writeStudentDetailSheet(f, detailSheet, exam.Name, sub, answers, evaluated, questions, st, tzOffset)
	}

	setXLSXSummaryLayout(f, summarySheet, len(headers))

	filename := fmt.Sprintf("Hasil_Ujian_%s_%s.xlsx",
		sanitizeFilename(exam.Name),
		time.Now().UTC().Format("20060102_150405"))
	writeXLSXResponse(c, f, filename)
}

func fetchSubmissionsByExam(ctx context.Context, pool *pgxpool.Pool, examID int) ([]models.Submission, error) {
	// Only rows that actually submitted answers: heartbeat/monitoring
	// placeholders (empty answers_json) must not appear in the export.
	rows, err := pool.Query(ctx,
		`SELECT id, exam_id, student_name, exam_number, student_class,
		 answers_json, score, start_time, mac_address, created_at, submitted_at, identity_data
		 FROM submissions WHERE exam_id = $1 AND answers_json IS NOT NULL AND answers_json != '' ORDER BY student_class, student_name`, examID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []models.Submission
	for rows.Next() {
		var s models.Submission
		var submittedAt *time.Time
		err := rows.Scan(
			&s.ID, &s.ExamID, &s.StudentName, &s.ExamNumber, &s.StudentClass,
			&s.AnswersJSON, &s.Score, &s.StartTime, &s.MACAddress, &s.CreatedAt,
			&submittedAt, &s.IdentityData,
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
	userID int, isSuper, isOp bool, tzOffset *int) {

	query := `SELECT s.id, s.exam_id, s.student_name, s.exam_number, s.student_class,
	s.identity_data, s.score, s.start_time, s.mac_address, s.created_at, s.submitted_at,
	e.name as exam_name
	FROM submissions s JOIN exams e ON s.exam_id = e.id`

	var conditions []string
	var args []interface{}
	argIdx := 1

	// Always exclude heartbeat/monitoring placeholders (empty answers_json):
	// the export must contain only students who actually submitted answers.
	conditions = append(conditions, submittedOnlyCondition)

	if isOp {
		// Fail-closed, canonical tenant identity (instansi_id-first) — same
		// contract as buildScopeConditions: unresolved scope → 500; bucket
		// scope (""/"personal"/"owner", any casing) → own-created exams only,
		// never an unscoped, whole-system export of every tenant's student
		// data. Name-only matching is wrong here: instansi.name is not unique.
		scope, err := getInstansiScopeForOperator(ctx, pool, userID)
		if err != nil {
			log.Printf("export all xlsx scope error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal mengekspor data")
			return
		}
		if scope.IsBucket() {
			conditions = append(conditions, fmt.Sprintf(`e.created_by = $%d`, argIdx))
			args = append(args, userID)
			argIdx++
		} else {
			frag, fargs := models.InstansiMatchSQL("", argIdx, scope)
			conditions = append(conditions,
				fmt.Sprintf(`e.created_by IN (SELECT id FROM admin_users WHERE %s)`, frag))
			args = append(args, fargs...)
			argIdx += len(fargs)
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

	// Collect rows first so we can derive the union of custom identity columns
	// across all exams in scope before writing the header.
	type allRow struct {
		id, examID   int
		studentName  string
		examNumber   string
		cls          string
		identityData map[string]interface{}
		score        *float64
		startTime    string
		macAddress   string
		createdAt    time.Time
		submittedAt  *time.Time
		examName     string
	}

	var data []allRow
	examIDs := map[int]bool{}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			rows.Close()
			errorResponse(c, http.StatusRequestTimeout, "Export dibatalkan: batas waktu terlampaui")
			return
		}
		var (
			id, examID                   int
			studentName, examNumber, cls string
			identityDataRaw              *string
			score                        *float64
			startTime, macAddress        *string
			createdAt                    time.Time
			submittedAt                  *time.Time
			examName                     string
		)
		// NOTE: scan targets must match the 12 selected columns exactly. A
		// previous mismatch (12 targets / identity_data scanned as *float64)
		// made every row fail the scan and produced an empty export.
		if err := rows.Scan(&id, &examID, &studentName, &examNumber, &cls,
			&identityDataRaw, &score, &startTime, &macAddress, &createdAt, &submittedAt,
			&examName); err != nil {
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

		idData := make(map[string]interface{})
		if identityDataRaw != nil && *identityDataRaw != "" {
			_ = json.Unmarshal([]byte(*identityDataRaw), &idData)
		}

		data = append(data, allRow{
			id: id, examID: examID, studentName: studentName, examNumber: examNumber,
			cls: cls, identityData: idData, score: score, startTime: startTimeStr,
			macAddress: macStr, createdAt: createdAt, submittedAt: submittedAt, examName: examName,
		})
		examIDs[examID] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}

	// Union of custom identity columns across all exams in scope.
	identityCols := collectAllIdentityColumns(ctx, pool, examIDs)

	f := excelize.NewFile()
	defer f.Close()
	sheet := "Semua Hasil"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		errorResponse(c, http.StatusInternalServerError, "Gagal mengekspor data")
		return
	}

	headers := append([]interface{}{"ID", "Nama Ujian", "Nama Siswa", "Nomor Ujian", "Kelas"},
		identityColLabels(identityCols)...)
	headers = append(headers, "Nilai", "Waktu Mulai", "Waktu Kumpul", "ID Perangkat")
	_ = f.SetSheetRow(sheet, "A1", &headers)
	if style, err := xlsxHeaderStyle(f); err == nil {
		lastCol, _ := excelize.ColumnNumberToName(len(headers))
		_ = f.SetCellStyle(sheet, "A1", lastCol+"1", style)
	}

	for i, r := range data {
		if err := ctx.Err(); err != nil {
			errorResponse(c, http.StatusRequestTimeout, "Export dibatalkan: batas waktu terlampaui")
			return
		}
		row := i + 2
		var scoreVal interface{}
		if r.score != nil {
			scoreVal = *r.score
		}

		_ = f.SetCellInt(sheet, cellRef(1, row), int64(r.id))
		_ = f.SetCellStr(sheet, cellRef(2, row), r.examName)
		_ = f.SetCellStr(sheet, cellRef(3, row), r.studentName)
		_ = f.SetCellStr(sheet, cellRef(4, row), r.examNumber)
		_ = f.SetCellStr(sheet, cellRef(5, row), r.cls)
		col := 6
		for _, ic := range identityCols {
			_ = f.SetCellStr(sheet, cellRef(col, row), identityValueString(r.identityData, ic.key))
			col++
		}
		_ = f.SetCellValue(sheet, cellRef(col, row), scoreVal)
		col++
		_ = f.SetCellStr(sheet, cellRef(col, row), localizeExportTimeStr(r.startTime, tzOffset))
		col++
		_ = f.SetCellStr(sheet, cellRef(col, row), localizeExportTime(submissionEnd(r.createdAt, r.submittedAt), tzOffset))
		col++
		_ = f.SetCellStr(sheet, cellRef(col, row), r.macAddress)
	}

	setXLSXSummaryLayout(f, sheet, len(headers))

	filename := fmt.Sprintf("hasil_ujian_%s.xlsx",
		time.Now().UTC().Format("20060102_150405"))
	writeXLSXResponse(c, f, filename)
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
// The workbook is written directly to the response writer (instead of
// buffering the whole file in memory via WriteToBuffer) so large exports do
// not double memory usage.
//
// NOTE: a serialization failure mid-stream is only visible in the server log
// (the client receives a truncated file). This is the accepted trade-off for
// streaming instead of buffering; headers have already been sent by then.
func writeXLSXResponse(c *gin.Context, f *excelize.File, filename string) {
	c.Header("Content-Type", xlsxContentType)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
	if err := f.Write(c.Writer); err != nil {
		log.Printf("export: stream workbook failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Export helper utilities
// ---------------------------------------------------------------------------

// defaultIdentityFields mirrors the public package's default identity fields
// used when an exam has no custom identity_fields configured.
var defaultIdentityFields []map[string]interface{}

func init() {
	_ = json.Unmarshal([]byte(`[
		{"key":"student_name","label":"Nama","required":true},
		{"key":"exam_number","label":"Nomor Ujian","required":true},
		{"key":"student_class","label":"Kelas","required":true}
	]`), &defaultIdentityFields)
}

// identityCol describes one custom identity column in the export.
type identityCol struct {
	key   string
	label string
}

// exportIdentityColumns derives the custom identity columns (excluding the
// three standard student fields) from an exam's identity_fields config.
func exportIdentityColumns(fields []map[string]interface{}) []identityCol {
	var cols []identityCol
	standard := map[string]bool{"student_name": true, "exam_number": true, "student_class": true}
	seen := map[string]bool{}
	for _, f := range fields {
		key, _ := f["key"].(string)
		if key == "" || standard[key] || seen[key] {
			continue
		}
		seen[key] = true
		label, _ := f["label"].(string)
		if label == "" {
			label = key
		}
		cols = append(cols, identityCol{key: key, label: label})
	}
	return cols
}

// identityColLabels returns the header labels for a set of identity columns.
func identityColLabels(cols []identityCol) []interface{} {
	labels := make([]interface{}, 0, len(cols))
	for _, c := range cols {
		labels = append(labels, c.label)
	}
	return labels
}

// identityValueString returns the string value of an identity key for a
// student, or "" when missing.
func identityValueString(data map[string]interface{}, key string) string {
	if v, ok := data[key]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// collectAllIdentityColumns fetches identity_fields for all exams in scope and
// returns the union of custom identity columns (first-seen label wins). IDs are
// sorted so the resulting column order is deterministic across exports.
func collectAllIdentityColumns(ctx context.Context, pool *pgxpool.Pool, examIDs map[int]bool) []identityCol {
	if len(examIDs) == 0 {
		return nil
	}
	ids := make([]int, 0, len(examIDs))
	for id := range examIDs {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	rows, err := pool.Query(ctx,
		`SELECT identity_fields FROM exams WHERE id = ANY($1)`, ids)
	if err != nil {
		log.Printf("export all: identity fields query error: %v", err)
		return nil
	}
	defer rows.Close()

	standard := map[string]bool{"student_name": true, "exam_number": true, "student_class": true}
	seen := map[string]bool{}
	var cols []identityCol
	for rows.Next() {
		var raw *string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		for _, f := range helpers.ParseIdentityFields(raw, defaultIdentityFields) {
			key, _ := f["key"].(string)
			if key == "" || standard[key] || seen[key] {
				continue
			}
			seen[key] = true
			label, _ := f["label"].(string)
			if label == "" {
				label = key
			}
			cols = append(cols, identityCol{key: key, label: label})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}
	return cols
}

// localizeExportTime converts a UTC time.Time to a localized display string
// using the browser timezone offset (minutes). When tzOffset is nil the time
// stays UTC without a suffix (cleaner for Excel cells) — matching the admin
// UI's localizeUTC behaviour. It delegates to helpers.LocalizeDateString so
// the timezone math lives in a single place.
func localizeExportTime(t time.Time, tzOffset *int) string {
	if tzOffset == nil {
		return t.UTC().Format("2006-01-02 15:04:05")
	}
	return helpers.LocalizeDateString(helpers.FormatISOUTC(t), tzOffset)
}

// localizeExportTimeStr localizes a legacy timestamp string (e.g.
// "2026-08-05 08:00:00", stored as UTC) using the browser timezone offset.
func localizeExportTimeStr(s string, tzOffset *int) string {
	if s == "" {
		return ""
	}
	return helpers.LocalizeDateString(s, tzOffset)
}

// localizeStatusText maps the English scoring statuses to Indonesian labels
// so exported sheets match the admin UI language.
func localizeStatusText(status string) string {
	switch status {
	case models.StatusCorrect:
		return "Benar"
	case models.StatusIncorrect:
		return "Salah"
	case models.StatusPartial:
		return "Sebagian"
	case models.StatusUnanswered:
		return "Tidak Dijawab"
	default:
		return status
	}
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
	questions []models.Question, st xlsxStyles, tzOffset *int) {

	startTimeStr := ""
	if sub.StartTime != nil {
		startTimeStr = localizeExportTimeStr(*sub.StartTime, tzOffset)
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
		{"Waktu Kumpul", localizeExportTime(submissionEndOf(sub), tzOffset)},
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
			statusText = localizeStatusText(eval.StatusText)
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
