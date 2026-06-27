package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Constants & helpers
// ---------------------------------------------------------------------------

const maxFileSize = 100 * 1024 * 1024 // 100 MB global limit

var tokenRegex = regexp.MustCompile(`^[A-Z0-9]{8}$`)

// generateToken creates an 8-character uppercase alphanumeric token (A-Z, 0-9).
func generateToken() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback: time-based token.
		return fmt.Sprintf("%08X", time.Now().UnixNano()%99999999)
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

// validatePDF checks that the uploaded data is a valid PDF and respects size
// limits. Returns (isValid, errorMessage).
func validatePDF(data []byte, filename string, contentType string, maxSize int64) (bool, string) {
	if len(data) > int(maxSize) {
		maxMB := maxSize / (1024 * 1024)
		return false, fmt.Sprintf("Ukuran file melebihi batas %dMB", maxMB)
	}
	if !bytes.HasPrefix(data, []byte("%PDF")) {
		return false, "File tidak valid (bukan PDF)"
	}
	return true, ""
}

// safeStoragePath resolves a path against the storage directory and prevents
// directory traversal.
func safeStoragePath(baseDir, relPath string) (string, error) {
	cleanBase := filepath.Clean(baseDir)
	full := filepath.Join(cleanBase, filepath.Clean(relPath))
	if !strings.HasPrefix(full, cleanBase) {
		return "", fmt.Errorf("path traversal detected: %s", relPath)
	}
	return full, nil
}

// ---------------------------------------------------------------------------
// 1. GET /admin/dashboard — rendered by Dashboard() already, but the user spec
//    lists a separate "GET /" handler for the dashboard; we provide it as
//    an alias so routing can point both paths here.
// ---------------------------------------------------------------------------

func DashboardRedirect() gin.HandlerFunc {
	return Dashboard()
}

// ---------------------------------------------------------------------------
// 2. POST /admin/api/upload — Create exam with PDF
// ---------------------------------------------------------------------------

func UploadExam() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		isSuper := isSuperAdmin(c)
		isOp := isOperator(c)
		ctx := c.Request.Context()

		name := strings.TrimSpace(c.PostForm("name"))
		if name == "" {
			errorResponse(c, http.StatusBadRequest, "Nama ujian wajib diisi")
			return
		}

		file, header, err := c.Request.FormFile("pdf_file")
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "File PDF wajib dipilih")
			return
		}
		defer file.Close()

		fileData, err := io.ReadAll(file)
		if err != nil {
			errorResponse(c, http.StatusInternalServerError, "Gagal membaca file")
			return
		}

		isValid, msg := validatePDF(fileData, header.Filename, header.Header.Get("Content-Type"), maxFileSize)
		if !isValid {
			errorResponse(c, http.StatusBadRequest, msg)
			return
		}

		// Per-user limits (unless super admin / operator)
		if !isSuper && !isOp {
			user, err := models.GetUserByID(ctx, pool, userID)
			if err != nil {
				errorResponse(c, http.StatusInternalServerError, "Gagal memuat data user")
				return
			}
			if user.MaxPDFSize > 0 && len(fileData) > user.MaxPDFSize {
				limitMB := roundTo(float64(user.MaxPDFSize)/(1024*1024), 2)
				errMsg := fmt.Sprintf("Ukuran file melebihi batas akun Anda (%.2fMB). Silakan hubungi Super Admin.", limitMB)
				errorResponse(c, http.StatusForbidden, errMsg)
				return
			}
			// Count existing exams (only enforce when MaxExams > 0 — 0 means unlimited)
			if user.MaxExams > 0 {
				opts := models.ListExamsOpts{CreatedBy: &userID}
				countResult, err := models.ListExams(ctx, pool, opts)
				if err == nil && countResult.Total >= user.MaxExams {
					errorResponse(c, http.StatusForbidden,
						fmt.Sprintf("Batas pembuatan ujian tercapai. Batas akun Anda adalah %d ujian.", user.MaxExams))
					return
				}
			}
		}

		customToken := strings.ToUpper(strings.TrimSpace(c.PostForm("custom_token")))
		token := ""

		if customToken != "" {
			if !tokenRegex.MatchString(customToken) {
				errorResponse(c, http.StatusBadRequest, "Token kustom harus terdiri dari 8 karakter alfanumerik")
				return
			}
			// Check uniqueness
			_, err := models.GetExamByToken(ctx, pool, customToken)
			if err == nil {
				errorResponse(c, http.StatusBadRequest, "Token kustom sudah digunakan oleh ujian lain")
				return
			}
			token = customToken
		} else {
			token = generateToken()
		}

		// Save file
		storageDir := getStoragePath(c)
		timestamp := time.Now().UTC().Format("20060102_150405")
		safeName := filepath.Base(header.Filename)
		if safeName == "." || safeName == "/" {
			safeName = "exam.pdf"
		}
		filename := fmt.Sprintf("%s_%s", timestamp, safeName)
		destPath := filepath.Join(storageDir, filename)

		if err := os.WriteFile(destPath, fileData, 0644); err != nil {
			log.Printf("upload save file error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan file")
			return
		}

		exam := &models.Exam{
			Name:          name,
			FilePath:      filename,
			SizeBytes:     int64(len(fileData)),
			Token:         token,
			Status:        "active",
			SecurityLevel: "medium",
			PublicResults: 1,
			CreatedBy:     userID,
		}

		created, err := models.CreateExam(ctx, pool, exam)
		if err != nil {
			log.Printf("upload create exam error: %v", err)
			// Clean up saved file
			os.Remove(destPath)
			errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan ujian")
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": fmt.Sprintf(`Ujian "%s" berhasil diupload dengan token: %s`, name, token),
			"token":   token,
			"id":      created.ID,
		})
	}
}

