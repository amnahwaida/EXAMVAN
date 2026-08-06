package admin

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/examvan/webui/internal/models"
)

// BillingPage renders the slimmed-down package & voucher page. Purchasing and
// payment-gateway flows were removed; the page now only shows the account's
// current package/quota usage and lets non-super-admins redeem a voucher code.
func BillingPage() gin.HandlerFunc {
	return func(c *gin.Context) {
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
			}
		}

		renderAdminPage(c, "admin/billing.html", gin.H{
			"active_page":          "billing",
			"voucher_enabled":      voucherEnabled,
			"user_package":         userPackage,
			"user_expires_at":      userExpires,
			"user_max_total_exams": userMaxTotal,
			"user_max_concurrent":  userMaxConcurrent,
			"user_max_pdf_size_mb": roundTo(float64(userMaxPDF)/(1024*1024), 1),
			"user_max_storage_mb":  roundTo(float64(userMaxStorage)/(1024*1024), 2),
			"user_is_super":        isSuper,
			"user_expired":         userExpired,
		})
	}
}