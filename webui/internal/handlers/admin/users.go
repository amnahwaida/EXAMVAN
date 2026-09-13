package admin

import (
	"context"
	cryptoRand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	r2client "github.com/examvan/webui/internal/handlers/r2"
	"github.com/examvan/webui/internal/helpers"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// effectiveBaseRoles returns the user's base roles for display on the users
// page. base_role is empty for fresh and pre-migration accounts (CreateUser
// does not set it, and the boot backfill only syncs package_role), so mirror
// applyRedemptionEntitlement's lazy init: base = current roles minus whatever
// the ACTIVE package grants. This keeps the badge split consistent with what
// the next package activation would compute.
func effectiveBaseRoles(u models.AdminUser) []string {
	base := parsePackageRoles(u.BaseRole)
	if u.BaseRole != "" {
		return base
	}
	pkg := parsePackageRoles(u.PackageRole)
	merged := models.ParseRoles(u.Role)
	out := make([]string, 0, len(merged))
	for _, r := range merged {
		if !containsRole(pkg, r) {
			out = append(out, r)
		}
	}
	return out
}

// reservedInstansiNames lists system instansi bucket names no school may
// claim. "owner" hosts the bootstrap superadmin (EnsureAdminUser); "personal"
// is the shared default bucket and is rejected separately elsewhere.
var reservedInstansiNames = map[string]bool{
	"owner": true,
}

// isReservedInstansiName reports whether the given instansi name is a system
// bucket name (case-insensitive, whitespace-trimmed).
func isReservedInstansiName(name string) bool {
	return reservedInstansiNames[strings.ToLower(strings.TrimSpace(name))]
}

// getInstansiForOperator retrieves the instansi of the current operator user.
// The error is propagated so data-exposing callers can fail CLOSED: an
// operator whose instansi cannot be resolved must never silently fall back to
// an empty scope (which would widen a query to every tenant).
func getInstansiForOperator(ctx context.Context, pool *pgxpool.Pool, userID int) (string, error) {
	var instansi string
	err := pool.QueryRow(ctx,
		`SELECT instansi FROM admin_users WHERE id = $1`, userID).Scan(&instansi)
	if err != nil {
		return "", err
	}
	return instansi, nil
}

// Sub-account free defaults: an operator-created account's OWN per-account
// quota columns are forced to the 'free' package defaults — the same values
// admin_users carries for a fresh self-registered account (schema.sql column
// defaults: 3 exams, 1 MB PDF, 2 concurrent, 50 MB storage) — regardless of
// the quota values in the create request. A sub-account's REAL limits come
// from the school pool while the operator's package is active (the pool gates
// exam/PDF/storage/concurrent at upload time), so these columns are only a
// fallback for the no-pool state (shared "personal" bucket, legacy school
// without an active redemption) — and in that state they must stay at free
// tier: a tampered request must never be able to grant a sub-account a bigger
// per-account quota than the free defaults (e.g. storage 0 = unlimited, or
// PDF/limits above the free row).
const (
	subAccountFreeMaxExams           = 3
	subAccountFreeMaxPDFSize         = 1048576 // 1 MB
	subAccountFreeMaxConcurrentExams = 2
	subAccountFreeMaxStorageSize     = 52428800 // 50 MB
)

// pgUniqueViolation is the PostgreSQL SQLSTATE for a unique-constraint
// violation (duplicate key), surfaced by pgx as *pgconn.PgError with Code ==
// this value.
const pgUniqueViolation = "23505"

// uniqueViolationMessage returns the friendly user-facing message for a
// duplicate-key (Postgres SQLSTATE 23505) violation raised by the admin_users
// INSERT in CreateUser, or "" when the error is not a unique violation on the
// account's username/email. The pre-tx uniqueness checks (GetUserByUsername /
// GetUserByEmail) run BEFORE the transaction, so two concurrent requests for
// the same username/email can both pass them and the loser surfaces here as a
// constraint violation instead. Mapping it back to the exact same 400 message
// keeps the concurrent loser indistinguishable from a sequential duplicate.
// Constraint names are stable schema facts: admin_users.username is declared
// UNIQUE (Postgres names the constraint admin_users_username_key) and email
// uniqueness lives in the partial unique index uq_admin_users_email.
func uniqueViolationMessage(err error) string {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgUniqueViolation {
		return ""
	}
	switch pgErr.ConstraintName {
	case "admin_users_username_key":
		return "Username sudah digunakan"
	case "uq_admin_users_email":
		return "Email sudah terdaftar. Gunakan email lain atau biarkan kosong."
	}
	// An unexpected unique constraint on the account INSERT: fail loud (the
	// caller logs and returns 500) rather than guessing a friendly message.
	return ""
}

// quotaQuerier abstracts a single-query source so loadOperatorAccountQuota
// can run over a pool (billing page display, tests) or over the CREATE
// transaction (CreateUser quota enforcement) with the same definition the two
// can never disagree on the counting rule.
type quotaQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// loadOperatorAccountQuota returns the school package's sub-account quota
// (maxUsers, 0 = unlimited) and the current number of accounts the operator
// may count against it (used, excluding the operator themself).
//
// For a REAL school instansi the quota is the SCHOOL's: the MAX over the
// instansi's operator(s) active redemption snapshots (mirror of
// schoolPoolQuotaForInstansi), so a second operator without a redemption — or
// with a smaller package — can neither widen nor shrink the school's cap, and
// the enforcement can never disagree with the pool that gates exam usage.
// Used counts every account in that instansi except the acting operator.
//
// For the shared "personal" bucket the acting operator's OWN active
// redemption snapshot is the source (a guru who redeemed a school voucher
// without yet setting a real school instansi is still bound by the school
// package they hold) and only the operator's OWN sub-accounts count: scoped
// by created_by (the operator id recorded at creation), so several
// personal-bucket operators no longer count each other's sub-accounts.
// Legacy operator-created rows predating the created_by column (created_by IS
// NULL) cannot be attributed to any specific operator, so they keep the
// conservative shared-bucket fallback (counted against every personal
// operator) — strictly safer than silently ignoring them.
//
// Operators without an active redemption (legacy/imported) fall back to the
// package_settings row for their package label. Self-registered personal
// accounts are not sub-accounts and never consume a school quota. Shared by
// the CreateUser enforcement and the billing page display so the two can
// never disagree. (0, 0, nil) when not applicable.
//
// FAIL-CLOSED on a real database error: only the legitimate "no active
// redemption" outcome (pgx.ErrNoRows) triggers the package_settings
// fallback, so an active snapshot of 0 (intentional unlimited) is never
// overridden — but a transient DB error (connection drop, pool exhaustion)
// returns an error instead of silently treating the quota as unlimited. A
// sub-account quota that quietly becomes 0 on a glitch would let the operator
// create unlimited accounts; the enforcement caller (CreateUser) rejects the
// request with 500 and the billing display simply omits the quota card.
func loadOperatorAccountQuota(ctx context.Context, q quotaQuerier, userID int, isOperator bool, instansi string) (maxUsers, used int64, err error) {
	instansi = strings.TrimSpace(instansi)
	if !isOperator || instansi == "" {
		return 0, 0, nil
	}
	// Shared "personal" bucket: the acting operator's OWN redemption snapshot
	// is the quota source — there is no school pool to key on, and a guru who
	// redeemed a school voucher without yet setting a real school instansi is
	// still bound by the school package they hold. No active redemption
	// (legacy/imported) falls back to the package_settings row for the
	// operator's package label. The used count is scoped per-operator (only
	// the CURRENT operator's own sub-accounts, created_by = userID), so
	// multiple personal-bucket operators never share each other's
	// sub-accounts. Legacy operator-created rows predating the created_by
	// column (operator_created = true, created_by IS NULL) cannot be
	// attributed to any specific operator, so they keep the conservative
	// shared-bucket fallback: counted against every personal operator, never
	// ignored. Self-registered personal accounts (operator_created = false)
	// are NOT sub-accounts and never consume a school quota.
	if strings.EqualFold(instansi, "personal") {
		// MAX() over the operator's active redemptions: an operator with
		// several active packages (e.g. a guru who redeemed multiple vouchers)
		// must get a deterministic cap — a plain first-row read would return
		// an arbitrary one. Mirrors the school path, which also MAXes.
		err = q.QueryRow(ctx, `
			SELECT COALESCE(MAX(max_users), 0) FROM voucher_redemptions
			WHERE user_id = $1 AND is_active`, userID).Scan(&maxUsers)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				// A REAL database error — fail closed, never unlimited.
				return 0, 0, fmt.Errorf("load operator account quota (redemption): %w", err)
			}
			if ferr := loadOperatorPackageFallback(ctx, q, userID, &maxUsers); ferr != nil {
				return 0, 0, ferr
			}
		}
		if maxUsers <= 0 {
			return 0, 0, nil
		}
		_ = q.QueryRow(ctx,
			`SELECT COUNT(*) FROM admin_users
			 WHERE LOWER(instansi) = LOWER($1) AND id <> $2
			   AND (created_by = $2 OR (created_by IS NULL AND operator_created))`,
			instansi, userID).Scan(&used)
		return maxUsers, used, nil
	}

	// Real school instansi: the sub-account quota is the SCHOOL's, not the
	// acting operator's own — every operator of the instansi draws from one
	// shared cap (mirror of schoolPoolQuotaForInstansi, which MAXes the
	// operators' active redemptions for exam/storage/PDF/concurrent). Reading
	// only the acting operator's own active redemption left two gaps: a
	// SECOND operator of the school without an active redemption fell back to
	// its own package label (free → 0 = unlimited) and could create unlimited
	// accounts in the school, and two concurrent operators could each pass
	// the count on their own snapshots. The school-wide MAX over the
	// instansi's ACTIVE operator redemptions makes the enforcement agree with
	// the pool, for every operator. When no operator of the school holds an
	// active redemption (legacy school / imported operators) the acting
	// operator's package_settings row is the fallback, as before.
	var activeOps int64
	err = q.QueryRow(ctx, `
		SELECT COALESCE(MAX(vr.max_users) FILTER (WHERE vr.is_active), 0),
		       COUNT(*) FILTER (WHERE vr.is_active)
		FROM voucher_redemptions vr
		JOIN admin_users u ON u.id = vr.user_id
		WHERE LOWER(u.instansi) = LOWER($1)
		  AND (u.role = 'operator' OR u.role ILIKE '%"operator"%')`, instansi).
		Scan(&maxUsers, &activeOps)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			// A REAL database error — fail closed, never unlimited.
			return 0, 0, fmt.Errorf("load operator account quota (school): %w", err)
		}
		maxUsers, activeOps = 0, 0
	}
	if activeOps == 0 {
		if ferr := loadOperatorPackageFallback(ctx, q, userID, &maxUsers); ferr != nil {
			return 0, 0, ferr
		}
	}
	if maxUsers <= 0 {
		return 0, 0, nil
	}
	_ = q.QueryRow(ctx,
		`SELECT COUNT(*) FROM admin_users WHERE LOWER(instansi) = LOWER($1) AND id <> $2`,
		instansi, userID).Scan(&used)
	return maxUsers, used, nil
}

// loadOperatorPackageFallback resolves max_users from the acting operator's
// package_settings row (its package label) when the operator holds no active
// redemption. FAIL-CLOSED on a real database error; the "no package_settings
// row for this label" outcome (ErrNoRows, anomalous data) stays 0 = unlimited
// exactly like the legacy behavior it replaces.
func loadOperatorPackageFallback(ctx context.Context, q quotaQuerier, userID int, maxUsers *int64) error {
	if ferr := q.QueryRow(ctx, `
		SELECT COALESCE(ps.max_users, 0)
		FROM package_settings ps
		JOIN admin_users u ON u.package = ps.pkg_key
		WHERE u.id = $1`, userID).Scan(maxUsers); ferr != nil {
		if !errors.Is(ferr, pgx.ErrNoRows) {
			return fmt.Errorf("load operator account quota (package fallback): %w", ferr)
		}
		*maxUsers = 0
	}
	return nil
}

// ---------------------------------------------------------------------------
// 1. Kelola User section data (merged into /admin/settings)
// ---------------------------------------------------------------------------