// ---------------------------------------------------------------------------
// 3. POST /admin/api/exams/:exam_id/toggle — Toggle active/inactive
// ---------------------------------------------------------------------------

func ToggleExam() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		if !checkExamOwnership(c, pool, examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak: Anda tidak memiliki akses ke ujian ini")
			return
		}

		newStatus, err := models.ToggleExamStatus(ctx, pool, examID)
		if err != nil {
			log.Printf("toggle exam error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal mengubah status ujian")
			return
		}

		successMessage(c, fmt.Sprintf("Status ujian diubah ke %s", newStatus))
	}
}

// ---------------------------------------------------------------------------
// 4. DELETE /admin/api/exams/:exam_id — Delete exam + PDF
// ---------------------------------------------------------------------------

func DeleteExam() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		if !checkExamOwnership(c, pool, examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak: Anda tidak memiliki akses ke ujian ini")
			return
		}

		exam, err := models.DeleteExam(ctx, pool, examID)
		if err != nil {
			log.Printf("delete exam error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menghapus ujian")
			return
		}

		// Clean up PDF
		storageDir := getStoragePath(c)
		if fp, err := safeStoragePath(storageDir, exam.FilePath); err == nil {
			if err := os.Remove(fp); err != nil && !os.IsNotExist(err) {
				log.Printf("delete exam file cleanup error: %v", err)
			}
		}

		successMessage(c, "Ujian berhasil dihapus")
	}
}

// ---------------------------------------------------------------------------
// 5. POST /admin/api/exams/:exam_id/edit — Edit name + optional new PDF
// ---------------------------------------------------------------------------

