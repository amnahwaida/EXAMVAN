package admin

import (
	"context"
	"encoding/json"
	"errors"
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

	"github.com/examvan/webui/internal/helpers"
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

		pengawasAssignments, err := models.GetPengawasAssignments(ctx, pool, examID)
		if err != nil {
			log.Printf("load pengawas assignments error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat penugasan pengawas")
			return
		}

		// Control (settings/start-stop) is management-level: excludes a
		// pengawas-only viewer and scopes operators to their instansi.
		canControl := models.UserCanControlExam(ctx, pool, userID, isSuper, examID)

		tokenMode := "dynamic"
		if exam.TokenMode != nil && *exam.TokenMode != "" {
			tokenMode = *exam.TokenMode
		}

		scheduleEnded := models.ExamScheduleEnded(&exam, time.Now().UTC())
		examStarted := exam.ExamStartedAt != nil
		renderAdminPage(c, "admin/pengawas_detail.html", gin.H{
			"exam":                 exam,
			"exam_started":         examStarted,
			"exam_live":            exam.IsActive() && examStarted && !scheduleEnded,
			"exam_schedule_ended":  scheduleEnded,
			"token_mode":           tokenMode,
			"creator_name":         creatorName,
			"delegated_name":       delegatedName,
			"pengawas_list":        pengawasAssignments,
			"active_page":          "pengawas",
			"can_control_settings": canControl,
			// server_now_ms: jam server saat render, dipakai halaman untuk
			// menghitung offset jam klien (countdown rotasi token & deteksi
			// Due selaras server, bukan jam PC pengawas yang bisa skew).
			"server_now_ms": time.Now().UnixMilli(),
		})
	}
}

