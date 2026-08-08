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
// Ignores non-numeric suffixes; mirrors middleware/version.go's parser.
func CompareVersions(a, b string) int {
	ap := parseVersionParts(a)
	bp := parseVersionParts(b)
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if ap[i] > bp[i] {
			return 1
		}
		if ap[i] < bp[i] {
			return -1
		}
	}
	switch {
	case len(ap) < len(bp):
		return -1
	case len(ap) > len(bp):
		return 1
	default:
		return 0
	}
}

// parseVersionParts splits a version string into integer parts.
// e.g. "2.1.10-beta" → []int{2, 1, 10}
func parseVersionParts(v string) []int {
	parts := strings.Split(v, ".")
	result := make([]int, 0, len(parts))
	for _, p := range parts {
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err == nil {
			result = append(result, n)
		}
	}
	return result
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
