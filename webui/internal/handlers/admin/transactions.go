package admin

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/models"
	"github.com/examvan/webui/internal/payment"
)

// CalculatePackagePrice calculates pricing based on penawaran.md (halved pricing)
func CalculatePackagePrice(pkgName, durationType string) int64 {
	switch pkgName {
	case "guru":
		switch durationType {
		case "bulanan":
			return 25000
		case "semester":
			return 125000
		case "tahunan":
			return 225000
		}
	case "individu":
		switch durationType {
		case "bulanan":
			return 50000
		case "semester":
			return 250000
		case "tahunan":
			return 450000
		}
	case "sekolah_kecil":
		switch durationType {
		case "bulanan":
			return 75000
		case "semester":
			return 375000
		case "tahunan":
			return 675000
		}
	case "sekolah_menengah":
		switch durationType {
		case "bulanan":
			return 175000
		case "semester":
			return 875000
		case "tahunan":
			return 1575000
		}
	case "sekolah_besar":
		switch durationType {
		case "bulanan":
			return 375000
		case "semester":
			return 1875000
		case "tahunan":
			return 3375000
		}
	case "sekolah_unggulan":
		switch durationType {
		case "bulanan":
			return 750000
		case "semester":
			return 3750000
		case "tahunan":
			return 6750000
		}
	}
	return 0
}

// ListTransactions handles GET /admin/api/transactions.
// Normal users see only their own transactions. Superadmin sees all.
func ListTransactions() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		role := getCurrentUserRole(c)
		ctx := c.Request.Context()

		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "10"))
		status := c.Query("status")

		opts := models.ListTransactionsOpts{
			Page:    page,
			PerPage: perPage,
			Status:  status,
		}

		// Non-superadmin can only see their own transactions
		if role != models.RoleSuperAdmin {
			opts.UserID = userID
		}

		result, err := models.ListTransactions(ctx, pool, opts)
		if err != nil {
			log.Printf("list transactions error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat transaksi")
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success":      true,
			"transactions": result.Transactions,
			"pagination": gin.H{
				"page":        result.Page,
				"per_page":    result.PerPage,
				"total":       result.Total,
				"total_pages": result.TotalPages,
			},
		})
	}
}

// CreateTransaction handles POST /admin/api/transactions (upload proof of payment).
func CreateTransaction(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		ctx := c.Request.Context()

		// Get form parameters
		pkgName := strings.TrimSpace(c.PostForm("package"))
		durationType := strings.TrimSpace(c.PostForm("duration_type")) // bulanan, semester, tahunan
		notes := strings.TrimSpace(c.PostForm("notes"))

		if pkgName == "" || durationType == "" {
			errorResponse(c, http.StatusBadRequest, "Paket dan durasi wajib ditentukan")
			return
		}

		// Calculate pricing based on penawaran.md (halved pricing)
		amount := CalculatePackagePrice(pkgName, durationType)
		if amount == 0 {
			errorResponse(c, http.StatusBadRequest, "Paket atau jenis durasi tidak valid")
			return
		}

		// File upload handling for proof of payment
		file, err := c.FormFile("proof")
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "Bukti transfer pembayaran wajib diunggah")
			return
		}

		// Validate file size (max 5MB)
		if file.Size > 5*1024*1024 {
			errorResponse(c, http.StatusBadRequest, "Ukuran file bukti transfer maksimal 5 MB")
			return
		}

		// Validate extension
		ext := strings.ToLower(filepath.Ext(file.Filename))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".pdf" {
			errorResponse(c, http.StatusBadRequest, "Format file tidak didukung (harus JPG, PNG, atau PDF)")
			return
		}

		// Generate unique file name
		filename := fmt.Sprintf("proof_%d_%d%s", userID, time.Now().UnixNano(), ext)
		proofsDir := filepath.Join(cfg.StoragePath, "proofs")

		// Create proofs directory if not exists
		if err := os.MkdirAll(proofsDir, 0755); err != nil {
			log.Printf("failed to create proofs directory: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memproses file bukti transfer")
			return
		}

		destPath := filepath.Join(proofsDir, filename)
		if err := c.SaveUploadedFile(file, destPath); err != nil {
			log.Printf("failed to save uploaded file: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan file bukti transfer")
			return
		}

		// proof_path for DB
		proofPathDB := filepath.Join("proofs", filename)

		// Create transaction row
		tx := &models.Transaction{
			UserID:        userID,
			Package:       pkgName,
			Amount:        amount,
			DurationType:  durationType,
			PaymentMethod: "transfer",
			ProofPath:     proofPathDB,
			Notes:         notes,
		}

		created, err := models.CreateTransaction(ctx, pool, tx)
		if err != nil {
			log.Printf("failed to create transaction row: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan data transaksi")
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Bukti transfer berhasil diunggah dan sedang menunggu persetujuan Admin.",
			"tx_id":   created.ID,
		})
	}
}