// ---------------------------------------------------------------------------
// statsWalkPerPage is the page size used when walking every accessible exam to
// build the tenant-wide header stats. 100 mirrors models' own page cap, so the
// walk costs one query per 100 exams.
const statsWalkPerPage = 100

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
		// Hoisted: the stats aggregate below re-walks the same scoped list and
		// needs the operator's resolved tenant scope.
		var opScope models.InstansiScope

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
			scope, err := getInstansiScopeForOperator(ctx, pool, userID)
			if err != nil {
				log.Printf("list pengawas exams: operator instansi lookup error: %v", err)
				errorResponse(c, http.StatusInternalServerError, "Gagal memuat daftar pengawas")
				return
			}
			opScope = scope
			if !scope.IsBucket() {
				opts.Instansi = scope
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
			// AutoApprove: the exam's server-side auto-approve flag, surfaced so
			// the pengawas list can badge exams whose auto-approve is still on
			// (visible before the exam is reused for the next session).
			AutoApprove bool `json:"auto_approve"`
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
				ID:            e.ID,
				Name:          e.Name,
				TokenMode:     tokenMode,
				Status:        e.Status,
				StartTime:     startStr,
				EndTime:       endStr,
				CreatedAt:     formatISOUTC(e.CreatedAt),
				ExamStartedAt: e.ExamStartedAt,
				CreatorName:   usernameMap[e.CreatedBy],
				Tombstoned:    e.TombstonedAt != nil,
				TombstonedAt:  tombstonedAt,
				AutoApprove:   e.AutoApprove,
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
				`WITH latest AS (
					SELECT DISTINCT ON (exam_id, mac_address) exam_id, answers_json
					FROM submissions
					WHERE exam_id = ANY($1)
					ORDER BY exam_id, mac_address, created_at DESC, id DESC
				 )
				 SELECT exam_id,
					COUNT(*) as total,
					SUM(CASE WHEN answers_json IS NOT NULL AND answers_json != '' THEN 1 ELSE 0 END) as submitted
				 FROM latest GROUP BY exam_id`, ids)
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

		// Header-card stats must cover EVERY accessible exam, not the current
		// page slice: the cards are labelled tenant-wide ("Ujian Diawasi",
		// "Total Perangkat", "Terkumpul") and sit above the pagination. Summing
		// result.Exams made the numbers change from page to page — page 1
		// showed 0 students, page 2 showed 3, for the same exam set.
		//
		// The exam IDs come from the SAME scoped ListExams call as the list, so
		// the aggregate can never cover a wider set than the user may see. Only
		// the page number differs, so this walks the pages (one extra query per
		// 100 exams) instead of duplicating the tenant WHERE clause — duplicating
		// that predicate is exactly how the submissions list drifted to
		// name-only matching earlier.
		accessibleIDs := make([]int, 0, len(result.Exams))
		accessibleActive := 0
		{
			seen := 0
			totalAccessible := -1
			// Walk the UNFILTERED scoped list, not result.Total: result is the
			// search-filtered page response, so a zero-result search used to
			// skip the walk entirely and zero every header card. Use the first
			// unfiltered page's total as the loop bound instead.
			for p := 1; ; p++ {
				var pageRes models.ListExamsResult
				var pageErr error
				if isSuper {
					pageRes, pageErr = models.ListExams(ctx, pool, models.ListExamsOpts{Page: p, PerPage: statsWalkPerPage})
				} else if isOp {
					o := models.ListExamsOpts{Page: p, PerPage: statsWalkPerPage}
					if opScope.IsBucket() {
						uid := userID
						o.CreatedBy = &uid
					} else {
						o.Instansi = opScope
					}
					pageRes, pageErr = models.ListExams(ctx, pool, o)
				} else {
					pageRes, pageErr = models.ListPengawasExams(ctx, pool, models.ListPengawasExamsOpts{
						Page: p, PerPage: statsWalkPerPage, UserID: userID,
						HasPengawasRole: hasCurrentRole(c, models.RolePengawas),
					})
				}
				if pageErr != nil || len(pageRes.Exams) == 0 {
					break
				}
				if totalAccessible < 0 {
					totalAccessible = pageRes.Total
				}
				for _, e := range pageRes.Exams {
					accessibleIDs = append(accessibleIDs, e.ID)
					// "Sedang Berlangsung" means active AND started AND inside its
					// schedule, matching examIsRunning used by the detail page. A raw
					// status='active' count included not-yet-started and already-ended
					// exams, so the header disagreed with the per-exam liveness.
					if examIsRunning(&e) && !models.ExamScheduleEnded(&e, time.Now()) {
						accessibleActive++
					}
				}
				seen += len(pageRes.Exams)
				if totalAccessible >= 0 && seen >= totalAccessible {
					break
				}
			}
		}

		// One aggregate over all accessible exams (same answers_json predicate
		// as the per-page counts, so heartbeat placeholders stay excluded).
		var aggStudents, aggSubmitted int
		if len(accessibleIDs) > 0 {
			if err := pool.QueryRow(ctx,
				`WITH latest AS (
					SELECT DISTINCT ON (exam_id, mac_address) exam_id, answers_json
					FROM submissions
					WHERE exam_id = ANY($1)
					ORDER BY exam_id, mac_address, created_at DESC, id DESC
				 )
				 SELECT COUNT(DISTINCT mac_address),
				        COUNT(DISTINCT CASE WHEN answers_json IS NOT NULL AND answers_json != '' THEN mac_address END)
				 FROM latest`, accessibleIDs).Scan(&aggStudents, &aggSubmitted); err != nil {
				log.Printf("pengawas exam stats aggregate: %v", err)
				aggStudents, aggSubmitted = 0, 0
			}
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
				"total_exams":     len(accessibleIDs),
				"active_exams":    accessibleActive,
				"total_students":  aggStudents,
				"total_submitted": aggSubmitted,
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
		// Exam-scoped authorization FIRST. This endpoint performs a WRITE
		// (autoResetActiveTokenIfNeeded rotates active_token and appends token
		// history), so authorizing after it let any authenticated account of
		// any tenant trigger a rotation on somebody else's exam and only then
		// receive a 403 — an authz-ordering defect (CWE-863) on a mutating side
		// effect. Every sibling endpoint in this file gates immediately after
		// GetExamByID; this one now matches.
		if !models.UserCanAccessExam(ctx, pool, userID, isSuper, examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak: Anda tidak memiliki wewenang untuk mengawasi ujian ini")
			return
		}
		autoResetActiveTokenIfNeeded(ctx, pool, &exam)

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
			ID          int    `json:"id"`
			StudentName string `json:"student_name"`
			// StudentKey dihitung server dengan urutan yang sama dengan client
			// (helpers.StudentKey). Dihitung di JS akan berisiko karena logikanya
			// jadi terduplikasi dan bisa melenceng tanpa ada yang memeriksa.
			StudentKey   string                 `json:"student_key"`
			ExamNumber   string                 `json:"exam_number"`
			StudentClass string                 `json:"student_class"`
			IdentityData map[string]interface{} `json:"identity_data"`
			Submitted    bool                   `json:"submitted"`
			// RepeatBlocked menandai siswa ini sudah punya attempt selesai
			// (di baris/perangkat lain) walau baris terbaru perangkat ini
			// masih placeholder "mengerjakan". Halaman pengawas memakai ini
			// untuk tetap menampilkan tombol Izinkan Mengulang.
			RepeatBlocked bool     `json:"repeat_blocked"`
			Score         *float64 `json:"score"`
			StartTime     string   `json:"start_time"`
			CreatedAt     string   `json:"created_at"`
			// SubmittedAt is the submit moment ("Waktu Kumpul"), empty for
			// approval placeholders and legacy rows — the card falls back to
			// created_at in that case.
			SubmittedAt       string              `json:"submitted_at"`
			FirstAccessAt     string              `json:"first_access_at"`
			LastAccessAt      string              `json:"last_access_at"`
			MACAddress        string              `json:"mac_address"`
			AccessLogs        []accessLogEntry    `json:"access_logs"`
			IsOnline          bool                `json:"is_online"`
			AttemptCount      int                 `json:"attempt_count"`
			SubmissionHistory []models.Submission `json:"submission_history"`
		}

		// Attempts belong to the student currently occupying the device, not
		// to the device itself. A shared device whose previous occupant
		// submitted and whose new occupant is working must read "1 attempt",
		// not "2" — mixing both students also made the history modal show a
		// stranger's answers.
		attemptCounts := make(map[string]int)
		rows, countErr := pool.Query(ctx,
			"SELECT mac_address, TRIM(COALESCE(exam_number, '')), COUNT(*) FROM submissions WHERE exam_id = $1 GROUP BY 1, 2", examID)
		if countErr == nil {
			for rows.Next() {
				var mac, examNumber string
				var count int
				if err := rows.Scan(&mac, &examNumber, &count); err == nil {
					attemptCounts[submissionStudentKey(mac, examNumber)] = count
				}
			}
			rows.Close()
		}

		// Batch-fetch per-device access logs and submission history in two
		// queries instead of one per submission row (N+1). Both are grouped by
		// MAC address in Go; each group stays chronologically ordered because
		// the SQL orders by created_at ASC globally, which preserves per-group
		// order. Empty MACs are skipped (they have no logs/history), and
		// duplicates are deduped to keep the IN-list small.
		macs := make([]string, 0, len(result.Submissions))
		seenMAC := make(map[string]bool)
		for _, sub := range result.Submissions {
			if sub.MACAddress == "" || seenMAC[sub.MACAddress] {
				continue
			}
			seenMAC[sub.MACAddress] = true
			macs = append(macs, sub.MACAddress)
		}
		accessLogsByMAC := fetchStudentAccessLogsBatch(ctx, pool, examID, macs)
		historyByMAC := fetchSubmissionHistoryBatch(ctx, pool, examID, macs)

		// Batch-fetch Redis heartbeat presence (is_online) for every device in
		// one pipeline round-trip instead of one Exists call per row — the last
		// N+1 in this handler. A device whose key is missing/expired (or a
		// failed lookup) simply reads as offline, matching the old per-row
		// behaviour; with no Redis configured every device stays offline.
		onlineByMAC := make(map[string]bool)
		if rdb, exists := c.Get("redis"); exists && rdb != nil {
			if redisClient, ok := rdb.(*redis.Client); ok && redisClient != nil && len(macs) > 0 {
				pipe := redisClient.Pipeline()
				cmds := make([]*redis.IntCmd, len(macs))
				for i, mac := range macs {
					cmds[i] = pipe.Exists(ctx, fmt.Sprintf("heartbeat:%d:%s", examID, mac))
				}
				if _, err := pipe.Exec(ctx); err != nil {
					// Per-command errors are still readable below; a broken
					// pipeline just means everyone shows offline for this
					// refresh (same as the old per-key check failing).
					log.Printf("batch heartbeat exists error: %v", err)
				}
				for i, mac := range macs {
					if cmds[i].Err() == nil && cmds[i].Val() > 0 {
						onlineByMAC[mac] = true
					}
				}
			}
		}

		// Students with at least one completed attempt for this exam, keyed
		// the same way as the repeat gate. Best-effort: a lookup failure must
		// not take down the monitoring table; the UI then simply falls back
		// to the old submitted-only button.
		submittedKeys, keysErr := models.SubmittedStudentKeys(ctx, pool, examID)
		if keysErr != nil {
			log.Printf("pengawas submitted student keys error: %v", keysErr)
			submittedKeys = map[string]bool{}
		}

		subsData := make([]subItem, 0, len(result.Submissions))
		for _, sub := range result.Submissions {
			identityData := make(map[string]interface{})
			if sub.IdentityData != nil && *sub.IdentityData != "" {
				json.Unmarshal([]byte(*sub.IdentityData), &identityData)
			}

			accessLogs := filterAccessLogsByExamNumber(accessLogsByMAC[sub.MACAddress], sub.ExamNumber)

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

			isOnline := onlineByMAC[sub.MACAddress]

			attKey := submissionStudentKey(sub.MACAddress, sub.ExamNumber)
			if sub.MACAddress == "" {
				attKey = "#" + strconv.Itoa(sub.ID)
			}
			attCount := attemptCounts[attKey]
			if attCount == 0 {
				attCount = 1
			}

			// Submission history for this student on this MAC (batch-fetched
			// above, then scoped so a shared device never leaks the previous
			// occupant's attempts into the current student's modal).
			subHistory := filterSubmissionHistoryByExamNumber(historyByMAC[sub.MACAddress], sub.ExamNumber)

			studentKey := helpers.StudentKey(sub.ExamNumber, sub.StudentName, sub.StudentClass)
			subsData = append(subsData, subItem{
				ID:                sub.ID,
				StudentName:       sub.StudentName,
				StudentKey:        studentKey,
				ExamNumber:        sub.ExamNumber,
				StudentClass:      sub.StudentClass,
				IdentityData:      identityData,
				Submitted:         submitted,
				RepeatBlocked:     submittedKeys[studentKey],
				Score:             sub.Score,
				StartTime:         startTimeStr,
				CreatedAt:         sub.CreatedAt.Format("2006-01-02T15:04:05Z"),
				SubmittedAt:       formatOptionalISOUTC(sub.SubmittedAt),
				FirstAccessAt:     firstAccess,
				LastAccessAt:      lastAccess,
				MACAddress:        sub.MACAddress,
				AccessLogs:        accessLogs,
				IsOnline:          isOnline,
				AttemptCount:      attCount,
				SubmissionHistory: subHistory,
			})
		}

		scheduleEnded := models.ExamScheduleEnded(&exam, time.Now().UTC())
		examStarted := exam.ExamStartedAt != nil
		examLive := exam.IsActive() && examStarted && !scheduleEnded
		c.JSON(http.StatusOK, gin.H{
			"success":           true,
			"exam_name":         exam.Name,
			"exam_active_token": exam.ActiveToken,
			// Server-side token rotation clock. The client MUST derive its
			// countdown from this instead of stamping new Date() on receipt:
			// device clocks drift, and mixing the two domains made the countdown
			// either hang or fire on every tick once it went negative.
			"exam_token_last_reset_at": formatNullableISOUTC(exam.TokenLastResetAt),
			// Auto-approve ikut di payload yang sama supaya hot reload tidak
			// perlu endpoint tambahan: tanpa ini halaman supervisor menampilkan
			// "Terima Otomatis: ON" padahal supervisor lain sudah mematikannya.
			"auto_approve_enabled": exam.AutoApprove,
			// Liveness of the exam itself. Every mutating action on this page
			// (approve, reject, start/stop) is refused once the exam is
			// inactive or its window has passed, but the polled payload said
			// nothing about it — so the supervisor kept looking at an enabled
			// control panel and only learned the truth from a failed tap. The
			// client uses these to disable the actions and say why.
			"exam_active":         exam.IsActive(),
			"exam_started":        examStarted,
			"exam_live":           examLive,
			"exam_schedule_ended": scheduleEnded,
			"submissions":         subsData,
			"page":                result.Page,
			"per_page":            result.PerPage,
			"total":               result.Total,
			"total_pages":         result.TotalPages,
			"stats":               result.Stats,
		})
	}
}