func EditExam() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		if !checkExamOwnership(c, pool, examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak: Anda tidak memiliki akses ke ujian ini")
			return
		}

		exam, err := models.GetExamByID(ctx, pool, examID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}

		name := strings.TrimSpace(c.PostForm("name"))
		if name == "" {
			errorResponse(c, http.StatusBadRequest, "Nama ujian wajib diisi")
			return
		}

		file, header, fileErr := c.Request.FormFile("pdf_file")

		if fileErr == nil && header != nil {
			defer file.Close()
			fileData, readErr := io.ReadAll(file)
			if readErr != nil {
				errorResponse(c, http.StatusInternalServerError, "Gagal membaca file")
				return
			}

			isValid, msg := validatePDF(fileData, header.Filename, header.Header.Get("Content-Type"), maxFileSize)
			if !isValid {
				errorResponse(c, http.StatusBadRequest, msg)
				return
			}

			// Delete old file
			storageDir := getStoragePath(c)
			if oldPath, err := safeStoragePath(storageDir, exam.FilePath); err == nil {
				os.Remove(oldPath)
			}

			// Save new file
			timestamp := time.Now().UTC().Format("20060102_150405")
			safeName := filepath.Base(header.Filename)
			if safeName == "." || safeName == "/" {
				safeName = "exam.pdf"
			}
			filename := fmt.Sprintf("%s_%s", timestamp, safeName)
			destPath := filepath.Join(storageDir, filename)

			if err := os.WriteFile(destPath, fileData, 0644); err != nil {
				log.Printf("edit exam save file error: %v", err)
				errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan file")
				return
			}

			exam.Name = name
			exam.FilePath = filename
			exam.SizeBytes = int64(len(fileData))
		} else {
			exam.Name = name
		}

		if err := models.UpdateExam(ctx, pool, examID, &exam); err != nil {
			log.Printf("edit exam error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui ujian")
			return
		}

		successMessage(c, fmt.Sprintf(`Ujian "%s" berhasil diperbarui`, name))
	}
}

// ---------------------------------------------------------------------------
// 6. GET /admin/api/exams/:exam_id/pdf — View/stream PDF
// ---------------------------------------------------------------------------

func ExamPDF() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		if !checkExamOwnership(c, pool, examID) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		exam, err := models.GetExamByID(ctx, pool, examID)
		if err != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		storageDir := getStoragePath(c)
		pdfPath, err := safeStoragePath(storageDir, exam.FilePath)
		if err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		if _, err := os.Stat(pdfPath); os.IsNotExist(err) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		download := c.Query("download") == "1"
		if download {
			c.Header("Content-Disposition",
				fmt.Sprintf(`attachment; filename="%s.pdf"`, filepath.Base(exam.Name)))
		} else {
			c.Header("Content-Disposition", `inline`)
		}
		c.Header("Content-Type", "application/pdf")
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
		c.Header("X-Content-Type-Options", "nosniff")
		c.File(pdfPath)
	}
}

// ---------------------------------------------------------------------------
// 7. POST /admin/api/exams/:exam_id/toggle-public-results
// ---------------------------------------------------------------------------

func TogglePublicResults() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		if !checkExamOwnership(c, pool, examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak")
			return
		}

		newVal, err := models.TogglePublicResults(ctx, pool, examID)
		if err != nil {
			log.Printf("toggle public results error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal mengubah pengaturan")
			return
		}

		statusStr := "diaktifkan"
		if newVal == 0 {
			statusStr = "dinonaktifkan"
		}

		c.JSON(http.StatusOK, gin.H{
			"success":        true,
			"message":        fmt.Sprintf("Halaman siswa berhasil %s", statusStr),
			"public_results": newVal,
		})
	}
}

// ---------------------------------------------------------------------------
// 8. POST /admin/api/exams/:exam_id/toggle-show-answers
// ---------------------------------------------------------------------------

func ToggleShowAnswers() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		if !checkExamOwnership(c, pool, examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak")
			return
		}

		newVal, err := models.ToggleShowAnswers(ctx, pool, examID)
		if err != nil {
			log.Printf("toggle show answers error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal mengubah pengaturan")
			return
		}

		statusStr := "ditampilkan"
		if newVal == 0 {
			statusStr = "disembunyikan"
		}

		c.JSON(http.StatusOK, gin.H{
			"success":       true,
			"message":       fmt.Sprintf("Kunci jawaban berhasil %s untuk siswa", statusStr),
			"show_answers":  newVal,
		})
	}
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------
// 8b. GET /admin/api/exams/:exam_id/questions — Get questions config
// ---------------------------------------------------------------------------

