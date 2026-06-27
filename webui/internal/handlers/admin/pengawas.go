package admin

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// 1. GET /admin/pengawas — Render pengawas monitoring page
// ---------------------------------------------------------------------------

func PengawasPage() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isSuperAdmin(c) && !isOperator(c) && !hasCurrentRole(c, models.RolePengawas) {
			c.Redirect(http.StatusFound, "/admin/dashboard")
			return
		}

		renderAdminPage(c, "admin/pengawas.html", gin.H{
			"active_page": "pengawas",
		})
	}
}

// ---------------------------------------------------------------------------
// 2. GET /admin/pengawas/:exam_id — Render detail monitoring page for an exam
// ---------------------------------------------------------------------------

func PengawasDetailPage() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			c.Redirect(http.StatusFound, "/admin/pengawas")
			return
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		isPrivileged := isSuperAdmin(c) || isOperator(c)
		ctx := c.Request.Context()

		if !isPrivileged {
			assigned, err := models.IsUserAssignedAsPengawas(ctx, pool, examID, userID)
			if err != nil || !assigned {
				c.Redirect(http.StatusFound, "/admin/pengawas")
				return
			}
		}

		exam, err := models.GetExamByID(ctx, pool, examID)
		if err != nil {
			c.Redirect(http.StatusFound, "/admin/pengawas")
			return
		}

		renderAdminPage(c, "admin/pengawas_detail.html", gin.H{
			"exam":        exam,
			"active_page": "pengawas",
		})
	}
}

// ---------------------------------------------------------------------------
// 3. GET /admin/api/pengawas/exams — List assigned exams (JSON)
// ---------------------------------------------------------------------------

func PengawasExams() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		isPrivileged := isSuperAdmin(c) || isOperator(c)
		ctx := c.Request.Context()

		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		if page < 1 {
			page = 1
		}
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "10"))
		if perPage < 5 {
			perPage = 5
		} else if perPage > 50 {
			perPage = 50
		}
		search := strings.TrimSpace(c.Query("search"))

		var result models.ListExamsResult
		var err error

		if isPrivileged {
			opts := models.ListExamsOpts{
				Page:    page,
				PerPage: perPage,
				Search:  search,
			}
			result, err = models.ListExams(ctx, pool, opts)
		} else {
			opts := models.ListPengawasExamsOpts{
				Page:    page,
				PerPage: perPage,
				Search:  search,
				UserID:  userID,
			}
			listResult, listErr := models.ListPengawasExams(ctx, pool, opts)
			result = listResult
			err = listErr
		}

		if err != nil {
			log.Printf("pengawas exams error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat daftar ujian")
			return
		}

		type examItem struct {
			ID             int    `json:"id"`
			Name           string `json:"name"`
			Token          string `json:"token"`
			Status         string `json:"status"`
			StartTime      string `json:"start_time"`
			EndTime        string `json:"end_time"`
			CreatorName    string `json:"creator_name"`
			TotalStudents  int    `json:"total_students"`
			SubmittedCount int    `json:"submitted_count"`
			CreatedAt      string `json:"created_at"`
		}

		examList := make([]examItem, 0, len(result.Exams))
		for _, e := range result.Exams {
			startStr := ""
			if e.StartTime != nil {
				startStr = e.StartTime.Format("2006-01-02 15:04:05")
			}
			endStr := ""
			if e.EndTime != nil {
				endStr = e.EndTime.Format("2006-01-02 15:04:05")
			}
			item := examItem{
				ID:        e.ID,
				Name:      e.Name,
				Token:     e.Token,
				Status:    e.Status,
				StartTime: startStr,
				EndTime:   endStr,
				CreatedAt: formatISOUTC(e.CreatedAt),
			}
			// Fetch submission counts
			var total, submitted int
			pool.QueryRow(ctx, `SELECT COUNT(*) FROM submissions WHERE exam_id = $1`, e.ID).Scan(&total)
			pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM submissions WHERE exam_id = $1 AND answers_json IS NOT NULL AND answers_json != ''`,
				e.ID).Scan(&submitted)
			item.TotalStudents = total
			item.SubmittedCount = submitted
			examList = append(examList, item)
		}

		// Overall stats across all matching exams
		var totalExamsCount, activeCount, totalStudents, totalSubmitted int
		for _, e := range result.Exams {
			totalExamsCount++
			if e.IsActive() {
				activeCount++
			}
			var cnt int
			pool.QueryRow(ctx, `SELECT COUNT(*) FROM submissions WHERE exam_id = $1`, e.ID).Scan(&cnt)
			totalStudents += cnt
			pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM submissions WHERE exam_id = $1 AND answers_json IS NOT NULL AND answers_json != ''`,
				e.ID).Scan(&cnt)
			totalSubmitted += cnt
		}

		c.JSON(http.StatusOK, gin.H{
			"success":       true,
			"exams":         examList,
			"is_privileged": isPrivileged,
			"page":          result.Page,
			"per_page":      result.PerPage,
			"total":         result.Total,
			"total_pages":   result.TotalPages,
			"stats": gin.H{
				"total_exams":     totalExamsCount,
				"active_exams":    activeCount,
				"total_students":  totalStudents,
				"total_submitted": totalSubmitted,
			},
		})
	}
}

