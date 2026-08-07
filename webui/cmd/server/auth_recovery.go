package main

import (
	"crypto/rand"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/helpers"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// otpTTL is how long a verification / reset OTP stays valid.
const otpTTL = 15 * time.Minute

// otpResendCooldown is the minimum gap between OTP (re)sends for one account.
const otpResendCooldown = 60 * time.Second

// maxOTPAttempts is the number of wrong OTP guesses allowed before the code is
// invalidated (the user must request a new one). Guards against brute force
// beyond the per-IP rate limit (e.g. distributed guessing).
const maxOTPAttempts = 5

// generateOTP returns a random 6-digit numeric OTP.
func generateOTP() string {
	const digits = "0123456789"
	b := make([]byte, 6)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			b[i] = digits[time.Now().UnixNano()%10]
		} else {
			b[i] = digits[n.Int64()]
		}
	}
	return string(b)
}

// smtpSettings bundles the SMTP configuration read from SaaS settings.
type smtpSettings struct {
	host, port, user, password, sender string
}

func (s smtpSettings) configured() bool { return s.host != "" && s.user != "" }

func loadSMTPSettings(c *gin.Context, pool *pgxpool.Pool) smtpSettings {
	ctx := c.Request.Context()
	return smtpSettings{
		host:     models.GetSaasSettingWithDefault(ctx, pool, models.SettingSMTPHost, "smtp.gmail.com"),
		port:     models.GetSaasSettingWithDefault(ctx, pool, models.SettingSMTPPort, "587"),
		user:     models.GetSaasSettingWithDefault(ctx, pool, models.SettingSMTPUser, ""),
		password: models.GetSaasSettingWithDefault(ctx, pool, models.SettingSMTPPassword, ""),
		sender:   models.GetSaasSettingWithDefault(ctx, pool, models.SettingSMTPSenderName, "EXAMVAN"),
	}
}

// dbFromContext returns the pool or nil.
func dbFromContext(c *gin.Context) *pgxpool.Pool {
	if v, ok := c.Get("db"); ok && v != nil {
		if p, ok := v.(*pgxpool.Pool); ok {
			return p
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Resend registration OTP — POST /register/resend  (JSON)
// ---------------------------------------------------------------------------

func resendOTPHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		username := strings.ToLower(strings.TrimSpace(c.Query("username")))
		if username == "" {
			username = strings.ToLower(strings.TrimSpace(c.PostForm("username")))
		}
		if username == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Username tidak valid."})
			return
		}

		// UNIFORM response for every outcome (sent, cooldown, non-pending,
		// non-existent, SMTP-missing) so an attacker cannot enumerate which
		// usernames are awaiting verification. The client enforces its own 60s
		// countdown for UX; a real send only happens when the account is
		// genuinely pending, eligible, and outside the cooldown.
		uniform := func() {
			c.JSON(http.StatusOK, gin.H{"success": true, "message": "Jika akun Anda menunggu verifikasi, kode baru telah dikirim ke email."})
		}

		pool := dbFromContext(c)
		if pool == nil {
			uniform()
			return
		}
		ctx := c.Request.Context()

		var (
			id        int
			email     string
			status    string
			otpExpiry *time.Time
		)
		err := pool.QueryRow(ctx,
			`SELECT id, COALESCE(email, ''), status, otp_expiry FROM admin_users WHERE LOWER(username) = LOWER($1)`,
			username).Scan(&id, &email, &status, &otpExpiry)
		if err != nil || status != models.UserStatusPendingOTP {
			uniform()
			return
		}

		// Cooldown: otp_expiry == issuedAt + otpTTL, so issuedAt = otp_expiry - otpTTL.
		if otpExpiry != nil && time.Until(otpExpiry.Add(-otpTTL).Add(otpResendCooldown)) > 0 {
			uniform()
			return
		}

		smtp := loadSMTPSettings(c, pool)
		if !smtp.configured() || email == "" {
			uniform()
			return
		}

		code := generateOTP()
		expiry := time.Now().UTC().Add(otpTTL)
		if _, err := pool.Exec(ctx,
			`UPDATE admin_users SET otp_code = $1, otp_expiry = $2, otp_attempts = 0 WHERE id = $3`, code, expiry, id); err != nil {
			log.Printf("resend otp: update error: %v", err)
			uniform()
			return
		}

		// Send asynchronously so response latency is identical for all cases
		// (no timing side-channel) and the request isn't blocked on SMTP.
		go func(s smtpSettings, to, uname, otp string) {
			if err := helpers.SendVerificationEmail(s.host, s.port, s.user, s.password, s.sender, to, uname, otp); err != nil {
				log.Printf("resend otp: send email error to %s: %v", to, err)
			}
		}(smtp, email, username, code)

		uniform()
	}
}

