package models

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SystemApp struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Platform  string    `json:"platform"`
	Version   string    `json:"version"`
	FilePath  string    `json:"file_path"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BestAndroidApp returns the system_app entry for the Android platform with the
// highest version. Returns nil when no Android entry exists.
func BestAndroidApp(apps []SystemApp) *SystemApp {
	var best *SystemApp
	for i := range apps {
		app := &apps[i]
		if app.Platform != "android" {
			continue
		}
		if best == nil || CompareVersions(app.Version, best.Version) > 0 {
			best = app
		} else if CompareVersions(app.Version, best.Version) == 0 &&
			strings.Contains(strings.ToLower(best.Name), "kiosk") &&
			!strings.Contains(strings.ToLower(app.Name), "kiosk") {
			// Tie-break versi sama: prefer the regular (non-kiosk) client so
			// the primary download card is never the kiosk flavor.
			best = app
		}
	}
	return best
}

// BestAndroidAppVersion returns the version string of the highest-version
// Android system_app, or "" when none exists.
func BestAndroidAppVersion(apps []SystemApp) string {
	if best := BestAndroidApp(apps); best != nil {
		return best.Version
	}
	return ""
}

// CompareVersions returns -1 when a < b, 0 when equal, 1 when a > b.
// Ignores non-numeric suffixes and treats missing trailing parts as 0, so
// "2.4" == "2.4.0" and "2.4.1" > "2.4". This mirrors the Android client's
// UpdateManager.compareVersions (getOrElse { 0 }).
func CompareVersions(a, b string) int {
	ap := parseVersionParts(a)
	bp := parseVersionParts(b)
	for i := 0; i < len(ap) || i < len(bp); i++ {
		var av, bv int
		if i < len(ap) {
			av = ap[i]
		}
		if i < len(bp) {
			bv = bp[i]
		}
		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}
	return 0
}

// parseVersionParts splits a version string into integer parts.
// e.g. "2.1.10-beta" → []int{2, 1, 10}
// A segment whose leading digits are empty (e.g. "2.x.1") contributes 0, never
// skipping the position — mirrors the Android client's UpdateManager parser so
// "2.x.1" and "2.0.1" compare equal on both sides.
func parseVersionParts(v string) []int {
	parts := strings.Split(v, ".")
	result := make([]int, 0, len(parts))
	for _, p := range parts {
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err != nil {
			n = 0
		}
		result = append(result, n)
	}
	return result
}

// EffectiveAndroidRequiredVersion returns the Android app version the server
// currently requires clients to run, based on what is actually available to
// download. It is the single source of truth for the version gate (used by the
// AndroidVersionCheck middleware and /api/health).
//
// When NO Android system_app exists (nothing published to R2), it returns "" —
// callers must then SKIP version enforcement entirely. Otherwise an outdated
// client would be blocked with 426 while having no newer APK to download
// (a deadlock: /download/apk would 503 and the download page would offer no
// button).
//
// When an APK IS available, the required version is the configured
// android_version saas_setting clamped to the available version: never demand
// a version higher than what can actually be downloaded. This also fixes the
// case where an admin raises android_version before uploading the new APK —
// clients are never locked out beyond the newest publishable release.
func EffectiveAndroidRequiredVersion(ctx context.Context, pool *pgxpool.Pool) string {
	if pool == nil {
		return ""
	}

	apps, err := GetAllSystemApps(ctx, pool)
	if err != nil {
		return ""
	}
	configured := GetSaasSettingWithDefault(ctx, pool, SettingAndroidVersion, "")
	return EffectiveAndroidRequiredVersionFrom(configured, apps)
}

// EffectiveAndroidRequiredVersionFrom computes the effective required Android
// version from the configured android_version saas_setting and the published
// system_apps, without touching the database. It backs
// EffectiveAndroidRequiredVersion so the pure decision logic can be unit-tested.
//
// Returns "" when no Android system_app is available (nothing downloadable →
// nothing to enforce). Otherwise returns the configured version clamped to the
// available version: a configured requirement higher than what can actually be
// downloaded is reduced to the available version, so an outdated client is
// never blocked with 426 while having no newer APK to fetch.
func EffectiveAndroidRequiredVersionFrom(configured string, apps []SystemApp) string {
	available := BestAndroidAppVersion(apps)
	if available == "" {
		return "" // nothing downloadable → nothing to enforce
	}
	if configured == "" || CompareVersions(configured, available) > 0 {
		return available
	}
	return configured
}

func GetAllSystemApps(ctx context.Context, pool *pgxpool.Pool) ([]SystemApp, error) {
	rows, err := pool.Query(ctx, "SELECT id, name, platform, version, file_path, size_bytes, created_at, updated_at FROM system_apps ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []SystemApp
	for rows.Next() {
		var app SystemApp
		err := rows.Scan(&app.ID, &app.Name, &app.Platform, &app.Version, &app.FilePath, &app.SizeBytes, &app.CreatedAt, &app.UpdatedAt)
		if err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	return apps, nil
}

func GetSystemAppByID(ctx context.Context, pool *pgxpool.Pool, id int) (SystemApp, error) {
	var app SystemApp
	err := pool.QueryRow(ctx, "SELECT id, name, platform, version, file_path, size_bytes, created_at, updated_at FROM system_apps WHERE id = $1", id).Scan(
		&app.ID, &app.Name, &app.Platform, &app.Version, &app.FilePath, &app.SizeBytes, &app.CreatedAt, &app.UpdatedAt,
	)
	return app, err
}

func CreateSystemApp(ctx context.Context, pool *pgxpool.Pool, app *SystemApp) error {
	return pool.QueryRow(ctx, `
		INSERT INTO system_apps (name, platform, version, file_path, size_bytes)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at
	`, app.Name, app.Platform, app.Version, app.FilePath, app.SizeBytes).Scan(&app.ID, &app.CreatedAt, &app.UpdatedAt)
}

func DeleteSystemApp(ctx context.Context, pool *pgxpool.Pool, id int) error {
	_, err := pool.Exec(ctx, "DELETE FROM system_apps WHERE id = $1", id)
	return err
}

func CheckSystemAppExists(ctx context.Context, pool *pgxpool.Pool, name, platform, version string) (bool, error) {
	var count int
	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM system_apps WHERE name = $1 AND platform = $2 AND version = $3", name, platform, version).Scan(&count)
	return count > 0, err
}