func GetQuestions() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		if !checkExamOwnership(c, pool, examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak")
			return
		}

		exam, err := models.GetExamByID(ctx, pool, examID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}

		// Parse questions
		questions := []interface{}{}
		if exam.QuestionsJSON != nil && *exam.QuestionsJSON != "" {
			json.Unmarshal([]byte(*exam.QuestionsJSON), &questions)
		}

		// Security level
		securityLevel := "medium"
		if exam.SecurityLevel != "" {
			securityLevel = exam.SecurityLevel
		}

		// Identity fields
		identityFields := []interface{}{}
		if exam.IdentityFields != nil && *exam.IdentityFields != "" {
			json.Unmarshal([]byte(*exam.IdentityFields), &identityFields)
		}

		// Panel color
		panelColor := ""
		if exam.PanelColor != nil {
			panelColor = *exam.PanelColor
		}

		// Schedule
		startTime := ""
		if exam.StartTime != nil {
			startTime = exam.StartTime.Format("2006-01-02 15:04")
		}
		endTime := ""
		if exam.EndTime != nil {
			endTime = exam.EndTime.Format("2006-01-02 15:04")
		}

		// Pengawas assignments
		assignments, _ := models.GetPengawasAssignments(ctx, pool, examID)
		assignedPengawas := make([]gin.H, 0, len(assignments))
		for _, a := range assignments {
			assignedPengawas = append(assignedPengawas, gin.H{
				"id":       a.UserID,
				"username": a.Username,
				"instansi": a.Instansi,
			})
		}

		// Available pengawas (same instansi as exam creator, active, pengawas role)
		var creatorInstansi string
		_ = pool.QueryRow(ctx,
			`SELECT COALESCE(instansi, '') FROM admin_users WHERE id = $1`,
			exam.CreatedBy).Scan(&creatorInstansi)

		availablePengawas := []gin.H{}
		if creatorInstansi != "" {
			rows, err := pool.Query(ctx,
				`SELECT id, username, COALESCE(instansi, '') as instansi FROM admin_users
				 WHERE instansi = $1 AND status = 'active' AND role ILIKE '%"pengawas"%'
				 ORDER BY username`, creatorInstansi)
			if err == nil {
				for rows.Next() {
					var id int
					var uname, inst string
					if err := rows.Scan(&id, &uname, &inst); err == nil {
						availablePengawas = append(availablePengawas, gin.H{
							"id": id, "username": uname, "instansi": inst,
						})
					}
				}
				rows.Close()
				if err := rows.Err(); err != nil {
					log.Printf("rows iteration error: %v", err)
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"success":            true,
			"questions":          questions,
			"security_level":     securityLevel,
			"strict_mode":        exam.StrictMode != 0,
			"identity_fields":    identityFields,
			"panel_color":        panelColor,
			"start_time":         startTime,
			"end_time":           endTime,
			"assigned_pengawas":  assignedPengawas,
			"available_pengawas": availablePengawas,
		})
	}
}

// ---------------------------------------------------------------------------
// 10. POST /admin/api/exams/:exam_id/questions — Save/update questions
// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