// loadUsersPageData computes the Kelola User section's data dict for the
// merged /admin/settings page. (The standalone /admin/users page was removed;
// its URL now 302-redirects to /admin/settings#users via SettingsRedirect.)
func loadUsersPageData(c *gin.Context) gin.H {
	pool := getPool(c)
	userID := getCurrentUserID(c)

	adminInstansi := ""
	var operatorExpiresAt *string

	if isOperator(c) {
		instansi, err := getInstansiForOperator(c.Request.Context(), pool, userID)
		if err != nil {
			log.Printf("users page: operator instansi lookup error: %v", err)
		}
		adminInstansi = instansi

		user, err := models.GetUserByID(c.Request.Context(), pool, userID)
		if err == nil && user.ExpiresAt != nil {
			s := user.ExpiresAt.Format("2006-01-02 15:04:05")
			operatorExpiresAt = &s
		}
	}

	freeMB := getFreeDiskSpace(getStoragePath(c)) / (1024 * 1024)
	// Preformatted "Sisa disk server: X GB/MB" supaya badge di header
	// Default Paket Pendaftaran langsung terisi saat render (tanpa flash
	// "memuat…"). Format disamakan dengan fmtStorageSize di admin.js.
	storageFreeDisplay := ""
	if freeMB > 0 {
		if freeMB >= 1024 {
			storageFreeDisplay = fmt.Sprintf("Sisa disk server: %.2f GB", freeMB/1024)
		} else {
			storageFreeDisplay = fmt.Sprintf("Sisa disk server: %.0f MB", freeMB)
		}
	}

	return gin.H{
		"admin_instansi":      adminInstansi,
		"operator_expires_at": operatorExpiresAt,
		// Free space on the storage partition (MB), so the Tambah User &
		// Atur Limit forms can cap Maks Storage at what the server disk can
		// actually hold (0 = tidak dapat ditentukan).
		"storage_free_mb":      roundTo(freeMB, 2),
		"storage_free_display": storageFreeDisplay,
	}
}

// ---------------------------------------------------------------------------
// 2. GET /admin/api/users — List users with search + pagination
// ---------------------------------------------------------------------------

// userItem is the JSON shape a single row of the Kelola User list serializes.
// Shared by ListUsers and GetUser (single-account detail) so the two endpoints
// can never disagree on the fields the page renders.
type userItem struct {
	ID                 int      `json:"id"`
	Username           string   `json:"username"`
	Name               string   `json:"name"`
	WhatsappNumber     string   `json:"whatsapp_number"`
	Email              string   `json:"email"`
	Status             string   `json:"status"`
	MaxExams           int      `json:"max_exams"`
	MaxPDFSize         int      `json:"max_pdf_size"`
	MaxConcurrentExams int      `json:"max_concurrent_exams"`
	MaxStorageSize     int64    `json:"max_storage_size"`
	MaxStorageMB       int      `json:"max_storage_mb"`
	Instansi           string   `json:"instansi"`
	Roles              []string `json:"roles"`         // merged role list (base ∪ package)
	BaseRoles          []string `json:"base_roles"`    // roles held independently of packages
	PackageRoles       []string `json:"package_roles"` // roles granted by the active package
	Role               string   `json:"role"`
	ExpiresAt          string   `json:"expires_at"`
	ExamCount          int      `json:"exam_count"`
	CreatedAt          string   `json:"created_at"`
	Package            string   `json:"package"`
	// OperatorCreated exposes the origin flag to the Kelola Users page so
	// it can render the "Dibuat oleh Operator" badge on sub-accounts.
	OperatorCreated bool `json:"operator_created"`
	// HasActivePackage tells the Kelola User page whether the account
	// currently runs an active package — the "Nonaktifkan Paket" action
	// only renders when there is something to deactivate.
	HasActivePackage bool `json:"has_active_package"`
}

func userItemFrom(u models.UserWithExamCount) userItem {
	expStr := ""
	if u.ExpiresAt != nil {
		expStr = u.ExpiresAt.Format("2006-01-02 15:04:05")
	}
	return userItem{
		ID:                 u.ID,
		Username:           u.Username,
		Name:               u.Name,
		WhatsappNumber:     u.WhatsappNumber,
		Email:              u.Email,
		Status:             u.Status,
		MaxExams:           u.MaxExams,
		MaxPDFSize:         u.MaxPDFSize,
		MaxConcurrentExams: u.MaxConcurrentExams,
		MaxStorageSize:     u.MaxStorageSize,
		MaxStorageMB:       int(u.MaxStorageSize / (1024 * 1024)),
		Instansi:           u.Instansi,
		Roles:              models.ParseRoles(u.Role),
		BaseRoles:          effectiveBaseRoles(u.AdminUser),  // lazy-derived when base_role is empty
		PackageRoles:       parsePackageRoles(u.PackageRole), // empty-safe: '' means no package roles
		Role:               models.SerializeRoles(models.ParseRoles(u.Role)),
		ExpiresAt:          expStr,
		ExamCount:          u.ExamCount,
		CreatedAt:          formatISOUTC(u.CreatedAt),
		Package:            u.Package,
		OperatorCreated:    u.OperatorCreated,
		HasActivePackage:   u.HasActivePackage,
	}
}

func ListUsers() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		isOp := isOperator(c)
		ctx := c.Request.Context()

		search := strings.TrimSpace(c.Query("search"))
		roleFilter := strings.TrimSpace(c.Query("role"))
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		if page < 1 {
			page = 1
		}
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "10"))
		if perPage < 5 {
			perPage = 5
		} else if perPage > 200 {
			perPage = 200
		}

		// Column sorting (whitelisted inside models.ListUsers — an unknown
		// sort_by silently falls back to the default role-priority order).
		sortBy := strings.ToLower(strings.TrimSpace(c.Query("sort_by")))
		sortDir := strings.ToUpper(strings.TrimSpace(c.Query("sort_dir")))
		if sortDir != "DESC" {
			sortDir = "ASC"
		}

		opts := models.ListUsersOpts{
			Page:              page,
			PerPage:           perPage,
			Search:            search,
			RoleFilter:        roleFilter,
			ExcludeSuperAdmin: !isSuperAdmin(c),
			ExcludeOperator:   isOp,
			SortBy:            sortBy,
			SortDir:           sortDir,
		}

		if isOp {
			// Fail CLOSED: an operator whose instansi cannot be resolved (DB
			// error) or an anomalous empty-instansi row must not silently drop
			// the scope filter — that would hand the user every tenant's
			// accounts. A healthy operator always has an instansi, so an
			// unresolvable one is a defect, not a data state.
			instansi, err := getInstansiForOperator(ctx, pool, userID)
			if err != nil || instansi == "" {
				log.Printf("list users: operator instansi unresolved (user %d, instansi=%q, err=%v)", userID, instansi, err)
				errorResponse(c, http.StatusInternalServerError, "Gagal memuat daftar user")
				return
			}
			opts.Instansi = instansi
		}

		result, err := models.ListUsers(ctx, pool, opts)
		if err != nil {
			log.Printf("list users error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat daftar user")
			return
		}

		users := make([]userItem, 0, len(result.Users))
		for _, u := range result.Users {
			users = append(users, userItemFrom(u))
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"users":   users,
			"pagination": gin.H{
				"page":        result.Page,
				"per_page":    result.PerPage,
				"total":       result.Total,
				"total_pages": result.TotalPages,
			},
		})
	}
}

// ---------------------------------------------------------------------------
// 2b. GET /admin/api/users/:user_id — single user detail
// ---------------------------------------------------------------------------

// GetUser returns ONE account in the exact JSON shape of a ListUsers row, so
// the Atur User modal loads its form data from the detail endpoint instead of
// paging through the entire list (per_page=1000) to find a single id — a
// request that got slower in lockstep with the account count.
//
// Scope rules mirror EditUser/ListUsers: fail CLOSED on an unresolvable
// operator instansi; an operator may view only its own instansi's accounts
// and never an operator/superadmin account.
func GetUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		targetID, err := strconv.Atoi(c.Param("user_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID user tidak valid")
			return
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		ctx := c.Request.Context()

		targetUser, err := models.GetUserWithExtras(ctx, pool, targetID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "User tidak ditemukan")
			return
		}

		if isOperator(c) {
			opInstansi, err := getInstansiForOperator(ctx, pool, userID)
			if err != nil || opInstansi == "" {
				log.Printf("get user: operator instansi unresolved (user %d, err=%v)", userID, err)
				errorResponse(c, http.StatusInternalServerError, "Gagal memuat data user")
				return
			}
			if targetUser.Instansi != opInstansi {
				errorResponse(c, http.StatusBadRequest, "Anda hanya dapat mengelola user dalam satu instansi yang sama")
				return
			}
			// Mirrors EditUser/ToggleUserStatus: an operator must not manage
			// peer operators (the cascade rules treat operator accounts as
			// school-level, not per-operator).
			if targetUser.IsOperator() {
				errorResponse(c, http.StatusBadRequest, "Operator tidak dapat mengelola akun dengan role Operator")
				return
			}
			if targetUser.Username == models.SuperAdminUsername {
				errorResponse(c, http.StatusBadRequest, "Operator tidak dapat mengelola akun Super Admin")
				return
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"user":    userItemFrom(targetUser),
		})
	}
}

// ---------------------------------------------------------------------------
// 3. POST /admin/api/users — Create new user
// ---------------------------------------------------------------------------

// validateStorageQuota returns an error message when the given per-user storage
// quota (MB) exceeds the free space of the server storage partition, or is
// negative. 0 = tidak terbatas (unlimited) is always allowed; an empty string
// means the value is acceptable. Mirrors the cap enforced on the SaaS "Maks
// Storage" default setting (settings.go). When free space cannot be determined
// (getFreeDiskSpace returns 0), the check is skipped (fail-open).
func validateStorageQuota(c *gin.Context, mb float64) string {
	return validateStorageQuotaFree(getFreeDiskSpace(getStoragePath(c)), mb)
}

// validateStorageQuotaFree is validateStorageQuota against a pre-computed
// freeBytes snapshot, so callers validating many values in one request (e.g.
// a package payload with several packages) reuse a single disk check instead
// of one statfs call per value.
func validateStorageQuotaFree(freeBytes float64, mb float64) string {
	return validateMBQuota("Maks Storage", freeBytes, mb)
}

// validatePDFQuota validates a max-PDF-size quota (MB) against the free disk
// space on the STORAGE_PATH partition, mirroring the storage rules: negative
// rejected, 0 = unlimited, fail-open when the free disk cannot be determined.
func validatePDFQuota(c *gin.Context, mb float64) string {
	return validatePDFQuotaFree(getFreeDiskSpace(getStoragePath(c)), mb)
}

// validatePDFQuotaFree is validatePDFQuota against a pre-computed freeBytes
// snapshot (single disk check for multi-value payloads).
func validatePDFQuotaFree(freeBytes float64, mb float64) string {
	return validateMBQuota("Maks Ukuran PDF", freeBytes, mb)
}

// validateMBQuota is the shared core for MB-based quota caps against the free
// disk space: 0 = unlimited (always accepted), negative rejected, and positive
// values above the free disk rejected (fail-open when freeBytes <= 0, i.e. the
// free disk cannot be determined).
func validateMBQuota(label string, freeBytes float64, mb float64) string {
	if mb < 0 {
		return fmt.Sprintf("%s tidak boleh bernilai negatif.", label)
	}
	if mb == 0 {
		return ""
	}
	if freeBytes > 0 && mb*1024*1024 > freeBytes {
		freeGB := freeBytes / (1024 * 1024 * 1024)
		return fmt.Sprintf("%s (%.2f MB) melebihi sisa kapasitas disk server (%.2f GB).", label, mb, freeGB)
	}
	return ""
}

func CreateUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Username           string   `json:"username"`
			Name               string   `json:"name"`
			Password           string   `json:"password"`
			WhatsappNumber     string   `json:"whatsapp_number"`
			Email              string   `json:"email"`
			Roles              []string `json:"roles"`
			Role               string   `json:"role"` // fallback if Roles is empty
			Instansi           string   `json:"instansi"`
			MaxExams           int      `json:"max_exams"`
			MaxPDFSizeMB       float64  `json:"max_pdf_size_mb"`
			MaxConcurrentExams int      `json:"max_concurrent_exams"`
			MaxStorageSizeMB   float64  `json:"max_storage_size_mb"`
			ExpiresAt          string   `json:"expires_at"`
			Package            string   `json:"package"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			errorResponse(c, http.StatusBadRequest, "Data tidak valid")
			return
		}

		// Storage quota must be validated before the account is created (a
		// rejected quota must not leave a half-created user). Same cap as the
		// SaaS "Maks Storage" default.
		if msg := validateStorageQuota(c, body.MaxStorageSizeMB); msg != "" {
			errorResponse(c, http.StatusBadRequest, msg)
			return
		}

		// PDF upload quota follows the same disk cap (validatePDFQuota):
		// negative rejected, 0 = tidak terbatas, positive value bounded by the
		// free disk.
		if msg := validatePDFQuota(c, body.MaxPDFSizeMB); msg != "" {
			errorResponse(c, http.StatusBadRequest, msg)
			return
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		isOp := isOperator(c)
		ctx := c.Request.Context()

		username := strings.ToLower(strings.TrimSpace(body.Username))
		password := body.Password

		if username == "" || password == "" {
			errorResponse(c, http.StatusBadRequest, "Username dan password wajib diisi")
			return
		}

		// Same minimum the UI enforces (minlength=8 on both create/edit forms,
		// settings hub Kelola User section): the server must not accept weaker
		// passwords from a hand-crafted request.
		if len(password) < 8 {
			errorResponse(c, http.StatusBadRequest, "Password minimal 8 karakter")
			return
		}

		if !models.IsValidUsername(username) {
			errorResponse(c, http.StatusBadRequest, "Username hanya boleh berisi huruf kecil, angka, titik, garis bawah, dan strip (3-32 karakter)")
			return
		}

		if username == models.SuperAdminUsername {
			errorResponse(c, http.StatusBadRequest,
				fmt.Sprintf("Username %s sudah terdaftar sebagai Super Admin", models.SuperAdminUsername))
			return
		}

		// Check uniqueness
		_, err := models.GetUserByUsername(ctx, pool, username)
		if err == nil {
			errorResponse(c, http.StatusBadRequest, "Username sudah digunakan")
			return
		}

		// Validate roles
		roles := body.Roles
		if len(roles) == 0 && body.Role != "" {
			roles = strings.Split(body.Role, ",")
		}
		if len(roles) == 0 {
			roles = []string{models.RoleGuru}
		}

		// All application defaults are read BEFORE any transaction starts:
		// the operator create path holds ONE tx connection (FOR UPDATE on the
		// operator row), so a pool call inside the open tx would need a
		// SECOND connection — once concurrent creates saturate the pool (each
		// holding its tx conn, queued on the row lock) that request would
		// wait forever while the lock holder itself waits for a free conn:
		// a classic pool deadlock observed in production-scale test runs.
		defaultDays := models.GetSaasSettingInt(ctx, pool, models.SettingDefaultActiveDays, 14)
		defaultMaxExams := models.GetSaasSettingInt(ctx, pool, models.SettingDefaultMaxExams, 3)
		defaultMaxPDFSize := models.GetSaasSettingInt(ctx, pool, models.SettingDefaultMaxPDFSize, 1048576)
		defaultMaxConcurrentExams := models.GetSaasSettingInt(ctx, pool, models.SettingDefaultMaxConcurrentExams, 2)

		// Email must be unique across accounts (partial unique index
		// uq_admin_users_email, email <> ''). Checked here so a duplicate form
		// email gets a friendly 400 instead of a raw DB constraint error —
		// mirrors the self-registration path in main.go. Runs before the tx
		// (a pool call inside the open tx would risk the connection
		// deadlock above).
		if em := strings.TrimSpace(body.Email); em != "" {
			existingByEmail, err := models.GetUserByEmail(ctx, pool, em)
			if err == nil && existingByEmail.Email != "" {
				errorResponse(c, http.StatusBadRequest, "Email sudah terdaftar. Gunakan email lain atau biarkan kosong.")
				return
			}
		}

		// Operator restrictions
		var expiresAtPtr *time.Time
		// inheritUnlimited marks an operator whose own expiry is NULL
		// (unlimited — e.g. an admin cleared it in the edit form): its
		// sub-accounts inherit the unlimited state instead of falling back to
		// the default_active_days trial, mirroring the "Force expiry: user
		// expiry = operator's expiry" rule for operators with a concrete expiry.
		inheritUnlimited := false
		instansi := strings.TrimSpace(body.Instansi)
		var opInstansiID *int
		var opInstansiCode *string

		// tx is non-nil ONLY on the operator path: the sub-account quota
		// check (count) and the INSERT must be atomic, and both must see the
		// operator's authoritative instansi/expiry. The operator's own row is
		// locked FOR UPDATE so two concurrent creates serialize on it and can
		// never both pass a full max_users quota (the race the separate
		// count-then-insert used to have). If the lock read fails, the create
		// ABORTS — the body's instansi must never be used as a fallback (a
		// hand-crafted request must not label an account with an instansi the
		// operator does not own).
		var tx pgx.Tx
		if isOp {
			// Operator cannot create operator accounts. Trim + case-insensitive
			// compare so a hand-crafted " operator" / "OPERATOR" / " Operator "
			// (variants that would otherwise fall through the exact-match
			// whitelist below and silently degrade to guru) is rejected with the
			// intended 400 — mirroring the EditUser guard. The whitelist filter
			// keeps storing only exact lowercase role names, so a non-canonical
			// spelling can never be persisted.
			for _, r := range roles {
				if strings.EqualFold(strings.TrimSpace(r), models.RoleOperator) {
					errorResponse(c, http.StatusBadRequest, "Operator tidak dapat membuat akun dengan role Operator")
					return
				}
			}

			var bErr error
			tx, bErr = pool.Begin(ctx)
			if bErr != nil {
				log.Printf("begin operator create tx error: %v", bErr)
				errorResponse(c, http.StatusInternalServerError, "Gagal membuat user")
				return
			}
			defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit

			var opExpiresAt *time.Time
			// instansi_code is read RAW (no COALESCE): a NULL operator code
			// must be inherited as NULL — writing '' would drift from the
			// school's canonical NULL and break "instansi_code IS NULL"
			// comparisons that treat '' as unset (schema.sql migration 511-522).
			err = tx.QueryRow(ctx,
				`SELECT instansi, instansi_id, instansi_code, expires_at
				   FROM admin_users WHERE id = $1 FOR UPDATE`, userID,
			).Scan(&instansi, &opInstansiID, &opInstansiCode, &opExpiresAt)
			if err != nil {
				log.Printf("lock operator row for create error: %v", err)
				errorResponse(c, http.StatusInternalServerError, "Gagal membuat user")
				return
			}
			// Trim the authoritative instansi before ANY use: the quota count
			// and the school pool both compare on the trimmed value, so the
			// INSERT must store the same canonical form. A padded DB value
			// (only possible via direct DB/import — every app path trims)
			// would otherwise label the new account with "SMK X " while the
			// quota/pool count "SMK X": the account would silently escape
			// both the max_users count and the school pool.
			instansi = strings.TrimSpace(instansi)
			// Fail CLOSED on an anomalous empty instansi: a healthy operator
			// always has one (self-registration defaults to "personal", the
			// needs_instansi onboarding forces a real school, and EditUser
			// forces it on the row). If the locked read came back empty, the
			// quota check below would be skipped (loadOperatorAccountQuota
			// returns 0,0 for instansi == "") and the account would silently
			// land in the shared "personal" bucket outside the school quota —
			// the same fail-closed rule ListUsers/EditUser/ToggleUserStatus
			// apply to an unresolvable operator instansi. The body's instansi
			// must never be used as a fallback here.
			if instansi == "" {
				log.Printf("create user: operator instansi unresolved (user %d, empty row)", userID)
				errorResponse(c, http.StatusInternalServerError, "Gagal membuat user")
				return
			}

			// School-wide serialization on top of the operator's own row lock:
			// the sub-account quota is the SCHOOL's (loadOperatorAccountQuota
			// reads the instansi-wide MAX), so concurrent creates by DIFFERENT
			// operators of the same school must serialize too — each operator's
			// FOR UPDATE row lock alone only serializes same-operator creates,
			// and two operators could otherwise both pass the shared count.
			// The advisory lock is transaction-scoped (released at commit /
			// rollback, never leaked) and keyed on the authoritative instansi
			// from the locked read — never the request body. In the shared
			// "personal" bucket the key is scoped PER OPERATOR: the FOR UPDATE
			// row lock already serializes same-operator creates there, so a
			// single global "personal" key would needlessly serialize ALL
			// personal-bucket operators on one lock.
			lockKey := instansi
			if strings.EqualFold(lockKey, "personal") {
				lockKey = fmt.Sprintf("personal:%d", userID)
			}
			if _, aErr := tx.Exec(ctx,
				`SELECT pg_advisory_xact_lock(hashtext('sub-account-quota:' || $1)::bigint)`, lockKey); aErr != nil {
				log.Printf("create user: lock school quota (instansi %q): %v", instansi, aErr)
				errorResponse(c, http.StatusInternalServerError, "Gagal membuat user")
				return
			}

			// NOTE: the operator's email is deliberately NOT copied onto the
			// sub-account. The unique index uq_admin_users_email (email <>
			// '') forbids two accounts sharing a non-empty email, and the
			// operator's own row already holds it — forcing it here made
			// EVERY operator-created account fail with a unique violation.
			// The sub-account keeps the email typed in the form (or none).
			// Force expiry: user expiry = operator's expiry (termasuk status
			// unlimited — operator dengan expires_at NULL membuat akun sub
			// yang unlimited juga, bukan trial default).
			if opExpiresAt != nil {
				expiresAt := *opExpiresAt
				expiresAtPtr = &expiresAt
			} else {
				inheritUnlimited = true
			}
		}

		if instansi == "" {
			instansi = "personal"
		}

		// School sub-account quota (max_users): an operator may only create
		// accounts while the ACTIVE package still has room (see
		// loadOperatorAccountQuota). 0 = unlimited; the count is all accounts in
		// the operator's instansi except the operator themself. Runs inside the
		// operator's locked transaction (see above), so the count and the
		// INSERT below share one snapshot and one lock — no over-quota race.
		if isOp {
			maxUsers, used, qErr := loadOperatorAccountQuota(ctx, tx, userID, true, instansi)
			if qErr != nil {
				// Fail CLOSED: a transient DB error must not become an
				// unlimited quota (loadOperatorAccountQuota returns an error
				// for real failures). The create ABORTS — a quota that cannot
				// be read must never be assumed to have room.
				log.Printf("create user: operator account quota unresolved (user %d, err=%v)", userID, qErr)
				errorResponse(c, http.StatusInternalServerError, "Gagal membuat user")
				return
			}
			if maxUsers > 0 && used >= maxUsers {
				errorResponse(c, http.StatusBadRequest,
					fmt.Sprintf("Kuota akun di instansi Anda telah mencapai batas paket (%d akun). Silakan hubungi administrator untuk menambah kuota.", maxUsers))
				return
			}
		}

		filteredRoles := make([]string, 0, len(roles))
		for _, r := range roles {
			switch r {
			case models.RoleGuru, models.RolePengawas, models.RoleOperator:
				filteredRoles = append(filteredRoles, r)
			}
		}
		if len(filteredRoles) == 0 {
			filteredRoles = []string{models.RoleGuru}
		}

		// One-operator-per-school policy: a SuperAdmin creating an account
		// with the operator role in a school that already has an operator
		// would create a second. The check and the INSERT run in ONE
		// transaction serialized per destination school via the same
		// school-claim advisory lock the claim/redeem/activate paths use —
		// without it, two concurrent SuperAdmin creates into the same empty
		// school would BOTH read the pre-insert snapshot and land two
		// operators. (The operator create path can never reach here —
		// operators are barred from granting the operator role above.) The
		// shared "personal" bucket is never a school: nothing to check, and
		// no serialization needed.
		if !isOp && containsRole(filteredRoles, models.RoleOperator) &&
			instansi != "" && !strings.EqualFold(instansi, "personal") {
			var bErr error
			tx, bErr = pool.Begin(ctx)
			if bErr != nil {
				log.Printf("begin operator-role create tx error: %v", bErr)
				errorResponse(c, http.StatusInternalServerError, "Gagal membuat user")
				return
			}
			defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit

			if _, aErr := tx.Exec(ctx,
				`SELECT pg_advisory_xact_lock(hashtext('school-claim:' || lower($1))::bigint)`, instansi); aErr != nil {
				log.Printf("create user: lock school-claim (instansi %q): %v", instansi, aErr)
				errorResponse(c, http.StatusInternalServerError, "Gagal membuat user")
				return
			}
			hasOp, opErr := schoolAlreadyHasOperator(ctx, tx, instansi, userID)
			if opErr != nil {
				log.Printf("create user: one-operator-per-school check error: %v", opErr)
				errorResponse(c, http.StatusInternalServerError, "Gagal membuat user")
				return
			}
			if hasOp {
				errorResponse(c, http.StatusBadRequest,
					"Instansi ini sudah memiliki operator. Satu sekolah hanya dapat memiliki satu operator.")
				return
			}
		}

		roleStr := models.SerializeRoles(filteredRoles)

		// Default expiry — only apply form value when operator didn't already set it.
		// Operator unlimited (inheritUnlimited) skips both the form value and the
		// default_active_days fallback so the sub-account stays unlimited.
		if expiresAtPtr == nil && !inheritUnlimited {
			expiresAtStr := strings.TrimSpace(body.ExpiresAt)
			if expiresAtStr != "" {
				t, err := time.Parse("2006-01-02 15:04:05", expiresAtStr)
				if err == nil {
					expiresAtPtr = &t
				}
			}
		}
		if expiresAtPtr == nil && !inheritUnlimited {
			t := time.Now().UTC().AddDate(0, 0, defaultDays)
			expiresAtPtr = &t
		}

		maxExams := body.MaxExams
		maxPDFSize := int(body.MaxPDFSizeMB * 1024 * 1024)
		maxConcurrentExams := body.MaxConcurrentExams
		maxStorageSize := int64(body.MaxStorageSizeMB * 1024 * 1024)
		if isOp {
			// Sub-account quota policy: the per-account quota columns of an
			// operator-created account are FORCED to the free defaults (see
			// subAccountFree* constants), regardless of the request body. The
			// sub-account's real limits come from the school pool while the
			// operator's package is active; these columns are only a fallback
			// for the no-pool state (shared "personal" bucket, legacy school),
			// and there they must stay at free tier — a tampered request must
			// never grant a sub-account a bigger per-account quota than the
			// free defaults (e.g. storage 0 = unlimited, or limits above the
			// free row). Mirrors the forced package='free' below.
			maxExams, maxPDFSize, maxConcurrentExams, maxStorageSize =
				subAccountFreeMaxExams, subAccountFreeMaxPDFSize,
				subAccountFreeMaxConcurrentExams, subAccountFreeMaxStorageSize
		} else {
			if maxExams <= 0 {
				maxExams = defaultMaxExams
			}
			if maxPDFSize <= 0 {
				maxPDFSize = defaultMaxPDFSize
			}
			if maxConcurrentExams <= 0 {
				maxConcurrentExams = defaultMaxConcurrentExams
			}
		}

		// Sub-account package policy: an operator may never choose the
		// subscription package of the accounts it creates. Accounts created by
		// an operator (operator_created sub-accounts) cannot run their own
		// package — they cannot redeem/activate vouchers, and their quota,
		// role and expiry all follow the operator's school package — so the
		// package label is forced to "free" (a tampered request can no longer
		// label a sub-account with a paid school package).
		pkg := strings.TrimSpace(body.Package)
		if pkg == "" || isOp {
			pkg = "free"
		}

		user := &models.AdminUser{
			Username:           username,
			Name:               strings.TrimSpace(body.Name),
			PasswordHash:       password, // will be hashed by CreateUser
			Status:             models.UserStatusActive,
			Instansi:           instansi,
			Role:               roleStr,
			MaxExams:           maxExams,
			MaxPDFSize:         maxPDFSize,
			MaxConcurrentExams: maxConcurrentExams,
			MaxStorageSize:     maxStorageSize,
			WhatsappNumber:     strings.TrimSpace(body.WhatsappNumber),
			Email:              strings.TrimSpace(body.Email),
			ExpiresAt:          expiresAtPtr,
			Package:            pkg,
			// Sub-account voucher policy: an account created BY an operator is
			// marked operator_created and can never claim/activate vouchers
			// (origin-based, immutable — see RedeemVoucherHandler).
			OperatorCreated: isOp,
			// CreatedBy records WHICH operator created the account (the precise
			// attribution behind the shared "personal" bucket — see
			// loadOperatorAccountQuota). Set once at creation, never changed.
		}
		if isOp {
			createdByID := userID
			user.CreatedBy = &createdByID
			// The school identity is inherited (instansi_id + instansi_code)
			// INSIDE the same INSERT that creates the account — not a second
			// UPDATE afterwards, whose failure used to be silently swallowed
			// and left the account without a school identity. A NULL operator
			// code is inherited as NULL (see the locked read above).
			user.InstansiID = opInstansiID
			user.InstansiCode = opInstansiCode
		}

		var created *models.AdminUser
		if tx != nil {
			created, err = models.CreateUserTx(ctx, tx, user)
		} else {
			created, err = models.CreateUser(ctx, pool, user)
		}
		if err != nil {
			// A racing duplicate (concurrent same-username/email request that
			// passed the pre-tx checks) lands here as a constraint violation;
			// answer with the same friendly 400 as the sequential duplicate
			// instead of a raw 500. On the operator path the deferred rollback
			// releases the advisory/row locks, so the next create proceeds.
			if msg := uniqueViolationMessage(err); msg != "" {
				errorResponse(c, http.StatusBadRequest, msg)
				return
			}
			log.Printf("create user error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal membuat user")
			return
		}

		// The operator path commits ONLY after the insert succeeded — the
		// FOR UPDATE lock is released at commit, so the next concurrent
		// create sees the new row in its quota count.
		if tx != nil {
			if cErr := tx.Commit(ctx); cErr != nil {
				log.Printf("commit operator create tx error: %v", cErr)
				errorResponse(c, http.StatusInternalServerError, "Gagal membuat user")
				return
			}
		}

		// Append-only audit trail for management actions (mirrors the exam
		// lifecycle audit): WHO created WHICH account, with the new account's
		// identity snapshotted in detail. Written after commit; best-effort.
		// examID = 0 → NULL (this is not an exam action).
		// The detail marks accounts created BY an operator as sub-accounts, so
		// the trail distinguishes them from SuperAdmin-created / self-
		// registered accounts at a glance (the actor username alone would
		// require knowing who is an operator).
		createdDetail := fmt.Sprintf("Akun dibuat: %s (%s), role %s", created.Username, created.Name, created.Role)
		if isOp {
			createdDetail += " (sub-akun, dibuat operator)"
		}
		if aErr := models.CreateAdminAuditLog(ctx, pool, userID, getCurrentUsername(c),
			models.ActionUserCreated, 0, createdDetail); aErr != nil {
			log.Printf("audit user created: %v", aErr)
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": fmt.Sprintf("User %s berhasil dibuat", created.Username),
			"user_id": created.ID,
		})
	}
}

// ---------------------------------------------------------------------------
// 4. POST /admin/api/users/:user_id/edit — Edit user
// ---------------------------------------------------------------------------

func EditUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		targetID, err := strconv.Atoi(c.Param("user_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID user tidak valid")
			return
		}

		var body struct {
			Name               *string  `json:"name"`
			MaxExams           *int     `json:"max_exams"`
			MaxPDFSizeMB       *float64 `json:"max_pdf_size_mb"`
			WhatsappNumber     *string  `json:"whatsapp_number"`
			Email              *string  `json:"email"`
			Status             *string  `json:"status"`
			Password           *string  `json:"password"`
			Instansi           *string  `json:"instansi"`
			Role               *string  `json:"role"`
			Roles              []string `json:"roles"`
			MaxConcurrentExams *int     `json:"max_concurrent_exams"`
			MaxStorageSizeMB   *float64 `json:"max_storage_size_mb"`
			ExpiresAt          *string  `json:"expires_at"`
			Package            *string  `json:"package"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			errorResponse(c, http.StatusBadRequest, "Data tidak valid")
			return
		}

		// Storage quota must be validated before any update is applied (a
		// rejected quota must not leave other fields half-edited). Same cap as
		// the SaaS "Maks Storage" default.
		if body.MaxStorageSizeMB != nil {
			if msg := validateStorageQuota(c, *body.MaxStorageSizeMB); msg != "" {
				errorResponse(c, http.StatusBadRequest, msg)
				return
			}
		}

		// PDF upload quota follows the same disk cap (only when provided).
		if body.MaxPDFSizeMB != nil {
			if msg := validatePDFQuota(c, *body.MaxPDFSizeMB); msg != "" {
				errorResponse(c, http.StatusBadRequest, msg)
				return
			}
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		isOp := isOperator(c)
		isSuper := isSuperAdmin(c)
		ctx := c.Request.Context()

		// opRoleTx serializes the one-operator-per-school role-grant against
		// every other operator-granting path. Opened lazily in the role
		// section below when the request grants the operator role to a target
		// that does not hold it yet; the final UPDATE runs inside it (via
		// UpdateUserTx) and commits, so the check and the write share one
		// transaction and one school-claim advisory lock.
		var opRoleTx pgx.Tx

		// Fetch target user
		targetUser, err := models.GetUserByID(ctx, pool, targetID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "User tidak ditemukan")
			return
		}

		isSuperAdminTarget := targetUser.Username == models.SuperAdminUsername

		// Operator restrictions
		if isOp {
			opInstansi, err := getInstansiForOperator(ctx, pool, userID)
			if err != nil || opInstansi == "" {
				log.Printf("edit user: operator instansi unresolved (user %d, err=%v)", userID, err)
				errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui user")
				return
			}
			if targetUser.Instansi != opInstansi {
				errorResponse(c, http.StatusBadRequest, "Anda hanya dapat mengelola user dalam satu instansi yang sama")
				return
			}
			if targetUser.IsOperator() {
				errorResponse(c, http.StatusBadRequest, "Operator tidak dapat mengelola akun dengan role Operator")
				return
			}
			// Operator cannot grant operator role. Check BOTH the plural
			// `roles` field and the singular `role` fallback, resolved exactly
			// like the apply logic below — otherwise sending {"role":"operator"}
			// (no `roles`) would bypass this restriction and escalate a user.
			requestedRoles := body.Roles
			if len(requestedRoles) == 0 && body.Role != nil {
				requestedRoles = strings.Split(*body.Role, ",")
			}
			for _, r := range requestedRoles {
				if strings.EqualFold(strings.TrimSpace(r), models.RoleOperator) {
					errorResponse(c, http.StatusBadRequest, "Operator tidak dapat memberikan role Operator")
					return
				}
			}
		}

		// Same minimum the UI enforces (minlength=8): a hand-crafted request
		// must not reset a password to something weaker than the panel does.
		if body.Password != nil && *body.Password != "" && len(*body.Password) < 8 {
			errorResponse(c, http.StatusBadRequest, "Password minimal 8 karakter")
			return
		}

		// Sub-account expiry policy (mirror of CreateUser's force-expiry rule):
		// a sub-account's lifetime ALWAYS follows the operator — the operator
		// can hand its own expiry to the accounts below it, but it must never
		// be able to grant a longer lifetime or an unlimited one (the school
		// package's expiry is the ceiling, and the expiry-cascade guards rely
		// on that invariant). Whatever the operator submits for expires_at is
		// replaced with the operator's CURRENT expiry (including NULL →
		// unlimited when the operator itself is unlimited). A SuperAdmin edit
		// is unaffected.
		expiryForcedMsg := ""
		if isOp && body.ExpiresAt != nil {
			submitted := strings.TrimSpace(*body.ExpiresAt)
			var opExpiresAt *time.Time
			if err := pool.QueryRow(ctx, `SELECT expires_at FROM admin_users WHERE id = $1`, userID).Scan(&opExpiresAt); err != nil {
				log.Printf("fetch operator expiry for edit error: %v", err)
				errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui user")
				return
			}
			if opExpiresAt != nil {
				// The forced value must round-trip through the DB as the
				// SAME instant: format the clock in UTC so the literal a
				// hand-crafted request could substitute never matters (the
				// string update is interpreted in the session timezone).
				forced := opExpiresAt.UTC().Format("2006-01-02 15:04:05")
				body.ExpiresAt = &forced
				if submitted != forced {
					expiryForcedMsg = fmt.Sprintf(" Masa aktif akun dipaksa mengikuti operator (%s) — nilai yang dikirim diabaikan.", forced)
				}
			} else {
				// Operator unlimited: the sub-account follows into unlimited,
				// exactly like CreateUser's inheritUnlimited ("" → NULL below).
				s := ""
				body.ExpiresAt = &s
				if submitted != "" {
					expiryForcedMsg = " Masa aktif akun dipaksa unlimited (mengikuti operator yang unlimited) — nilai yang dikirim diabaikan."
				}
			}
		}

		updates := make(map[string]interface{})

		if body.Name != nil {
			updates["name"] = strings.TrimSpace(*body.Name)
		}

		// Sub-account quota policy (mirror of CreateUser): an operator may
		// never change the per-account quota columns of the accounts below it,
		// so the four quota fields are IGNORED on the operator path — the
		// columns are left EXACTLY as they are, not forced to the free
		// defaults. Forcing would destroy a quota a SuperAdmin deliberately
		// raised (e.g. a legacy no-pool school where the SuperAdmin grants a
		// specific sub more storage): the only other writer of these columns
		// is a SuperAdmin (operators are barred here), so the existing value
		// is always trusted. Ignoring the submitted fields neutralizes the
		// tamper exactly like forcing did — a hand-crafted request can never
		// raise the columns — without the clobbering side effect.
		quotaBlockedMsg := ""
		if isOp && (body.MaxExams != nil || body.MaxPDFSizeMB != nil || body.MaxConcurrentExams != nil || body.MaxStorageSizeMB != nil) {
			quotaBlockedMsg = " Kuota akun sub tidak dapat diubah oleh operator — nilai yang dikirim diabaikan."
		}
		if !isOp {
			if body.MaxExams != nil {
				updates["max_exams"] = *body.MaxExams
			}
			if body.MaxPDFSizeMB != nil {
				pdfSize := int(*body.MaxPDFSizeMB * 1024 * 1024)
				updates["max_pdf_size"] = pdfSize
			}
			if body.MaxConcurrentExams != nil {
				updates["max_concurrent_exams"] = *body.MaxConcurrentExams
			}
			if body.MaxStorageSizeMB != nil {
				storageSize := int64(*body.MaxStorageSizeMB * 1024 * 1024)
				updates["max_storage_size"] = storageSize
			}
		}

		if body.WhatsappNumber != nil {
			updates["whatsapp_number"] = strings.TrimSpace(*body.WhatsappNumber)
		}

		// Identity fields on the bootstrap superadmin account: only the
		// superadmin themself (or another superadmin) may edit them. An
		// operator passing the name-equality tenant gate against the "owner"
		// bucket must never be able to swap the superadmin's email (→ public
		// forgot-password takeover), name, whatsapp, or expiry. Mirrors the
		// status/role/password guards below.
		if isSuperAdminTarget && !isSuper &&
			(body.Name != nil || body.Email != nil || body.WhatsappNumber != nil || body.ExpiresAt != nil) {
			errorResponse(c, http.StatusForbidden, "Tidak memiliki izin untuk mengubah akun superadmin")
			return
		}

		if body.Email != nil {
			updates["email"] = strings.TrimSpace(*body.Email)
		}

		if body.Instansi != nil {
			instansi := strings.TrimSpace(*body.Instansi)

			// Reserved system bucket names are never a valid instansi value —
			// no caller (superadmin included) may rename anyone into the
			// superadmin's "owner" bucket or the shared "personal" bucket.
			if instansi != "" && isReservedInstansiName(instansi) {
				errorResponse(c, http.StatusBadRequest, "Nama instansi tidak dapat digunakan (nama sistem terlarang)")
				return
			}

			// Operator tidak boleh mengubah instansi user
			if isOp {
				opInstansi, err := getInstansiForOperator(ctx, pool, userID)
				if err != nil || opInstansi == "" {
					log.Printf("edit user: operator instansi unresolved (user %d, err=%v)", userID, err)
					errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui user")
					return
				}
				if instansi != "" && instansi != opInstansi {
					errorResponse(c, http.StatusBadRequest, "Operator tidak dapat mengubah instansi user")
					return
				}
				// Force instansi untuk operator — operator tidak boleh mengganti instansi
				if instansi == "" || instansi != opInstansi {
					updates["instansi"] = opInstansi
				} else {
					updates["instansi"] = instansi
				}
			} else {
				if instansi == "" {
					errorResponse(c, http.StatusBadRequest, "Instansi tidak boleh kosong")
					return
				}

				// Cascade instansi: if the target is an operator and their
				// instansi changed, update all users in the same instansi too.
				if targetUser.IsOperator() && targetUser.Instansi != instansi {
					// One-operator-per-school policy: moving an operator into a
					// school that ALREADY has an operator would create a second
					// operator — the same violation the redeem/activate/create/
					// edit-role/claim paths reject. The check runs against the
					// DESTINATION instansi with the acting target excluded, so a
					// school rename (destination is fresh/empty) stays allowed.
					// Serialized per destination school via the school-claim
					// advisory lock (the check and the UPDATE below share one
					// transaction via opRoleTx) — without it, two concurrent
					// moves into the same empty school would both read the
					// pre-move snapshot and land two operators. Mutually
					// exclusive with the role-grant serialization below (this
					// branch requires the target to ALREADY hold the operator
					// role), so opRoleTx is opened at most once per request. The
					// shared "personal" bucket is never a school: nothing to
					// check, and no serialization needed.
					if instansi != "" && !strings.EqualFold(instansi, "personal") {
						var bErr error
						opRoleTx, bErr = pool.Begin(ctx)
						if bErr != nil {
							log.Printf("begin operator-move tx error: %v", bErr)
							errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui user")
							return
						}
						defer func() { _ = opRoleTx.Rollback(ctx) }() // no-op after a successful Commit

						if _, aErr := opRoleTx.Exec(ctx,
							`SELECT pg_advisory_xact_lock(hashtext('school-claim:' || lower($1))::bigint)`, instansi); aErr != nil {
							log.Printf("edit user: lock school-claim for move (%q): %v", instansi, aErr)
							errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui user")
							return
						}
						hasOp, opErr := schoolAlreadyHasOperator(ctx, opRoleTx, instansi, targetID)
						if opErr != nil {
							log.Printf("edit user: move operator one-operator-per-school check error: %v", opErr)
							errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui user")
							return
						}
						if hasOp {
							errorResponse(c, http.StatusBadRequest,
								"Instansi ini sudah memiliki operator. Satu sekolah hanya dapat memiliki satu operator.")
							return
						}
					}
					oldInstansi := targetUser.Instansi
					_, err := pool.Exec(ctx,
						`UPDATE admin_users SET instansi = $1 WHERE LOWER(instansi) = LOWER($2) AND id != $3`,
						instansi, oldInstansi, targetID)
					if err != nil {
						log.Printf("cascade instansi update error: %v", err)
						// Non-fatal — operator's own instansi is still updated
					}
				}

				updates["instansi"] = instansi
			}
		}

		if !isSuperAdminTarget && body.Status != nil {
			status := *body.Status
			switch status {
			case models.UserStatusActive, models.UserStatusSuspended, models.UserStatusPendingOTP:
				updates["status"] = status
				if status == models.UserStatusSuspended {
					// Record when the suspension starts so reactivation can
					// freeze the account clock for the suspension period.
					updates["suspended_at"] = time.Now().UTC()
				}
			}
		}

		if !isSuperAdminTarget && (body.Role != nil || body.Roles != nil) {
			roles := body.Roles
			if len(roles) == 0 && body.Role != nil {
				roles = strings.Split(*body.Role, ",")
			}
			filtered := make([]string, 0, len(roles))
			for _, r := range roles {
				switch r {
				case models.RoleGuru, models.RolePengawas, models.RoleOperator:
					filtered = append(filtered, r)
				}
			}
			if len(filtered) > 0 {
				// One-operator-per-school policy: granting the operator role to a
				// target who does not hold it yet, in a school that already has
				// an operator, would create a second. The check and the UPDATE
				// run in ONE transaction serialized per destination school via
				// the school-claim advisory lock (see opRoleTx) — without it,
				// two concurrent grants into the same empty school would BOTH
				// read the pre-update snapshot and land two operators. The
				// target's own current role (already an operator) and the
				// shared "personal" bucket are never blocked.
				if containsRole(filtered, models.RoleOperator) && !models.HasRole(targetUser.Role, models.RoleOperator) {
					// The check runs against the school the target will END UP in:
					// when the request also changes instansi, the destination is
					// the new value — checking only the CURRENT instansi would let
					// a single request move a guru into an occupied school AND
					// grant the operator role past the policy.
					checkInstansi := targetUser.Instansi
					if !isOp && body.Instansi != nil {
						if ni := strings.TrimSpace(*body.Instansi); ni != "" {
							checkInstansi = ni
						}
					}
					// The shared "personal" bucket is never a school: nothing to
					// check, and no serialization needed.
					if checkInstansi != "" && !strings.EqualFold(checkInstansi, "personal") {
						var bErr error
						opRoleTx, bErr = pool.Begin(ctx)
						if bErr != nil {
							log.Printf("begin operator-role grant tx error: %v", bErr)
							errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui user")
							return
						}
						defer func() { _ = opRoleTx.Rollback(ctx) }() // no-op after a successful Commit

						if _, aErr := opRoleTx.Exec(ctx,
							`SELECT pg_advisory_xact_lock(hashtext('school-claim:' || lower($1))::bigint)`, checkInstansi); aErr != nil {
							log.Printf("edit user: lock school-claim (%q): %v", checkInstansi, aErr)
							errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui user")
							return
						}
						hasOp, opErr := schoolAlreadyHasOperator(ctx, opRoleTx, checkInstansi, targetID)
						if opErr != nil {
							log.Printf("edit user: one-operator-per-school check error: %v", opErr)
							errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui user")
							return
						}
						if hasOp {
							errorResponse(c, http.StatusBadRequest,
								"Instansi ini sudah memiliki operator. Satu sekolah hanya dapat memiliki satu operator.")
							return
						}
					}
				}
				updates["role"] = models.SerializeRoles(filtered)
				// Keep base_role in sync so admin-granted roles survive package
				// switches: base = (new roles minus whatever the current package
				// grants) ∪ (roles already held independently that are still
				// listed). The second term preserves an admin-granted role that
				// coincides with a package grant — e.g. an admin-granted operator
				// on a user whose active sekolah package also grants operator —
				// while an explicit removal still drops it. The union never
				// exceeds the roles the admin listed, so no role can appear in
				// base that the admin did not choose.
				var pkgRole, curBaseRole string
				_ = pool.QueryRow(ctx,
					`SELECT COALESCE(package_role, ''), COALESCE(base_role, '') FROM admin_users WHERE id = $1`,
					targetID).Scan(&pkgRole, &curBaseRole)
				pkgRoles := parsePackageRoles(pkgRole)
				base := make([]string, 0, len(filtered))
				for _, r := range filtered {
					if !containsRole(pkgRoles, r) {
						base = append(base, r)
					}
				}
				for _, r := range parsePackageRoles(curBaseRole) {
					if containsRole(filtered, r) && !containsRole(base, r) {
						base = append(base, r)
					}
				}
				if len(base) == 0 {
					base = []string{models.RoleGuru}
				}
				updates["base_role"] = models.SerializeRoles(base)
			}
		}

		if !isSuperAdminTarget && body.Password != nil && *body.Password != "" {
			updates["password_hash"] = *body.Password
		}

		if body.ExpiresAt != nil {
			expVal := strings.TrimSpace(*body.ExpiresAt)
			if expVal == "" {
				updates["expires_at"] = nil
			} else {
				if !strings.Contains(expVal, " ") {
					expVal += " 23:59:59"
				}
				updates["expires_at"] = expVal
			}
		}

		// Sub-account package policy (mirror of CreateUser): an operator may
		// never change the subscription package of the accounts below it — a
		// sub-account cannot run its own package, so the label stays as the
		// SuperAdmin left it ("free" for accounts the operator created). Only
		// a SuperAdmin may assign a package.
		if body.Package != nil && !isOp {
			updates["package"] = strings.TrimSpace(*body.Package)
		}

		// cascadeMsg collects the operator-expiry cascade result (set expiry or
		// cleared to unlimited) so the final success message surfaces it.
		var cascadeMsg string

		// Cascade: if operator's expiry changed, sync to all users in same instansi
		if targetUser.IsOperator() && body.ExpiresAt != nil {
			expVal := strings.TrimSpace(*body.ExpiresAt)
			if expVal != "" {
				if !strings.Contains(expVal, " ") {
					expVal += " 23:59:59"
				}
				// Same guards as the unlimited branch below (and the other
				// expiry-alignment paths): sub-accounts that run their own
				// active package (NOT EXISTS), hold an operator role themselves,
				// or are already unlimited (NULL) keep their own state — only
				// accounts that follow the operator get the new expiry.
				if _, err := pool.Exec(ctx, `
					UPDATE admin_users
					SET expires_at = $1::timestamp
					WHERE LOWER(instansi) = LOWER($2) AND id <> $3
					  AND expires_at IS NOT NULL
					  AND NOT (role = 'operator' OR role ILIKE '%"operator"%')
					  AND NOT EXISTS (
					      SELECT 1 FROM voucher_redemptions vr
					      WHERE vr.user_id = admin_users.id AND vr.is_active
					  )`, expVal, targetUser.Instansi, targetID); err != nil {
					log.Printf("cascade expiry for instansi %s error: %v", targetUser.Instansi, err)
				}
				// Align the instansi users' active packages with the new expiry too
				// (their expires_at was just rewritten above, so the shared helper
				// reads the authoritative per-account expiry).
				if err := models.SyncInstansiActiveRedemptionsToExpiry(ctx, pool, targetUser.Instansi, targetID); err != nil {
					log.Printf("cascade sync redemption expiry for instansi %s error: %v", targetUser.Instansi, err)
				}
			} else {
				// Operator cleared to unlimited (expires_at = "" → NULL): the
				// school's sub-accounts follow the operator's unlimited state —
				// mirror of the CreateUser inheritUnlimited rule, applied to
				// already-created accounts. Guards are consistent with the other
				// expiry-alignment paths: sub-accounts that run their own active
				// package (NOT EXISTS), hold an operator role themselves, or are
				// already unlimited (NULL) keep their own state untouched.
				tag, err := pool.Exec(ctx, `
					UPDATE admin_users
					SET expires_at = NULL
					WHERE LOWER(instansi) = LOWER($1) AND id <> $2
					  AND expires_at IS NOT NULL
					  AND NOT (role = 'operator' OR role ILIKE '%"operator"%')
					  AND NOT EXISTS (
					      SELECT 1 FROM voucher_redemptions vr
					      WHERE vr.user_id = admin_users.id AND vr.is_active
					  )`, targetUser.Instansi, targetID)
				if err != nil {
					log.Printf("cascade unlimited for instansi %s error: %v", targetUser.Instansi, err)
				} else if n := tag.RowsAffected(); n > 0 {
					cascadeMsg = fmt.Sprintf(". %d akun di instansi %s ikut menjadi unlimited (masa aktif dihapus).", n, targetUser.Instansi)
				}
			}
		}

		if len(updates) > 0 {
			var uErr error
			if opRoleTx != nil {
				// The operator-role grant UPDATE runs inside the transaction
				// holding the school-claim advisory lock, so a concurrent
				// grant to the same school can never observe the pre-update
				// snapshot.
				uErr = models.UpdateUserTx(ctx, opRoleTx, targetID, updates)
				if uErr == nil {
					if cErr := opRoleTx.Commit(ctx); cErr != nil {
						log.Printf("commit operator-role grant tx error: %v", cErr)
						errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui user")
						return
					}
				}
			} else {
				uErr = models.UpdateUser(ctx, pool, targetID, updates)
			}
			if uErr != nil {
				log.Printf("edit user error: %v", uErr)
				errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui user")
				return
			}
		}

		// Keep the active package's clock in sync with an admin-set expiry so
		// the billing display and later pause computations always match the
		// account (the account expiry is the authoritative clock).
		if body.ExpiresAt != nil {
			expVal := strings.TrimSpace(*body.ExpiresAt)
			if expVal != "" {
				if !strings.Contains(expVal, " ") {
					expVal += " 23:59:59"
				}
				if t, err := time.Parse("2006-01-02 15:04:05", expVal); err == nil {
					if syncErr := models.SyncActiveRedemptionToExpiry(ctx, pool, targetID, t); syncErr != nil {
						log.Printf("edit user: sync redemption expiry error: %v", syncErr)
					}
				}
			}
		}

		// Reactivating via the edit form: apply the suspension freeze (extend
		// expiry by the suspension duration) so the package clock did not burn
		// while the account was suspended. When a fresh expiry is explicitly set
		// in the same request, that grant supersedes the freeze — but the
		// suspension markers STILL must go: leaving suspended_at dangling would
		// let a later reactivation freeze the clock a second time
		// (double-freeze), and leaving suspended_by_cascade dangling would let
		// a later cascade restore re-suspend an account an admin explicitly
		// revived. (The no-explicit-expiry branch below relies on
		// ResumeSuspendedAccountClock, which clears the markers itself; the
		// explicit-expiry branch clears them here and now.)
		var freezeMsg string
		if body.Status != nil && *body.Status == models.UserStatusActive && !isSuperAdminTarget {
			if body.ExpiresAt == nil {
				if extendedExpiry, err := models.ResumeSuspendedAccountClock(ctx, pool, targetID); err != nil {
					log.Printf("edit user: resume suspended account clock error: %v", err)
				} else if extendedExpiry != nil {
					// The suspension clock was frozen: expires_at was extended by the
					// suspension duration — which may even resurrect an account whose
					// expiry had already passed. Surface the new expiry so the admin
					// is not surprised by the extended lifetime.
					freezeMsg = fmt.Sprintf(" Masa aktif diperpanjang sampai %s (jam dijeda selama suspend)",
						extendedExpiry.Format("2006-01-02 15:04:05"))
				}
			} else {
				if _, cerr := pool.Exec(ctx,
					`UPDATE admin_users SET suspended_at = NULL, suspended_by_cascade = FALSE WHERE id = $1`,
					targetID); cerr != nil {
					log.Printf("edit user: clear suspension markers error: %v", cerr)
				}
			}
		}

		msg := fmt.Sprintf("Pengaturan user %s berhasil diperbarui.", targetUser.Username)
		if cascadeMsg != "" {
			msg += cascadeMsg
		}
		if expiryForcedMsg != "" {
			msg += expiryForcedMsg
		}
		if quotaBlockedMsg != "" {
			msg += quotaBlockedMsg
		}
		if freezeMsg != "" {
			msg += freezeMsg
		}

		// Append-only audit trail for management actions: WHO edited WHICH
		// account and WHICH fields changed. Written after the update committed;
		// only when something actually changed. Best-effort. examID = 0 → NULL.
		if len(updates) > 0 {
			cols := make([]string, 0, len(updates))
			for col := range updates {
				cols = append(cols, col)
			}
			sort.Strings(cols)
			if aErr := models.CreateAdminAuditLog(ctx, pool, userID, getCurrentUsername(c),
				models.ActionUserEdited, 0,
				fmt.Sprintf("Akun diubah: %s (%s) — field: %s", targetUser.Username, targetUser.Name, strings.Join(cols, ", "))); aErr != nil {
				log.Printf("audit user edited: %v", aErr)
			}
		}

		successMessage(c, msg)
	}
}

