package models

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PackageSetting is one row of the SuperAdmin-editable package quota table.
// Sizes are in bytes. Role is a serialized role JSON string ("" = none).
type PackageSetting struct {
	Key                string    `json:"key"`
	Label              string    `json:"label"`
	MaxExams           int64     `json:"max_exams"`
	MaxPDFSize         int64     `json:"max_pdf_size"`
	MaxConcurrentExams int64     `json:"max_concurrent_exams"`
	MaxStorageSize     int64     `json:"max_storage_size"`
	MaxUsers           int64     `json:"max_users"` // sub-account quota (0 = unlimited)
	Role               string    `json:"role"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// PackageSettingKeys is the canonical display order of the fixed packages.
var PackageSettingKeys = []string{
	"free",
	"guru",
	"individu",
	"sekolah_kecil",
	"sekolah_menengah",
	"sekolah_besar",
	"sekolah_unggulan",
}

// ListPackageSettings returns all package quota rows in canonical order.
func ListPackageSettings(ctx context.Context, pool *pgxpool.Pool) ([]PackageSetting, error) {
	rows, err := pool.Query(ctx, `
		SELECT pkg_key, COALESCE(label, ''), max_exams, max_pdf_size,
		       max_concurrent_exams, max_storage_size, max_users, COALESCE(role, ''), updated_at
		FROM package_settings
		ORDER BY array_position($1::text[], pkg_key)`, PackageSettingKeys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var settings []PackageSetting
	for rows.Next() {
		var s PackageSetting
		if err := rows.Scan(
			&s.Key, &s.Label, &s.MaxExams, &s.MaxPDFSize,
			&s.MaxConcurrentExams, &s.MaxStorageSize, &s.MaxUsers, &s.Role, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		settings = append(settings, s)
	}
	if settings == nil {
		settings = []PackageSetting{}
	}
	return settings, nil
}
