package admin

import (
	"bytes"
	"context"
	cryptoRand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/config"
	r2client "github.com/examvan/webui/internal/handlers/r2"
	"github.com/examvan/webui/internal/helpers"
	"github.com/examvan/webui/internal/models"
	"github.com/examvan/webui/internal/websocket"
)

// ---------------------------------------------------------------------------
// Constants & helpers
// ---------------------------------------------------------------------------

const maxFileSize = 100 * 1024 * 1024 // 100 MB global limit

var tokenRegex = regexp.MustCompile(fmt.Sprintf(`^[A-Z0-9]{%d}$`, config.TokenLength))

// jakartaLocation returns the Asia/Jakarta timezone, falling back to UTC when
// the IANA tz database is unavailable. This avoids a nil *time.Location, which
// would make time.Time.In / time.ParseInLocation panic.
func jakartaLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil || loc == nil {
		return time.UTC
	}
	return loc
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

	// Validate PDF EOF marker (%%EOF) in the last 1024 bytes
	lastBytes := data
	if len(data) > 1024 {
		lastBytes = data[len(data)-1024:]
	}
	if !bytes.Contains(lastBytes, []byte("%%EOF")) {
		return false, "File tidak valid (PDF rusak atau tidak lengkap)"
	}

	// Detect HTML/JS polyglots to prevent stored XSS
	dangerousSigs := [][]byte{
		[]byte("<script"),
		[]byte("<html"),
		[]byte("<iframe"),
		[]byte("javascript:"),
		[]byte("<body"),
	}
	dataLower := bytes.ToLower(data)
	for _, sig := range dangerousSigs {
		if bytes.Contains(dataLower, sig) {
			return false, "File terdeteksi mengandung konten berbahaya (potensi XSS)"
		}
	}

	return true, ""
}

// cleanUploadedFilename extracts the base name and cleans it to prevent path traversal
func cleanUploadedFilename(filename string) string {
	parts := strings.FieldsFunc(filename, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	if len(parts) == 0 {
		return "exam.pdf"
	}
	base := parts[len(parts)-1]

	reg := regexp.MustCompile(`[^a-zA-Z0-9._-]`)
	safe := reg.ReplaceAllString(base, "_")

	if safe == "" || safe == "." || safe == ".." {
		return "exam.pdf"
	}

	if !strings.HasSuffix(strings.ToLower(safe), ".pdf") {
		safe = safe + ".pdf"
	}
	return safe
}

// newExamObjectName builds the R2 object name (without the "pdfs/" prefix)
// for an uploaded PDF: timestamp + random suffix + sanitized filename. The
// random suffix guarantees uniqueness even when two files with the SAME
// original name are uploaded within the SAME second — before it existed the
// name was timestamp + filename, so the second upload silently overwrote the
// first object in R2.
func newExamObjectName(originalName string) string {
	return examObjectNameAt(time.Now().UTC(), cleanUploadedFilename(originalName))
}

// examObjectNameAt is newExamObjectName against an explicit clock (pure, so
// the uniqueness property is unit-testable).
func examObjectNameAt(ts time.Time, safeName string) string {
	return fmt.Sprintf("%s_%s_%s", ts.Format("20060102_150405"), randomObjectSuffix(), safeName)
}

// randomObjectSuffix returns a short random hex string for R2 object-name
// disambiguation.
func randomObjectSuffix() string {
	b := make([]byte, 4)
	if _, err := cryptoRand.Read(b); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b)
}

