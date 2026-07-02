package api

import (
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/examvan/webui/internal/models"
)

// WhatsappWebhook handles incoming WhatsApp messages from Fonnte API webhook.
func WhatsappWebhook() gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Sender  string `json:"sender"`
			Message string `json:"message"`
		}

		if err := c.ShouldBindJSON(&body); err != nil {
			body.Sender = c.PostForm("sender")
			body.Message = c.PostForm("message")
		}

		sender := strings.TrimSpace(body.Sender)
		message := strings.TrimSpace(body.Message)

		if sender == "" || message == "" {
			c.JSON(http.StatusOK, gin.H{"status": false, "message": "Payload tidak lengkap"})
			return
		}

		// Check if message matches the verification format
		if !strings.Contains(strings.ToLower(message), "verifikasi pendaftaran") {
			c.JSON(http.StatusOK, gin.H{"status": false, "message": "Pesan bukan verifikasi pendaftaran"})
			return
		}

		username, code := parseWebhookMessage(message)
		if username == "" || code == "" {
			c.JSON(http.StatusOK, gin.H{"status": false, "message": "Format pesan tidak sesuai template"})
			return
		}

		pool := getPool(c)
		if pool == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"status": false, "message": "Database error"})
			return
		}
		ctx := c.Request.Context()

		var u models.AdminUser
		err := pool.QueryRow(ctx,
			`SELECT id, username, whatsapp_number, status, otp_code FROM admin_users 
			 WHERE LOWER(username) = LOWER($1) AND otp_code = $2 AND status = 'pending_otp'`,
			username, code).Scan(&u.ID, &u.Username, &u.WhatsappNumber, &u.Status, &u.OTPCode)

		if err != nil {
			log.Printf("Webhook: User tidak ditemukan / kode tidak valid untuk username=%s, kode=%s: %v", username, code, err)
			c.JSON(http.StatusOK, gin.H{"status": false, "message": "Akun tidak ditemukan atau kode kedaluwarsa"})
			return
		}

		// Normalize phone numbers to ensure exact match
		normSender := normalizePhoneNumber(sender)
		normRegistered := normalizePhoneNumber(u.WhatsappNumber)

		if normSender != normRegistered {
			log.Printf("Webhook: Ketidakcocokan nomor WhatsApp. Pengirim=%s (norm=%s), Terdaftar=%s (norm=%s)",
				sender, normSender, u.WhatsappNumber, normRegistered)
			c.JSON(http.StatusOK, gin.H{"status": false, "message": "Nomor WhatsApp pengirim tidak cocok dengan yang didaftarkan"})
			return
		}

		// Activate user status
		_, err = pool.Exec(ctx,
			`UPDATE admin_users SET status = 'active', otp_code = NULL, otp_expiry = NULL WHERE id = $1`,
			u.ID)
		if err != nil {
			log.Printf("Webhook: Gagal mengaktifkan user %s: %v", username, err)
			c.JSON(http.StatusInternalServerError, gin.H{"status": false, "message": "Gagal mengaktifkan akun"})
			return
		}

		log.Printf("Webhook: User %s (ID: %d) berhasil diverifikasi dan diaktifkan via WhatsApp", username, u.ID)

		c.JSON(http.StatusOK, gin.H{
			"status":  true,
			"message": "Verifikasi sukses! Akun Anda telah aktif.",
		})
	}
}

// GetRegisterStatus returns the activation status of a pending user.
func GetRegisterStatus() gin.HandlerFunc {
	return func(c *gin.Context) {
		username := strings.TrimSpace(c.Query("username"))
		if username == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Username wajib diisi"})
			return
		}

		pool := getPool(c)
		if pool == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Database error"})
			return
		}
		ctx := c.Request.Context()

		var status string
		err := pool.QueryRow(ctx, `SELECT status FROM admin_users WHERE LOWER(username) = LOWER($1)`, username).Scan(&status)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "User tidak ditemukan"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"status":  status,
		})
	}
}

// Helper to normalize phone number (handles 08 -> 628 transition and symbols stripping)
func normalizePhoneNumber(num string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, num)

	if strings.HasPrefix(cleaned, "0") {
		cleaned = "62" + cleaned[1:]
	}
	return cleaned
}

// Helper to parse message using regex to find username and code
func parseWebhookMessage(msg string) (string, string) {
	reUser := regexp.MustCompile(`(?i)username:\s*([a-zA-Z0-9_.-]+)`)
	reCode := regexp.MustCompile(`(?i)kode:\s*([a-zA-Z0-9]+)`)

	matchUser := reUser.FindStringSubmatch(msg)
	matchCode := reCode.FindStringSubmatch(msg)

	var username, code string
	if len(matchUser) > 1 {
		username = matchUser[1]
	}
	if len(matchCode) > 1 {
		code = matchCode[1]
	}

	return username, code
}
