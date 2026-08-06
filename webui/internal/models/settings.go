package models

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ParseDomainList splits a whitelist string (comma / semicolon / whitespace /
// newline separated) into normalized, lowercased domains, stripping any
// leading "@", "*.", or dots. Order is preserved; blanks are dropped.
func ParseDomainList(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		d := strings.ToLower(strings.TrimSpace(f))
		d = strings.TrimPrefix(d, "@")
		d = strings.TrimPrefix(d, "*.")
		d = strings.Trim(d, ".")
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

// EmailDomainAllowed reports whether the given email's domain is permitted by
// the whitelist. An empty whitelist allows ANY domain. Each entry matches the
// exact domain OR any of its subdomains (e.g. "sch.id" allows "x.sch.id").
func EmailDomainAllowed(whitelist, email string) bool {
	entries := ParseDomainList(whitelist)
	if len(entries) == 0 {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return false
	}
	dom := strings.ToLower(strings.TrimSpace(email[at+1:]))
	if dom == "" {
		return false
	}
	for _, e := range entries {
		if dom == e || strings.HasSuffix(dom, "."+e) {
			return true
		}
	}
	return false
}

// SaasSetting represents a row from the saas_settings table.
type SaasSetting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Default settings keys.
const (
	SettingEmailVerificationEnabled  = "email_verification_enabled"
	SettingEmailDomainWhitelist      = "email_domain_whitelist"
	SettingSMTPHost                  = "smtp_host"
	SettingSMTPPort                  = "smtp_port"
	SettingSMTPUser                  = "smtp_user"
	SettingSMTPPassword              = "smtp_password"
	SettingSMTPSenderName            = "smtp_sender_name"
	SettingDefaultMaxExams           = "default_max_exams"
	SettingDefaultMaxPDFSize         = "default_max_pdf_size"
	SettingDefaultActiveDays         = "default_active_days"
	SettingDefaultMaxConcurrentExams = "default_max_concurrent_exams"
	SettingAndroidVersion            = "android_version"
	SettingWebappVersion             = "webapp_version"
	SettingCertificateFingerprint    = "certificate_fingerprint"
	SettingSEOTitle                  = "seo_title"
	SettingSEODescription            = "seo_description"
	SettingSEOKeywords               = "seo_keywords"
	SettingSEOIndex                  = "seo_index"
	SettingFooterText                = "footer_text"
	SettingFooterTagline             = "footer_tagline"

	// Monetization feature toggles (SuperAdmin). "1" = enabled, "0" = disabled.
	SettingVoucherRedeemEnabled = "voucher_redeem_enabled" // redeem promo/voucher codes

	// Cloudflare Turnstile bot protection on the public registration form.
	// Enabled = "1" makes /register render the Turnstile widget and rejects
	// submissions whose response token fails siteverify. Keys are managed by
	// SuperAdmin in the SaaS settings panel (https://dash.cloudflare.com).
	SettingTurnstileEnabled   = "turnstile_enabled"
	SettingTurnstileSiteKey   = "turnstile_site_key"
	SettingTurnstileSecretKey = "turnstile_secret_key"
)

// Default settings values as defined in the Python app.py.
var DefaultSettings = map[string]string{
	SettingEmailVerificationEnabled: "0",
	// Trusted email domains allowed for registration. Each entry also matches
	// its subdomains (e.g. "sch.id" allows "smanegeri1.sch.id"). Empty = allow
	// any domain. SuperAdmin can edit this in SaaS settings.
	SettingEmailDomainWhitelist:      "gmail.com,googlemail.com,yahoo.com,yahoo.co.id,outlook.com,hotmail.com,live.com,icloud.com,proton.me,protonmail.com,sch.id,ac.id,go.id",
	SettingSMTPHost:                  "smtp.gmail.com",
	SettingSMTPPort:                  "587",
	SettingSMTPUser:                  "",
	SettingSMTPPassword:              "",
	SettingSMTPSenderName:            "EXAMVAN",
	SettingDefaultMaxExams:           "3",
	SettingDefaultMaxPDFSize:         "1048576",
	SettingDefaultActiveDays:         "1",
	SettingDefaultMaxConcurrentExams: "2",
	SettingAndroidVersion:            "2.2.0",
	SettingWebappVersion:             "2.2.0",
	SettingCertificateFingerprint:    "",
	SettingSEOTitle:                  "EXAMVAN - Aplikasi Ujian Online Aman & Tertib",
	SettingSEODescription:            "EXAMVAN adalah aplikasi ujian online mandiri dengan sistem keamanan tinggi terhindar dari kecurangan.",
	SettingSEOKeywords:               "examvan, ujian online, ujian sekolah",
	SettingSEOIndex:                  "1",

	// Footer teks yang tampil di semua halaman publik (SuperAdmin bisa edit).
	SettingFooterText:    "© 2026 EXAMVAN Team. All rights reserved.",
	SettingFooterTagline: "Dibuat khusus untuk pengujian sekolah digital mandiri yang aman.",

	// Monetization toggle defaults to enabled to preserve existing behavior.
	SettingVoucherRedeemEnabled: "1",

	// Turnstile bot protection defaults to disabled; the SuperAdmin enables it
	// and supplies site + secret keys from Cloudflare's Turnstile dashboard.
	SettingTurnstileEnabled:   "0",
	SettingTurnstileSiteKey:   "",
	SettingTurnstileSecretKey: "",
}

// GetSaasSetting retrieves a setting value by key.
// Returns the value as a string. If the key does not exist, returns an empty string
// and no error (the caller should use GetSaasSettingWithDefault for a fallback).
func GetSaasSetting(ctx context.Context, pool *pgxpool.Pool, key string) (string, error) {
	var value string
	err := pool.QueryRow(ctx, `SELECT value FROM saas_settings WHERE key = $1`, key).Scan(&value)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("get setting %s: %w", key, err)
	}
	return value, nil
}

// GetSaasSettingWithDefault retrieves a setting value, falling back to a default if not found.
func GetSaasSettingWithDefault(ctx context.Context, pool *pgxpool.Pool, key, defaultVal string) string {
	value, err := GetSaasSetting(ctx, pool, key)
	if err != nil || value == "" {
		return defaultVal
	}
	return value
}

// SetSaasSetting upserts a setting value. If the key already exists it is updated;
// otherwise a new row is inserted.
func SetSaasSetting(ctx context.Context, pool *pgxpool.Pool, key, value string) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO saas_settings (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = $2`,
		key, value)
	if err != nil {
		return fmt.Errorf("set setting %s: %w", key, err)
	}
	return nil
}

// GetAllSaasSettings retrieves all settings as a map.
func GetAllSaasSettings(ctx context.Context, pool *pgxpool.Pool) (map[string]string, error) {
	rows, err := pool.Query(ctx, `SELECT key, value FROM saas_settings`)
	if err != nil {
		return nil, fmt.Errorf("get all settings: %w", err)
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		settings[k] = v
	}
	if err := rows.Err(); err != nil {
		log.Printf("rows iteration error: %v", err)
	}

	for k, v := range DefaultSettings {
		if _, exists := settings[k]; !exists {
			settings[k] = v
		}
	}
	return settings, nil
}

// SeedDefaultSettings ensures all default settings exist in the database.
// Non-existent keys are inserted; existing keys are left unchanged unless
// they match known upgrade paths (e.g., version bumps).
func SeedDefaultSettings(ctx context.Context, pool *pgxpool.Pool) error {
	for k, v := range DefaultSettings {
		existing, err := GetSaasSetting(ctx, pool, k)
		if err != nil {
			return fmt.Errorf("seed setting %s: %w", k, err)
		}
		if existing == "" {
			if err := SetSaasSetting(ctx, pool, k, v); err != nil {
				return fmt.Errorf("seed setting %s: %w", k, err)
			}
		}
	}
	return nil
}

// GetSaasSettingInt retrieves a setting and parses it as an integer.
// Returns 0 if not found or not parseable.
func GetSaasSettingInt(ctx context.Context, pool *pgxpool.Pool, key string, defaultVal int) int {
	val, err := GetSaasSetting(ctx, pool, key)
	if err != nil || val == "" {
		return defaultVal
	}
	var i int
	if _, err := fmt.Sscanf(val, "%d", &i); err != nil {
		return defaultVal
	}
	return i
}

// GetSaasSettingBool retrieves a setting and parses it as a boolean.
// Returns true when the value is "1" or "true".
func GetSaasSettingBool(ctx context.Context, pool *pgxpool.Pool, key string, defaultVal bool) bool {
	val, err := GetSaasSetting(ctx, pool, key)
	if err != nil || val == "" {
		return defaultVal
	}
	return val == "1" || val == "true"
}