func SaveQuestions() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		if !checkExamOwnership(c, pool, examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak")
			return
		}

		var body struct {
			Questions      []map[string]interface{} `json:"questions"`
			SecurityLevel  string                    `json:"security_level"`
			IdentityFields []map[string]interface{} `json:"identity_fields"`
			PanelColor     string                    `json:"panel_color"`
			StartTime      string                    `json:"start_time"`
			EndTime        string                    `json:"end_time"`
			PengawasIDs    []int                     `json:"pengawas_ids"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			errorResponse(c, http.StatusBadRequest, "Data tidak valid")
			return
		}

		securityLevel := body.SecurityLevel
		if securityLevel != "low" && securityLevel != "medium" && securityLevel != "high" {
			securityLevel = "medium"
		}

		strictMode := 0
		if securityLevel == "high" {
			strictMode = 1
		}

		questionsJSON := "[]"
		if body.Questions != nil {
			raw, _ := json.Marshal(body.Questions)
			questionsJSON = string(raw)
		}

		// Identity fields
		identityFieldsJSON := ""
		if body.IdentityFields != nil {
			raw, _ := json.Marshal(body.IdentityFields)
			identityFieldsJSON = string(raw)
		}

		panelColor := body.PanelColor
		if panelColor != "" && !strings.HasPrefix(panelColor, "#") {
			panelColor = ""
		}
		if len(panelColor) > 7 {
			panelColor = panelColor[:7]
		}

		startTime := body.StartTime
		endTime := body.EndTime
		// Convert empty strings to nil so PostgreSQL doesn't choke on "" as timestamp
		var startTimePtr *string
		var endTimePtr *string
		if startTime != "" {
			startTimePtr = &startTime
		}
		if endTime != "" {
			endTimePtr = &endTime
		}

		if err := models.UpdateExamQuestions(ctx, pool, examID,
			&questionsJSON, &securityLevel, &identityFieldsJSON,
			&panelColor, startTimePtr, endTimePtr, strictMode); err != nil {
			log.Printf("save questions error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan konfigurasi soal")
			return
		}

		// Save pengawas assignments
		if body.PengawasIDs != nil {
			if err := models.SetPengawasForExam(ctx, pool, examID, body.PengawasIDs); err != nil {
				log.Printf("save pengawas error: %v", err)
				// Non-fatal: questions were saved
			}
		}

		// Recalculate scores for existing submissions
		go recalculateScores(ctx, pool, examID)

		successMessage(c, "Konfigurasi soal berhasil disimpan")
	}
}

func recalculateScores(ctx context.Context, pool *pgxpool.Pool, examID int) {
	if err := models.RecalculateAllScoresForExam(ctx, pool, examID); err != nil {
		log.Printf("recalculate scores error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 10. POST /admin/api/exams/regenerate-token — Regenerate token for an exam
// ---------------------------------------------------------------------------

func RegenerateToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		if !checkExamOwnership(c, pool, examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak")
			return
		}

		newToken := generateToken()
		if err := models.UpdateExamToken(ctx, pool, examID, newToken); err != nil {
			log.Printf("regenerate token error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui token")
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": fmt.Sprintf("Token ujian berhasil diperbarui: %s", newToken),
			"token":   newToken,
		})
	}
}

// ---------------------------------------------------------------------------
// 11. POST /admin/api/exams/edit-token — Set custom token for an exam
// ---------------------------------------------------------------------------

func EditToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		var body struct {
			NewToken string `json:"token"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			errorResponse(c, http.StatusBadRequest, "Data tidak valid")
			return
		}

		customToken := strings.ToUpper(strings.TrimSpace(body.NewToken))
		if customToken == "" {
			errorResponse(c, http.StatusBadRequest, "Token tidak boleh kosong")
			return
		}
		if !tokenRegex.MatchString(customToken) {
			errorResponse(c, http.StatusBadRequest, "Token harus terdiri dari 8 karakter alfanumerik")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		if !checkExamOwnership(c, pool, examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak")
			return
		}

		// Check uniqueness (excluding current exam)
		existing, err := models.GetExamByToken(ctx, pool, customToken)
		if err == nil && existing.ID != examID {
			errorResponse(c, http.StatusBadRequest, "Token sudah digunakan oleh ujian lain")
			return
		}

		if err := models.UpdateExamToken(ctx, pool, examID, customToken); err != nil {
			log.Printf("edit token error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui token")
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": fmt.Sprintf("Token ujian berhasil diubah menjadi: %s", customToken),
			"token":   customToken,
		})
	}
}

// ---------------------------------------------------------------------------
// 12. POST /admin/exams/bulk-delete — Bulk delete exams
// ---------------------------------------------------------------------------

