package models

import (
	"context"
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
