package models

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// DB-backed CreateUser regression guards (skipped when TEST_DATABASE_URL is
// unset — same infra as authenticate_test.go, reusing setupAuthTestDB).
//
// The operator-account bug class: a CreateUser INSERT that trips a DB
// constraint or drifts from the schema fails EVERY account creation with a
// raw 500 ("Gagal membuat user"), which the UI shows as "gagal membuat akun".
// These tests pin the INSERT contract so a future schema/column change that
// would silently break all account creation fails loudly here instead.
// ---------------------------------------------------------------------------

// TestCreateUserAllColumnsRoundTrip inserts a user with EVERY column the
// CreateUser INSERT supports populated, then reads the row back and asserts
// every value survived. A column added to the schema (or dropped from the
// INSERT / DefaultAdminUserColumns) would make this test fail — the same
// class of silent breakage that previously hid behind the operator email bug.
func TestCreateUserAllColumnsRoundTrip(t *testing.T) {
	pool := setupAuthTestDB(t)
	ctx := context.Background()

	// A real operator row to reference from created_by (FK to admin_users.id).
	op, err := CreateUser(ctx, pool, &AdminUser{
		Username: "roundtrip-op", Name: "Roundtrip Op",
		PasswordHash: "pass-roundtrip-op", Status: UserStatusActive,
		Instansi: "SMK Roundtrip", Package: "free",
		Role: SerializeRoles([]string{RoleOperator}),
	})
	if err != nil {
		t.Fatalf("create operator fixture: %v", err)
	}

	expiresAt := time.Now().UTC().Add(30 * 24 * time.Hour)
	otpExpiry := time.Now().UTC().Add(15 * time.Minute)
	otpCode := "123456"
	u, err := CreateUser(ctx, pool, &AdminUser{
		Username:           "roundtrip-sub",
		Name:               "Roundtrip Sub",
		PasswordHash:       "pass-roundtrip-sub",
		Status:             UserStatusPendingOTP,
		Instansi:           "SMK Roundtrip",
		Role:               SerializeRoles([]string{RoleGuru, RolePengawas}),
		MaxExams:           7,
		MaxPDFSize:         3 * 1024 * 1024,
		MaxConcurrentExams: 5,
		MaxStorageSize:     120 * 1024 * 1024,
		WhatsappNumber:     "081234567890",
		Email:              "roundtrip@sekolah.sch.id",
		RegisteredIP:       "203.0.113.99",
		ExpiresAt:          &expiresAt,
		Package:            "free",
		OTPCode:            &otpCode,
		OTPExpiry:          &otpExpiry,
		OperatorCreated:    true,
		CreatedBy:          &op.ID,
	})
	if err != nil {
		t.Fatalf("create user with all columns: %v", err)
	}

	got, err := GetUserByID(ctx, pool, u.ID)
	if err != nil {
		t.Fatalf("reload created user: %v", err)
	}

	if got.Username != "roundtrip-sub" {
		t.Errorf("username = %q, want roundtrip-sub", got.Username)
	}
	if got.Name != "Roundtrip Sub" {
		t.Errorf("name = %q, want Roundtrip Sub", got.Name)
	}
	if got.Status != UserStatusPendingOTP {
		t.Errorf("status = %q, want %q", got.Status, UserStatusPendingOTP)
	}
	if got.Instansi != "SMK Roundtrip" {
		t.Errorf("instansi = %q, want SMK Roundtrip", got.Instansi)
	}
	if got.Role != SerializeRoles([]string{RoleGuru, RolePengawas}) {
		t.Errorf("role = %q, want guru+pengawas", got.Role)
	}
	if got.MaxExams != 7 || got.MaxPDFSize != 3*1024*1024 || got.MaxConcurrentExams != 5 || got.MaxStorageSize != 120*1024*1024 {
		t.Errorf("quotas = (%d,%d,%d,%d), want (7,3145728,5,125829120)",
			got.MaxExams, got.MaxPDFSize, got.MaxConcurrentExams, got.MaxStorageSize)
	}
	if got.WhatsappNumber != "081234567890" {
		t.Errorf("whatsapp_number = %q, want 081234567890", got.WhatsappNumber)
	}
	if got.Email != "roundtrip@sekolah.sch.id" {
		t.Errorf("email = %q, want roundtrip@sekolah.sch.id", got.Email)
	}
	if got.RegisteredIP != "203.0.113.99" {
		t.Errorf("registered_ip = %q, want 203.0.113.99", got.RegisteredIP)
	}
	if got.Package != "free" {
		t.Errorf("package = %q, want free", got.Package)
	}
	if !got.OperatorCreated {
		t.Error("operator_created = false, want true")
	}
	if got.CreatedBy == nil || *got.CreatedBy != op.ID {
		t.Errorf("created_by = %v, want operator id %d", got.CreatedBy, op.ID)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Truncate(time.Microsecond).Equal(expiresAt.Truncate(time.Microsecond)) {
		t.Errorf("expires_at = %v, want %v (Postgres stores microsecond precision)", got.ExpiresAt, expiresAt)
	}
	if got.OTPCode == nil || *got.OTPCode != otpCode {
		t.Errorf("otp_code = %v, want %s", got.OTPCode, otpCode)
	}
	if got.OTPExpiry == nil || !got.OTPExpiry.Truncate(time.Microsecond).Equal(otpExpiry.Truncate(time.Microsecond)) {
		t.Errorf("otp_expiry = %v, want %v (Postgres stores microsecond precision)", got.OTPExpiry, otpExpiry)
	}
	// The plaintext password must never be stored.
	if got.PasswordHash == "pass-roundtrip-sub" {
		t.Error("password_hash stored the plaintext password")
	}
	if !strings.HasPrefix(got.PasswordHash, "$2") {
		t.Errorf("password_hash = %q, want a bcrypt hash", got.PasswordHash)
	}
}

// TestCreateUserEmailConstraintBackstop pins the DB-level guarantee behind the
// handler's friendly pre-check: uq_admin_users_email (LOWER(email) WHERE email
// <> '') rejects ANY second account with the same non-empty email — exact or
// case-variant — even when the handler check is bypassed (direct model call),
// so no account-creation path can ever silently create duplicate-email rows.
// Empty emails are exempt (legacy rows and accounts without a form email).
func TestCreateUserEmailConstraintBackstop(t *testing.T) {
	pool := setupAuthTestDB(t)
	ctx := context.Background()

	mk := func(username, email string) (*AdminUser, error) {
		return CreateUser(ctx, pool, &AdminUser{
			Username: username, Name: username,
			PasswordHash: "pass-" + username, Status: UserStatusActive,
			Instansi: "personal", Package: "free",
			Role:     SerializeRoles([]string{RoleGuru}),
			MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
			MaxStorageSize: 50 * 1024 * 1024,
			Email:          email,
		})
	}

	first, err := mk("email-backstop-1", "Backstop@Sekolah.Sch.ID")
	if err != nil {
		t.Fatalf("create first account: %v", err)
	}
	_ = first

	// Exact duplicate must be rejected by the constraint.
	if _, err := mk("email-backstop-2", "Backstop@Sekolah.Sch.ID"); err == nil {
		t.Fatal("exact duplicate email was accepted — uq_admin_users_email must reject it")
	}
	// Case-variant duplicate must be rejected (index is on LOWER(email)).
	if _, err := mk("email-backstop-3", "backstop@sekolah.sch.id"); err == nil {
		t.Fatal("case-variant duplicate email was accepted — index is on LOWER(email)")
	}

	// Empty emails never collide (partial index WHERE email <> '').
	if _, err := mk("email-backstop-4", ""); err != nil {
		t.Fatalf("first empty-email account: %v", err)
	}
	if _, err := mk("email-backstop-5", ""); err != nil {
		t.Fatalf("second empty-email account: %v", err)
	}
}