// ProcessTransactionApproval approves payment, updates user's package and limits. Shared by admin and Doku webhook.
// Uses SELECT FOR UPDATE inside DB transaction to prevent race conditions.
func ProcessTransactionApproval(ctx context.Context, pool *pgxpool.Pool, txID int, notes string) error {
	// Start DB transaction immediately — everything inside is atomic
	dbTx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("gagal memulai transaksi database: %w", err)
	}
	defer func() {
		_ = dbTx.Rollback(ctx)
	}()

	// Lock & fetch transaction row with FOR UPDATE to prevent concurrent approval
	var tx models.Transaction
	err = dbTx.QueryRow(ctx,
		`SELECT id, user_id, package, amount, duration_type, status, payment_method, proof_path, created_at, updated_at, notes
		FROM transactions WHERE id = $1 FOR UPDATE`,
		txID,
	).Scan(
		&tx.ID, &tx.UserID, &tx.Package, &tx.Amount, &tx.DurationType, &tx.Status,
		&tx.PaymentMethod, &tx.ProofPath, &tx.CreatedAt, &tx.UpdatedAt, &tx.Notes,
	)
	if err != nil {
		return fmt.Errorf("transaksi tidak ditemukan: %w", err)
	}

	if tx.Status != models.TxStatusPending {
		return fmt.Errorf("transaksi sudah diproses sebelumnya")
	}

	// Lock & fetch user inside same transaction
	var user models.AdminUser
	err = dbTx.QueryRow(ctx,
		`SELECT id, username, name, email, role, package, max_exams, max_pdf_size, max_drafts,
		        max_draft_size, max_storage_size, expires_at, status, instansi, instansi_id
		FROM admin_users WHERE id = $1 FOR UPDATE`,
		tx.UserID,
	).Scan(
		&user.ID, &user.Username, &user.Name, &user.Email, &user.Role,
		&user.Package, &user.MaxExams, &user.MaxPDFSize, &user.MaxDrafts,
		&user.MaxDraftSize, &user.MaxStorageSize, &user.ExpiresAt, &user.Status,
		&user.Instansi, &user.InstansiID,
	)
	if err != nil {
		return fmt.Errorf("user tidak ditemukan: %w", err)
	}

	// Calculate expiry days extension
	var days int
	switch tx.DurationType {
	case "bulanan":
		days = 30
	case "semester":
		days = 180
	case "tahunan":
		days = 365
	default:
		days = 30
	}

	var newExpiry time.Time
	// If user already active and not expired, extend from user's current expires_at. Otherwise extend from now.
	if user.ExpiresAt != nil && user.ExpiresAt.After(time.Now()) {
		newExpiry = user.ExpiresAt.AddDate(0, 0, days)
	} else {
		newExpiry = time.Now().AddDate(0, 0, days)
	}

	// Quotas based on penawaran.md
	var exams, pdf, drafts, storage int64
	switch tx.Package {
	case "guru":
		exams = 1
		pdf = 10 * 1024 * 1024
		drafts = 10
		storage = 100 * 1024 * 1024
	case "individu":
		exams = 2
		pdf = 30 * 1024 * 1024
		drafts = 30
		storage = 300 * 1024 * 1024
	case "sekolah_kecil":
		exams = 3
		pdf = 50 * 1024 * 1024
		drafts = 50
		storage = 500 * 1024 * 1024
	case "sekolah_menengah":
		exams = 5
		pdf = 200 * 1024 * 1024
		drafts = 200
		storage = 2000 * 1024 * 1024
	case "sekolah_besar":
		exams = 10
		pdf = 500 * 1024 * 1024
		drafts = 500
		storage = 5000 * 1024 * 1024
	case "sekolah_unggulan":
		exams = 99999
		pdf = 99999 * 1024 * 1024 // virtually unlimited
		drafts = 99999
		storage = 999999 * 1024 * 1024
	default:
		exams = 1
		pdf = 1 * 1024 * 1024
		drafts = 1
		storage = 50 * 1024 * 1024
	}

	// Extra safety: only approve if still pending (atomic check + update)
	result, err := dbTx.Exec(ctx,
		`UPDATE transactions SET status = $1, notes = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3 AND status = $4`,
		models.TxStatusApproved, notes, txID, models.TxStatusPending,
	)
	if err != nil {
		return fmt.Errorf("gagal memperbarui transaksi: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("transaksi sudah diproses oleh pengguna lain")
	}

	// Upgrade role — sekolah packages get operator privileges
	newRole := ""
	switch tx.Package {
	case "sekolah_kecil", "sekolah_menengah", "sekolah_besar", "sekolah_unggulan":
		newRole = models.SerializeRoles([]string{models.RoleOperator})
	}

	// Update user limits and package
	var updateSQL string
	var updateArgs []interface{}
	if newRole != "" {
		updateSQL = `UPDATE admin_users SET
			package = $1, max_exams = $2, max_pdf_size = $3,
			max_drafts = $4, max_storage_size = $5,
			expires_at = $6, status = 'active', role = $7
			WHERE id = $8`
		updateArgs = []interface{}{tx.Package, exams, pdf, drafts, storage, newExpiry, newRole, tx.UserID}
	} else {
		updateSQL = `UPDATE admin_users SET
			package = $1, max_exams = $2, max_pdf_size = $3,
			max_drafts = $4, max_storage_size = $5,
			expires_at = $6, status = 'active'
			WHERE id = $7`
		updateArgs = []interface{}{tx.Package, exams, pdf, drafts, storage, newExpiry, tx.UserID}
	}
	_, err = dbTx.Exec(ctx, updateSQL, updateArgs...)
	if err != nil {
		return fmt.Errorf("gagal memperbarui kuota paket pengguna: %w", err)
	}

	// Commit
	if err := dbTx.Commit(ctx); err != nil {
		return fmt.Errorf("gagal menyimpan perubahan database: %w", err)
	}

	return nil
}

