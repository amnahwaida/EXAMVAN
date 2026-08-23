package models

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

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
	SettingDefaultMaxStorageSize     = "default_max_storage_size"
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

	// SettingMaxAccountsPerIP caps how many accounts may be registered from a
	// single IP within 24 hours ("0" = unlimited). Defense-in-depth layered on
	// top of Turnstile against mass-registration.
	SettingMaxAccountsPerIP = "max_accounts_per_ip"

	// SettingMaxApprovalsPerExam caps how many devices may hold an APPROVED
	// approval row for one exam ("0" = unlimited). When the server-side
	// auto-approve flag is on and the cap is reached, further request-approval
	// calls fall back to the pending queue instead of approving — an
	// anti-spam brake so a leaked/stolen token cannot mint unlimited approved
	// devices (and their submissions rows) for a single exam.
	SettingMaxApprovalsPerExam = "max_approvals_per_exam"

	// Approval-cleanup job tuning (see admin.StartApprovalCleanupJob): how
	// often the stale-approvals purge pass runs, how long after an exam's
	// end_time its rows are left alone (grace), and how old a pending/approved
	// row on an INACTIVE exam must be before it is purged. Units: minutes for
	// the interval, hours for the two staleness windows. SuperAdmin-tunable so
	// an unusually spam-heavy school can tighten the purge without a redeploy.
	SettingApprovalCleanupIntervalMinutes  = "approval_cleanup_interval_minutes"
	SettingApprovalCleanupEndedGraceHours  = "approval_cleanup_ended_grace_hours"
	SettingApprovalCleanupInactiveTTLHours = "approval_cleanup_inactive_ttl_hours"

	// Access-log retention tuning (see admin.StartAccessLogRetentionJob):
	// student_access_logs tumbuh sebanding dengan jumlah perangkat × durasi
	// ujian (tiap perangkat ±1 heartbeat/menit di-flush ke DB oleh
	// heartbeat-flusher). Tanpa pembersihan berkala, partisi disk server yang
	// kecil (mis. thin-client 13 GB) bisa habis dalam hitungan jam pada event
	// besar. Units: days for the retention window, minutes for the loop
	// interval. SuperAdmin-tunable via saas_settings.
	SettingAccessLogRetentionDays            = "access_log_retention_days"
	SettingAccessLogRetentionIntervalMinutes = "access_log_retention_interval_minutes"
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
	SettingDefaultActiveDays:         "14",
	SettingDefaultMaxConcurrentExams: "2",
	SettingDefaultMaxStorageSize:     "52428800",
	SettingAndroidVersion:            "2.7.3",
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

	// Default per-IP cap: 3 accounts per 24h. Schools behind a shared NAT can
	// raise this in SaaS settings if several teachers register from one IP.
	SettingMaxAccountsPerIP: "3",

	// Default per-exam auto-approve device cap (see SettingMaxApprovalsPerExam).
	// 500 covers even large exam rooms; schools beyond that can raise it.
	SettingMaxApprovalsPerExam: "500",

	// Approval-cleanup job defaults mirror the original code constants: one
	// pass every 15 minutes, 1h grace after an exam ends, 24h TTL on inactive
	// exams (see admin/approval_cleanup_job.go).
	SettingApprovalCleanupIntervalMinutes:  "15",
	SettingApprovalCleanupEndedGraceHours:  "1",
	SettingApprovalCleanupInactiveTTLHours: "24",

	// Access-log retention defaults: keep audit history for 90 days, sweep
	// once per hour. 90 hari cukup untuk rekap semester tanpa membiarkan
	// tabel tumbuh tanpa batas di disk server yang kecil.
	SettingAccessLogRetentionDays:            "90",
	SettingAccessLogRetentionIntervalMinutes: "60",
}