// sanitizeFilename removes characters from a filename that could break HTTP
// Content-Disposition headers or cause filesystem issues.
func sanitizeFilename(name string) string {
	s := strings.TrimSpace(name)
	// Replace any non-printable, quote, backslash, or slash characters.
	s = regexp.MustCompile(`[^\x20-\x7E]`).ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, `"`, "")
	s = strings.ReplaceAll(s, `\`, "")
	s = strings.ReplaceAll(s, "/", "")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) > 100 {
		s = s[:100]
	}
	if s == "" {
		s = "exam"
	}
	return s
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
		if errors.Is(err, http.ErrMissingFile) {
			errorResponse(c, http.StatusBadRequest, "File PDF wajib dipilih")
			return
		}
		if err != nil {
			// Multipart gagal di-parse — mis. body melebihi LimitBodySize
			// (MaxBytesError) atau form rusak. Balas error keras, bukan
			// "file tidak dipilih".
			errorResponse(c, http.StatusBadRequest, "Gagal membaca file PDF — periksa ukuran dan format file")
			return
		}
		defer file.Close()

		// Tolak file terlalu besar SEBELUM membuffernya ke RAM: header.Size
		// sudah diketahui dari parse multipart tanpa membaca isi part.
		// validatePDF di bawah tetap mengecek ulang setelah read sebagai
		// lapis kedua (dan menolak PDF rusak/polyglot).
		if header.Size > maxFileSize {
			maxMB := maxFileSize / (1024 * 1024)
			errorResponse(c, http.StatusBadRequest, fmt.Sprintf("Ukuran file melebihi batas %dMB", maxMB))
			return
		}

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

		// Quota layers: the account's OWN limits (unless super admin /
		// operator) AND the shared school pool when the instansi runs a school
		// package. The pool is a single bucket for EVERY account in the
		// instansi — the operator included — so the school's total
		// exams/storage/PDF can never exceed the package (5 sub-accounts × 3
		// exams can no longer overspend a 3-exam school package). maxExams
		// tracks the per-account quota for the (atomic) check+insert below;
		// -1 means "no per-account limit" (super/operator accounts bypass
		// their own limits, matching previous behaviour).
		maxExams := -1
		var poolMaxExams, poolMaxPDF, poolMaxStorage int64
		var poolInstansi string
		poolActive := false
		if !isSuper {
			poolMaxExams, poolMaxPDF, _, poolMaxStorage, poolInstansi, poolActive =
				schoolPoolQuota(ctx, pool, userID)
			// Pool PDF-size limit: applies to the operator too (their uploads
			// spend the school package).
			if poolActive && poolMaxPDF > 0 && int64(len(fileData)) > poolMaxPDF {
				limitMB := roundTo(float64(poolMaxPDF)/(1024*1024), 2)
				errorResponse(c, http.StatusForbidden,
					fmt.Sprintf("Ukuran file melebihi batas paket sekolah (%.2fMB). Silakan hubungi Super Admin.", limitMB))
				return
			}
			// Pool storage pre-check (friendly early rejection; the atomic
			// check inside the transaction below is the hard gate).
			if poolActive && poolMaxStorage > 0 {
				current, err := models.SumStorageByInstansi(ctx, pool, poolInstansi)
				if err == nil && current+int64(len(fileData)) > poolMaxStorage {
					limitMB := roundTo(float64(poolMaxStorage)/(1024*1024), 1)
					errorResponse(c, http.StatusForbidden,
						fmt.Sprintf("Batas kapasitas storage sekolah tercapai. Batas paket sekolah adalah %.1f MB.", limitMB))
					return
				}
			}
		}
		if !isSuper && !isOp {
			user, err := models.GetUserByID(ctx, pool, userID)
			if err != nil {
				errorResponse(c, http.StatusInternalServerError, "Gagal memuat data user")
				return
			}
			// School-pool sub-accounts: when the school pool is active it IS
			// the account's quota (see schoolPoolCovers) — the per-account
			// columns (forced free defaults) must not gate below it, so they
			// are skipped: maxExams stays -1 (no per-account limit) and the
			// pool's atomic exam/PDF/storage gates below are the only caps.
			if user.OperatorCreated && poolActive {
				// pool-covered — the checks above and the atomic pool gate below apply
			} else {
				maxExams = user.MaxExams
				if user.MaxPDFSize > 0 && len(fileData) > user.MaxPDFSize {
					limitMB := roundTo(float64(user.MaxPDFSize)/(1024*1024), 2)
					errMsg := fmt.Sprintf("Ukuran file melebihi batas akun Anda (%.2fMB). Silakan hubungi Super Admin.", limitMB)
					errorResponse(c, http.StatusForbidden, errMsg)
					return
				}
				// Check storage limit (only enforce when MaxStorageSize > 0)
				if user.MaxStorageSize > 0 {
					var currentStorageBytes int64
					err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(size_bytes), 0) FROM exams WHERE created_by = $1`, userID).Scan(&currentStorageBytes)
					if err == nil && currentStorageBytes+int64(len(fileData)) > user.MaxStorageSize {
						limitMB := roundTo(float64(user.MaxStorageSize)/(1024*1024), 1)
						errorResponse(c, http.StatusForbidden,
							fmt.Sprintf("Batas kapasitas storage tercapai. Batas akun Anda adalah %.1f MB.", limitMB))
						return
					}
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
			// Check uniqueness with retry for race condition.
			existing, err := models.GetExamByToken(ctx, pool, customToken)
			if err == nil && existing.ID > 0 {
				errorResponse(c, http.StatusBadRequest, "Token kustom sudah digunakan oleh ujian lain")
				return
			}
			token = customToken
		} else {
			// Retry loop for auto-generated token to handle race conditions.
			// Token bebas dilaporkan GetExamByToken sebagai pgx.ErrNoRows
			// (lookup tak menemukan baris) — itu kasus normal, langsung pakai.
			// Error DB nyata atau token yang memang sudah dipakai → lanjut coba
			// token berikutnya. Jika setelah 5 percobaan masih bentrok, INSERT
			// kena unique violation yang dipetakan ke 400 di bawah.
			for i := 0; i < 5; i++ {
				token = helpers.GenerateExamToken()
				existing, err := models.GetExamByToken(ctx, pool, token)
				if errors.Is(err, pgx.ErrNoRows) || (err == nil && existing.ID == 0) {
					break
				}
				if err != nil {
					log.Printf("upload: cek token gagal (percobaan %d): %v", i+1, err)
				}
			}
		}

		filename := newExamObjectName(header.Filename)

		// Upload to R2 (Mandatory): reject a missing OR disabled backend with a
		// clear message (mirrors UploadSystemApp) instead of attempting an
		// upload against a backend that is not ready.
		if r2c, exists := c.Get("r2"); exists {
			client := r2client.FromContext(r2c)
			if client == nil || !client.Enabled() {
				errorResponseWithCode(c, http.StatusInternalServerError, r2client.ErrCodeNotConfigured, r2client.ErrMsgNotConfigured)
				return
			}
			r2Key := fmt.Sprintf("pdfs/%s", filename)
			if err := client.UploadBytes(ctx, r2Key, fileData); err != nil {
				log.Printf("admin: R2 upload error: %v", err)
				errorResponseWithCode(c, http.StatusInternalServerError, r2client.ErrCodeUploadFailed, r2client.ErrMsgUploadFailed)
				return
			}
			log.Printf("admin: PDF uploaded to R2: %s", r2Key)
		} else {
			errorResponseWithCode(c, http.StatusInternalServerError, r2client.ErrCodeNotConfigured, r2client.ErrMsgNotConfigured)
			return
		}

		defaultMode := "dynamic"
		defaultInterval := 5

		var delegatedTo *int
		if hasCurrentRole(c, models.RoleGuru) {
			delegatedTo = &userID
		}

		exam := &models.Exam{
			Name:      name,
			FilePath:  filename,
			SizeBytes: int64(len(fileData)),
			Token:     token,
			// New exams start INACTIVE: they only become joinable for students
			// after the admin explicitly activates and starts them.
			Status:             "inactive",
			SecurityLevel:      "medium",
			PublicResults:      1,
			CreatedBy:          userID,
			DelegatedTo:        delegatedTo,
			TokenMode:          &defaultMode,
			TokenResetInterval: &defaultInterval,
		}

		// Atomic quota check + insert: lock the user row (and, when a school
		// pool applies, every account row of the instansi) FOR UPDATE so two
		// concurrent uploads for the same account / school are serialized (this
		// closes the check-then-insert race window), then count + insert in one
		// transaction. Only enforced when a per-account quota applies
		// (maxExams >= 0; 0 = unlimited) or a school pool is active.
		var created *models.Exam
		if maxExams >= 0 || poolActive {
			tx, err := pool.Begin(ctx)
			if err != nil {
				log.Printf("upload begin tx error: %v", err)
				errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan ujian")
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()

			if poolActive {
				// Lock the whole instansi's account rows: concurrent uploads by
				// ANY account in the school serialize on the shared pool (the
				// row-lock pattern of the per-user check, widened to the pool).
				if _, err := tx.Exec(ctx, `SELECT id FROM admin_users WHERE instansi = $1 FOR UPDATE`, poolInstansi); err != nil {
					log.Printf("upload lock school pool error: %v", err)
					// The PDF is already in R2 but no DB row exists: remove the
					// orphan object so a failed create does not leak storage.
					cleanupR2Orphan(c, ctx, filename)
					errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan ujian")
					return
				}
				var poolCnt int64
				if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM exams e JOIN admin_users u ON e.created_by = u.id WHERE u.instansi = $1`, poolInstansi).Scan(&poolCnt); err != nil {
					log.Printf("upload count school exams error: %v", err)
					cleanupR2Orphan(c, ctx, filename)
					errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan ujian")
					return
				}
				if poolMaxExams > 0 && poolCnt >= poolMaxExams {
					_ = tx.Rollback(ctx)
					// Remove the just-uploaded R2 object so we do not leak an orphan.
					cleanupR2Orphan(c, ctx, filename)
					errorResponse(c, http.StatusForbidden,
						fmt.Sprintf("Batas pembuatan ujian sekolah tercapai. Paket sekolah Anda adalah %d ujian.", poolMaxExams))
					return
				}
				// Atomic pool storage gate (same transaction, same lock): the
				// pre-check above is a friendly early rejection, this is the
				// hard cap that cannot be raced past.
				if poolMaxStorage > 0 {
					var poolUsed int64
					if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(e.size_bytes), 0) FROM exams e JOIN admin_users u ON e.created_by = u.id WHERE u.instansi = $1`, poolInstansi).Scan(&poolUsed); err != nil {
						log.Printf("upload sum school storage error: %v", err)
						cleanupR2Orphan(c, ctx, filename)
						errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan ujian")
						return
					}
					if poolUsed+int64(len(fileData)) > poolMaxStorage {
						_ = tx.Rollback(ctx)
						cleanupR2Orphan(c, ctx, filename)
						limitMB := roundTo(float64(poolMaxStorage)/(1024*1024), 1)
						errorResponse(c, http.StatusForbidden,
							fmt.Sprintf("Batas kapasitas storage sekolah tercapai. Batas paket sekolah adalah %.1f MB.", limitMB))
						return
					}
				}
			}

			var lockedMaxExams int
			if err := tx.QueryRow(ctx,
				`SELECT max_exams FROM admin_users WHERE id = $1 FOR UPDATE`, userID).Scan(&lockedMaxExams); err != nil {
				log.Printf("upload lock user error: %v", err)
				// The PDF is already in R2 but no DB row exists: remove the
				// orphan object so a failed create does not leak storage.
				cleanupR2Orphan(c, ctx, filename)
				errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan ujian")
				return
			}

			var cnt int
			if err := tx.QueryRow(ctx,
				`SELECT COUNT(*) FROM exams WHERE created_by = $1`, userID).Scan(&cnt); err != nil {
				log.Printf("upload count exams error: %v", err)
				cleanupR2Orphan(c, ctx, filename)
				errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan ujian")
				return
			}
			if lockedMaxExams > 0 && cnt >= lockedMaxExams {
				_ = tx.Rollback(ctx)
				// Remove the just-uploaded R2 object so we do not leak an orphan.
				cleanupR2Orphan(c, ctx, filename)
				errorResponse(c, http.StatusForbidden,
					fmt.Sprintf("Batas pembuatan ujian tercapai. Batas akun Anda adalah %d ujian.", lockedMaxExams))
				return
			}

			created, err = models.CreateExamTx(ctx, tx, exam)
			if err != nil {
				log.Printf("upload create exam error: %v", err)
				cleanupR2Orphan(c, ctx, filename)
				// Race token: dua upload konkuren memakai token yang sama
				// sama-sama lolos pre-check unik → INSERT kena unique violation
				// (satu-satunya constraint unik di exams adalah token).
				// Integritas tetap aman; ubah 500 yang menyesatkan jadi 400.
				if isTokenUniqueViolation(err) {
					errorResponse(c, http.StatusBadRequest, "Token sudah digunakan oleh ujian lain")
					return
				}
				errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan ujian")
				return
			}
			if err := tx.Commit(ctx); err != nil {
				log.Printf("upload commit error: %v", err)
				// NOTE: no orphan cleanup here — a commit error is ambiguous: the
				// row may actually have been committed, and deleting the R2 object
				// would then break a live exam's PDF. An orphan object is the
				// lesser evil (best-effort swept by admin cleanup) vs. destroying
				// a possibly-committed exam.
				errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan ujian")
				return
			}
		} else {
			created, err = models.CreateExam(ctx, pool, exam)
			if err != nil {
				log.Printf("upload create exam error: %v", err)
				cleanupR2Orphan(c, ctx, filename)
				if isTokenUniqueViolation(err) {
					errorResponse(c, http.StatusBadRequest, "Token sudah digunakan oleh ujian lain")
					return
				}
				errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan ujian")
				return
			}
		}

		// Append-only audit trail: the exam lifecycle starts here (created →
		// pdf_replaced → deleted). Written only after the row committed, with
		// the exam_id FK targeting the new row; best-effort — a failed audit
		// row never fails the creation itself.
		detail := fmt.Sprintf("Ujian dibuat: %s", name)
		if err := models.CreateAdminAuditLog(ctx, pool, userID, getCurrentUsername(c),
			models.ActionExamCreated, created.ID, detail); err != nil {
			log.Printf("audit exam created: %v", err)
		}

		// Auto-assign creator as pengawas for this exam
		if err := models.CreateExamPengawas(ctx, pool, created.ID, userID); err != nil {
			log.Printf("auto-assign pengawas error: %v", err)
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": fmt.Sprintf(`Ujian "%s" berhasil diupload dalam status nonaktif dengan token: %s`, name, token),
			"token":   token,
			"id":      created.ID,
		})
	}
}