// ---------------------------------------------------------------------------
// 5. POST /admin/api/users/:user_id/toggle-status — Suspend / activate
// ---------------------------------------------------------------------------

func ToggleUserStatus() gin.HandlerFunc {
	return func(c *gin.Context) {
		targetID, err := strconv.Atoi(c.Param("user_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID user tidak valid")
			return
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		isOp := isOperator(c)
		ctx := c.Request.Context()

		targetUser, err := models.GetUserByID(ctx, pool, targetID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "User tidak ditemukan")
			return
		}

		if targetUser.Username == models.SuperAdminUsername {
			errorResponse(c, http.StatusBadRequest, "Status Super Admin tidak dapat diubah")
			return
		}

		if isOp {
			opInstansi, err := getInstansiForOperator(ctx, pool, userID)
			if err != nil || opInstansi == "" {
				log.Printf("toggle user status: operator instansi unresolved (user %d, err=%v)", userID, err)
				errorResponse(c, http.StatusInternalServerError, "Gagal mengubah status user")
				return
			}
			if targetUser.Instansi != opInstansi {
				errorResponse(c, http.StatusBadRequest, "Anda hanya dapat mengelola user dalam satu instansi yang sama")
				return
			}
			// An operator must not toggle another operator's status (mirrors
			// EditUser). Otherwise operator A could suspend peer operator B and
			// the cascade below would suspend the entire instansi.
			if targetUser.IsOperator() {
				errorResponse(c, http.StatusBadRequest, "Operator tidak dapat mengelola akun dengan role Operator")
				return
			}
		}

		newStatus, msg, err := models.ToggleUserStatus(ctx, pool, targetID)
		if err != nil {
			log.Printf("toggle user status error: %v", err)
			// A pending_otp account cannot be toggled — surface the specific
			// reason (it must be verified via OTP or the manual Verify action)
			// instead of a generic failure message.
			if errors.Is(err, models.ErrPendingOTPToggleBlocked) {
				errorResponse(c, http.StatusBadRequest, err.Error())
				return
			}
			errorResponse(c, http.StatusInternalServerError, "Gagal mengubah status user")
			return
		}

		// Cascade: track suspension/activation of operator's instansi.
		if targetUser.IsOperator() {
			opInstansi := targetUser.Instansi
			if opInstansi != "" {
				if newStatus == models.UserStatusActive {
					// Restore all cascade-suspended users
					var count int
					pool.QueryRow(ctx,
						`SELECT COUNT(*) FROM admin_users WHERE LOWER(instansi) = LOWER($1) AND suspended_by_cascade = TRUE AND id != $2`,
						opInstansi, targetID).Scan(&count)
					if count > 0 {
						// Freeze the restored accounts' clocks for the suspension
						// period (expires_at extended by the suspension duration) and
						// realign their active packages with the frozen expiry. The
						// helper fails as a unit, so a partial failure (restore done
						// but the redemption sync failed) suppresses the confirmation
						// message below — the log records the failure.
						if err := models.RestoreCascadeSuspendedInstansi(ctx, pool, opInstansi, targetID); err != nil {
							log.Printf("cascade restore for instansi %s error: %v", opInstansi, err)
						} else {
							msg += fmt.Sprintf(". %d user di instansi %s juga diaktifkan kembali.", count, opInstansi)
						}
					}

					// Align the sub-accounts' expiry with the operator's renewed
					// expiry (mirror of the restore branch in
					// syncInstansiWithOperatorRole): sub-accounts that do not run
					// their own active package follow the operator's expiry — a
					// school operator's accounts follow the operator's expiry — so
					// reactivating an expired operator also re-opens its school's
					// accounts instead of leaving them feature-locked. GREATEST
					// only ever EXTENDS an existing expiry (never shortens a
					// sub-account whose own expiry is already later than the
					// operator's new one), and sub-accounts with their own active
					// package (NOT EXISTS guard) or their own operator role are
					// left on their own clocks.
					var opExpiresAt *time.Time
					_ = pool.QueryRow(ctx, `SELECT expires_at FROM admin_users WHERE id = $1`, targetID).Scan(&opExpiresAt)
					if opExpiresAt != nil && opExpiresAt.After(time.Now().UTC()) {
						tag, err := pool.Exec(ctx, `
							UPDATE admin_users u
							SET expires_at = GREATEST(COALESCE(u.expires_at, $2::timestamptz), $2::timestamptz)
							WHERE LOWER(u.instansi) = LOWER($1) AND u.id <> $3
							  AND u.expires_at IS NOT NULL -- unlimited (NULL) subs keep their admin-set unlimited state
							  AND NOT (u.role = 'operator' OR u.role ILIKE '%"operator"%')
							  AND NOT EXISTS (
							      SELECT 1 FROM voucher_redemptions vr
							      WHERE vr.user_id = u.id AND vr.is_active
							  )`, opInstansi, *opExpiresAt, targetID)
						if err != nil {
							log.Printf("cascade expiry for instansi %s error: %v", opInstansi, err)
						} else if n := tag.RowsAffected(); n > 0 {
							// Realign the sub-accounts' active package clocks with
							// their new expiry (their rows were just rewritten above,
							// so the per-account expiry is authoritative).
							if err := models.SyncInstansiActiveRedemptionsToExpiry(ctx, pool, opInstansi, targetID); err != nil {
								log.Printf("cascade sync redemption expiry for instansi %s error: %v", opInstansi, err)
							}
							msg += fmt.Sprintf(". %d akun di instansi %s ikut diperpanjang mengikuti masa aktif operator.", n, opInstansi)
						}
					}
				} else if newStatus == models.UserStatusSuspended {
					// Suspend active users with cascade flag
					var count int
					pool.QueryRow(ctx,
						`SELECT COUNT(*) FROM admin_users WHERE LOWER(instansi) = LOWER($1) AND status = 'active' AND id != $2`,
						opInstansi, targetID).Scan(&count)
					if count > 0 {
						if _, err := pool.Exec(ctx,
							`UPDATE admin_users SET status = 'suspended', suspended_by_cascade = TRUE, suspended_at = now() WHERE LOWER(instansi) = LOWER($1) AND status = 'active' AND id != $2`,
							opInstansi, targetID); err != nil {
							log.Printf("cascade suspend for instansi %s error: %v", opInstansi, err)
						} else {
							msg += fmt.Sprintf(". %d user di instansi %s juga dinonaktifkan.", count, opInstansi)
						}
					}
					// Tombstone the school's unpublished exams (policy B,
					// mirroring the voucher switch): a manual suspension freezes
					// the whole school, so every active-but-unstarted exam in the
					// instansi goes inactive (spareOperatorRoleCreators=false —
					// even operator-role subs are suspended by this cascade).
					// Exams already running stay untouched so students can
					// finish, and the tombstone is not auto-reversed when the
					// operator is reactivated.
					if err := tombstoneUnstartedInstansiExams(ctx, pool, opInstansi, false); err != nil {
						log.Printf("cascade tombstone exams for instansi %s error: %v", opInstansi, err)
					}
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"message":    msg,
			"new_status": newStatus,
		})
	}
}

// ---------------------------------------------------------------------------
// 6. POST /admin/api/users/:user_id/verify — Manually verify a pending user
// ---------------------------------------------------------------------------

func VerifyUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		targetID, err := strconv.Atoi(c.Param("user_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID user tidak valid")
			return
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		isOp := isOperator(c)
		ctx := c.Request.Context()

		targetUser, err := models.GetUserByID(ctx, pool, targetID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "User tidak ditemukan")
			return
		}

		if isOp {
			opInstansi, err := getInstansiForOperator(ctx, pool, userID)
			if err != nil || opInstansi == "" {
				log.Printf("verify user: operator instansi unresolved (user %d, err=%v)", userID, err)
				errorResponse(c, http.StatusInternalServerError, "Gagal memverifikasi user")
				return
			}
			if targetUser.Instansi != opInstansi {
				errorResponse(c, http.StatusBadRequest, "Anda hanya dapat mengelola user dalam satu instansi yang sama")
				return
			}
			// An operator must not verify another operator (mirrors EditUser &
			// ToggleUserStatus): the verify action activates a pending account,
			// and peer operators in the same instansi must stay out of each
			// other's account lifecycle.
			if targetUser.IsOperator() {
				errorResponse(c, http.StatusBadRequest, "Operator tidak dapat mengelola akun dengan role Operator")
				return
			}
		}

		// Defense-in-depth: the bootstrap superadmin account is never
		// activatable by an operator (mirrors DeleteUser).
		if targetUser.Username == models.SuperAdminUsername {
			errorResponse(c, http.StatusForbidden, "Akun superadmin tidak dapat dikelola oleh operator")
			return
		}

		if err := models.VerifyUserManual(ctx, pool, targetID); err != nil {
			log.Printf("verify user error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memverifikasi user")
			return
		}

		successMessage(c, fmt.Sprintf("User %s berhasil diaktifkan secara manual", targetUser.Username))
	}
}