// ---------------------------------------------------------------------------
// saas_settings read cache
// ---------------------------------------------------------------------------
//
// Why this exists: GetSaasSetting sits on the HOT student path. The
// middleware.AndroidVersionCheck calls it for EVERY /api/exams/* request, and
// SubmitExam/AccessLog call it again inside their handlers. At wave scale
// (hundreds of devices joining one exam through a single NAT, see README
// "Kapasitas Satu NAT") that is +1-2 PostgreSQL queries per request to read
// values that almost never change — pure load on the weakest component of a
// small server, bought for nothing.
//
// Design: an in-process TTL cache keyed per (pool, key).
//   - Keyed per POOL so integration tests that run several pools inside one
//     process never observe each other's (stale) values.
//   - TTL 30s bounds cross-process staleness: a SuperAdmin edit made in
//     another replica/process becomes visible here within 30s, while edits
//     made IN THIS PROCESS are visible immediately because SetSaasSetting
//     invalidates the entry it writes.
//   - Missing rows are ALSO cached (found=false): endpoints polling a key
//     that was never set stop hitting the DB for it. SeedDefaultSettings
//     inserts every default at boot, so misses should be rare — but they are
//     cheap to remember.
//
// Memory bound: entries are bounded by (#pools × #distinct keys read), i.e.
// a handful of pools × ~40 known setting names — kilobytes.
const saasSettingCacheTTL = 30 * time.Second

type saasSettingCacheKey struct {
	pool *pgxpool.Pool
	key  string
}

type saasSettingCacheEntry struct {
	value   string
	found   bool
	expires time.Time
}

var (
	saasSettingCacheMu sync.RWMutex
	saasSettingCache   = make(map[saasSettingCacheKey]saasSettingCacheEntry)
)

// invalidateSaasSettingCache drops the cached entry for (pool, key) so the
// next read goes back to the database. Called after every successful write.
func invalidateSaasSettingCache(pool *pgxpool.Pool, key string) {
	saasSettingCacheMu.Lock()
	delete(saasSettingCache, saasSettingCacheKey{pool: pool, key: key})
	saasSettingCacheMu.Unlock()
}

// GetSaasSetting retrieves a setting value by key.
// Returns the value as a string. If the key does not exist, returns an empty string
// and no error (the caller should use GetSaasSettingWithDefault for a fallback).
// Reads are served from the per-process TTL cache (see above); a database
// error other than a missing row is never cached.
func GetSaasSetting(ctx context.Context, pool *pgxpool.Pool, key string) (string, error) {
	cacheKey := saasSettingCacheKey{pool: pool, key: key}

	saasSettingCacheMu.RLock()
	entry, ok := saasSettingCache[cacheKey]
	saasSettingCacheMu.RUnlock()
	if ok && time.Now().Before(entry.expires) {
		if !entry.found {
			return "", nil // row memang tidak ada — konsisten dengan perilaku lama
		}
		return entry.value, nil
	}

	var value string
	err := pool.QueryRow(ctx, `SELECT value FROM saas_settings WHERE key = $1`, key).Scan(&value)
	if err != nil {
		if err == pgx.ErrNoRows {
			saasSettingCacheMu.Lock()
			saasSettingCache[cacheKey] = saasSettingCacheEntry{
				expires: time.Now().Add(saasSettingCacheTTL),
			}
			saasSettingCacheMu.Unlock()
			return "", nil
		}
		return "", fmt.Errorf("get setting %s: %w", key, err)
	}

	saasSettingCacheMu.Lock()
	saasSettingCache[cacheKey] = saasSettingCacheEntry{
		value:   value,
		found:   true,
		expires: time.Now().Add(saasSettingCacheTTL),
	}
	saasSettingCacheMu.Unlock()
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
// otherwise a new row is inserted. The read cache entry for this key is
// invalidated on success so the new value is visible immediately in this
// process (other processes catch up within saasSettingCacheTTL).
func SetSaasSetting(ctx context.Context, pool *pgxpool.Pool, key, value string) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO saas_settings (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = $2`,
		key, value)
	if err != nil {
		return fmt.Errorf("set setting %s: %w", key, err)
	}
	invalidateSaasSettingCache(pool, key)
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