// cleanupR2Orphan best-effort deletes the R2 object that was just uploaded
// when the exam row never materialized (quota reject, create/commit failure
// after the upload). Without it every rejected upload would leak an orphan
// object in the bucket that no exam references — the same class of storage
// leak as the ghost files on the delete path.
func cleanupR2Orphan(c *gin.Context, ctx context.Context, filename string) {
	if r2c, ok := c.Get("r2"); ok {
		if client := r2client.FromContext(r2c); client != nil {
			key := fmt.Sprintf("pdfs/%s", filename)
			if err := client.Delete(ctx, key); err != nil {
				log.Printf("admin: cleanup orphan R2 object %s failed: %v", key, err)
			} else {
				log.Printf("admin: cleaned up orphan R2 object %s", key)
			}
		}
	}
}

// isTokenUniqueViolation reports whether err is a Postgres unique-violation
// (SQLSTATE 23505) on the exams.token constraint. exams.token is the only
// unique constraint on the table, so a 23505 from the INSERT inside
// UploadExam can only mean a token collision (e.g. two concurrent uploads
// raced past the pre-insert uniqueness check).
func isTokenUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "token")
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

		// Enforce the concurrent-exam quota (max_concurrent_exams) when
		// re-activating an exam that has ALREADY been started: such an exam
		// becomes "running" again on activation even without going through
		// StartExam (ToggleExamStatus does not clear exam_started_at). Same two
		// layers as StartExam: the account's own quota (sub-accounts) and the
		// shared school pool (every account in the instansi, operator included).
		//
		// Atomic when any limit applies: the count + toggle run inside one
		// transaction with the school pool's (or the owner's) rows locked
		// FOR UPDATE, so two concurrent re-activations can never both count
		// the same free slot. Super admins bypass everything.
		toggled := false
		if !isSuperAdmin(c) {
			exam, err := models.GetExamByID(ctx, pool, examID)
			if err == nil && exam.Status == "inactive" && exam.ExamStartedAt != nil {
				owner, err := models.GetUserByID(ctx, pool, exam.CreatedBy)
				if err == nil {
					_, _, poolMaxConcurrent, _, poolInstansi, poolActive := schoolPoolQuota(ctx, pool, exam.CreatedBy)
					// School-pool sub-accounts: when the pool is active it is the
					// account's ONLY concurrent quota (schoolPoolCovers), so the
					// per-account max_concurrent_exams must not gate below it.
					perUserLimit := !isOperator(c) && !(owner.OperatorCreated && poolActive) && owner.MaxConcurrentExams > 0
					poolConcActive := poolActive && poolMaxConcurrent > 0

					if perUserLimit || poolConcActive {
						tx, err := pool.Begin(ctx)
						if err != nil {
							log.Printf("toggle begin tx error: %v", err)
							errorResponse(c, http.StatusInternalServerError, "Gagal mengubah status ujian")
							return
						}
						defer func() { _ = tx.Rollback(ctx) }()

						if poolConcActive {
							if _, err := tx.Exec(ctx, `SELECT id FROM admin_users WHERE instansi = $1 FOR UPDATE`, poolInstansi); err != nil {
								log.Printf("toggle lock school pool error: %v", err)
								errorResponse(c, http.StatusInternalServerError, "Gagal mengubah status ujian")
								return
							}
							running, err := models.CountRunningExamsByInstansi(ctx, tx, poolInstansi, examID)
							if err == nil && running >= int(poolMaxConcurrent) {
								_ = tx.Rollback(ctx)
								errorResponse(c, http.StatusForbidden,
									fmt.Sprintf("Batas ujian serentak sekolah tercapai. Maksimal %d ujian sekolah dapat berjalan bersamaan.", poolMaxConcurrent))
								return
							}
						}
						if perUserLimit {
							var locked int
							if err := tx.QueryRow(ctx, `SELECT max_concurrent_exams FROM admin_users WHERE id = $1 FOR UPDATE`, exam.CreatedBy).Scan(&locked); err != nil {
								log.Printf("toggle lock owner error: %v", err)
								errorResponse(c, http.StatusInternalServerError, "Gagal mengubah status ujian")
								return
							}
							running, err := models.CountRunningExams(ctx, tx, exam.CreatedBy, examID)
							if err == nil && running >= locked {
								_ = tx.Rollback(ctx)
								errorResponse(c, http.StatusForbidden,
									fmt.Sprintf("Batas ujian serentak tercapai. Maksimal %d ujian dapat berjalan bersamaan.", locked))
								return
							}
						}

						if _, err := models.ToggleExamStatusTx(ctx, tx, examID); err != nil {
							log.Printf("toggle exam tx error: %v", err)
							errorResponse(c, http.StatusInternalServerError, "Gagal mengubah status ujian")
							return
						}
						if err := tx.Commit(ctx); err != nil {
							log.Printf("toggle commit error: %v", err)
							errorResponse(c, http.StatusInternalServerError, "Gagal mengubah status ujian")
							return
						}
						toggled = true
					}
				}
			}
		}

		if toggled {
			c.JSON(http.StatusOK, gin.H{
				"success":    true,
				"message":    "Status ujian diubah ke active",
				"new_status": "active",
			})
			return
		}

		newStatus, err := models.ToggleExamStatus(ctx, pool, examID)
		if err != nil {
			log.Printf("toggle exam error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal mengubah status ujian")
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"message":    fmt.Sprintf("Status ujian diubah ke %s", newStatus),
			"new_status": newStatus,
		})
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

		// Load the exam once: it is needed for the audit detail AND to decide
		// whether R2 cleanup is mandatory (an exam with a stored PDF must have
		// a working R2 backend before its row is removed).
		auditExam, auditErr := models.GetExamByID(ctx, pool, examID)

		// R2 pre-check (Mandatory, mirrors UploadExam/EditExam): refuse BEFORE
		// the DB row is removed when the backend is missing/disabled —
		// otherwise every delete of an R2-backed exam would silently orphan
		// its pdfs/<file_path> object. The error_code lets the frontend show
		// the R2_NOT_CONFIGURED setup warning instead of a generic failure.
		if auditErr == nil && auditExam.FilePath != "" {
			r2c, exists := c.Get("r2")
			client := r2client.FromContext(r2c)
			if !exists || client == nil || !client.Enabled() {
				errorResponseWithCode(c, http.StatusInternalServerError, r2client.ErrCodeNotConfigured, r2client.ErrMsgNotConfigured)
				return
			}
		}

		// Append-only audit trail: who deleted the exam and when. Written
		// BEFORE the row is removed because exam_id is ON DELETE SET NULL —
		// the row survives the deletion (exam_id → NULL) with the name
		// snapshotted in detail, exactly the survival path the schema
		// describes. Best-effort: a failed audit row never blocks the delete.
		// If the delete itself then fails, the row documents an attempted
		// deletion by an authorized actor — acceptable for the append-only
		// trail. Do NOT move the write after the delete: exam_id would
		// reference a now-missing row and violate the FK.
		if auditErr == nil {
			detail := fmt.Sprintf("Ujian dihapus: %s", auditExam.Name)
			if err := models.CreateAdminAuditLog(ctx, pool, getCurrentUserID(c), getCurrentUsername(c),
				models.ActionExamDeleted, examID, detail); err != nil {
				log.Printf("audit exam deleted: %v", err)
			}
		}

		exam, err := models.DeleteExam(ctx, pool, examID)
		if err != nil {
			log.Printf("delete exam error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menghapus ujian")
			return
		}

		// Skip file cleanup on legacy rows without a stored file_path: an empty
		// path would resolve SafeStoragePath to the storage dir itself.
		if exam.FilePath == "" {
			successMessage(c, "Ujian berhasil dihapus")
			return
		}

		// Delete from R2 (guaranteed enabled by the pre-check above): the
		// backend may still fail transiently, which is logged and swept by the
		// admin cleanup job — the row is already gone, so the delete must not
		// be reported as failed afterwards (mirrors BulkDelete/DeleteUser).
		if r2c, exists := c.Get("r2"); exists {
			client := r2client.FromContext(r2c)
			if client != nil && client.Enabled() {
				r2Key := fmt.Sprintf("pdfs/%s", exam.FilePath)
				if err := client.Delete(ctx, r2Key); err != nil {
					log.Printf("admin: R2 delete error: %v", err)
				}
			}
		}

		// Clean up the local storage file (best-effort, same guard as the bulk
		// path): a deleted exam must not leave its PDF occupying disk forever —
		// otherwise FreeDiskSpace never recovers and the storage quota checks
		// keep counting the ghost file. Path traversal is rejected by
		// SafeStoragePath.
		storageDir := getStoragePath(c)
		if fp, err := helpers.SafeStoragePath(storageDir, exam.FilePath); err == nil {
			if err := os.Remove(fp); err != nil && !os.IsNotExist(err) {
				log.Printf("admin: delete exam file error: %v", err)
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
		oldFilePath := "" // set when a replacement PDF is uploaded (cleanup target)
		filename := ""    // set when a replacement PDF is uploaded (audit detail)

		// Beda-beda: "tidak ada file" (rename-only) vs "multipart gagal
		// dibaca" (mis. body melebihi LimitBodySize). Sebelumnya error parse
		// dianggap "tidak ada file", sehingga ganti PDF yang gagal diam-diam
		// hanya mengganti nama dan membalas sukses — sekarang jadi error keras.
		if fileErr != nil && !errors.Is(fileErr, http.ErrMissingFile) {
			errorResponse(c, http.StatusBadRequest, "Gagal membaca file PDF — periksa ukuran dan format file")
			return
		}

		if fileErr == nil && header != nil {
			defer file.Close()

			// Tolak file terlalu besar sebelum buffering ke RAM (header.Size
			// diketahui tanpa membaca isi part); validatePDF mengecek ulang
			// setelah read sebagai lapis kedua.
			if header.Size > maxFileSize {
				maxMB := maxFileSize / (1024 * 1024)
				errorResponse(c, http.StatusBadRequest, fmt.Sprintf("Ukuran file melebihi batas %dMB", maxMB))
				return
			}

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

			// Quota gates for a PDF replacement, mirroring UploadExam: the
			// shared school pool (every non-super account in a school
			// instansi, the operator included) and the owner's own per-account
			// limits (sub-accounts only). Both run BEFORE the new PDF reaches
			// R2, so a rejected replacement never uploads an object in the
			// first place. Storage is a DELTA check: the old PDF's bytes leave
			// the pool when the row is updated, so only the growth counts.
			if !isSuperAdmin(c) {
				_, poolMaxPDF, _, poolMaxStorage, poolInstansi, poolActive := schoolPoolQuota(ctx, pool, exam.CreatedBy)
				if poolActive && poolMaxPDF > 0 && int64(len(fileData)) > poolMaxPDF {
					limitMB := roundTo(float64(poolMaxPDF)/(1024*1024), 2)
					errorResponse(c, http.StatusForbidden,
						fmt.Sprintf("Ukuran file melebihi batas paket sekolah (%.2fMB). Silakan hubungi Super Admin.", limitMB))
					return
				}
				if poolActive && poolMaxStorage > 0 {
					poolUsed, err := models.SumStorageByInstansi(ctx, pool, poolInstansi)
					if err == nil && poolUsed-int64(exam.SizeBytes)+int64(len(fileData)) > poolMaxStorage {
						limitMB := roundTo(float64(poolMaxStorage)/(1024*1024), 1)
						errorResponse(c, http.StatusForbidden,
							fmt.Sprintf("Batas kapasitas storage sekolah tercapai. Batas paket sekolah adalah %.1f MB.", limitMB))
						return
					}
				}
				owner, err := models.GetUserByID(ctx, pool, exam.CreatedBy)
				if err == nil && !isOperator(c) && !(owner.OperatorCreated && poolActive) {
					// School-pool sub-accounts: when the pool is active it is the
					// account's quota (schoolPoolCovers) — the per-account PDF/
					// storage columns must not gate below it.
					if owner.MaxPDFSize > 0 && len(fileData) > owner.MaxPDFSize {
						limitMB := roundTo(float64(owner.MaxPDFSize)/(1024*1024), 2)
						errorResponse(c, http.StatusForbidden,
							fmt.Sprintf("Ukuran file melebihi batas akun Anda (%.2fMB). Silakan hubungi Super Admin.", limitMB))
						return
					}
					if owner.MaxStorageSize > 0 {
						var used int64
						err := pool.QueryRow(ctx,
							`SELECT COALESCE(SUM(size_bytes), 0) FROM exams WHERE created_by = $1`, exam.CreatedBy).Scan(&used)
						if err == nil && used-int64(exam.SizeBytes)+int64(len(fileData)) > owner.MaxStorageSize {
							limitMB := roundTo(float64(owner.MaxStorageSize)/(1024*1024), 1)
							errorResponse(c, http.StatusForbidden,
								fmt.Sprintf("Batas kapasitas storage tercapai. Batas akun Anda adalah %.1f MB.", limitMB))
							return
						}
					}
				}
			}

			// Save new file to R2 (Mandatory) FIRST, so a failed replacement
			// never destroys the still-referenced old file: the old R2 object is
			// deleted only after the new one is confirmed uploaded.
			oldFilePath = exam.FilePath
			filename = newExamObjectName(header.Filename)

			if r2c, exists := c.Get("r2"); exists {
				client := r2client.FromContext(r2c)
				// Reject a missing OR disabled backend with a clear message
				// (mirrors UploadSystemApp / UploadExam).
				if client == nil || !client.Enabled() {
					errorResponseWithCode(c, http.StatusInternalServerError, r2client.ErrCodeNotConfigured, r2client.ErrMsgNotConfigured)
					return
				}
				r2Key := fmt.Sprintf("pdfs/%s", filename)
				if err := client.UploadBytes(ctx, r2Key, fileData); err != nil {
					log.Printf("admin: R2 upload error: %v", err)
					errorResponseWithCode(c, http.StatusInternalServerError, r2client.ErrCodeUploadFailed, r2client.ErrMsgUploadFailed)
					return
				}
				log.Printf("admin: PDF uploaded to R2: %s", r2Key)
			} else {
				errorResponseWithCode(c, http.StatusInternalServerError, r2client.ErrCodeNotConfigured, r2client.ErrMsgNotConfigured)
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
			// PDF pengganti sudah ter-upload ke R2 tapi tidak ada baris yang
			// mereferensikannya: hapus object orphan agar edit yang gagal tidak
			// membocorkan storage (meniru cleanupR2Orphan di jalur UploadExam
			// untuk kegagalan pasca-upload).
			if filename != "" {
				cleanupR2Orphan(c, ctx, filename)
			}
			errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui ujian")
			return
		}

		// The update committed — the exam now points at the new file. Only now
		// are the OLD artifacts cleaned up (best-effort): deleting them before
		// UpdateExam would leave the row referencing files that a failed DB
		// update had already destroyed.
		if fileErr == nil && header != nil {
			// Delete old file from R2 (Mandatory, best-effort): only touch an
			// enabled backend (mirrors BulkDelete/DeleteUser/DeleteExam).
			if r2c, exists := c.Get("r2"); exists {
				client := r2client.FromContext(r2c)
				if client != nil && client.Enabled() {
					oldR2Key := fmt.Sprintf("pdfs/%s", oldFilePath)
					if err := client.Delete(ctx, oldR2Key); err != nil {
						log.Printf("admin: R2 delete old file error: %v", err)
					}
				}
			}

			// Clean up the old local storage file (same guard as the delete
			// paths): replacing the PDF must not leave its old file occupying
			// disk forever — otherwise FreeDiskSpace never recovers and the
			// storage quota checks keep counting the ghost file. Path traversal
			// is rejected by SafeStoragePath. Skipped on legacy rows without a
			// stored file_path (an empty path would resolve SafeStoragePath to
			// the storage dir itself).
			if oldFilePath != "" {
				storageDir := getStoragePath(c)
				if fp, err := helpers.SafeStoragePath(storageDir, oldFilePath); err == nil {
					if err := os.Remove(fp); err != nil && !os.IsNotExist(err) {
						log.Printf("admin: delete old exam file error: %v", err)
					}
				}
			}

			// Append-only audit trail: who replaced the PDF and when (with the
			// old → new file names), so the "Riwayat Audit" panel shows the
			// change. Written only after the update committed and the old
			// artifacts were cleaned up; best-effort — a failed audit row must
			// not fail the edit itself.
			detail := fmt.Sprintf("PDF baru diunggah: %s", filename)
			if oldFilePath != "" {
				detail = fmt.Sprintf("PDF diganti: %s -> %s", oldFilePath, filename)
			}
			if err := models.CreateAdminAuditLog(ctx, pool, getCurrentUserID(c), getCurrentUsername(c),
				models.ActionExamPDFReplaced, examID, detail); err != nil {
				log.Printf("audit exam pdf replaced: %v", err)
			}
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

		// Serve PDF via Cloudflare R2 signed URL (Mandatory): only an ENABLED
		// backend may sign (mirrors BulkDelete/DeleteUser/download guards).
		if r2c, exists := c.Get("r2"); exists {
			client := r2client.FromContext(r2c)
			if client != nil && client.Enabled() {
				r2Key := fmt.Sprintf("pdfs/%s", exam.FilePath)
				signedURL, err := client.SignedURL(ctx, r2Key, 1*time.Hour)
				if err == nil {
					c.Redirect(http.StatusFound, signedURL)
					return
				}
				log.Printf("admin: R2 signed URL error: %v", err)
				// Backend IS enabled but signing failed: report the distinct
				// SIGNED_URL_FAILED cause (not NOT_CONFIGURED) so clients can
				// branch on the exact failure — mirrors the api ExamPDF path.
				errorResponseWithCode(c, http.StatusInternalServerError, r2client.ErrCodeSignURLFailed, r2client.ErrMsgSignURLFailed)
				return
			}
		}

		errorResponseWithCode(c, http.StatusInternalServerError, r2client.ErrCodeNotConfigured, r2client.ErrMsgNotConfigured)
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
			"success":      true,
			"message":      fmt.Sprintf("Kunci jawaban berhasil %s untuk siswa", statusStr),
			"show_answers": newVal,
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

		// Schedule — stored as UTC, display in WIB
		jakartaLoc := jakartaLocation()
		startTime := ""
		if exam.StartTime != nil {
			startTime = exam.StartTime.In(jakartaLoc).Format("2006-01-02 15:04")
		}
		endTime := ""
		if exam.EndTime != nil {
			endTime = exam.EndTime.In(jakartaLoc).Format("2006-01-02 15:04")
		}

		isOp := isOperator(c)
		isSuper := isSuperAdmin(c)
		if isOp || isSuper {
			// Pengawas assignments — only for operator/superadmin
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
				"congrats_message":   exam.CongratsMessage,
				"token":              exam.Token,
				"public_results":     exam.PublicResults,
				"show_answers":       exam.ShowAnswers,
				"assigned_pengawas":  assignedPengawas,
				"available_pengawas": availablePengawas,
			})
		} else {
			c.JSON(http.StatusOK, gin.H{
				"success":          true,
				"questions":        questions,
				"security_level":   securityLevel,
				"strict_mode":      exam.StrictMode != 0,
				"identity_fields":  identityFields,
				"panel_color":      panelColor,
				"start_time":       startTime,
				"end_time":         endTime,
				"congrats_message": exam.CongratsMessage,
				"token":            exam.Token,
				"public_results":   exam.PublicResults,
				"show_answers":     exam.ShowAnswers,
			})
		}
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
			Questions       []map[string]interface{} `json:"questions"`
			SecurityLevel   string                   `json:"security_level"`
			IdentityFields  []map[string]interface{} `json:"identity_fields"`
			PanelColor      string                   `json:"panel_color"`
			StartTime       string                   `json:"start_time"`
			EndTime         string                   `json:"end_time"`
			CongratsMessage string                   `json:"congrats_message"`
			PengawasIDs     []int                    `json:"pengawas_ids"`
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

		// Parse schedule as WIB (Asia/Jakarta), convert to UTC for storage
		jakartaLoc := jakartaLocation()
		startTime := body.StartTime
		endTime := body.EndTime
		var startTimePtr *string
		var endTimePtr *string
		if startTime != "" {
			t, err := time.ParseInLocation("2006-01-02 15:04", startTime, jakartaLoc)
			if err != nil {
				// Jangan telan input tak valid diam-diam — guru harus tahu
				// jadwalnya tidak tersimpan.
				errorResponse(c, http.StatusBadRequest, "Format jadwal mulai tidak valid — gunakan format: YYYY-MM-DD HH:MM")
				return
			}
			utc := t.UTC().Format("2006-01-02T15:04:05Z")
			startTimePtr = &utc
		}
		if endTime != "" {
			t, err := time.ParseInLocation("2006-01-02 15:04", endTime, jakartaLoc)
			if err != nil {
				errorResponse(c, http.StatusBadRequest, "Format jadwal selesai tidak valid — gunakan format: YYYY-MM-DD HH:MM")
				return
			}
			utc := t.UTC().Format("2006-01-02T15:04:05Z")
			endTimePtr = &utc
		}

		// Custom congratulations message: free text (not HTML), trimmed; an
		// empty value is stored as NULL so the Android app falls back to its
		// default wording.
		var congratsMessage *string
		if msg := strings.TrimSpace(body.CongratsMessage); msg != "" {
			congratsMessage = &msg
		}

		if err := models.UpdateExamQuestions(ctx, pool, examID,
			&questionsJSON, &securityLevel, &identityFieldsJSON,
			&panelColor, startTimePtr, endTimePtr, congratsMessage, strictMode); err != nil {
			log.Printf("save questions error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan konfigurasi soal")
			return
		}

		// Save pengawas assignments
		userID := getCurrentUserID(c)
		isOp := isOperator(c)
		if isOp || isSuperAdmin(c) {
			// Operator/superadmin manage the pengawas roster via the delegate
			// modal, which sends pengawas_ids. Only replace the roster when the
			// field is actually present.
			if body.PengawasIDs != nil {
				if err := models.SetPengawasForExam(ctx, pool, examID, body.PengawasIDs); err != nil {
					log.Printf("save pengawas error: %v", err)
				}
			}
		} else {
			// Guru: ensure the creator can supervise, but do NOT wipe the
			// existing roster (e.g. pengawas assigned by an operator). The guru
			// questions editor does not manage pengawas.
			if err := models.CreateExamPengawas(ctx, pool, examID, userID); err != nil {
				log.Printf("ensure creator pengawas error: %v", err)
			}
		}

		// Recalculate scores for existing submissions (background context,
		// not the HTTP request context which may be cancelled).
		go recalculateScores(context.Background(), pool, examID)

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

		newToken := helpers.GenerateExamToken()
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
// 11b. POST /admin/api/exams/:exam_id/token-mode — Set token mode (static/dynamic)
// ---------------------------------------------------------------------------

func UpdateTokenMode() gin.HandlerFunc {
	return func(c *gin.Context) {
		examID, err := strconv.Atoi(c.Param("exam_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID ujian tidak valid")
			return
		}

		var body struct {
			TokenMode     string `json:"token_mode"`
			ResetInterval *int   `json:"reset_interval"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			errorResponse(c, http.StatusBadRequest, "Data tidak valid")
			return
		}

		if body.TokenMode != "static" && body.TokenMode != "dynamic" {
			errorResponse(c, http.StatusBadRequest, "Mode token tidak valid")
			return
		}

		var interval *int
		if body.TokenMode == "dynamic" {
			if body.ResetInterval == nil || *body.ResetInterval < 1 {
				errorResponse(c, http.StatusBadRequest, "Interval reset harus diisi (minimal 1 menit)")
				return
			}
			interval = body.ResetInterval
		} else {
			interval = nil
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		if !checkExamOwnership(c, pool, examID) {
			errorResponse(c, http.StatusForbidden, "Akses ditolak")
			return
		}

		if err := models.UpdateExamTokenMode(ctx, pool, examID, body.TokenMode, interval); err != nil {
			log.Printf("update token mode error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui mode token")
			return
		}

		successMessage(c, "Mode token berhasil diperbarui")
	}
}

// StartExam marks an exam as started (sets exam_started_at timestamp).
// POST /admin/api/exams/:exam_id/start
func StartExam() gin.HandlerFunc {
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

		if exam.ExamStartedAt != nil {
			errorResponse(c, http.StatusBadRequest, "Ujian sudah dimulai")
			return
		}

		// Enforce the concurrent-exam quota: starting an exam is what makes it
		// "running" for students, so this is the primary enforcement point. Two
		// layers: the account's own max_concurrent_exams (sub-accounts only;
		// the operator's own account bypasses its own limits, mirroring the
		// upload bypass) and — when the school runs a package — the shared
		// school pool, which counts EVERY running exam in the instansi, the
		// operator's included.
		//
		// Atomic when any limit applies: the count + start run inside one
		// transaction with the school pool's (or the owner's) rows locked
		// FOR UPDATE, so two concurrent starts can never both count the same
		// free slot (check-then-update race). Super admins bypass everything.
		started := false
		if !isSuperAdmin(c) {
			owner, err := models.GetUserByID(ctx, pool, exam.CreatedBy)
			if err == nil {
				_, _, poolMaxConcurrent, _, poolInstansi, poolActive := schoolPoolQuota(ctx, pool, exam.CreatedBy)
				// School-pool sub-accounts: when the pool is active it is the
				// account's ONLY concurrent quota (schoolPoolCovers), so the
				// per-account max_concurrent_exams must not gate below it.
				perUserLimit := !isOperator(c) && !(owner.OperatorCreated && poolActive) && owner.MaxConcurrentExams > 0
				poolConcActive := poolActive && poolMaxConcurrent > 0

				if perUserLimit || poolConcActive {
					tx, err := pool.Begin(ctx)
					if err != nil {
						log.Printf("start begin tx error: %v", err)
						errorResponse(c, http.StatusInternalServerError, "Gagal memulai ujian")
						return
					}
					defer func() { _ = tx.Rollback(ctx) }()

					if poolConcActive {
						// Lock the whole instansi's account rows: concurrent
						// starts by ANY account in the school serialize on the
						// shared pool.
						if _, err := tx.Exec(ctx, `SELECT id FROM admin_users WHERE instansi = $1 FOR UPDATE`, poolInstansi); err != nil {
							log.Printf("start lock school pool error: %v", err)
							errorResponse(c, http.StatusInternalServerError, "Gagal memulai ujian")
							return
						}
						running, err := models.CountRunningExamsByInstansi(ctx, tx, poolInstansi, examID)
						if err == nil && running >= int(poolMaxConcurrent) {
							_ = tx.Rollback(ctx)
							errorResponse(c, http.StatusForbidden,
								fmt.Sprintf("Batas ujian serentak sekolah tercapai. Maksimal %d ujian sekolah dapat berjalan bersamaan.", poolMaxConcurrent))
							return
						}
					}
					if perUserLimit {
						var locked int
						if err := tx.QueryRow(ctx, `SELECT max_concurrent_exams FROM admin_users WHERE id = $1 FOR UPDATE`, exam.CreatedBy).Scan(&locked); err != nil {
							log.Printf("start lock owner error: %v", err)
							errorResponse(c, http.StatusInternalServerError, "Gagal memulai ujian")
							return
						}
						running, err := models.CountRunningExams(ctx, tx, exam.CreatedBy, examID)
						if err == nil && running >= locked {
							_ = tx.Rollback(ctx)
							errorResponse(c, http.StatusForbidden,
								fmt.Sprintf("Batas ujian serentak tercapai. Maksimal %d ujian dapat berjalan bersamaan.", locked))
							return
						}
					}

					if err := models.StartExamTx(ctx, tx, examID); err != nil {
						log.Printf("start exam tx error: %v", err)
						errorResponse(c, http.StatusInternalServerError, "Gagal memulai ujian")
						return
					}
					// Saat mulai ujian, gunakan token permanen sebagai active_token awal.
					// Untuk mode dynamic, token ini baru akan di-rotate/regenerasi setelah interval waktu terlewati.
					if err := models.UpdateExamActiveTokenTx(ctx, tx, examID, exam.Token); err != nil {
						log.Printf("start exam: set active token to permanent error: %v", err)
					}
					if err := tx.Commit(ctx); err != nil {
						log.Printf("start commit error: %v", err)
						errorResponse(c, http.StatusInternalServerError, "Gagal memulai ujian")
						return
					}
					started = true
				}
			}
		}

		if !started {
			if err := models.StartExam(ctx, pool, examID); err != nil {
				log.Printf("start exam error: %v", err)
				errorResponse(c, http.StatusInternalServerError, "Gagal memulai ujian")
				return
			}

			// Saat mulai ujian, gunakan token permanen sebagai active_token awal.
			// Untuk mode dynamic, token ini baru akan di-rotate/regenerasi setelah interval waktu terlewati.
			if err := models.UpdateExamActiveToken(ctx, pool, examID, exam.Token); err != nil {
				log.Printf("start exam: set active token to permanent error: %v", err)
			}
		}

		successMessage(c, "Ujian berhasil dimulai")
	}
}

// StopExam marks an exam as inactive and stops supervision.
// POST /admin/api/exams/:exam_id/stop
func StopExam() gin.HandlerFunc {
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

		if err := models.StopExam(ctx, pool, examID); err != nil {
			log.Printf("stop exam error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menghentikan ujian")
			return
		}

		if hubVal, exists := c.Get("ws_hub"); exists && hubVal != nil {
			if hub, ok := hubVal.(*websocket.Hub); ok {
				hub.BroadcastToRoom(strconv.Itoa(examID), "exam_terminated", map[string]interface{}{
					"exam_id": examID,
					"message": "Ujian dihentikan oleh pengawas",
				})
			}
		}

		successMessage(c, "Pengawasan berhasil dihentikan, ujian dinonaktifkan")
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

		// For non-super users, filter to only exams they may manage (own or
		// delegated, or — for operators — within their own instansi). Operators
		// previously bypassed this filter entirely and could delete any
		// tenant's exams; pengawas-only assignments do not grant deletion.
		examIDs := body.IDs
		if !isSuper {
			filtered, err := models.FilterAccessibleExamIDs(ctx, pool, userID, examIDs)
			if err != nil {
				errorResponse(c, http.StatusInternalServerError, "Gagal memverifikasi kepemilikan")
				return
			}
			examIDs = filtered
			if len(examIDs) == 0 {
				errorResponse(c, http.StatusBadRequest, "Tidak ada ujian yang dapat dihapus")
				return
			}
		}

		// Append-only audit trail: one row per deleted exam, written BEFORE the
		// rows are removed (exam_id becomes NULL via ON DELETE SET NULL; detail
		// keeps each exam's name). Best-effort: failed audit rows never block
		// the bulk delete.
		if nameRows, err := pool.Query(ctx, `SELECT id, name FROM exams WHERE id = ANY($1)`, examIDs); err == nil {
			for nameRows.Next() {
				var eid int
				var ename string
				if err := nameRows.Scan(&eid, &ename); err == nil {
					detail := fmt.Sprintf("Ujian dihapus: %s", ename)
					if err := models.CreateAdminAuditLog(ctx, pool, userID, getCurrentUsername(c),
						models.ActionExamDeleted, eid, detail); err != nil {
						log.Printf("audit bulk exam deleted: %v", err)
					}
				}
			}
			if err := nameRows.Err(); err != nil {
				log.Printf("bulk delete audit names iteration error: %v", err)
			}
			nameRows.Close()
		} else {
			log.Printf("bulk delete audit names query error: %v", err)
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
			if fp, err := helpers.SafeStoragePath(storageDir, p); err == nil {
				os.Remove(fp)
			}
		}

		// Delete from R2 if configured. FromContext can return a nil interface
		// (key present but not a Client, e.g. a disabled backend) — Enabled()
		// only guards a typed-nil receiver, so check client != nil first.
		if r2c, exists := c.Get("r2"); exists {
			client := r2client.FromContext(r2c)
			if client != nil && client.Enabled() {
				for _, p := range paths {
					r2Key := fmt.Sprintf("pdfs/%s", p)
					if err := client.Delete(ctx, r2Key); err != nil {
						log.Printf("admin: R2 bulk delete error for %s: %v", r2Key, err)
					}
				}
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
				"current_owner":         currentOwner,
				"delegated_to":          delegatedTo,
				"available_gurus":       availableGurus,
				"available_pengawas":    availablePengawas,
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
					errorResponse(c, http.StatusInternalServerError, "Gagal mengatur Guru")
					return
				}
			} else {
				// Remove delegation
				if err := models.DelegateExam(ctx, pool, examID, nil); err != nil {
					log.Printf("delegate exam remove error: %v", err)
					errorResponse(c, http.StatusInternalServerError, "Gagal menghapus Guru")
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

		// For non-super users, filter to only exams they may manage. Operators
		// previously bypassed this and could toggle any tenant's exams;
		// pengawas-only assignments do not grant status changes.
		examIDs := body.IDs
		if !isSuper {
			filtered, err := models.FilterAccessibleExamIDs(ctx, pool, userID, examIDs)
			if err != nil {
				errorResponse(c, http.StatusInternalServerError, "Gagal memverifikasi kepemilikan")
				return
			}
			examIDs = filtered
			if len(examIDs) == 0 {
				errorResponse(c, http.StatusBadRequest, "Tidak ada ujian yang dapat diperbarui")
				return
			}
		}

		// Enforce the concurrent-exam quota for bulk activation: selected exams
		// that have already been started (exam_started_at set) would become
		// running once activated, so they count against the owner's quota and —
		// when the school runs a package — the shared school pool (which
		// counts EVERY running exam in the instansi, the operator's included).
		if body.Status == "active" && !isSuper && len(examIDs) > 0 {
			if !isOp {
				counts, err := models.RunningExamCountsAfterActivation(ctx, pool, examIDs)
				if err == nil {
					for ownerID, after := range counts {
						owner, err := models.GetUserByID(ctx, pool, ownerID)
						// School-pool sub-accounts are gated by the shared pool check
						// below, never by their own (forced free) concurrent column.
						if err != nil || owner.MaxConcurrentExams <= 0 || schoolPoolCovers(ctx, pool, owner) {
							continue
						}
						// `after > max` (not >=) matches the single-exam ToggleExam
						// semantics: reaching the limit is allowed, only exceeding it is
						// rejected.
						if after > owner.MaxConcurrentExams {
							errorResponse(c, http.StatusForbidden,
								fmt.Sprintf("Batas ujian serentak tercapai. Maksimal %d ujian dapat berjalan bersamaan.", owner.MaxConcurrentExams))
							return
						}
					}
				}
			}
			// School pool: applies to EVERY non-super user, the operator
			// included.
			countsByInst, err := models.RunningExamCountsAfterActivationByInstansi(ctx, pool, examIDs)
			if err == nil {
				for instansi, after := range countsByInst {
					_, _, poolMaxConcurrent, _, poolActive := schoolPoolQuotaForInstansi(ctx, pool, instansi)
					if !poolActive || poolMaxConcurrent <= 0 {
						continue
					}
					if after > int(poolMaxConcurrent) {
						errorResponse(c, http.StatusForbidden,
							fmt.Sprintf("Batas ujian serentak sekolah tercapai. Maksimal %d ujian sekolah dapat berjalan bersamaan.", poolMaxConcurrent))
						return
					}
				}
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
