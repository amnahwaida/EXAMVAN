package helpers

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"time"
)

// sendSMTPMessage delivers a plain-text UTF-8 email via SMTP. It is the shared
// transport used by the verification and password-reset emails.
func sendSMTPMessage(smtpHost, smtpPort, smtpUser, smtpPassword, senderName, toEmail, subject, body string) error {
	msg := []byte(fmt.Sprintf(
		"From: %s <%s>\r\n"+
			"To: %s\r\n"+
			"Subject: %s\r\n"+
			"MIME-Version: 1.0\r\n"+
			"Content-Type: text/plain; charset=utf-8\r\n"+
			"\r\n"+
			"%s",
		senderName, smtpUser, toEmail, subject, body,
	))

	addr := fmt.Sprintf("%s:%s", smtpHost, smtpPort)
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}

	var conn net.Conn
	var err error

	if smtpPort == "465" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
			ServerName: smtpHost,
		})
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	client, err := smtp.NewClient(conn, smtpHost)
	if err != nil {
		return fmt.Errorf("failed to initialize SMTP client: %w", err)
	}
	defer client.Close()

	if smtpPort != "465" {
		if hasStartTLS, _ := client.Extension("STARTTLS"); hasStartTLS {
			config := &tls.Config{ServerName: smtpHost}
			if err := client.StartTLS(config); err != nil {
				return fmt.Errorf("failed to start STARTTLS: %w", err)
			}
		}
	}

	if smtpUser != "" || smtpPassword != "" {
		auth := smtp.PlainAuth("", smtpUser, smtpPassword, smtpHost)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}

	if err := client.Mail(smtpUser); err != nil {
		return fmt.Errorf("SMTP mail command failed: %w", err)
	}
	if err := client.Rcpt(toEmail); err != nil {
		return fmt.Errorf("SMTP rcpt command failed: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP data command failed: %w", err)
	}
	if _, err = w.Write(msg); err != nil {
		return fmt.Errorf("failed to write email body: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("failed to close data writer: %w", err)
	}

	return client.Quit()
}

// SendVerificationEmail sends an email containing the OTP verification code.
func SendVerificationEmail(smtpHost, smtpPort, smtpUser, smtpPassword, senderName, toEmail, username, otpCode string) error {
	subject := "Verifikasi Pendaftaran Akun EXAMVAN"
	body := fmt.Sprintf(
		"Halo %s,\n\n"+
			"Terima kasih telah mendaftar di EXAMVAN.\n"+
			"Berikut adalah kode verifikasi OTP Anda:\n\n"+
			"👉 KODE OTP: %s\n\n"+
			"Masukkan kode di atas pada halaman verifikasi untuk mengaktifkan akun Anda.\n"+
			"Kode ini berlaku selama 15 menit.\n\n"+
			"Salam,\n%s",
		username, otpCode, senderName,
	)
	return sendSMTPMessage(smtpHost, smtpPort, smtpUser, smtpPassword, senderName, toEmail, subject, body)
}

// SendPasswordResetEmail sends an email containing the password-reset OTP code.
func SendPasswordResetEmail(smtpHost, smtpPort, smtpUser, smtpPassword, senderName, toEmail, username, otpCode string) error {
	subject := "Reset Password Akun EXAMVAN"
	body := fmt.Sprintf(
		"Halo %s,\n\n"+
			"Kami menerima permintaan untuk mereset password akun EXAMVAN Anda.\n"+
			"Berikut adalah kode verifikasi (OTP) untuk mereset password:\n\n"+
			"👉 KODE OTP: %s\n\n"+
			"Masukkan kode di atas beserta password baru Anda pada halaman reset password.\n"+
			"Kode ini berlaku selama 15 menit.\n\n"+
			"Jika Anda tidak meminta reset password, abaikan email ini — password Anda tidak berubah.\n\n"+
			"Salam,\n%s",
		username, otpCode, senderName,
	)
	return sendSMTPMessage(smtpHost, smtpPort, smtpUser, smtpPassword, senderName, toEmail, subject, body)
}

// TestSMTPConnection tests the SMTP server connection and credentials.
func TestSMTPConnection(smtpHost, smtpPort, smtpUser, smtpPassword string) error {
	if smtpHost == "" || smtpPort == "" {
		return fmt.Errorf("SMTP Host dan SMTP Port tidak boleh kosong")
	}

	addr := fmt.Sprintf("%s:%s", smtpHost, smtpPort)
	dialer := &net.Dialer{
		Timeout: 8 * time.Second,
	}

	var conn net.Conn
	var err error

	if smtpPort == "465" {
		// Secure connection directly via SSL/TLS
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
			ServerName: smtpHost,
		})
	} else {
		// Non-secure standard TCP connection (usually 587 or 25)
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("gagal terhubung ke host SMTP %s: %w", addr, err)
	}
	defer conn.Close()

	// Set connection deadline to prevent hangs (e.g., waiting for SMTP banner on SSL/TLS port mismatch)
	_ = conn.SetDeadline(time.Now().Add(6 * time.Second))

	client, err := smtp.NewClient(conn, smtpHost)
	if err != nil {
		return fmt.Errorf("gagal menginisialisasi client SMTP: %w", err)
	}
	defer client.Close()

	// Negotiate STARTTLS for non-465 ports if the server supports it
	if smtpPort != "465" {
		if hasStartTLS, _ := client.Extension("STARTTLS"); hasStartTLS {
			config := &tls.Config{ServerName: smtpHost}
			if err := client.StartTLS(config); err != nil {
				return fmt.Errorf("gagal memulai STARTTLS: %w", err)
			}
		}
	}

	// Perform plain auth if user credentials are provided
	if smtpUser != "" || smtpPassword != "" {
		auth := smtp.PlainAuth("", smtpUser, smtpPassword, smtpHost)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("autentikasi gagal (periksa kembali email/password): %w", err)
		}
	}

	return nil
}