// presenceEntry is the lightweight per-device liveness payload. The detail
// page polls this instead of the heavy submissions endpoint: the table does not
// need to be rebuilt just because a heartbeat appeared or expired.
type presenceEntry struct {
	Online   bool   `json:"online"`
	LastSeen string `json:"last_seen,omitempty"`
}

// PengawasExamPresence returns heartbeat presence for every device of one exam
// in a single Redis pipeline. It is intentionally read-only and cheap (one
// indexed DISTINCT query + one pipeline), so the detail page can refresh the
// "Daring/Luring" column without re-running the six-query submissions load.
func PengawasExamPresence() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()
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
			`SELECT DISTINCT mac_address FROM submissions
			 WHERE exam_id = $1 AND mac_address <> ''`, examID)
		if err != nil {
			log.Printf("pengawas presence query error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat presence perangkat")
			return
		}
		defer rows.Close()

		macs := make([]string, 0, 64)
		for rows.Next() {
			var mac string
			if err := rows.Scan(&mac); err != nil {
				continue
			}
			macs = append(macs, mac)
		}
		if err := rows.Err(); err != nil {
			log.Printf("pengawas presence rows error: %v", err)
		}

		presence := make(map[string]presenceEntry, len(macs))
		for _, mac := range macs {
			presence[mac] = presenceEntry{}
		}

		if rdbRaw, exists := c.Get("redis"); exists && rdbRaw != nil {
			if rdb, ok := rdbRaw.(*redis.Client); ok && rdb != nil && len(macs) > 0 {
				pipe := rdb.Pipeline()
				cmds := make([]*redis.StringCmd, len(macs))
				for i, mac := range macs {
					cmds[i] = pipe.Get(ctx, fmt.Sprintf("heartbeat:%d:%s", examID, mac))
				}
				if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
					log.Printf("pengawas presence pipeline error: %v", err)
				}
				for i, mac := range macs {
					if cmds[i].Err() != nil {
						continue
					}
					entry := presenceEntry{Online: true}
					var hb struct {
						LastSeen string `json:"last_seen"`
					}
					if json.Unmarshal([]byte(cmds[i].Val()), &hb) == nil {
						entry.LastSeen = hb.LastSeen
					}
					presence[mac] = entry
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"success":  true,
			"presence": presence,
		})
	}
}

