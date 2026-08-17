package admin

import (
	"log"
	"time"

	"github.com/examvan/webui/internal/models"
	"github.com/gin-gonic/gin"
)

// loadBillingPageData computes the Paket & Voucher (billing) section's data
// dict for the merged /admin/settings page. (The standalone /admin/billing
// page was removed; its URL now 302-redirects to /admin/settings#billing via
// SettingsRedirect — the billing section remains the ONE section a
// feature-locked account may use to renew.)
func loadBillingPageData(c *gin.Context) gin.H {
	pool := getPool(c)
	ctx := c.Request.Context()

	voucherEnabled := true
	if pool != nil {
		voucherEnabled = models.GetSaasSettingBool(ctx, pool, models.SettingVoucherRedeemEnabled, true)
	}

	userPackage := "free"
	userMaxPDF := int64(1048576)
	userMaxTotal := int64(1)
	userMaxConcurrent := int64(1)
	var userMaxStorage int64
	var userExpires string
	userExpired := false
	var userMaxAccounts int64 // sub-account quota of the active package (0 = n/a or unlimited)
	var userAccountsUsed int64
	userOperatorCreated := false // account created by an operator (sub-account voucher policy)
	userAccountsPct := 0
	var userAccountsRemaining int64
	// userPackageName is the human-readable "Paket Saat Ini" label. Empty
	// for regular accounts (the template falls back to the raw package key
	// as before); set for sub-accounts whose package is the school's, so
	// the card never reads the sub-account's forced 'free' row.
	userPackageName := ""
	isSuper := getCurrentUserRole(c) == models.RoleSuperAdmin

	if pool != nil {
		userID := getCurrentUserID(c)
		user, err := models.GetUserByID(ctx, pool, userID)
		if err != nil {
			log.Printf("billing: load user error: %v", err)
		} else {
			if user.Package != "" {
				userPackage = user.Package
			}
			if user.MaxPDFSize > 0 {
				userMaxPDF = int64(user.MaxPDFSize)
			}
			if user.MaxExams > 0 {
				userMaxTotal = int64(user.MaxExams)
			}
			if user.MaxConcurrentExams > 0 {
				userMaxConcurrent = int64(user.MaxConcurrentExams)
			}
			userMaxStorage = int64(user.MaxStorageSize)
			if user.ExpiresAt != nil {
				userExpires = user.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
				if user.ExpiresAt.Before(time.Now().UTC()) {
					userExpired = true
				}
			}

			// Sub-account quota (school packages): show how many accounts the
			// operator may create and how many already exist, so the limit
			// is visible before it is reached. Same source and count as the
			// CreateUser enforcement (loadOperatorAccountQuota), so display
			// and enforcement can never disagree. The error is deliberately
			// ignored here (fail-open DISPLAY only): a transient DB error
			// omits the quota card (0 = unlimited) instead of breaking the
			// billing page — the enforcement path (CreateUser) fails
			// CLOSED on the same error, so the operator cannot create more
			// accounts on a glitch even though the card hides.
			userMaxAccounts, userAccountsUsed, _ = loadOperatorAccountQuota(ctx, pool, userID, user.IsOperator(), user.Instansi)
			if userMaxAccounts > 0 {
				userAccountsPct = int(userAccountsUsed * 100 / userMaxAccounts)
				if userAccountsPct > 100 {
					userAccountsPct = 100
				}
				userAccountsRemaining = userMaxAccounts - userAccountsUsed
				if userAccountsRemaining < 0 {
					userAccountsRemaining = 0
				}
			}
			// Sub-account voucher policy: hide the voucher-claim UI entirely
			// for accounts created by an operator — they can never
			// redeem/activate a voucher (the API rejects them with 403), so
			// the form would only mislead. Their package/kuota are managed
			// by the school operator. Read from the user already loaded
			// above (no second query).
			userOperatorCreated = user.OperatorCreated

			// Sub-account quota display: an operator-created account's REAL
			// limits come from the school pool — the instansi's operator(s)
			// active redemption snapshot (schoolPoolQuotaForInstansi), the
			// same source that gates exam/PDF/storage/concurrent quota at
			// upload/start time. Its own admin_users columns only hold the
			// 'free' defaults (CreateUser forces package='free' and the
			// default quotas on sub-accounts), so showing them would
			// misrepresent the limits that actually apply. When a school
			// pool exists (a real school instansi whose operator holds an
			// active package), the quota cards show the pool values and
			// "Paket Saat Ini" shows the school package label. Without a
			// pool (operator still in the shared "personal" bucket, or no
			// active operator redemption) exam creation falls back to the
			// per-account columns, so the default display stays accurate.
			if userOperatorCreated {
				if maxExams, maxPDF, maxConcurrent, maxStorage, _, ok := schoolPoolQuota(ctx, pool, userID); ok {
					userMaxTotal = maxExams
					userMaxPDF = maxPDF
					userMaxConcurrent = maxConcurrent
					userMaxStorage = maxStorage
					// The school package label comes from the operator's
					// active redemption snapshot — a sub-account's package
					// is the school's, never its own forced 'free' row.
					// packageDisplayName maps known keys to the "Paket …"
					// labels (mirroring the dashboard), with a generic
					// fallback when the label cannot be resolved.
					var schoolPkg string
					if err := pool.QueryRow(ctx, `
							SELECT COALESCE(vr.package, '')
							FROM voucher_redemptions vr
							JOIN admin_users u ON u.id = vr.user_id
							WHERE vr.is_active AND u.instansi = $1 AND u.role ILIKE '%"operator"%'
							ORDER BY vr.redeemed_at DESC, vr.id DESC
							LIMIT 1`, user.Instansi).Scan(&schoolPkg); err != nil || schoolPkg == "" {
						userPackageName = "Paket Sekolah"
					} else {
						userPackageName = packageDisplayName(schoolPkg)
					}
				}
			}
		}
	}

	return gin.H{
		"voucher_enabled":         voucherEnabled,
		"user_package":            userPackage,
		"user_package_name":       userPackageName,
		"user_expires_at":         userExpires,
		"user_max_total_exams":    userMaxTotal,
		"user_max_concurrent":     userMaxConcurrent,
		"user_max_pdf_size_mb":    roundTo(float64(userMaxPDF)/(1024*1024), 1),
		"user_max_storage_mb":     roundTo(float64(userMaxStorage)/(1024*1024), 2),
		"user_is_super":           isSuper,
		"user_expired":            userExpired,
		"user_max_accounts":       userMaxAccounts,
		"user_accounts_used":      userAccountsUsed,
		"user_accounts_pct":       userAccountsPct,
		"user_accounts_remaining": userAccountsRemaining,
		"user_operator_created":   userOperatorCreated,
	}
}
