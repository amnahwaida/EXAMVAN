package admin

import (
	"log"
	"math"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/examvan/webui/internal/models"
)

// Dashboard renders the admin dashboard page with stats and exam list.
func Dashboard() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		isSuper := isSuperAdmin(c)
		isOp := isOperator(c)
		roles := getCurrentUserRoles(c)
		ctx := c.Request.Context()

		// Guru-only access gate (mirrors Python: redirect pengawas-only to /admin/pengawas)
		isGuru := false
		for _, r := range roles {
			if r == models.RoleGuru {
				isGuru = true
				break
			}
		}
		if !isSuper && !isOp && !isGuru {
			c.Redirect(http.StatusFound, "/admin/pengawas")
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
		search := c.Query("search")
		statusFilter := c.Query("status")

		opts := models.ListExamsOpts{
			Page:    page,
			PerPage: perPage,
			Search:  search,
			Status:  statusFilter,
		}

		if !isSuper && !isOp {
			uid := userID
			opts.UserID = &uid
			opts.IsPengawas = hasCurrentRole(c, models.RolePengawas)
			opts.IsGuru = hasCurrentRole(c, models.RoleGuru)
		}

		// Get the user's instansi if operator
		if isOp {
			var instansi string
			err := pool.QueryRow(ctx, `SELECT instansi FROM admin_users WHERE id = $1`, userID).Scan(&instansi)
			if err == nil {
				opts.Instansi = instansi
			}
		}

		result, err := models.ListExams(ctx, pool, opts)
		if err != nil {
			log.Printf("ERROR ListExams: %v", err)
			// Render dashboard with empty data instead of JSON error
			result = models.ListExamsResult{}
		}

		// Fetch pengawas for each exam (batched)
		examPengawasMap := make(map[int][]string)
		if len(result.Exams) > 0 {
			ids := make([]int, len(result.Exams))
			for i, e := range result.Exams {
				ids[i] = e.ID
			}
			pRows, pErr := pool.Query(ctx,
				`SELECT ep.exam_id, u.username FROM exam_pengawas ep
				 JOIN admin_users u ON ep.user_id = u.id
				 WHERE ep.exam_id = ANY($1) ORDER BY u.username`, ids)
			if pErr == nil {
				for pRows.Next() {
					var eid int
					var uname string
					pRows.Scan(&eid, &uname)
					examPengawasMap[eid] = append(examPengawasMap[eid], uname)
				}
				pRows.Close()
				if err := pRows.Err(); err != nil {
					log.Printf("rows iteration error: %v", err)
				}
			}
		}

		// Stats: aggregate queries (no full table scan)
		var statsTotal int
		var statsActive int
		var storageBytes int64

		var statsWheres []string
		var statsArgs []interface{}
		statsArgIdx := 1

		if isSuper {
			// all — no filter
		} else if isOp {
			var instansi string
			pool.QueryRow(ctx, `SELECT instansi FROM admin_users WHERE id = $1`, userID).Scan(&instansi)
			if instansi != "" {
				statsWheres = append(statsWheres, fmt.Sprintf(`e.created_by IN (SELECT id FROM admin_users WHERE instansi = $%d)`, statsArgIdx))
				statsArgs = append(statsArgs, instansi)
				statsArgIdx++
			}
		} else {
			uid := userID
			isPengawas := hasCurrentRole(c, models.RolePengawas)
			isGuru := hasCurrentRole(c, models.RoleGuru)
			if isPengawas && isGuru {
				statsWheres = append(statsWheres, fmt.Sprintf(`(e.created_by = $%d OR e.delegated_to = $%d OR e.id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = $%d))`, statsArgIdx, statsArgIdx, statsArgIdx))
				statsArgs = append(statsArgs, uid)
				statsArgIdx++
			} else if isPengawas {
				statsWheres = append(statsWheres, fmt.Sprintf(`e.id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = $%d)`, statsArgIdx))
				statsArgs = append(statsArgs, uid)
				statsArgIdx++
			} else {
				statsWheres = append(statsWheres, fmt.Sprintf(`(e.created_by = $%d OR e.delegated_to = $%d)`, statsArgIdx, statsArgIdx))
				statsArgs = append(statsArgs, uid)
				statsArgIdx++
			}
		}

		// Add search filter to stats (same as ListExams).
		if search != "" {
			searchPattern := "%" + search + "%"
			statsWheres = append(statsWheres,
				fmt.Sprintf("(e.name ILIKE $%d OR e.token ILIKE $%d OR u.username ILIKE $%d)", statsArgIdx, statsArgIdx+1, statsArgIdx+2))
			statsArgs = append(statsArgs, searchPattern, searchPattern, searchPattern)
			statsArgIdx += 3
		}

		// Aggregate queries: 2 fast queries instead of full ListExams
		fromClause := " FROM exams e LEFT JOIN admin_users u ON e.created_by = u.id"
		whereClause := ""
		if len(statsWheres) > 0 {
			whereClause = " WHERE " + strings.Join(statsWheres, " AND ")
		}
		pool.QueryRow(ctx, `SELECT COUNT(*), COALESCE(SUM(e.size_bytes), 0)`+fromClause+whereClause, statsArgs...).Scan(&statsTotal, &storageBytes)
		activeWhereClause := whereClause
		if activeWhereClause == "" {
			activeWhereClause = " WHERE e.status = 'active'"
		} else {
			activeWhereClause += " AND e.status = 'active'"
		}
		pool.QueryRow(ctx, `SELECT COUNT(*)`+fromClause+activeWhereClause, statsArgs...).Scan(&statsActive)

		// Per-user limits
		userMaxPDF := int64(1048576)
		userMaxExams := "3"
		var remainingStorage string
		var accountExpires *string

		if !isSuper && !isOp {
			user, err := models.GetUserByID(ctx, pool, userID)
			if err == nil {
				if user.MaxPDFSize > 0 {
					userMaxPDF = int64(user.MaxPDFSize)
				} else {
					userMaxPDF = 100 * 1024 * 1024 // 100MB limit globally if 0
				}
				
				if user.MaxExams > 0 {
					userMaxExams = strconv.Itoa(user.MaxExams)
				} else {
					userMaxExams = "Tidak Terbatas"
				}

				if user.MaxStorageSize > 0 {
					var currentStorageBytes int64
					pool.QueryRow(ctx, `SELECT COALESCE(SUM(size_bytes), 0) FROM exams WHERE created_by = $1`, userID).Scan(&currentStorageBytes)
					rem := user.MaxStorageSize - currentStorageBytes
					if rem < 0 {
						rem = 0
					}
					remMB := float64(rem) / (1024 * 1024)
					if remMB >= 1024 {
						remainingStorage = fmt.Sprintf("%.2f GB", remMB/1024)
					} else {
						remainingStorage = fmt.Sprintf("%.2f MB", remMB)
					}
				} else {
					remainingStorage = "Tidak Terbatas"
				}

				if user.ExpiresAt != nil {
					s := user.ExpiresAt.Format("2006-01-02 15:04:05")
					accountExpires = &s
				}
			}
		} else {
			userMaxPDF = 100 * 1024 * 1024
			userMaxExams = "Tidak Terbatas"
			remainingStorage = "Tidak Terbatas"
		}

		totalPages := int(math.Max(1, float64((result.Total+perPage-1)/perPage)))
		storageMB := roundTo(float64(storageBytes)/(1024*1024), 2)

		// Build exam list with pengawas names
		type examItem struct {
			ID                 int        `json:"id"`
			Name               string     `json:"name"`
			Status             string     `json:"status"`
			Token              string     `json:"token"`
			ActiveToken        string     `json:"active_token"`
			SizeMB             float64    `json:"size_mb"`
			SubCount           int        `json:"sub_count"`
			CreatorName        string     `json:"creator_name"`
			CreatedBy          int        `json:"created_by"`
			DelegatedName      string     `json:"delegated_name"`
			DelegatedTo        *int       `json:"delegated_to,omitempty"`
			CreatedAt          string     `json:"created_at"`
			PublicResults      int        `json:"public_results"`
			ShowAnswers        int        `json:"show_answers"`
			Pengawas           []string   `json:"pengawas"`
			TokenMode          string     `json:"token_mode"`
			TokenResetInterval *int       `json:"token_reset_interval"`
			ExamStartedAt      *time.Time `json:"exam_started_at"`
		}

		// Batch lookup usernames for creators and delegated users
		userIDs := make(map[int]bool)
		for _, e := range result.Exams {
			userIDs[e.CreatedBy] = true
			if e.DelegatedTo != nil {
				userIDs[*e.DelegatedTo] = true
			}
		}
		usernameMap := make(map[int]string)
		if len(userIDs) > 0 {
			ids := make([]int, 0, len(userIDs))
			for id := range userIDs {
				ids = append(ids, id)
			}
			rows, err := pool.Query(ctx, `SELECT id, username FROM admin_users WHERE id = ANY($1)`, ids)
			if err == nil {
				for rows.Next() {
					var id int
					var uname string
					if err := rows.Scan(&id, &uname); err == nil {
						usernameMap[id] = uname
					}
				}
				rows.Close()
				if err := rows.Err(); err != nil {
					log.Printf("rows iteration error: %v", err)
				}
			}
		}

		// Batch fetch submission counts
	subCountMap := make(map[int]int)
	if len(result.Exams) > 0 {
		examIDs := make([]int, len(result.Exams))
		for i, e := range result.Exams {
			examIDs[i] = e.ID
		}
		rows, err := pool.Query(ctx,
			`SELECT exam_id, COUNT(*) FROM submissions WHERE exam_id = ANY($1) GROUP BY exam_id`, examIDs)
		if err == nil {
			for rows.Next() {
				var eid, cnt int
				rows.Scan(&eid, &cnt)
				subCountMap[eid] = cnt
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				log.Printf("rows iteration error: %v", err)
			}

		}
	}

		examItems := make([]examItem, 0, len(result.Exams))
		for _, e := range result.Exams {
			delegatedName := ""
			if e.DelegatedTo != nil {
				delegatedName = usernameMap[*e.DelegatedTo]
			}
			tokenMode := "dynamic"
			if e.TokenMode != nil && *e.TokenMode != "" {
				tokenMode = *e.TokenMode
			}
			examItems = append(examItems, examItem{
				ID:                 e.ID,
				Name:               e.Name,
				Status:             e.Status,
				Token:              e.Token,
				ActiveToken:        e.ActiveToken,
				SizeMB:             roundTo(float64(e.SizeBytes)/(1024*1024), 2),
				SubCount:           subCountMap[e.ID],
				CreatorName:        usernameMap[e.CreatedBy],
				CreatedBy:          e.CreatedBy,
				DelegatedName:      delegatedName,
				DelegatedTo:        e.DelegatedTo,
				CreatedAt:          formatISOUTC(e.CreatedAt),
				PublicResults:      e.PublicResults,
				ShowAnswers:        e.ShowAnswers,
				Pengawas:           examPengawasMap[e.ID],
				TokenMode:          tokenMode,
				TokenResetInterval: e.TokenResetInterval,
				ExamStartedAt:      e.ExamStartedAt,
			})
		}

		activePct := float64(0)
		if statsTotal > 0 {
			activePct = roundTo(float64(statsActive)/float64(statsTotal)*100, 1)
		}

		// remainingStorage is already calculated above

		scheme := "http"
		if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		serverURL := fmt.Sprintf("%s://%s", scheme, c.Request.Host)

		renderAdminPage(c, "admin/dashboard.html", gin.H{
			"exams":             examItems,
			"exam_pengawas_map": examPengawasMap,
			"stats": gin.H{
				"total":      statsTotal,
				"active":     statsActive,
				"inactive":   statsTotal - statsActive,
				"storage_mb": storageMB,
				"total_all":  statsTotal,
				"active_pct": activePct,
			},
			"max_size_mb":       roundTo(float64(userMaxPDF)/(1024*1024), 1),
			"max_exams":         userMaxExams,
			"account_expires":   accountExpires,
			"remaining_storage": remainingStorage,
			"server_url":        serverURL,
			"active_page":       "dashboard",
			"page":              page,
			"per_page":          perPage,
			"total_pages":       totalPages,
			"total_exams":       result.Total,
			"search":            search,
			"search_active":     search != "",
			"status_filter":     statusFilter,
			"query_base":        buildFilterQuery(search, statusFilter),
		})
	}
}