// A device can be reassigned to another student between exam sessions without
// changing its MAC. Monitoring stays per-device, but attempts and timeline
// entries belong to the student currently occupying it; these limits also keep
// a long-running or duplicated device from turning one page load into tens of
// thousands of rows of JSON.
const (
	maxAccessLogsPerStudent        = 100
	maxSubmissionHistoryPerStudent = 50
)

func submissionStudentKey(mac, examNumber string) string {
	return mac + "\x1f" + strings.TrimSpace(examNumber)
}

func studentMatchesExamNumber(rowExamNumber, currentExamNumber string) bool {
	// Legacy rows can predate exam_number capture. Rows without a number carry
	// no identity to compare, so keep them rather than hiding the current
	// student's login/heartbeat/history entirely. Only rows that explicitly
	// belong to a DIFFERENT exam number are excluded.
	currentExamNumber = strings.TrimSpace(currentExamNumber)
	rowExamNumber = strings.TrimSpace(rowExamNumber)
	if currentExamNumber == "" || rowExamNumber == "" {
		return true
	}
	return rowExamNumber == currentExamNumber
}

func filterAccessLogsByExamNumber(logs []accessLogEntry, examNumber string) []accessLogEntry {
	out := make([]accessLogEntry, 0, len(logs))
	for _, entry := range logs {
		if studentMatchesExamNumber(entry.ExamNumber, examNumber) {
			out = append(out, entry)
		}
	}
	return out
}

