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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	redis "github.com/redis/go-redis/v9"

	"github.com/examvan/webui/internal/models"
	"github.com/examvan/webui/internal/services/examtoken"
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
		isSuper := isSuperAdmin(c)
		ctx := c.Request.Context()

		exam, err := models.GetExamByID(ctx, pool, examID)
		if err != nil {
			c.Redirect(http.StatusFound, "/admin/pengawas")
			return
		}

		// Authorization is exam-scoped: operators are constrained to their own
		// instansi (UserCanAccessExam), not treated as globally privileged.
		if !models.UserCanAccessExam(ctx, pool, userID, isSuper, examID) {
			c.Redirect(http.StatusFound, "/admin/pengawas")
			return
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

		// Control (settings/start-stop) is management-level: excludes a
		// pengawas-only viewer and scopes operators to their instansi.
		canControl := models.UserCanControlExam(ctx, pool, userID, isSuper, examID)

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
		isSuper := isSuperAdmin(c)
		isOp := isOperator(c)
		// UI flag only; the data query below is tenant-scoped for operators.
		isPrivileged := isSuper || isOp
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

		if isSuper {
			// SuperAdmin sees all exams.
			result, err = models.ListExams(ctx, pool, models.ListExamsOpts{
				Page:    page,
				PerPage: perPage,
				Search:  search,
			})
		} else if isOp {
			// Operators are scoped to their own instansi — previously this
			// listed every tenant's exams (including active tokens). "" and the
			// "personal" sentinel are NOT real tenants (the shared default
			// bucket), so an operator without a real instansi sees only own exams.
			opts := models.ListExamsOpts{Page: page, PerPage: perPage, Search: search}
			opInstansi := getInstansiForOperator(ctx, pool, userID)
			if opInstansi != "" && opInstansi != "personal" {
				opts.Instansi = opInstansi
			} else {
				uid := userID
				opts.CreatedBy = &uid
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
			// Tombstoned: the exam was auto-inactivated (policy B) when the
			// school's operator was cut off (voucher switch / manual
			// suspension), not manually. Lets the pengawas page explain why a
			// Nonaktif exam is dormant instead of leaving the operator puzzled.
			Tombstoned   bool   `json:"tombstoned"`
			TombstonedAt string `json:"tombstoned_at"`
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
			tombstonedAt := ""
			if e.TombstonedAt != nil {
				tombstonedAt = formatISOUTC(*e.TombstonedAt)
			}
			item := examItem{
				ID:             e.ID,
				Name:           e.Name,
				Token:          e.Token,
				ActiveToken:    e.ActiveToken,
				TokenMode:      tokenMode,
				Status:         e.Status,
				StartTime:      startStr,
				EndTime:        endStr,
				CreatedAt:      formatISOUTC(e.CreatedAt),
				ExamStartedAt:  e.ExamStartedAt,
				CreatorName:    usernameMap[e.CreatedBy],
				Tombstoned:     e.TombstonedAt != nil,
				TombstonedAt:   tombstonedAt,
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
		isSuper := isSuperAdmin(c)
		ctx := c.Request.Context()

		exam, err := models.GetExamByID(ctx, pool, examID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}
		autoResetActiveTokenIfNeeded(ctx, pool, &exam)

		// Exam-scoped authorization (operators limited to their instansi).
		if !models.UserCanAccessExam(ctx, pool, userID, isSuper, examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak: Anda tidak memiliki wewenang untuk mengawasi ujian ini")
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
			IsOnline          bool                   `json:"is_online"`
			AttemptCount      int                    `json:"attempt_count"`
			SubmissionHistory []models.Submission    `json:"submission_history"`
		}

		attemptCounts := make(map[string]int)
		rows, countErr := pool.Query(ctx, "SELECT mac_address, COUNT(*) FROM submissions WHERE exam_id = $1 GROUP BY 1", examID)
		if countErr == nil {
			for rows.Next() {
				var key string
				var count int
				if err := rows.Scan(&key, &count); err == nil {
					attemptCounts[key] = count
				}
			}
			rows.Close()
		}

		subsData := make([]subItem, 0, len(result.Submissions))
		for _, sub := range result.Submissions {
			identityData := make(map[string]interface{})
			if sub.IdentityData != nil && *sub.IdentityData != "" {
				json.Unmarshal([]byte(*sub.IdentityData), &identityData)
			}

			var accessLogs []accessLogEntry
			accessLogs = fetchStudentAccessLogs(ctx, pool, examID, sub.MACAddress)

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

			attKey := sub.MACAddress
			if attKey == "" {
				attKey = strconv.Itoa(sub.ID)
			}
			attCount := attemptCounts[attKey]
			if attCount == 0 {
				attCount = 1
			}

			// Fetch submission history for this MAC Address
			var subHistory []models.Submission
			histRows, histErr := pool.Query(ctx, 
				"SELECT id, start_time, created_at, answers_json, score FROM submissions WHERE exam_id = $1 AND mac_address = $2 ORDER BY created_at ASC", 
				examID, sub.MACAddress)
			if histErr == nil {
				for histRows.Next() {
					var h models.Submission
					var created time.Time
					histRows.Scan(&h.ID, &h.StartTime, &created, &h.AnswersJSON, &h.Score)
					h.CreatedAt = created
					subHistory = append(subHistory, h)
				}
				histRows.Close()
			}

			subsData = append(subsData, subItem{
				ID:                sub.ID,
				StudentName:       sub.StudentName,
				ExamNumber:        sub.ExamNumber,
				StudentClass:      sub.StudentClass,
				IdentityData:      identityData,
				Submitted:         submitted,
				Score:             sub.Score,
				StartTime:         startTimeStr,
				CreatedAt:         sub.CreatedAt.Format("2006-01-02T15:04:05Z"),
				FirstAccessAt:     firstAccess,
				LastAccessAt:      lastAccess,
				MACAddress:        sub.MACAddress,
				AccessLogs:        accessLogs,
				IsOnline:          isOnline,
				AttemptCount:      attCount,
				SubmissionHistory: subHistory,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"success":           true,
			"exam_name":         exam.Name,
			"exam_active_token": exam.ActiveToken,
			"submissions":       subsData,
			"page":              result.Page,
			"per_page":          result.PerPage,
			"total":             result.Total,
			"total_pages":       result.TotalPages,
			"stats":             result.Stats,
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
			log.Printf("scan error: %v", err)
			continue
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

func fetchStudentAccessLogsByExamNumber(ctx context.Context, pool *pgxpool.Pool, examID int, examNumber string) []accessLogEntry {
	if examNumber == "" {
		return nil
	}

	rows, err := pool.Query(ctx,
		`SELECT event, ip_address, device_info, created_at,
		 student_name, exam_number, student_class, identity_data, student_identifier
		 FROM student_access_logs
		 WHERE exam_id = $1 AND exam_number = $2
		 ORDER BY created_at ASC`, examID, examNumber)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var logs []accessLogEntry
	for rows.Next() {
		var entry accessLogEntry
		var createdAt time.Time
		var identityDataStr *string
		var macAddr *string
		if err := rows.Scan(&entry.Event, &entry.IPAddress, &entry.DeviceInfo,
			&createdAt, &entry.StudentName, &entry.ExamNumber, &entry.StudentClass, &identityDataStr, &macAddr); err != nil {
			continue
		}
		entry.CreatedAt = createdAt.Format(time.RFC3339)
		if identityDataStr != nil {
			var idData map[string]interface{}
			if err := json.Unmarshal([]byte(*identityDataStr), &idData); err == nil {
				entry.IdentityData = idData
			}
		}
		if macAddr != nil {
			entry.DeviceInfo = *macAddr + " - " + entry.DeviceInfo
		}
		logs = append(logs, entry)
	}
	return logs
}

func autoResetActiveTokenIfNeeded(ctx context.Context, pool *pgxpool.Pool, exam *models.Exam) {
	if err := examtoken.MaybeResetActiveToken(ctx, pool, exam, time.Now().UTC()); err != nil {
		log.Printf("auto reset active token error: %v", err)
	}
}

func GetPendingApprovals() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, _ := strconv.Atoi(c.Param("exam_id"))
		pool := getPool(c)
		ctx := c.Request.Context()

		// Exam-scoped authorization (operators limited to their instansi).
		userID := getCurrentUserID(c)
		if _, err := models.GetExamByID(ctx, pool, examID); err != nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}
		if !models.UserCanAccessExam(ctx, pool, userID, isSuperAdmin(c), examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak: Anda tidak memiliki wewenang untuk mengawasi ujian ini")
			return
		}

		rows, err := pool.Query(ctx,
			`SELECT mac_address, student_name, exam_number, student_class, identity_data, created_at, status
			 FROM exam_approvals
			 WHERE exam_id = $1 AND status = 'pending'
			 ORDER BY created_at ASC`, examID)
		
		if err != nil {
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat persetujuan")
			return
		}
		defer rows.Close()

		type approvalItem struct {
			MACAddress   string                 `json:"mac_address"`
			StudentName  string                 `json:"student_name"`
			ExamNumber   string                 `json:"exam_number"`
			StudentClass string                 `json:"student_class"`
			IdentityData map[string]interface{} `json:"identity_data"`
			CreatedAt    string                 `json:"created_at"`
			Status       string                 `json:"status"`
		}

		var items []approvalItem
		for rows.Next() {
			var i approvalItem
			var created time.Time
			var idDataStr string
			if err := rows.Scan(&i.MACAddress, &i.StudentName, &i.ExamNumber, &i.StudentClass, &idDataStr, &created, &i.Status); err == nil {
				i.CreatedAt = created.Format("2006-01-02T15:04:05Z")
				i.IdentityData = make(map[string]interface{})
				if idDataStr != "" {
					json.Unmarshal([]byte(idDataStr), &i.IdentityData)
				}
				items = append(items, i)
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    items,
		})
	}
}

func SetApprovalStatus() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, _ := strconv.Atoi(c.Param("exam_id"))
		macAddress := c.Param("mac_address")
		
		var req struct {
			Status string `json:"status"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			errorResponse(c, http.StatusBadRequest, "Payload tidak valid")
			return
		}

		if req.Status != "approved" && req.Status != "rejected" {
			errorResponse(c, http.StatusBadRequest, "Status tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		// Exam-scoped authorization (operators limited to their instansi).
		// Approving/rejecting devices is part of the pengawas role, so a valid
		// pengawas/owner/delegate/in-instansi-operator/super may act.
		userID := getCurrentUserID(c)
		if _, err := models.GetExamByID(ctx, pool, examID); err != nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}
		if !models.UserCanAccessExam(ctx, pool, userID, isSuperAdmin(c), examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak: Anda tidak memiliki wewenang untuk mengawasi ujian ini")
			return
		}

		_, err := pool.Exec(ctx,
			`UPDATE exam_approvals SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE exam_id = $2 AND mac_address = $3`,
			req.Status, examID, macAddress)
		
		if err != nil {
			errorResponse(c, http.StatusInternalServerError, "Gagal mengubah status persetujuan")
			return
		}

		if req.Status == "approved" {
			var sName, eNum, sClass, iData string
			if err := pool.QueryRow(ctx, "SELECT student_name, exam_number, student_class, COALESCE(identity_data, '{}') FROM exam_approvals WHERE exam_id=$1 AND mac_address=$2", examID, macAddress).Scan(&sName, &eNum, &sClass, &iData); err == nil {
				// Check if the latest submission is already submitted
				var latestAnswers *string
				errLookup := pool.QueryRow(ctx, "SELECT answers_json FROM submissions WHERE exam_id=$1 AND mac_address=$2 ORDER BY created_at DESC LIMIT 1", examID, macAddress).Scan(&latestAnswers)
				
				// Create new row if no submissions exist, or if the latest one is already submitted
				if errLookup == pgx.ErrNoRows || (errLookup == nil && latestAnswers != nil && *latestAnswers != "") {
					_, err = pool.Exec(ctx, `
						INSERT INTO submissions (exam_id, mac_address, student_name, exam_number, student_class, identity_data, start_time, created_at)
						VALUES ($1, $2, $3, $4, $5, $6, $7, CURRENT_TIMESTAMP)
					`, examID, macAddress, sName, eNum, sClass, iData, time.Now().UTC().Format(time.RFC3339))
					if err != nil {
						log.Printf("failed to insert submission on approval: %v", err)
					}
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
		})
	}
}