// Stats returns JSON dashboard statistics scoped to the user's visible exams.
func Stats() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		isSuper := isSuperAdmin(c)
		isOp := isOperator(c)
		ctx := c.Request.Context()

		var statsWheres []string
		var statsArgs []interface{}
		statsArgIdx := 1

		if isSuper {
			// all — no filter
		} else if isOp {
			var instansi string
			pool.QueryRow(ctx, `SELECT instansi FROM admin_users WHERE id = $1`, userID).Scan(&instansi)
			if instansi != "" {
				statsWheres = append(statsWheres, fmt.Sprintf(`e.created_by IN (SELECT id FROM admin_users WHERE instansi = $%d)`, statsArgIdx))
				statsArgs = append(statsArgs, instansi)
				statsArgIdx++
			}
		} else {
			uid := userID
			isPengawas := hasCurrentRole(c, models.RolePengawas)
			isGuru := hasCurrentRole(c, models.RoleGuru)
			if isPengawas && isGuru {
				statsWheres = append(statsWheres, fmt.Sprintf(`(e.created_by = $%d OR e.delegated_to = $%d OR e.id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = $%d))`, statsArgIdx, statsArgIdx, statsArgIdx))
				statsArgs = append(statsArgs, uid)
				statsArgIdx++
			} else if isPengawas {
				statsWheres = append(statsWheres, fmt.Sprintf(`e.id IN (SELECT exam_id FROM exam_pengawas WHERE user_id = $%d)`, statsArgIdx))
				statsArgs = append(statsArgs, uid)
				statsArgIdx++
			} else {
				statsWheres = append(statsWheres, fmt.Sprintf(`(e.created_by = $%d OR e.delegated_to = $%d)`, statsArgIdx, statsArgIdx))
				statsArgs = append(statsArgs, uid)
				statsArgIdx++
			}
		}

		fromClause := " FROM exams e LEFT JOIN admin_users u ON e.created_by = u.id"
		whereClause := ""
		if len(statsWheres) > 0 {
			whereClause = " WHERE " + strings.Join(statsWheres, " AND ")
		}

		var total int
		var active int
		var storageBytes int64
		pool.QueryRow(ctx, `SELECT COUNT(*), COALESCE(SUM(e.size_bytes), 0)`+fromClause+whereClause, statsArgs...).Scan(&total, &storageBytes)

		activeWhere := whereClause
		if activeWhere == "" {
			activeWhere = " WHERE e.status = 'active'"
		} else {
			activeWhere += " AND e.status = 'active'"
		}
		pool.QueryRow(ctx, `SELECT COUNT(*)`+fromClause+activeWhere, statsArgs...).Scan(&active)

		successData(c, gin.H{
			"total":      total,
			"active":     active,
			"inactive":   total - active,
			"storage_mb": roundTo(float64(storageBytes)/(1024*1024), 2),
		})
	}
}

// buildFilterQuery builds the query string fragment for search and status
// filters, to be appended to pagination links. Returns leading "&" or empty.
func buildFilterQuery(search, status string) string {
	var parts []string
	if search != "" {
		parts = append(parts, "search="+url.QueryEscape(search))
	}
	if status != "" {
		parts = append(parts, "status="+url.QueryEscape(status))
	}
	if len(parts) == 0 {
		return ""
	}
	return "&" + strings.Join(parts, "&")
}