func filterSubmissionHistoryByExamNumber(history []models.Submission, examNumber string) []models.Submission {
	out := make([]models.Submission, 0, len(history))
	for _, submission := range history {
		if studentMatchesExamNumber(submission.ExamNumber, examNumber) {
			out = append(out, submission)
		}
	}
	return out
}

// fetchStudentAccessLogsBatch loads the access logs for many devices in a
// single query and groups them by student_identifier (MAC). Each (MAC,
// exam_number) pair keeps at most maxAccessLogsPerStudent newest rows; the
// caller filters the group by the current student's exam_number. Rows are
// ordered by created_at ASC, so each group stays chronological.
func fetchStudentAccessLogsBatch(ctx context.Context, pool *pgxpool.Pool, examID int, macs []string) map[string][]accessLogEntry {
	out := make(map[string][]accessLogEntry)
	if len(macs) == 0 {
		return out
	}

	rows, err := pool.Query(ctx,
		`SELECT student_identifier, event, ip_address, device_info, created_at,
		 student_name, exam_number, student_class, identity_data
		 FROM (
			SELECT id, student_identifier, exam_number, event, ip_address, device_info,
			       created_at, student_name, student_class, identity_data,
			       ROW_NUMBER() OVER (
					PARTITION BY student_identifier, exam_number
					ORDER BY created_at DESC, id DESC
			       ) AS rn
			FROM student_access_logs
			WHERE exam_id = $1 AND student_identifier = ANY($2)
		 ) ranked
		 WHERE rn <= $3
		 ORDER BY created_at ASC, id ASC`, examID, macs, maxAccessLogsPerStudent)
	if err != nil {
		log.Printf("batch access logs query error: %v", err)
		return out
	}
	defer rows.Close()

	for rows.Next() {
		var entry accessLogEntry
		var mac string
		var createdAt time.Time
		var identityDataStr *string
		if err := rows.Scan(&mac, &entry.Event, &entry.IPAddress, &entry.DeviceInfo,
			&createdAt, &entry.StudentName, &entry.ExamNumber, &entry.StudentClass, &identityDataStr); err != nil {
			log.Printf("access logs scan error: %v", err)
			continue
		}
		entry.CreatedAt = createdAt.Format(time.RFC3339)
		if identityDataStr != nil {
			var idData map[string]interface{}
			if err := json.Unmarshal([]byte(*identityDataStr), &idData); err == nil {
				entry.IdentityData = idData
			}
		}
		out[mac] = append(out[mac], entry)
	}
	if err := rows.Err(); err != nil {
		log.Printf("access logs rows iteration error: %v", err)
	}

	return out
}