// ProcessTransactionReversal handles transaction cancellations, refunds, voids, and reversals.
// If the transaction was previously approved, it downgrades the user back to the default 'free' limits and package.
func ProcessTransactionReversal(ctx context.Context, pool *pgxpool.Pool, txID int, newStatus string, notes string) error {
	dbTx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = dbTx.Rollback(ctx)
	}()

	var tx models.Transaction
	err = dbTx.QueryRow(ctx,
		`SELECT id, user_id, package, amount, duration_type, status, created_at, updated_at
		FROM transactions WHERE id = $1 FOR UPDATE`,
		txID,
	).Scan(
		&tx.ID, &tx.UserID, &tx.Package, &tx.Amount, &tx.DurationType, &tx.Status,
		&tx.CreatedAt, &tx.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("transaction not found: %w", err)
	}

	// Only proceed if status is different
	if tx.Status == newStatus {
		return nil
	}

	// Update transaction status
	_, err = dbTx.Exec(ctx,
		`UPDATE transactions SET status = $1, notes = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3`,
		newStatus, notes, txID,
	)
	if err != nil {
		return fmt.Errorf("failed to update transaction status: %w", err)
	}

	// If transaction was previously approved, we need to revert the user's package and limits to 'free'
	if tx.Status == models.TxStatusApproved {
		_, err = dbTx.Exec(ctx,
			`UPDATE admin_users SET
				package = 'free',
				max_exams = 1,
				max_pdf_size = 1048576, -- 1 MB
				max_drafts = 1,
				max_storage_size = 52428800, -- 50 MB
				expires_at = NULL
			WHERE id = $1`,
			tx.UserID,
		)
		if err != nil {
			return fmt.Errorf("failed to downgrade user limits: %w", err)
		}
	}

	return dbTx.Commit(ctx)
}