// DeactivateUserPackage manually ends the TARGET account's ACTIVE package
// (SuperAdmin only — mirroring the package-assignment policy: operators may
// never change the subscription package of the accounts below them, and
// removing a package is strictly more powerful than assigning one). The
// active redemption is BURNED (remaining_seconds = 0, is_active = false) so
// the user cannot re-activate it from the billing page — same terminal state
// the package-expiry job produces.
//
// After the burn, in ONE transaction:
//   - if the account holds another claimed-but-paused voucher with remaining
//     lifetime, that one is activated and its snapshot applied (quota, role,
//     expiry) — the account keeps access via the fallback, exactly like the
//     expiry job's fallback path;
//   - otherwise the account REVERTS TO THE FREE TRIAL: per-account quotas are
//     reset to the free defaults, the package label to "free", package-granted
//     roles are clawed back (role = base ∪ ∅, so an operator from a school
//     package loses the role), and expires_at = now + default_active_days —
//     the same state a fresh self-registered account starts in.
//
// The role clawback runs through syncInstansiWithOperatorRole, so deactivating
// the last school package of an instansi cascade-suspends its sub-accounts and
// tombstones its unstarted exams, exactly as a natural package loss does (an
// account that is itself suspended is rejected up front: a suspended account
// must not silently receive a fresh trial).
func DeactivateUserPackage() gin.HandlerFunc {
	return func(c *gin.Context) {
		targetID, err := strconv.Atoi(c.Param("user_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID user tidak valid")
			return
		}

		pool := getPool(c)
		actorID := getCurrentUserID(c)
		ctx := c.Request.Context()

		// Hoist the trial length BEFORE the transaction (anti-deadlock
		// pattern: no DB read inside the tx after the user row lock).
		trialDays := models.GetSaasSettingInt(ctx, pool, models.SettingDefaultActiveDays, 14)
		if trialDays < 1 {
			trialDays = 14
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			log.Printf("deactivate package: begin tx: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menonaktifkan paket")
			return
		}
		defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit

		// Lock the target user row so a concurrent redeem/activate/expiry job
		// cannot interleave with the deactivation.
		var target models.AdminUser
		var targetStatus string
		err = tx.QueryRow(ctx, `SELECT id, username, status, role,
			COALESCE(package_role, ''), COALESCE(base_role, '')
			FROM admin_users WHERE id = $1 FOR UPDATE`, targetID).
			Scan(&target.ID, &target.Username, &targetStatus, &target.Role,
				&target.PackageRole, &target.BaseRole)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				errorResponse(c, http.StatusNotFound, "User tidak ditemukan")
				return
			}
			log.Printf("deactivate package: lock target user %d: %v", targetID, err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menonaktifkan paket")
			return
		}

		if targetStatus == models.UserStatusSuspended {
			// A suspended account's clock is frozen by design; granting it a
			// fresh free trial would silently re-open a disabled account.
			errorResponse(c, http.StatusBadRequest,
				"Akun dalam status dinonaktifkan (suspended). Aktifkan akun terlebih dahulu sebelum menonaktifkan paketnya.")
			return
		}

		// Lock the active redemption (the row being burned).
		var active models.VoucherRedemption
		var activePkg string
		err = tx.QueryRow(ctx, `SELECT id, COALESCE(package, '')
			FROM voucher_redemptions WHERE user_id = $1 AND is_active FOR UPDATE`, targetID).
			Scan(&active.ID, &activePkg)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				errorResponse(c, http.StatusBadRequest, "Akun ini tidak memiliki paket aktif untuk dinonaktifkan.")
				return
			}
			log.Printf("deactivate package: lock active redemption (user %d): %v", targetID, err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menonaktifkan paket")
			return
		}

		// 1. Burn the active package — terminal state, cannot be re-activated
		//    from the billing page (it has no remaining lifetime left).
		if _, err := tx.Exec(ctx, `
			UPDATE voucher_redemptions
			SET remaining_seconds = 0, activated_at = NULL, is_active = false
			WHERE id = $1`, active.ID); err != nil {
			log.Printf("deactivate package: burn active redemption (user %d): %v", targetID, err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menonaktifkan paket")
			return
		}

		prevRole := target.Role
		now := time.Now().UTC()

		// 2. Try the best paused claimed voucher as fallback (same selection
		//    rule as the package-expiry job).
		var fb models.VoucherRedemption
		err = tx.QueryRow(ctx, `
			SELECT id, voucher_id, user_id, redeemed_at, remaining_seconds, activated_at, is_active,
			       COALESCE(package, ''), COALESCE(max_exams, 0),
			       COALESCE(max_pdf_size, 0), COALESCE(max_concurrent_exams, 0),
			       COALESCE(max_storage_size, 0), COALESCE(role, '')
			FROM voucher_redemptions
			WHERE user_id = $1 AND NOT is_active AND remaining_seconds > 0
			ORDER BY remaining_seconds DESC, redeemed_at ASC, id ASC
			LIMIT 1
			FOR UPDATE`, targetID).Scan(
			&fb.ID, &fb.VoucherID, &fb.UserID, &fb.RedeemedAt, &fb.RemainingSeconds, &fb.ActivatedAt, &fb.IsActive,
			&fb.Package, &fb.MaxExams, &fb.MaxPDFSize, &fb.MaxConcurrentExams,
			&fb.MaxStorageSize, &fb.Role,
		)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			log.Printf("deactivate package: find fallback (user %d): %v", targetID, err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menonaktifkan paket")
			return
		}

		var newPkg string
		var newExpiryStr string
		if err == nil {
			// Fallback found: activate it and apply its snapshot (quota, merged
			// role, expiry, and the school cascade).
			fb.ActivatedAt = &now
			if _, err := tx.Exec(ctx, `
				UPDATE voucher_redemptions SET is_active = true, activated_at = $2
				WHERE id = $1`, fb.ID, now); err != nil {
				log.Printf("deactivate package: activate fallback (user %d): %v", targetID, err)
				errorResponse(c, http.StatusInternalServerError, "Gagal menonaktifkan paket")
				return
			}
			if err := applyRedemptionEntitlement(ctx, tx, targetID, &fb); err != nil {
				log.Printf("deactivate package: apply fallback entitlement (user %d): %v", targetID, err)
				errorResponse(c, http.StatusInternalServerError, "Gagal menonaktifkan paket")
				return
			}
			newPkg = fb.Package
			newExpiryStr = now.Add(time.Duration(fb.RemainingSeconds) * time.Second).Format("2006-01-02 15:04:05")
		} else {
			// No fallback: revert to the free trial. The role clawback goes
			// through nextRolesState with an empty package grant (role = base
			// ∪ ∅; a SuperAdmin's role is never touched), then
			// syncInstansiWithOperatorRole handles the school cascade when the
			// deactivated package was the instansi's only operator grant.
			trialExpiry := now.AddDate(0, 0, trialDays)
			roleJSON, nextPkgRole, nextBaseRole := nextRolesState(prevRole, target.PackageRole, target.BaseRole, "")
			if _, err := tx.Exec(ctx, `UPDATE admin_users SET
				package = 'free', max_exams = $2, max_pdf_size = $3,
				max_concurrent_exams = $4, max_storage_size = $5,
				expires_at = $6, role = $7, package_role = $8, base_role = $9
				WHERE id = $1`,
				targetID, subAccountFreeMaxExams, subAccountFreeMaxPDFSize,
				subAccountFreeMaxConcurrentExams, subAccountFreeMaxStorageSize,
				trialExpiry, roleJSON, nextPkgRole, nextBaseRole); err != nil {
				log.Printf("deactivate package: revert to free trial (user %d): %v", targetID, err)
				errorResponse(c, http.StatusInternalServerError, "Gagal menonaktifkan paket")
				return
			}
			if err := syncInstansiWithOperatorRole(ctx, tx, targetID, prevRole, roleJSON, trialExpiry); err != nil {
				log.Printf("deactivate package: sync instansi (user %d): %v", targetID, err)
				errorResponse(c, http.StatusInternalServerError, "Gagal menonaktifkan paket")
				return
			}
			newPkg = "free"
			newExpiryStr = trialExpiry.Format("2006-01-02 15:04:05")
		}

		if err := tx.Commit(ctx); err != nil {
			log.Printf("deactivate package: commit (user %d): %v", targetID, err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menonaktifkan paket")
			return
		}

		// Append-only audit trail (written after commit, best-effort): who
		// deactivated WHICH package, and what the account fell back to.
		if aErr := models.CreateAdminAuditLog(ctx, pool, actorID, getCurrentUsername(c),
			models.ActionVoucherDeactivated, 0,
			fmt.Sprintf("Paket %s dinonaktifkan oleh admin — akun %s kembali ke %s (masa aktif sampai %s)",
				strings.ToUpper(activePkg), target.Username, strings.ToUpper(newPkg), newExpiryStr)); aErr != nil {
			log.Printf("audit voucher deactivated: %v", aErr)
		}

		successMessage(c, fmt.Sprintf(
			"Paket %s pada akun %s dinonaktifkan. Akun kini menggunakan %s (masa aktif sampai %s).",
			strings.ToUpper(activePkg), target.Username, strings.ToUpper(newPkg), newExpiryStr))
	}
}