// fetchSubmissionHistoryBatch loads the submission history (every attempt —
// open rows and completed submissions) for many devices in a single query,
// grouped by mac_address in chronological order. Each (MAC, exam_number) pair
// keeps at most maxSubmissionHistoryPerStudent rows; the caller filters by the
// current student's exam_number so a shared device never shows a prior
// occupant's attempts.
func fetchSubmissionHistoryBatch(ctx context.Context, pool *pgxpool.Pool, examID int, macs []string) map[string][]models.Submission {
	out := make(map[string][]models.Submission)
	if len(macs) == 0 {
		return out
	}

	rows, err := pool.Query(ctx,
		`SELECT mac_address, exam_number, id, start_time, created_at, submitted_at, answers_json, score
		 FROM (
			SELECT id, mac_address, exam_number, start_time, created_at, submitted_at,
			       answers_json, score,
			       ROW_NUMBER() OVER (
					PARTITION BY mac_address, exam_number
					ORDER BY created_at DESC, id DESC
			       ) AS rn
			FROM submissions
			WHERE exam_id = $1 AND mac_address = ANY($2)
		 ) ranked
		 WHERE rn <= $3
		 ORDER BY created_at ASC, id ASC`, examID, macs, maxSubmissionHistoryPerStudent)
	if err != nil {
		log.Printf("batch submission history query error: %v", err)
		return out
	}
	defer rows.Close()

	for rows.Next() {
		var h models.Submission
		var mac string
		var created time.Time
		if err := rows.Scan(&mac, &h.ExamNumber, &h.ID, &h.StartTime, &created, &h.SubmittedAt, &h.AnswersJSON, &h.Score); err != nil {
			log.Printf("submission history scan error: %v", err)
			continue
		}
		h.MACAddress = mac
		h.CreatedAt = created
		out[mac] = append(out[mac], h)
	}
	if err := rows.Err(); err != nil {
		log.Printf("submission history rows iteration error: %v", err)
	}

	return out
}

func autoResetActiveTokenIfNeeded(ctx context.Context, pool *pgxpool.Pool, exam *models.Exam) {
	if err := examtoken.MaybeResetActiveToken(ctx, pool, exam, time.Now().UTC()); err != nil {
		log.Printf("auto reset active token error: %v", err)
	}
}