// accessLogEntry is a package-level type for student access log entries.
type accessLogEntry struct {
	Event        string `json:"event"`
	IPAddress    string `json:"ip_address"`
	DeviceInfo   string `json:"device_info"`
	CreatedAt    string `json:"created_at"`
	StudentName  string `json:"student_name"`
	ExamNumber   string `json:"exam_number"`
	StudentClass string `json:"student_class"`
}

// ---------------------------------------------------------------------------
// 4. GET /admin/api/pengawas/exams/:exam_id/submissions — List submissions
// ---------------------------------------------------------------------------

func PengawasExamSubmissions() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		isPrivileged := isSuperAdmin(c) || isOperator(c)
		ctx := c.Request.Context()

		if !isPrivileged {
			assigned, err := models.IsUserAssignedAsPengawas(ctx, pool, examID, userID)
			if err != nil || !assigned {
				errorResponse(c, http.StatusForbidden, "Akses ditolak: Anda tidak ditugaskan sebagai pengawas ujian ini")
				return
			}
		}

		exam, err := models.GetExamByID(ctx, pool, examID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}

		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		if page < 1 {
			page = 1
		}
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
		if perPage < 5 {
			perPage = 5
		} else if perPage > 100 {
			perPage = 100
		}
		search := strings.TrimSpace(c.Query("search"))

		opts := models.ListSubmissionsByExamOpts{
			ExamID:  examID,
			Page:    page,
			PerPage: perPage,
			Search:  search,
		}

		result, err := models.ListSubmissionsByExam(ctx, pool, opts)
		if err != nil {
			log.Printf("pengawas exam submissions error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat data peserta")
			return
		}

		type subItem struct {
			ID            int                    `json:"id"`
			StudentName   string                 `json:"student_name"`
			ExamNumber    string                 `json:"exam_number"`
			StudentClass  string                 `json:"student_class"`
			IdentityData  map[string]interface{} `json:"identity_data"`
			Submitted     bool                   `json:"submitted"`
			Score         *float64               `json:"score"`
			StartTime     string                 `json:"start_time"`
			CreatedAt     string                 `json:"created_at"`
			FirstAccessAt string                 `json:"first_access_at"`
			LastAccessAt  string                 `json:"last_access_at"`
			MACAddress    string                 `json:"mac_address"`
			AccessLogs    []accessLogEntry       `json:"access_logs"`
			IsOnline      bool                   `json:"is_online"`
		}

		subsData := make([]subItem, 0, len(result.Submissions))
		for _, sub := range result.Submissions {
			identityData := make(map[string]interface{})
			if sub.IdentityData != nil && *sub.IdentityData != "" {
				json.Unmarshal([]byte(*sub.IdentityData), &identityData)
			}

			accessLogs := fetchStudentAccessLogs(ctx, pool, examID, sub.MACAddress)

			firstAccess := ""
			lastAccess := sub.CreatedAt.Format("2006-01-02T15:04:05Z")

			if len(accessLogs) > 0 {
				firstLog := accessLogs[0]
				lastLog := accessLogs[len(accessLogs)-1]
				firstAccess = firstLog.CreatedAt
				lastAccess = lastLog.CreatedAt
			}

			startTimeStr := ""
			if sub.StartTime != nil {
				startTimeStr = *sub.StartTime
			}

			submitted := sub.AnswersJSON != nil && *sub.AnswersJSON != ""

			subsData = append(subsData, subItem{
				ID:            sub.ID,
				StudentName:   sub.StudentName,
				ExamNumber:    sub.ExamNumber,
				StudentClass:  sub.StudentClass,
				IdentityData:  identityData,
				Submitted:     submitted,
				Score:         sub.Score,
				StartTime:     startTimeStr,
				CreatedAt:     sub.CreatedAt.Format("2006-01-02T15:04:05Z"),
				FirstAccessAt: firstAccess,
				LastAccessAt:  lastAccess,
				MACAddress:    sub.MACAddress,
				AccessLogs:    accessLogs,
				IsOnline:      false,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"success":     true,
			"exam_name":   exam.Name,
			"submissions": subsData,
			"page":        result.Page,
			"per_page":    result.PerPage,
			"total":       result.Total,
			"total_pages": result.TotalPages,
			"stats":       result.Stats,
		})
	}
}

func fetchStudentAccessLogs(ctx context.Context, pool *pgxpool.Pool, examID int, macAddress string) []accessLogEntry {
	if macAddress == "" {
		return nil
	}

	rows, err := pool.Query(ctx,
		`SELECT event, ip_address, device_info, created_at,
		 student_name, exam_number, student_class
		 FROM student_access_logs
		 WHERE exam_id = $1 AND student_identifier = $2
		 ORDER BY created_at ASC`, examID, macAddress)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var logs []accessLogEntry
	for rows.Next() {
		var entry accessLogEntry
		var createdAt string
		if err := rows.Scan(&entry.Event, &entry.IPAddress, &entry.DeviceInfo,
			&createdAt, &entry.StudentName, &entry.ExamNumber, &entry.StudentClass); err != nil {
			continue
		}
		entry.CreatedAt = createdAt
		logs = append(logs, entry)
	}
	return logs
}