// DeleteUser removes a user and cascades their exams.
func DeleteUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		targetID, err := strconv.Atoi(c.Param("user_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID user tidak valid")
			return
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		isOp := isOperator(c)
		isSuper := isSuperAdmin(c)
		ctx := c.Request.Context()

		if targetID == userID {
			errorResponse(c, http.StatusBadRequest, "Tidak dapat menghapus akun sendiri")
			return
		}

		targetUser, err := models.GetUserByID(ctx, pool, targetID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "User tidak ditemukan")
			return
		}

		if isSuper {
			// super admin can delete anyone
		} else if isOp {
			opInstansi, err := getInstansiForOperator(ctx, pool, userID)
			if err != nil || opInstansi == "" {
				log.Printf("delete user: operator instansi unresolved (user %d, err=%v)", userID, err)
				errorResponse(c, http.StatusInternalServerError, "Gagal menghapus user")
				return
			}
			if targetUser.Instansi != opInstansi {
				errorResponse(c, http.StatusBadRequest, "Anda hanya dapat mengelola user dalam satu instansi yang sama")
				return
			}
			// An operator must not delete a PEER operator of the same
			// instansi (mirrors EditUser / ToggleUserStatus / VerifyUser):
			// deleting an operator account cascades over its whole instansi —
			// every sub-account, every exam, and every account sharing the
			// instansi label goes with it (the "pelanggan hilang" contract in
			// the README applies to the SUPERADMIN). A peer deletion would
			// wipe the entire school, including the acting operator themself.
			if targetUser.IsOperator() {
				errorResponse(c, http.StatusBadRequest, "Operator tidak dapat mengelola akun dengan role Operator")
				return
			}
		} else {
			errorResponse(c, http.StatusForbidden, "Tidak memiliki izin")
			return
		}

		// Defense-in-depth: the bootstrap superadmin account must never be
		// deletable by an operator (the account is only re-seeded at startup;
		// deleting it would lock all superadmin access out of the deployment).
		if targetUser.Username == models.SuperAdminUsername {
			errorResponse(c, http.StatusForbidden, "Akun superadmin tidak dapat dihapus")
			return
		}

		// Append-only audit trail: every exam removed by this cascade gets an
		// exam_deleted row — the target's own exams plus, when the target is an
		// operator with a real instansi, the exams of its instansi
		// sub-accounts (the exact set models.DeleteUser removes). Written
		// BEFORE the rows are deleted so exam_id survives via ON DELETE
		// SET NULL with the name snapshotted in detail; attributed to the
		// ACTING admin. Best-effort: a failed audit row never blocks the
		// delete.
		auditQuery := `SELECT id, name FROM exams WHERE created_by = $1`
		auditArgs := []interface{}{targetID}
		if targetUser.HasRole(models.RoleOperator) && targetUser.Instansi != "" {
			auditQuery = `SELECT id, name FROM exams
			              WHERE created_by = $1 OR created_by IN (SELECT id FROM admin_users WHERE LOWER(instansi) = LOWER($2) AND id != $1)`
			auditArgs = append(auditArgs, targetUser.Instansi)
		}
		if auditRows, err := pool.Query(ctx, auditQuery, auditArgs...); err == nil {
			for auditRows.Next() {
				var eid int
				var ename string
				if err := auditRows.Scan(&eid, &ename); err == nil {
					detail := fmt.Sprintf("Ujian dihapus: %s", ename)
					if err := models.CreateAdminAuditLog(ctx, pool, userID, getCurrentUsername(c),
						models.ActionExamDeleted, eid, detail); err != nil {
						log.Printf("audit exam deleted (user cascade): %v", err)
					}
				}
			}
			if err := auditRows.Err(); err != nil {
				log.Printf("delete user audit names iteration error: %v", err)
			}
			auditRows.Close()
		} else {
			log.Printf("delete user audit names query error: %v", err)
		}

		// Append-only audit trail for the ACCOUNT deletion itself: one
		// user_deleted row per account the delete removes — the target plus,
		// when the target is an operator with a real instansi, every
		// sub-account sharing that instansi (the exact set models.DeleteUser
		// cascades). Written BEFORE the rows are deleted so user_id survives
		// via ON DELETE SET NULL with the identity snapshotted in detail;
		// attributed to the ACTING admin. Best-effort: a failed audit row
		// never blocks the delete.
		userAuditQuery := `SELECT id, username, COALESCE(role, ''), COALESCE(name, '') FROM admin_users WHERE id = $1`
		userAuditArgs := []interface{}{targetID}
		cascadeAudit := false
		if targetUser.HasRole(models.RoleOperator) && targetUser.Instansi != "" {
			userAuditQuery = `SELECT id, username, COALESCE(role, ''), COALESCE(name, '')
			                  FROM admin_users
			                  WHERE id = $1 OR (LOWER(instansi) = LOWER($2) AND id != $1)`
			userAuditArgs = append(userAuditArgs, targetUser.Instansi)
			cascadeAudit = true
		}
		if auditUsers, err := pool.Query(ctx, userAuditQuery, userAuditArgs...); err == nil {
			for auditUsers.Next() {
				var uid int
				var uname, urole, uname2 string
				if err := auditUsers.Scan(&uid, &uname, &urole, &uname2); err == nil {
					detail := fmt.Sprintf("Akun dihapus: %s (%s), role %s", uname, uname2, urole)
					if cascadeAudit && uid != targetID {
						detail += fmt.Sprintf(" — ikut terhapus dengan operator (instansi %s)", targetUser.Instansi)
					}
					if err := models.CreateAdminAuditLog(ctx, pool, userID, getCurrentUsername(c),
						models.ActionUserDeleted, 0, detail); err != nil {
						log.Printf("audit user deleted: %v", err)
					}
				}
			}
			if err := auditUsers.Err(); err != nil {
				log.Printf("delete user audit identities iteration error: %v", err)
			}
			auditUsers.Close()
		} else {
			log.Printf("delete user audit identities query error: %v", err)
		}

		paths, err := models.DeleteUser(ctx, pool, targetID)
		if err != nil {
			log.Printf("delete user error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menghapus user")
			return
		}

		// Clean up associated files (the exams owned by the deleted user(s) are
		// gone, so their PDFs must not linger on disk — same contract as the
		// exam delete paths).
		storageDir := getStoragePath(c)
		for _, p := range paths {
			if fp, err := helpers.SafeStoragePath(storageDir, p); err == nil {
				os.Remove(fp)
			}
		}

		// Delete from R2 if configured — mirrors DeleteExam/BulkDelete so a
		// deleted user's exam PDFs do not linger in object storage either.
		// Guard nil interface (key present but not a Client) before Enabled().
		if r2c, exists := c.Get("r2"); exists {
			client := r2client.FromContext(r2c)
			if client != nil && client.Enabled() {
				for _, p := range paths {
					r2Key := fmt.Sprintf("pdfs/%s", p)
					if err := client.Delete(ctx, r2Key); err != nil {
						log.Printf("admin: R2 delete error for %s: %v", r2Key, err)
					}
				}
			}
		}

		msg := fmt.Sprintf("User %s berhasil dihapus", targetUser.Username)
		if targetUser.HasRole(models.RoleOperator) && targetUser.Instansi != "" {
			msg += fmt.Sprintf(" beserta semua user instansi %s", targetUser.Instansi)
		}
		successMessage(c, msg)
	}
}