// ApproveTransaction handles POST /admin/api/transactions/:id/approve (Superadmin only).
// Approves payment, updates user's package and limits.
func ApproveTransaction() gin.HandlerFunc {
	return func(c *gin.Context) {
		txID, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID transaksi tidak valid")
			return
		}

		notes := strings.TrimSpace(c.PostForm("notes"))
		pool := getPool(c)
		ctx := c.Request.Context()

		if err := ProcessTransactionApproval(ctx, pool, txID, notes); err != nil {
			errorResponse(c, http.StatusInternalServerError, err.Error())
			return
		}

		// Fetch transaction and user to display correct info in success message
		tx, errTx := models.GetTransactionByID(ctx, pool, txID)
		if errTx != nil {
			successMessage(c, "Transaksi berhasil disetujui")
			return
		}
		user, errUser := models.GetUserByID(ctx, pool, tx.UserID)
		if errUser != nil {
			successMessage(c, "Transaksi berhasil disetujui")
			return
		}

		expiryStr := "—"
		if user.ExpiresAt != nil {
			expiryStr = user.ExpiresAt.Format("2006-01-02 15:04:05")
		}

		successMessage(c, fmt.Sprintf("Transaksi approved. Akun %s telah diaktifkan ke %s s.d %s", 
			user.Username, tx.Package, expiryStr))
	}
}