// ---------------------------------------------------------------------------
// Forgot password — GET/POST /forgot-password
// ---------------------------------------------------------------------------

func forgotPasswordPageHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		data := middleware.TemplateData(c)
		data["version"] = cfg.Version
		applyTurnstileData(c, data)
		c.HTML(http.StatusOK, "public/forgot_password.html", data)
	}
}

func forgotPasswordPostHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		username := strings.ToLower(strings.TrimSpace(c.PostForm("username")))
		data := middleware.TemplateData(c)
		data["version"] = cfg.Version

		applyTurnstileData(c, data)

		if username == "" {
			data["error"] = "Silakan masukkan username akun Anda."
			c.HTML(http.StatusOK, "public/forgot_password.html", data)
			return
		}

		pool := dbFromContext(c)
		if pool == nil {
			data["error"] = "Database tidak tersedia."
			c.HTML(http.StatusOK, "public/forgot_password.html", data)
			return
		}
		ctx := c.Request.Context()

		// Cloudflare Turnstile bot protection (SuperAdmin-managed), fail-closed
		// like registration/login: a failed check stops the neutral flow so
		// scripts cannot spam OTP requests.
		if models.GetSaasSettingBool(ctx, pool, models.SettingTurnstileEnabled, false) {
			secret := models.GetSaasSettingWithDefault(ctx, pool, models.SettingTurnstileSecretKey, "")
			token := strings.TrimSpace(c.PostForm("cf-turnstile-response"))
			if !verifyTurnstileToken(ctx, secret, token, c.ClientIP()) {
				log.Printf("forgot-password blocked: turnstile verification failed for username=%q", username)
				data["error"] = "Verifikasi keamanan gagal. Silakan coba lagi."
				c.HTML(http.StatusOK, "public/forgot_password.html", data)
				return
			}
		}

		// Look up the account. We proceed to the reset page regardless of whether
		// it exists (neutral flow) so account existence is not revealed; the OTP
		// is only actually sent when the account is active + has email + SMTP is
		// configured + no reset code was issued within the cooldown window.
		var (
			id        int
			email     string
			status    string
			otpExpiry *time.Time
		)
		err := pool.QueryRow(ctx,
			`SELECT id, COALESCE(email, ''), status, otp_expiry FROM admin_users WHERE LOWER(username) = LOWER($1)`,
			username).Scan(&id, &email, &status, &otpExpiry)

		inCooldown := otpExpiry != nil && time.Until(otpExpiry.Add(-otpTTL).Add(otpResendCooldown)) > 0
		if err == nil && status == models.UserStatusActive && email != "" && !inCooldown {
			smtp := loadSMTPSettings(c, pool)
			if smtp.configured() {
				code := generateOTP()
				expiry := time.Now().UTC().Add(otpTTL)
				if _, e := pool.Exec(ctx,
					`UPDATE admin_users SET otp_code = $1, otp_expiry = $2, otp_attempts = 0 WHERE id = $3`, code, expiry, id); e == nil {
					// Async send: no timing side-channel + not blocked on SMTP.
					go func(s smtpSettings, to, uname, otp string) {
						if err := helpers.SendPasswordResetEmail(s.host, s.port, s.user, s.password, s.sender, to, uname, otp); err != nil {
							log.Printf("forgot password: send email error to %s: %v", to, err)
						}
					}(smtp, email, username, code)
				} else {
					log.Printf("forgot password: update otp error: %v", e)
				}
			} else {
				log.Printf("forgot password: SMTP not configured, cannot send reset to %s", username)
			}
		}

		// Always show the same neutral outcome and move to the reset step.
		c.Redirect(http.StatusFound, "/reset-password?username="+url.QueryEscape(username))
	}
}