func BulkDelete() gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			IDs []int `json:"ids"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || len(body.IDs) == 0 {
			errorResponse(c, http.StatusBadRequest, "Tidak ada ujian yang dipilih")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()
		userID := getCurrentUserID(c)
		isSuper := isSuperAdmin(c)
		isOp := isOperator(c)

		// For non-privileged users, filter to only owned exams
		examIDs := body.IDs
		if !isSuper && !isOp {
			filtered := make([]int, 0, len(examIDs))
			// Re-query exams explicitly
			rows, err := pool.Query(ctx,
				`SELECT id FROM exams WHERE id = ANY($1) AND created_by = $2`, examIDs, userID)
			if err != nil {
				errorResponse(c, http.StatusInternalServerError, "Gagal memverifikasi kepemilikan")
				return
			}
			for rows.Next() {
				var id int
				rows.Scan(&id)
				filtered = append(filtered, id)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				log.Printf("rows iteration error: %v", err)
			}
			examIDs = filtered
			if len(examIDs) == 0 {
				errorResponse(c, http.StatusBadRequest, "Tidak ada ujian yang dapat dihapus")
				return
			}
		}

		// Collect file paths for cleanup
		paths, err := models.BulkDeleteExams(ctx, pool, examIDs)
		if err != nil {
			log.Printf("bulk delete error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menghapus ujian")
			return
		}

		// Clean up files
		storageDir := getStoragePath(c)
		for _, p := range paths {
			if fp, err := safeStoragePath(storageDir, p); err == nil {
				os.Remove(fp)
			}
		}

		successMessage(c, fmt.Sprintf("%d ujian berhasil dihapus", len(examIDs)))
	}
}

// ---------------------------------------------------------------------------
// 13. GET /admin/api/exams/:exam_id/delegate-data — Delegation modal data
// ---------------------------------------------------------------------------

func DelegateData() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		if !isOperator(c) && !isSuperAdmin(c) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak.")
			return
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		ctx := c.Request.Context()

		// Get operator's instansi
		var opInstansi string
		err = pool.QueryRow(ctx, `SELECT instansi FROM admin_users WHERE id = $1`, userID).Scan(&opInstansi)
		if err != nil || opInstansi == "" {
			errorResponse(c, http.StatusBadRequest, "Instansi tidak ditemukan")
			return
		}

		// Get exam
		exam, err := models.GetExamByID(ctx, pool, examID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}

		// Verify exam belongs to same instansi
		var examInstansi string
		err = pool.QueryRow(ctx,
			`SELECT COALESCE(u.instansi, '') FROM admin_users u WHERE u.id = $1`,
			exam.CreatedBy).Scan(&examInstansi)
		if err != nil || examInstansi != opInstansi {
			errorResponse(c, http.StatusBadRequest, "Ujian tidak berada dalam instansi Anda")
			return
		}

		// Get current owner
		type userInfo struct {
			ID       int    `json:"id"`
			Username string `json:"username"`
		}
		var currentOwner *userInfo
		var ownerUsername string
		var ownerInstansi string
		err = pool.QueryRow(ctx,
			`SELECT username, COALESCE(instansi, '') FROM admin_users WHERE id = $1`,
			exam.CreatedBy).Scan(&ownerUsername, &ownerInstansi)
		if err == nil {
			currentOwner = &userInfo{ID: exam.CreatedBy, Username: ownerUsername}
		}

		// Get current delegated_to
		var delegatedTo *userInfo
		if exam.DelegatedTo != nil {
			var dtUsername string
			err = pool.QueryRow(ctx, `SELECT username FROM admin_users WHERE id = $1`, *exam.DelegatedTo).Scan(&dtUsername)
			if err == nil {
				delegatedTo = &userInfo{ID: *exam.DelegatedTo, Username: dtUsername}
			}
		}

		// Get available gurus (same instansi, active, guru role, excluding exam creator)
		type guruItem struct {
			ID       int    `json:"id"`
			Username string `json:"username"`
			Instansi string `json:"instansi"`
		}
		availableGurus := []guruItem{}
		{
			rows, err := pool.Query(ctx, `
				SELECT id, username, COALESCE(instansi, '') as instansi
				FROM admin_users
				WHERE instansi = $1
				  AND status = 'active'
				  AND role ILIKE '%"guru"%'
				  AND id != $2
				ORDER BY username`, opInstansi, exam.CreatedBy)
			if err == nil {
				for rows.Next() {
					var g guruItem
					if err := rows.Scan(&g.ID, &g.Username, &g.Instansi); err == nil {
						availableGurus = append(availableGurus, g)
					}
				}
				rows.Close()
				if err := rows.Err(); err != nil {
					log.Printf("rows iteration error: %v", err)
				}
			}
		}

		// Get available pengawas (same instansi, active, pengawas role)
		type pengawasItem struct {
			ID       int    `json:"id"`
			Username string `json:"username"`
			Instansi string `json:"instansi"`
		}
		availablePengawas := []pengawasItem{}
		{
			rows, err := pool.Query(ctx, `
				SELECT id, username, COALESCE(instansi, '') as instansi
				FROM admin_users
				WHERE instansi = $1
				  AND status = 'active'
				  AND role ILIKE '%"pengawas"%'
				ORDER BY username`, opInstansi)
			if err == nil {
				for rows.Next() {
					var p pengawasItem
					if err := rows.Scan(&p.ID, &p.Username, &p.Instansi); err == nil {
						availablePengawas = append(availablePengawas, p)
					}
				}
				rows.Close()
				if err := rows.Err(); err != nil {
					log.Printf("rows iteration error: %v", err)
				}
			}
		}

		// Get assigned pengawas IDs
		assignedIDs, _ := models.GetPengawasIDs(ctx, pool, examID)

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"current_owner":        currentOwner,
				"delegated_to":         delegatedTo,
				"available_gurus":      availableGurus,
				"available_pengawas":   availablePengawas,
				"assigned_pengawas_ids": assignedIDs,
			},
		})
	}
}

// ---------------------------------------------------------------------------
// 14. POST /admin/api/exams/:exam_id/delegate — Set delegation
// ---------------------------------------------------------------------------

func PostDelegateExam() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		if !isOperator(c) && !isSuperAdmin(c) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak.")
			return
		}

		var body struct {
			NewOwnerID  *int  `json:"new_owner_id"`
			PengawasIDs []int `json:"pengawas_ids"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			errorResponse(c, http.StatusBadRequest, "Data tidak valid")
			return
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		ctx := c.Request.Context()

		// Get operator's instansi
		var opInstansi string
		err = pool.QueryRow(ctx, `SELECT instansi FROM admin_users WHERE id = $1`, userID).Scan(&opInstansi)
		if err != nil || opInstansi == "" {
			errorResponse(c, http.StatusBadRequest, "Instansi tidak ditemukan")
			return
		}

		// Get exam
		exam, err := models.GetExamByID(ctx, pool, examID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "Ujian tidak ditemukan")
			return
		}

		// Verify exam belongs to same instansi
		var examInstansi string
		err = pool.QueryRow(ctx,
			`SELECT COALESCE(u.instansi, '') FROM admin_users u WHERE u.id = $1`,
			exam.CreatedBy).Scan(&examInstansi)
		if err != nil || examInstansi != opInstansi {
			errorResponse(c, http.StatusBadRequest, "Ujian tidak berada dalam instansi Anda")
			return
		}

		// Process new_owner_id
		if body.NewOwnerID != nil {
			newOwnerID := *body.NewOwnerID
			if newOwnerID > 0 {
				// Validate that the target user exists, is active, has guru role, and is in same instansi
				var targetInstansi string
				var targetRole string
				var targetStatus string
				err := pool.QueryRow(ctx,
					`SELECT COALESCE(instansi, ''), role, status FROM admin_users WHERE id = $1`,
					newOwnerID).Scan(&targetInstansi, &targetRole, &targetStatus)
				if err != nil {
					errorResponse(c, http.StatusBadRequest, "User tujuan tidak ditemukan")
					return
				}
				if targetInstansi != opInstansi {
					errorResponse(c, http.StatusBadRequest, "User tujuan tidak berada dalam instansi yang sama")
					return
				}
				if targetStatus != models.UserStatusActive {
					errorResponse(c, http.StatusBadRequest, "User tujuan tidak aktif")
					return
				}
				if !strings.Contains(targetRole, "guru") {
					errorResponse(c, http.StatusBadRequest, "User tujuan harus memiliki role Guru")
					return
				}

				if err := models.DelegateExam(ctx, pool, examID, &newOwnerID); err != nil {
					log.Printf("delegate exam error: %v", err)
					errorResponse(c, http.StatusInternalServerError, "Gagal mengatur penanggung jawab")
					return
				}
			} else {
				// Remove delegation
				if err := models.DelegateExam(ctx, pool, examID, nil); err != nil {
					log.Printf("delegate exam remove error: %v", err)
					errorResponse(c, http.StatusInternalServerError, "Gagal menghapus penanggung jawab")
					return
				}
			}
		}

		// Process pengawas_ids
		if body.PengawasIDs != nil {
			// Validate that all target users exist, are active, have pengawas role, and are in same instansi
			for _, pid := range body.PengawasIDs {
				var targetInstansi string
				var targetRole string
				var targetStatus string
				err := pool.QueryRow(ctx,
					`SELECT COALESCE(instansi, ''), role, status FROM admin_users WHERE id = $1`,
					pid).Scan(&targetInstansi, &targetRole, &targetStatus)
				if err != nil {
					errorResponse(c, http.StatusBadRequest, fmt.Sprintf("Pengawas dengan ID %d tidak ditemukan", pid))
					return
				}
				if targetInstansi != opInstansi {
					errorResponse(c, http.StatusBadRequest, fmt.Sprintf("Pengawas %d tidak berada dalam instansi yang sama", pid))
					return
				}
				if targetStatus != models.UserStatusActive {
					errorResponse(c, http.StatusBadRequest, fmt.Sprintf("Pengawas %d tidak aktif", pid))
					return
				}
				if !strings.Contains(targetRole, "pengawas") {
					errorResponse(c, http.StatusBadRequest, fmt.Sprintf("User %d tidak memiliki role Pengawas", pid))
					return
				}
			}

			if err := models.SetPengawasForExam(ctx, pool, examID, body.PengawasIDs); err != nil {
				log.Printf("delegate pengawas error: %v", err)
				errorResponse(c, http.StatusInternalServerError, "Gagal mengatur pengawas")
				return
			}
		}

		successMessage(c, "Delegasi ujian berhasil diperbarui")
	}
}