// CreateDokuTransaction handles POST /admin/api/transactions/doku.
func CreateDokuTransaction(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		ctx := c.Request.Context()

		pkgName := strings.TrimSpace(c.PostForm("package"))
		durationType := strings.TrimSpace(c.PostForm("duration_type")) // bulanan, semester, tahunan
		notes := strings.TrimSpace(c.PostForm("notes"))

		log.Printf("CreateDokuTransaction Request: pkg=%s, duration=%s, notes=%s", pkgName, durationType, notes)

		if pkgName == "" || durationType == "" {
			errorResponse(c, http.StatusBadRequest, "Paket dan durasi wajib ditentukan")
			return
		}

		amount := CalculatePackagePrice(pkgName, durationType)
		if amount == 0 {
			errorResponse(c, http.StatusBadRequest, "Paket atau jenis durasi tidak valid")
			return
		}

		// Limit 1 pending per (user, package) to prevent invoice flooding
		var existsPending bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS(
				SELECT 1 FROM transactions 
				WHERE user_id = $1 AND package = $2 AND status = 'pending'
			)`,
			userID, pkgName,
		).Scan(&existsPending)
		if err != nil {
			log.Printf("failed to check existing pending transaction: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memproses pembuatan invoice")
			return
		}
		if existsPending {
			errorResponse(c, http.StatusConflict, "Anda sudah memiliki transaksi pending untuk paket ini. Silakan selesaikan pembayaran sebelumnya.")
			return
		}

		// Create transaction row with pending status and DOKU payment method
		tx := &models.Transaction{
			UserID:        userID,
			Package:       pkgName,
			Amount:        amount,
			DurationType:  durationType,
			PaymentMethod: "doku",
			Notes:         notes,
			Status:        models.TxStatusPending,
		}

		created, err := models.CreateTransaction(ctx, pool, tx)
		if err != nil {
			log.Printf("failed to create transaction row for doku: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan data transaksi")
			return
		}

		// Fetch user to supply customer name/email to DOKU
		user, err := models.GetUserByID(ctx, pool, userID)
		if err != nil {
			log.Printf("failed to get user: %v", err)
			errorResponse(c, http.StatusInternalServerError, "User tidak ditemukan")
			return
		}

		customerName := user.Name
		if customerName == "" {
			customerName = user.Username
		}
		customerEmail := user.Email
		if customerEmail == "" {
			customerEmail = "info@examvan.com" // fallback
		}

		// Get baseURL
		scheme := "http"
		if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		baseURL := fmt.Sprintf("%s://%s", scheme, c.Request.Host)
		callbackURL := fmt.Sprintf("%s/admin/billing?status=success", baseURL)
		callbackURLCancel := fmt.Sprintf("%s/admin/billing?status=cancel", baseURL)
		invoiceNumber := fmt.Sprintf("EXAMVAN-TX-%d", created.ID)

		// Check if DOKU settings are configured
		if cfg.DokuClientID == "" || cfg.DokuSecretKey == "" {
			log.Printf("DOKU payment gateway is not fully configured. ClientID: %s, SecretKey: %s", cfg.DokuClientID, cfg.DokuSecretKey)
			errorResponse(c, http.StatusInternalServerError, "Metode pembayaran DOKU sedang tidak tersedia. Silakan hubungi Admin.")
			return
		}

		// Read active payment methods from settings (comma-separated)
		dokuPaymentMethods := models.GetSaasSettingWithDefault(ctx, pool, models.SettingDokuPaymentMethods,
			"VIRTUAL_ACCOUNT_BCA,VIRTUAL_ACCOUNT_MANDIRI,VIRTUAL_ACCOUNT_BRI,VIRTUAL_ACCOUNT_BNI,QRIS,EMONEY_SHOPEEPAY,EMONEY_DANA,EMONEY_OVO,CREDIT_CARD")

		// Initialize DokuClient
		dokuClient := payment.NewDokuClient(cfg.DokuClientID, cfg.DokuSecretKey, cfg.DokuAPIURL)
		redirectURL, err := dokuClient.CreateCheckout(invoiceNumber, amount, callbackURL, callbackURLCancel, customerName, customerEmail, dokuPaymentMethods)
		if err != nil {
			log.Printf("failed to create checkout session with DOKU: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal membuat sesi pembayaran DOKU: "+err.Error())
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success":      true,
			"redirect_url": redirectURL,
			"tx_id":        created.ID,
		})
	}
}

// RejectTransaction handles POST /admin/api/transactions/:id/reject (Superadmin only).
// Uses atomic UPDATE with status check to prevent race conditions.
func RejectTransaction() gin.HandlerFunc {
	return func(c *gin.Context) {
		txID, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID transaksi tidak valid")
			return
		}

		notes := strings.TrimSpace(c.PostForm("notes"))
		pool := getPool(c)
		ctx := c.Request.Context()

		result, err := pool.Exec(ctx,
			`UPDATE transactions SET status = $1, notes = $2, updated_at = CURRENT_TIMESTAMP
			WHERE id = $3 AND status = $4`,
			models.TxStatusRejected, notes, txID, models.TxStatusPending,
		)
		if err != nil {
			log.Printf("reject transaction error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menolak transaksi")
			return
		}

		if result.RowsAffected() == 0 {
			errorResponse(c, http.StatusBadRequest, "Transaksi tidak ditemukan atau sudah diproses sebelumnya")
			return
		}

		successMessage(c, "Bukti pembayaran ditolak")
	}
}

// ServeProofFile serves uploaded payment proof images/PDFs. (Superadmin only for security).
func ServeProofFile(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		filename := filepath.Base(c.Param("filename"))
		filePath := filepath.Join(cfg.StoragePath, "proofs", filename)

		// Check if file exists
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"message": "File bukti pembayaran tidak ditemukan.",
			})
			return
		}

		c.File(filePath)
	}
}

// BillingPage renders GET /admin/billing page.
func BillingPage() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		ctx := c.Request.Context()

		// Get current user package details
		user, err := models.GetUserByID(ctx, pool, userID)
		if err != nil {
			log.Printf("billing page user fetch error: %v", err)
			c.Redirect(http.StatusFound, "/admin/dashboard")
			return
		}

		expiresStr := "Tidak Terbatas"
		if user.ExpiresAt != nil {
			expiresStr = user.ExpiresAt.Format("2006-01-02 15:04:05")
		}

		renderAdminPage(c, "admin/billing.html", gin.H{
			"active_page":          "billing",
			"user_package":         user.Package,
			"user_expires_at":      expiresStr,
			"user_max_exams":       user.MaxExams,
			"user_max_pdf_size_mb": float64(user.MaxPDFSize) / (1024 * 1024),
			"user_max_storage_mb":  float64(user.MaxStorageSize) / (1024 * 1024),
		})
	}
}

