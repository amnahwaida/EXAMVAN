package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	redis "github.com/redis/go-redis/v9"

	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// 1. GET /admin/pengawas — Render pengawas monitoring page
// ---------------------------------------------------------------------------

func PengawasPage() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isSuperAdmin(c) && !isOperator(c) && !hasCurrentRole(c, models.RolePengawas) && !hasCurrentRole(c, models.RoleGuru) {
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

		exam, err := models.GetExamByID(ctx, pool, examID)
		if err != nil {
			c.Redirect(http.StatusFound, "/admin/pengawas")
			return
		}

		if !isPrivileged {
			isCreator := exam.CreatedBy == userID
			isCoordinator := exam.DelegatedTo != nil && *exam.DelegatedTo == userID
			
			isAssigned := false
			if hasCurrentRole(c, models.RolePengawas) {
				assigned, err := models.IsUserAssignedAsPengawas(ctx, pool, examID, userID)
				isAssigned = err == nil && assigned
			}

			if !isCreator && !isCoordinator && !isAssigned {
				c.Redirect(http.StatusFound, "/admin/pengawas")
				return
			}
		}

		autoResetActiveTokenIfNeeded(ctx, pool, &exam)
		
		creatorName := "-"
		creator, err := models.GetUserByID(ctx, pool, exam.CreatedBy)
		if err == nil {
			if creator.Name != "" {
				creatorName = fmt.Sprintf("%s (%s)", creator.Name, creator.Username)
			} else {
				creatorName = creator.Username
			}
		}
		
		delegatedName := "-"
		if exam.DelegatedTo != nil {
			delegated, err := models.GetUserByID(ctx, pool, *exam.DelegatedTo)
			if err == nil {
				if delegated.Name != "" {
					delegatedName = fmt.Sprintf("%s (%s)", delegated.Name, delegated.Username)
				} else {
					delegatedName = delegated.Username
				}
			}
		}
		
		pengawasAssignments, _ := models.GetPengawasAssignments(ctx, pool, examID)

		canControl := isPrivileged || exam.CreatedBy == userID || (exam.DelegatedTo != nil && *exam.DelegatedTo == userID)

		tokenMode := "dynamic"
		if exam.TokenMode != nil && *exam.TokenMode != "" {
			tokenMode = *exam.TokenMode
		}

		renderAdminPage(c, "admin/pengawas_detail.html", gin.H{
			"exam":                 exam,
			"token_mode":           tokenMode,
			"creator_name":         creatorName,
			"delegated_name":       delegatedName,
			"pengawas_list":        pengawasAssignments,
			"active_page":          "pengawas",
			"can_control_settings": canControl,
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
				Page:            page,
				PerPage:         perPage,
				Search:          search,
				UserID:          userID,
				HasPengawasRole: hasCurrentRole(c, models.RolePengawas),
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
			ID             int        `json:"id"`
			Name           string     `json:"name"`
			Token          string     `json:"token"`
			ActiveToken    string     `json:"active_token"`
			TokenMode      string     `json:"token_mode"`
			Status         string     `json:"status"`
			StartTime      string     `json:"start_time"`
			EndTime        string     `json:"end_time"`
			CreatorName    string     `json:"creator_name"`
			TotalStudents  int        `json:"total_students"`
			SubmittedCount int        `json:"submitted_count"`
			CreatedAt      string     `json:"created_at"`
			ExamStartedAt  *time.Time `json:"exam_started_at"`
		}

		// Build username map
		usernameMap := make(map[int]string)
		userIDs := make(map[int]bool)
		for _, e := range result.Exams {
			userIDs[e.CreatedBy] = true
		}
		if len(userIDs) > 0 {
			ids := make([]int, 0, len(userIDs))
			for id := range userIDs {
				ids = append(ids, id)
			}
			uRows, uErr := pool.Query(ctx, `SELECT id, username FROM admin_users WHERE id = ANY($1)`, ids)
			if uErr == nil {
				for uRows.Next() {
					var uid int
					var uname string
					if err := uRows.Scan(&uid, &uname); err == nil {
						usernameMap[uid] = uname
					}
				}
				uRows.Close()
			}
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
			tokenMode := "dynamic"
			if e.TokenMode != nil && *e.TokenMode != "" {
				tokenMode = *e.TokenMode
			}
			item := examItem{
				ID:            e.ID,
				Name:          e.Name,
				Token:         e.Token,
				ActiveToken:   e.ActiveToken,
				TokenMode:     tokenMode,
				Status:        e.Status,
				StartTime:     startStr,
				EndTime:       endStr,
				CreatedAt:     formatISOUTC(e.CreatedAt),
				ExamStartedAt: e.ExamStartedAt,
				CreatorName:   usernameMap[e.CreatedBy],
			}
			examList = append(examList, item)
		}

		// Batch fetch submission counts (single query)
		examCountMap := make(map[int][2]int)
		if len(result.Exams) > 0 {
			ids := make([]int, len(result.Exams))
			for i, e := range result.Exams {
				ids[i] = e.ID
			}
			cRows, cErr := pool.Query(ctx,
				`SELECT exam_id,
					COUNT(*) as total,
					SUM(CASE WHEN answers_json IS NOT NULL AND answers_json != '' THEN 1 ELSE 0 END) as submitted
				 FROM submissions WHERE exam_id = ANY($1) GROUP BY exam_id`, ids)
			if cErr == nil {
				for cRows.Next() {
					var eid, total, submitted int
					cRows.Scan(&eid, &total, &submitted)
					examCountMap[eid] = [2]int{total, submitted}
				}
				cRows.Close()
			}
		}

		// Apply batch counts
		for i := range examList {
			e := result.Exams[i]
			counts := examCountMap[e.ID]
			examList[i].TotalStudents = counts[0]
			examList[i].SubmittedCount = counts[1]
		}

		// Overall stats from batch data
		var totalExamsCount, activeCount, totalStudents, totalSubmitted int
		for _, e := range result.Exams {
			totalExamsCount++
			if e.IsActive() {
				activeCount++
			}
			counts := examCountMap[e.ID]
			totalStudents += counts[0]
			totalSubmitted += counts[1]
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
	Event        string                 `json:"event"`
	IPAddress    string                 `json:"ip_address"`
	DeviceInfo   string                 `json:"device_info"`
	CreatedAt    string                 `json:"created_at"`
	StudentName  string                 `json:"student_name"`
	ExamNumber   string                 `json:"exam_number"`
	StudentClass string                 `json:"student_class"`
	IdentityData map[string]interface{} `json:"identity_data"`
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

		exam, err := models.GetExamByID(ctx, pool, examID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}
		autoResetActiveTokenIfNeeded(ctx, pool, &exam)

		if !isPrivileged {
			isCreator := exam.CreatedBy == userID
			isCoordinator := exam.DelegatedTo != nil && *exam.DelegatedTo == userID
			
			isAssigned := false
			if hasCurrentRole(c, models.RolePengawas) {
				assigned, err := models.IsUserAssignedAsPengawas(ctx, pool, examID, userID)
				isAssigned = err == nil && assigned
			}

			if !isCreator && !isCoordinator && !isAssigned {
				errorResponse(c, http.StatusForbidden, "Akses ditolak: Anda tidak memiliki wewenang untuk mengawasi ujian ini")
				return
			}
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
		status := strings.TrimSpace(c.Query("status"))

		opts := models.ListSubmissionsByExamOpts{
			ExamID:  examID,
			Page:    page,
			PerPage: perPage,
			Search:  search,
			Status:  status,
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

			isOnline := false
			if rdb, exists := c.Get("redis"); exists && rdb != nil {
				if redisClient, ok := rdb.(*redis.Client); ok {
					key := fmt.Sprintf("heartbeat:%d:%s", examID, sub.MACAddress)
					existsVal, err := redisClient.Exists(ctx, key).Result()
					isOnline = (err == nil && existsVal > 0)
				}
			}

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
				IsOnline:      isOnline,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"success":          true,
			"exam_name":        exam.Name,
			"exam_active_token": exam.ActiveToken,
			"submissions":     subsData,
			"page":            result.Page,
			"per_page":        result.PerPage,
			"total":           result.Total,
			"total_pages":     result.TotalPages,
			"stats":           result.Stats,
		})
	}
}

func fetchStudentAccessLogs(ctx context.Context, pool *pgxpool.Pool, examID int, macAddress string) []accessLogEntry {
	if macAddress == "" {
		return nil
	}

	rows, err := pool.Query(ctx,
		`SELECT event, ip_address, device_info, created_at,
		 student_name, exam_number, student_class, identity_data
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
		var createdAt time.Time
		var identityDataStr *string
		if err := rows.Scan(&entry.Event, &entry.IPAddress, &entry.DeviceInfo,
			&createdAt, &entry.StudentName, &entry.ExamNumber, &entry.StudentClass, &identityDataStr); err != nil {
			log.Printf("scan error: %v", err); continue
		}
		entry.CreatedAt = createdAt.Format(time.RFC3339)
		if identityDataStr != nil {
			var idData map[string]interface{}
			if err := json.Unmarshal([]byte(*identityDataStr), &idData); err == nil {
				entry.IdentityData = idData
			}
		}
		logs = append(logs, entry)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}

	return logs
}

func autoResetActiveTokenIfNeeded(ctx context.Context, pool *pgxpool.Pool, exam *models.Exam) {
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
				now := time.Now().UTC()
				exam.TokenLastResetAt = &now
			}
		}
	}
}