// ---------------------------------------------------------------------------
// 15. POST /admin/exams/bulk-toggle — Bulk toggle exam status
// ---------------------------------------------------------------------------

func BulkToggle() gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			IDs    []int  `json:"ids"`
			Status string `json:"status"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || len(body.IDs) == 0 {
			errorResponse(c, http.StatusBadRequest, "Tidak ada ujian yang dipilih")
			return
		}

		if body.Status != "active" && body.Status != "inactive" {
			errorResponse(c, http.StatusBadRequest, "Status tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()
		userID := getCurrentUserID(c)
		isSuper := isSuperAdmin(c)
		isOp := isOperator(c)

		examIDs := body.IDs
		if !isSuper && !isOp {
			filtered := make([]int, 0, len(examIDs))
			rows, err := pool.Query(ctx,
				`SELECT id FROM exams WHERE id = ANY($1) AND created_by = $2`, examIDs, userID)
			if err != nil {
				errorResponse(c, http.StatusInternalServerError, "Gagal memverifikasi kepemilikan")
				return
			}
			for rows.Next() {
				var id int
				rows.Scan(&id)
				filtered = append(filtered, id)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				log.Printf("rows iteration error: %v", err)
			}
			examIDs = filtered
			if len(examIDs) == 0 {
				errorResponse(c, http.StatusBadRequest, "Tidak ada ujian yang dapat diperbarui")
				return
			}
		}

		if err := models.BulkToggleExamStatus(ctx, pool, examIDs, body.Status); err != nil {
			log.Printf("bulk toggle error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui status ujian")
			return
		}

		successMessage(c, fmt.Sprintf("Status %d ujian berhasil diperbarui ke %s", len(examIDs), body.Status))
	}
}