// ---------------------------------------------------------------------------
// Reset password — GET/POST /reset-password
// ---------------------------------------------------------------------------

func resetPasswordPageHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		username := strings.TrimSpace(c.Query("username"))
		if username == "" {
			c.Redirect(http.StatusFound, "/forgot-password")
			return
		}
		data := middleware.TemplateData(c)
		data["version"] = cfg.Version
		data["username"] = username
		c.HTML(http.StatusOK, "public/reset_password.html", data)
	}
}

func resetPasswordPostHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		username := strings.ToLower(strings.TrimSpace(c.Query("username")))
		if username == "" {
			username = strings.ToLower(strings.TrimSpace(c.PostForm("username")))
		}
		otpCode := strings.TrimSpace(c.PostForm("otp_code"))
		password := c.PostForm("password")
		confirm := c.PostForm("password_confirm")

		data := middleware.TemplateData(c)
		data["version"] = cfg.Version
		data["username"] = username

		render := func(msg string) {
			data["error"] = msg
			c.HTML(http.StatusOK, "public/reset_password.html", data)
		}

		if username == "" {
			c.Redirect(http.StatusFound, "/forgot-password")
			return
		}
		if otpCode == "" || password == "" {
			render("Kode OTP dan password baru wajib diisi.")
			return
		}
		if len(password) < 8 {
			render("Password baru minimal 8 karakter.")
			return
		}
		if password != confirm {
			render("Konfirmasi password tidak cocok.")
			return
		}

		pool := dbFromContext(c)
		if pool == nil {
			render("Database tidak tersedia.")
			return
		}
		ctx := c.Request.Context()

		var (
			id        int
			status    string
			dbOTP     *string
			otpExpiry *time.Time
			attempts  int
		)
		err := pool.QueryRow(ctx,
			`SELECT id, status, otp_code, otp_expiry, otp_attempts FROM admin_users WHERE LOWER(username) = LOWER($1)`,
			username).Scan(&id, &status, &dbOTP, &otpExpiry, &attempts)
		// No live reset code -> generic message (does not reveal existence and is
		// not counted as a guess). Covers unknown user, pending account, or a
		// code that was already used/invalidated.
		if err != nil || status == models.UserStatusPendingOTP || dbOTP == nil || *dbOTP == "" {
			render("Kode OTP salah atau sudah tidak berlaku. Silakan minta kode baru.")
			return
		}
		if otpExpiry == nil || time.Now().UTC().After(*otpExpiry) {
			render("Kode OTP telah kedaluwarsa. Silakan minta kode baru.")
			return
		}
		// Wrong code: count the guess and invalidate the OTP once the limit is
		// hit — but keep the message identical to the "no live code" case above
		// so an attacker cannot distinguish (via forgot->reset) which accounts
		// actually have a pending reset code.
		if *dbOTP != otpCode {
			if attempts+1 >= maxOTPAttempts {
				_, _ = pool.Exec(ctx, `UPDATE admin_users SET otp_code = NULL, otp_expiry = NULL, otp_attempts = 0 WHERE id = $1`, id)
			} else {
				_, _ = pool.Exec(ctx, `UPDATE admin_users SET otp_attempts = otp_attempts + 1 WHERE id = $1`, id)
			}
			render("Kode OTP salah atau sudah tidak berlaku. Silakan minta kode baru.")
			return
		}

		hash, err := models.HashPassword(password)
		if err != nil {
			log.Printf("reset password: hash error: %v", err)
			render("Gagal memproses password baru. Coba lagi.")
			return
		}
		if _, err := pool.Exec(ctx,
			`UPDATE admin_users SET password_hash = $1, otp_code = NULL, otp_expiry = NULL, otp_attempts = 0 WHERE id = $2`,
			hash, id); err != nil {
			log.Printf("reset password: update error: %v", err)
			render("Gagal menyimpan password baru. Coba lagi.")
			return
		}

		session := sessions.Default(c)
		session.AddFlash("Password berhasil diubah. Silakan login dengan password baru Anda.")
		_ = session.Save()
		c.Redirect(http.StatusFound, "/login")
	}
}