// ---------------------------------------------------------------------------
// 8. POST /admin/api/instansi/update — Update Operator Instansi (management
// level: SuperAdmin & Operator, enforced at route registration)
// ---------------------------------------------------------------------------

func generateInstansiCode() string {
	b := make([]byte, 4)
	_, _ = cryptoRand.Read(b)
	hexStr := strings.ToUpper(hex.EncodeToString(b))
	return fmt.Sprintf("SCH-%s-%s", hexStr[:4], hexStr[4:])
}

// UpdateInstansi handles POST /admin/api/instansi/update (route is registered
// under AdminManagementRequired — only SuperAdmin & Operator may rename a
// school instansi, since the rename applies to every account sharing the
// instansi_id).
func UpdateInstansi() gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Instansi string `json:"instansi" form:"instansi"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			_ = c.ShouldBind(&body)
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		ctx := c.Request.Context()

		newInstansi := strings.TrimSpace(body.Instansi)
		if newInstansi == "" || strings.ToLower(newInstansi) == "personal" || len(newInstansi) < 3 {
			errorResponse(c, http.StatusBadRequest, "Nama instansi/sekolah tidak valid (min. 3 karakter)")
			return
		}
		// Reserved system bucket names: "owner" is where the bootstrap
		// superadmin lives (EnsureAdminUser seeds instansi='owner'). A school
		// named "owner" would let an operator land in the superadmin's
		// free-text instansi bucket and pass every name-equality tenant gate
		// against it (EditUser/DeleteUser/VerifyUser). Kept in one place so
		// future system buckets are added here once.
		if isReservedInstansiName(newInstansi) {
			errorResponse(c, http.StatusBadRequest, "Nama instansi tidak dapat digunakan (nama sistem terlarang)")
			return
		}

		user, err := models.GetUserByID(ctx, pool, userID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "Pengguna tidak ditemukan")
			return
		}

		// The claim/rename runs in ONE transaction so the one-operator-per-
		// school guard and the instansi writes are atomic — and the whole
		// school-claim operation is serialized per destination name via an
		// advisory lock. Without it, two personal-bucket operators claiming
		// the SAME school name concurrently would BOTH pass the guard (each
		// reads the pre-claim snapshot) and both become operators of one
		// school. The lock key lowercases the name so case-variant claims of
		// the same school serialize too (the guard compares case-insensitively).
		tx, bErr := pool.Begin(ctx)
		if bErr != nil {
			log.Printf("begin instansi update tx error: %v", bErr)
			errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui instansi")
			return
		}
		defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit

		if _, aErr := tx.Exec(ctx,
			`SELECT pg_advisory_xact_lock(hashtext('school-claim:' || lower($1))::bigint)`, newInstansi); aErr != nil {
			log.Printf("update instansi: lock school claim (%q): %v", newInstansi, aErr)
			errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui instansi")
			return
		}

		// One-operator-per-school policy: an operator claiming a school that
		// already has ANOTHER operator would create a second operator in that
		// school. Checked INSIDE the transaction (after the advisory lock, so
		// a concurrent claimer sees the winner's committed state) — the
		// user's own school rename is excluded via userID, and gurus joining
		// a school are never affected.
		if user.IsOperator() {
			hasOp, opErr := schoolAlreadyHasOperator(ctx, tx, newInstansi, userID)
			if opErr != nil {
				log.Printf("update instansi: one-operator-per-school check error: %v", opErr)
				errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui instansi")
				return
			}
			if hasOp {
				errorResponse(c, http.StatusBadRequest,
					"Instansi ini sudah memiliki operator. Satu sekolah hanya dapat memiliki satu operator.")
				return
			}
		}

		// If user already has instansi_id, update name across all linked users
		var instansiID int
		var instansiCode string

		if user.InstansiID != nil && *user.InstansiID > 0 {
			instansiID = *user.InstansiID
			_ = tx.QueryRow(ctx, `SELECT COALESCE(code, '') FROM instansi WHERE id = $1`, instansiID).Scan(&instansiCode)
			if instansiCode == "" {
				instansiCode = generateInstansiCode()
				_, _ = tx.Exec(ctx, `UPDATE instansi SET code = $1 WHERE id = $2`, instansiCode, instansiID)
			}
			_, _ = tx.Exec(ctx, `UPDATE instansi SET name = $1 WHERE id = $2`, newInstansi, instansiID)
			_, err = tx.Exec(ctx, `UPDATE admin_users SET instansi = $1, instansi_code = $2 WHERE instansi_id = $3`, newInstansi, instansiCode, instansiID)
		} else {
			codeCandidate := generateInstansiCode()
			err = tx.QueryRow(ctx, `
				INSERT INTO instansi (name, code)
				VALUES ($1, $2)
				RETURNING id, code`, newInstansi, codeCandidate).Scan(&instansiID, &instansiCode)
			if err != nil {
				codeCandidate = generateInstansiCode()
				_ = tx.QueryRow(ctx, `
					INSERT INTO instansi (name, code)
					VALUES ($1, $2)
					RETURNING id, code`, newInstansi, codeCandidate).Scan(&instansiID, &instansiCode)
			}

			_, err = tx.Exec(ctx, `
				UPDATE admin_users 
				SET instansi = $1, instansi_id = $2, instansi_code = $3 
				WHERE id = $4`, newInstansi, instansiID, instansiCode, userID)
		}
		if err != nil {
			log.Printf("failed to update instansi for user %d: %v", userID, err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui instansi")
			return
		}
		if cErr := tx.Commit(ctx); cErr != nil {
			log.Printf("commit instansi update tx error: %v", cErr)
			errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui instansi")
			return
		}

		// Sub-account migration: an operator who created sub-accounts while
		// still in the shared "personal" bucket (school instansi not yet set —
		// see loadOperatorAccountQuota) has those sub-accounts moved to the new
		// school instansi (instansi + instansi_id + instansi_code). Without
		// this, the subs keep their "personal" label and stop counting toward
		// the school quota (which counts by the operator's instansi label),
		// letting the operator create max_users more on top of the ones already
		// created. Scoped by created_by = the operator, so only sub-accounts
		// THIS operator created move — the precise per-operator attribution
		// that replaces the old shared-bucket approximation (where one personal
		// operator claiming a school would sweep another operator's personal
		// sub-accounts along). Legacy rows with created_by IS NULL cannot be
		// attributed and are deliberately left in "personal" (their quota
		// still counts via the loadOperatorAccountQuota legacy fallback).
		// Self-registered accounts (operator_created=false) are never touched.
		if user.IsOperator() && strings.EqualFold(strings.TrimSpace(user.Instansi), "personal") {
			if _, err := pool.Exec(ctx, `
				UPDATE admin_users
				SET instansi = $1, instansi_id = $2, instansi_code = $3
				WHERE created_by = $4 AND LOWER(instansi) = 'personal' AND id <> $4`,
				newInstansi, instansiID, instansiCode, userID); err != nil {
				log.Printf("migrate personal-bucket sub-accounts for user %d error: %v", userID, err)
			}
		}

		session := sessions.Default(c)
		session.Set(middleware.SessionKeyInstansi, newInstansi)
		_ = session.Save()

		c.JSON(http.StatusOK, gin.H{
			"success":       true,
			"message":       fmt.Sprintf("Nama Instansi '%s' berhasil disimpan! (Kode Unik: %s)", newInstansi, instansiCode),
			"instansi_code": instansiCode,
		})
	}
}