func GetPendingApprovals() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}
		pool := getPool(c)
		ctx := c.Request.Context()

		// Exam-scoped authorization (operators limited to their instansi).
		userID := getCurrentUserID(c)
		exam, examErr := models.GetExamByID(ctx, pool, examID)
		if examErr != nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}
		if !models.UserCanAccessExam(ctx, pool, userID, isSuperAdmin(c), examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak: Anda tidak memiliki wewenang untuk mengawasi ujian ini")
			return
		}
		// A queue only makes sense while the exam is live: RequestApproval no
		// longer adds rows to a stopped/ended exam, so a read here would only
		// serve dead rows. Same gates as the write endpoints for consistency.
		if !exam.IsActive() {
			errorResponse(c, http.StatusBadRequest, "Ujian tidak aktif")
			return
		}
		if exam.ExamStartedAt == nil {
			errorResponse(c, http.StatusBadRequest, "Ujian belum dimulai — antrean persetujuan belum tersedia")
			return
		}
		if models.ExamScheduleEnded(&exam, time.Now().UTC()) {
			errorResponse(c, http.StatusBadRequest, "Waktu ujian telah berakhir")
			return
		}

		// Pagination (anti-DoS): a spam-flooded pending queue must not dump
		// unbounded rows onto the page. Oldest first — those are the requests
		// the pengawas cares about first.
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		if page < 1 {
			page = 1
		}
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
		if limit < 1 {
			limit = 1
		} else if limit > 500 {
			limit = 500
		}
		offset := (page - 1) * limit

		var total int
		if err := pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM exam_approvals WHERE exam_id = $1 AND status = 'pending'`,
			examID).Scan(&total); err != nil {
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat persetujuan")
			return
		}

		rows, err := pool.Query(ctx,
			`SELECT mac_address, student_name, exam_number, student_class, identity_data, created_at, status
			 FROM exam_approvals
			 WHERE exam_id = $1 AND status = 'pending'
			 ORDER BY created_at ASC, id ASC
			 LIMIT $2 OFFSET $3`, examID, limit, offset)

		if err != nil {
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat persetujuan")
			return
		}
		defer rows.Close()

		// Mark repeat requests so the pengawas can tell them apart from a
		// first-time device. One query for the whole page (not N+1): the same
		// student-key rule the repeat gate uses.
		submittedKeys, subErr := models.SubmittedStudentKeys(ctx, pool, examID)
		if subErr != nil {
			log.Printf("pending approvals: submitted student keys: %v", subErr)
			submittedKeys = map[string]bool{}
		}

		type approvalItem struct {
			MACAddress   string                 `json:"mac_address"`
			StudentName  string                 `json:"student_name"`
			ExamNumber   string                 `json:"exam_number"`
			StudentClass string                 `json:"student_class"`
			IdentityData map[string]interface{} `json:"identity_data"`
			CreatedAt    string                 `json:"created_at"`
			Status       string                 `json:"status"`
			IsRepeat     bool                   `json:"is_repeat"`
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
				i.IsRepeat = submittedKeys[helpers.StudentKey(i.ExamNumber, i.StudentName, i.StudentClass)]
				items = append(items, i)
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    items,
			"total":   total,
			"page":    page,
			"limit":   limit,
		})
	}
}

func SetApprovalStatus() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}
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
		exam, examErr := models.GetExamByID(ctx, pool, examID)
		if examErr != nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}
		if !models.UserCanAccessExam(ctx, pool, userID, isSuperAdmin(c), examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak: Anda tidak memiliki wewenang untuk mengawasi ujian ini")
			return
		}
		// Decisions only make sense while the exam is running: approving or
		// revoking a device for a stopped exam would write an audit row (and
		// possibly a monitoring row) that nobody can act on.
		if !exam.IsActive() {
			errorResponse(c, http.StatusBadRequest, "Ujian tidak aktif — keputusan perangkat tidak dapat diubah")
			return
		}
		// Approvals are only created after the exam starts (RequestApproval is
		// reachable only once a student can join). A legacy/queued row on an
		// active-but-not-started exam must not be decidable: the client disables
		// these controls for the same reason, so the server is the backstop.
		if exam.ExamStartedAt == nil {
			errorResponse(c, http.StatusBadRequest, "Ujian belum dimulai — keputusan perangkat belum dapat dibuat")
			return
		}
		// Same for an exam whose schedule has ended (end_time + grace) — the
		// public gates already refuse new joins/submits, so a decision here
		// would only write a trail nobody can act on. Same cutoff as the
		// public join/submit gates (models.ExamScheduleEnded).
		if models.ExamScheduleEnded(&exam, time.Now().UTC()) {
			errorResponse(c, http.StatusBadRequest, "Waktu ujian telah berakhir — keputusan perangkat tidak dapat diubah")
			return
		}

		// Entry bookkeeping must happen BEFORE the status flips to approved.
		// The student's 5s poll treats an open submission row created after
		// the latest completed attempt as proof that this repeat was approved;
		// creating it after the UPDATE would leave a window where the poll
		// sees approved-without-open-row and demotes the row back to pending.
		//
		// For a first-time approval the same call creates the monitoring row
		// (see models.EnsureFreshSubmissionOnApproval). It is a no-op when the
		// device has no approval row, so the unknown-device contract below is
		// unchanged.
		if req.Status == "approved" {
			if err := models.EnsureFreshSubmissionOnApproval(ctx, pool, examID, macAddress); err != nil {
				log.Printf("failed to insert submission on approval: %v", err)
				errorResponse(c, http.StatusInternalServerError, "Gagal menyiapkan sesi peserta")
				return
			}
		}

		// Update atomically and snapshot the student identity in one statement
		// (RETURNING): no separate read, and the "did anything match" signal
		// comes from the same round trip, so a row deleted in between cannot
		// produce a stale detail or a forged trail.
		var studentName string
		err = pool.QueryRow(ctx,
			`UPDATE exam_approvals SET status = $1, updated_at = CURRENT_TIMESTAMP
			 WHERE exam_id = $2 AND mac_address = $3
			 RETURNING student_name`,
			req.Status, examID, macAddress).Scan(&studentName)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// The device matched nothing — no decision was made, so nothing
				// is audited. Keep the historical 200 contract for unknown
				// devices (the queue UI tolerates a no-op); only the trail stays
				// silent so it cannot be forged with rows the pengawas never made.
				c.JSON(http.StatusOK, gin.H{"success": true})
				return
			}
			errorResponse(c, http.StatusInternalServerError, "Gagal mengubah status persetujuan")
			return
		}

		// Append-only audit trail for per-device decisions (who allowed or
		// rejected which device, and when). Note PostgreSQL counts matched rows
		// in an UPDATE, so re-deciding an already-approved device still leaves
		// a row — an explicit decision is an action, same as the auto-approve
		// toggle. Best-effort: a failed audit row never rolls back the decision.
		action := models.ActionApprovalRejected
		detail := fmt.Sprintf("Perangkat ditolak: %s", macAddress)
		if req.Status == "approved" {
			action = models.ActionApprovalApproved
			detail = fmt.Sprintf("Perangkat diizinkan: %s", macAddress)
		}
		if studentName != "" {
			detail = fmt.Sprintf("%s (%s)", detail, studentName)
		}
		if err := models.CreateAdminAuditLog(ctx, pool, userID, getCurrentUsername(c), action, examID, detail); err != nil {
			log.Printf("audit approval decision: %v", err)
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
		})
	}
}

// ---------------------------------------------------------------------------
// 7. Auto-approve flag (server-side)
// ---------------------------------------------------------------------------

// GetAutoApprove returns the exam's server-side auto-approve flag.
// GET /admin/api/pengawas/exams/:exam_id/auto-approve
func GetAutoApprove() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		// Exam-scoped authorization, same as the approvals endpoints.
		userID := getCurrentUserID(c)
		if _, err := models.GetExamByID(ctx, pool, examID); err != nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}
		if !models.UserCanAccessExam(ctx, pool, userID, isSuperAdmin(c), examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak: Anda tidak memiliki wewenang untuk mengawasi ujian ini")
			return
		}

		var enabled bool
		if err := pool.QueryRow(ctx, `SELECT auto_approve FROM exams WHERE id = $1`, examID).Scan(&enabled); err != nil {
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat pengaturan auto-approve")
			return
		}

		resp := gin.H{
			"success": true,
			"enabled": enabled,
		}

		// Accountability hint for the monitoring page: who last toggled
		// auto-approve (and when). Best effort — an absent trail just omits
		// the fields, the toggle still works.
		if last, err := models.LatestExamAuditLogByActions(ctx, pool, examID, []string{
			models.ActionAutoApproveEnable, models.ActionAutoApproveDisable,
		}); err == nil && last != nil {
			resp["last_changed_by"] = last.Username
			resp["last_changed_at"] = last.CreatedAt.Format(time.RFC3339)
			resp["last_action"] = last.Action
		}

		c.JSON(http.StatusOK, resp)
	}
}

// SetAutoApprove toggles the exam's server-side auto-approve flag. The flag is
// stored on the exam row so auto-approve keeps running even when no pengawas
// monitoring page is open.
// POST /admin/api/pengawas/exams/:exam_id/auto-approve {enabled: bool}
func SetAutoApprove() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			errorResponse(c, http.StatusBadRequest, "Payload tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		// Exam-scoped authorization, same as the approvals endpoints.
		userID := getCurrentUserID(c)
		exam, examErr := models.GetExamByID(ctx, pool, examID)
		if examErr != nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}
		if !models.UserCanAccessExam(ctx, pool, userID, isSuperAdmin(c), examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak: Anda tidak memiliki wewenang untuk mengawasi ujian ini")
			return
		}
		// Toggling auto-approve only matters while the exam is live: the flag
		// does nothing on a stopped/ended exam (RequestApproval refuses to
		// apply it there), so reject the toggle instead of writing a flag and
		// an audit row nobody can act on. Same gates as the other approval
		// endpoints.
		if !exam.IsActive() {
			errorResponse(c, http.StatusBadRequest, "Ujian tidak aktif")
			return
		}
		if models.ExamScheduleEnded(&exam, time.Now().UTC()) {
			errorResponse(c, http.StatusBadRequest, "Waktu ujian telah berakhir")
			return
		}

		if err := models.SetExamAutoApprove(ctx, pool, examID, req.Enabled); err != nil {
			log.Printf("set auto-approve error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan pengaturan auto-approve")
			return
		}

		// Append-only audit trail: who toggled server-side auto-approve, and
		// when. Logged only after a successful write and only for authorized
		// callers (the auth gate above already rejected 403s). Best effort — a
		// failed audit row must not roll back the toggle itself.
		action := models.ActionAutoApproveDisable
		detail := "Auto-approve dimatikan"
		if req.Enabled {
			action = models.ActionAutoApproveEnable
			detail = "Auto-approve diaktifkan"
		}
		if err := models.CreateAdminAuditLog(ctx, pool, userID, getCurrentUsername(c), action, examID, detail); err != nil {
			log.Printf("audit auto-approve toggle: %v", err)
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"enabled": req.Enabled,
		})
	}
}

// GetExamAuditLogs returns the full append-only admin audit trail for an exam
// — every auto-approve toggle (who, when, action, detail), newest first — so
// the monitoring page can render the complete history behind the single
// "last changed" hint.
// GET /admin/api/pengawas/exams/:exam_id/audit-logs?limit=100
func GetExamAuditLogs() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		// Exam-scoped authorization, same as the approvals/auto-approve
		// endpoints: an unassigned pengawas must not read the trail either.
		userID := getCurrentUserID(c)
		if _, err := models.GetExamByID(ctx, pool, examID); err != nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}
		if !models.UserCanAccessExam(ctx, pool, userID, isSuperAdmin(c), examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak: Anda tidak memiliki wewenang untuk mengawasi ujian ini")
			return
		}

		// Bounded payload (anti-DoS): the panel renders a capped window.
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
		if limit < 1 {
			limit = 1
		} else if limit > 500 {
			limit = 500
		}

		logs, err := models.ListExamAuditLogs(ctx, pool, examID, limit)
		if err != nil {
			log.Printf("exam audit logs error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat riwayat audit")
			return
		}

		type auditItem struct {
			ID        int    `json:"id"`
			Username  string `json:"username"`
			Action    string `json:"action"`
			Detail    string `json:"detail"`
			CreatedAt string `json:"created_at"`
		}
		items := make([]auditItem, 0, len(logs))
		for _, l := range logs {
			items = append(items, auditItem{
				ID:        l.ID,
				Username:  l.Username,
				Action:    l.Action,
				Detail:    l.Detail,
				CreatedAt: l.CreatedAt.Format(time.RFC3339),
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"logs":    items,
		})
	}
}
