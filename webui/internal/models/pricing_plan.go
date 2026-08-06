package models

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PricingPlan represents a pricing package shown on the public /pricing page.
type PricingPlan struct {
	ID            int      `json:"id"`
	Key           string   `json:"key"`
	Title         string   `json:"title"`
	Audience      string   `json:"audience"`
	Popular       bool     `json:"popular"`
	Accent        string   `json:"accent"`
	Icon          string   `json:"icon"`
	MaxExams      string   `json:"max_exams"`
	PdfLimit      string   `json:"pdf_limit"`
	StorageLimit  string   `json:"storage_limit"`
	TokenMode     string   `json:"token_mode"`
	Results       string   `json:"results"`
	Answers       string   `json:"answers"`
	Features      string   `json:"features"`      // comma-separated in DB
	FeaturesSlice []string `json:"features_list"` // parsed from Features, not in DB
	PriceBulanan  int64    `json:"price_bulanan"`
	PriceSemester int64    `json:"price_semester"`
	PriceTahunan  int64    `json:"price_tahunan"`
	SortOrder     int      `json:"sort_order"`
	Active        bool     `json:"active"`
}

// parseFeatures splits the comma-separated Features string into FeaturesSlice,
// trimming whitespace from each element.
func (p *PricingPlan) parseFeatures() {
	if p.Features == "" {
		p.FeaturesSlice = []string{}
		return
	}
	parts := strings.Split(p.Features, ",")
	p.FeaturesSlice = make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			p.FeaturesSlice = append(p.FeaturesSlice, trimmed)
		}
	}
}

// InitPricingPlansTable creates the pricing_plans table if it does not exist.
func InitPricingPlansTable(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS pricing_plans (
			id SERIAL PRIMARY KEY,
			key TEXT NOT NULL UNIQUE,
			title TEXT NOT NULL,
			audience TEXT NOT NULL DEFAULT '',
			popular BOOLEAN NOT NULL DEFAULT false,
			accent TEXT NOT NULL DEFAULT '',
			icon TEXT NOT NULL DEFAULT '',
			max_exams TEXT NOT NULL DEFAULT '',
			pdf_limit TEXT NOT NULL DEFAULT '',
			storage_limit TEXT NOT NULL DEFAULT '',
			token_mode TEXT NOT NULL DEFAULT '',
			results TEXT NOT NULL DEFAULT '',
			answers TEXT NOT NULL DEFAULT '',
			features TEXT NOT NULL DEFAULT '',
			price_bulanan BIGINT NOT NULL DEFAULT 0,
			price_semester BIGINT NOT NULL DEFAULT 0,
			price_tahunan BIGINT NOT NULL DEFAULT 0,
			sort_order INT NOT NULL DEFAULT 0,
			active BOOLEAN NOT NULL DEFAULT true,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`)
	if err != nil {
		return fmt.Errorf("init pricing_plans table: %w", err)
	}
	return nil
}

// SeedDefaultPricingPlans inserts the 6 default pricing plans if the table is empty.
func SeedDefaultPricingPlans(ctx context.Context, pool *pgxpool.Pool) error {
	var count int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM pricing_plans`).Scan(&count)
	if err != nil {
		return fmt.Errorf("seed pricing plans count: %w", err)
	}
	if count > 0 {
		return nil
	}

	defaults := []PricingPlan{
		{
			Key: "guru", Title: "Paket Guru",
			Audience: "Guru les, bimbel kecil, tryout kelas",
			Accent: "guru", Icon: "👨‍🏫",
			MaxExams: "1 ujian aktif", PdfLimit: "1 MB per PDF",
			StorageLimit: "100 MB storage",
			TokenMode: "Statis", Results: "Aktif", Answers: "Nonaktif",
			Features:      "Cocok untuk kelas kecil,Support email",
			PriceBulanan:  25000,
			PriceSemester: 125000,
			PriceTahunan:  225000,
			SortOrder:     1, Active: true,
		},
		{
			Key: "individu", Title: "Paket Individu",
			Audience: "Pembuat tryout online, bimbel 1-2 kelas",
			Accent: "individu", Icon: "💻",
			MaxExams: "2 ujian aktif", PdfLimit: "3 MB per PDF",
			StorageLimit: "300 MB storage",
			TokenMode: "Statis + Dinamis", Results: "Aktif", Answers: "Aktif",
			Features:      "Export CSV,Support email + WA",
			PriceBulanan:  50000,
			PriceSemester: 250000,
			PriceTahunan:  450000,
			SortOrder:     2, Active: true,
		},
		{
			Key: "sekolah_kecil", Title: "Sekolah Kecil",
			Audience: "SD / MI, ujian PH / UTS",
			Accent: "kecil", Icon: "🏫",
			MaxExams: "3 ujian aktif", PdfLimit: "3 MB per PDF",
			StorageLimit: "500 MB storage",
			TokenMode: "Statis + Dinamis", Results: "Aktif", Answers: "Nonaktif",
			Features:      "Manajemen Pengguna,Paket hemat sekolah dasar",
			PriceBulanan:  75000,
			PriceSemester: 375000,
			PriceTahunan:  675000,
			SortOrder:     3, Active: true,
		},
		{
			Key: "sekolah_menengah", Title: "Sekolah Menengah",
			Audience: "SMP / MTs, ujian PAS / PAT",
			Accent: "menengah", Icon: "🏢",
			MaxExams: "5 ujian aktif", PdfLimit: "5 MB per PDF",
			StorageLimit: "2 GB storage",
			TokenMode: "Statis + Dinamis", Results: "Aktif", Answers: "Aktif",
			Features:      "Manajemen Pengguna,Panel warna,Dukungan email + WA",
			PriceBulanan:  175000,
			PriceSemester: 875000,
			PriceTahunan:  1575000,
			SortOrder:     4, Active: true,
		},
		{
			Key: "sekolah_besar", Title: "Sekolah Besar",
			Audience: "SMA / MA / SMK, tryout skala besar",
			Popular: true, Accent: "besar", Icon: "🏛️",
			MaxExams: "10 ujian aktif", PdfLimit: "10 MB per PDF",
			StorageLimit: "5 GB storage",
			TokenMode: "Statis + dinamis", Results: "Aktif", Answers: "Aktif",
			Features:      "Manajemen Pengguna,Realtime pengawas,Strict mode,Export CSV lengkap",
			PriceBulanan:  375000,
			PriceSemester: 1875000,
			PriceTahunan:  3375000,
			SortOrder:     5, Active: true,
		},
		{
			Key: "sekolah_unggulan", Title: "Sekolah Unggulan",
			Audience: "Kampus, yayasan, skala kabupaten/kota",
			Accent: "unggulan", Icon: "⭐",
			MaxExams: "Tak terbatas", PdfLimit: "Tak terbatas",
			StorageLimit: "Tak terbatas",
			TokenMode: "Statis + dinamis", Results: "Aktif", Answers: "Aktif",
			Features:      "Manajemen Pengguna,Prioritas infrastruktur,Backup mingguan,SLA 99% uptime",
			PriceBulanan:  750000,
			PriceSemester: 3750000,
			PriceTahunan:  6750000,
			SortOrder:     6, Active: true,
		},
	}

	for _, plan := range defaults {
		_, err := pool.Exec(ctx, `
			INSERT INTO pricing_plans (key, title, audience, popular, accent, icon,
				max_exams, pdf_limit, storage_limit, token_mode,
				results, answers, features,
				price_bulanan, price_semester, price_tahunan, sort_order, active)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		`, plan.Key, plan.Title, plan.Audience, plan.Popular, plan.Accent, plan.Icon,
			plan.MaxExams, plan.PdfLimit, plan.StorageLimit, plan.TokenMode,
			plan.Results, plan.Answers, plan.Features,
			plan.PriceBulanan, plan.PriceSemester, plan.PriceTahunan, plan.SortOrder, plan.Active,
		)
		if err != nil {
			return fmt.Errorf("seed pricing plan %s: %w", plan.Key, err)
		}
	}
	return nil
}

// planColumns is the shared column list for SELECT queries.
const planColumns = `id, key, title, audience, popular, accent, icon,
	max_exams, pdf_limit, storage_limit, token_mode,
	results, answers, features,
	price_bulanan, price_semester, price_tahunan, sort_order, active`

// scanPlan scans a single PricingPlan row from the given scanner.
func scanPlan(scan func(dest ...any) error) (PricingPlan, error) {
	var p PricingPlan
	err := scan(
		&p.ID, &p.Key, &p.Title, &p.Audience, &p.Popular, &p.Accent, &p.Icon,
		&p.MaxExams, &p.PdfLimit, &p.StorageLimit, &p.TokenMode,
		&p.Results, &p.Answers, &p.Features,
		&p.PriceBulanan, &p.PriceSemester, &p.PriceTahunan, &p.SortOrder, &p.Active,
	)
	if err != nil {
		return p, err
	}
	p.parseFeatures()
	return p, nil
}

// GetAllPricingPlans returns all active plans ordered by sort_order.
func GetAllPricingPlans(ctx context.Context, pool *pgxpool.Pool) ([]PricingPlan, error) {
	rows, err := pool.Query(ctx,
		`SELECT `+planColumns+` FROM pricing_plans WHERE active = true ORDER BY sort_order ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plans []PricingPlan
	for rows.Next() {
		p, err := scanPlan(rows.Scan)
		if err != nil {
			return nil, err
		}
		plans = append(plans, p)
	}
	return plans, nil
}

// GetAllPricingPlansAdmin returns ALL plans (including inactive) for admin management.
func GetAllPricingPlansAdmin(ctx context.Context, pool *pgxpool.Pool) ([]PricingPlan, error) {
	rows, err := pool.Query(ctx,
		`SELECT `+planColumns+` FROM pricing_plans ORDER BY sort_order ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plans []PricingPlan
	for rows.Next() {
		p, err := scanPlan(rows.Scan)
		if err != nil {
			return nil, err
		}
		plans = append(plans, p)
	}
	return plans, nil
}

// GetPricingPlanByID returns a single pricing plan by its ID.
func GetPricingPlanByID(ctx context.Context, pool *pgxpool.Pool, id int) (PricingPlan, error) {
	row := pool.QueryRow(ctx,
		`SELECT `+planColumns+` FROM pricing_plans WHERE id = $1`, id,
	)
	return scanPlan(row.Scan)
}

// CreatePricingPlan inserts a new pricing plan and returns the generated ID.
func CreatePricingPlan(ctx context.Context, pool *pgxpool.Pool, plan *PricingPlan) error {
	return pool.QueryRow(ctx, `
		INSERT INTO pricing_plans (key, title, audience, popular, accent, icon,
			max_exams, pdf_limit, storage_limit, token_mode,
			results, answers, features,
			price_bulanan, price_semester, price_tahunan, sort_order, active)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		RETURNING id
	`, plan.Key, plan.Title, plan.Audience, plan.Popular, plan.Accent, plan.Icon,
		plan.MaxExams, plan.PdfLimit, plan.StorageLimit, plan.TokenMode,
		plan.Results, plan.Answers, plan.Features,
		plan.PriceBulanan, plan.PriceSemester, plan.PriceTahunan, plan.SortOrder, plan.Active,
	).Scan(&plan.ID)
}

// UpdatePricingPlan updates an existing pricing plan by ID.
func UpdatePricingPlan(ctx context.Context, pool *pgxpool.Pool, plan *PricingPlan) error {
	_, err := pool.Exec(ctx, `
		UPDATE pricing_plans SET
			key = $1, title = $2, audience = $3, popular = $4, accent = $5, icon = $6,
			max_exams = $7, pdf_limit = $8, storage_limit = $9,
			token_mode = $10, results = $11, answers = $12, features = $13,
			price_bulanan = $14, price_semester = $15, price_tahunan = $16,
			sort_order = $17, active = $18, updated_at = NOW()
		WHERE id = $19
	`, plan.Key, plan.Title, plan.Audience, plan.Popular, plan.Accent, plan.Icon,
		plan.MaxExams, plan.PdfLimit, plan.StorageLimit,
		plan.TokenMode, plan.Results, plan.Answers, plan.Features,
		plan.PriceBulanan, plan.PriceSemester, plan.PriceTahunan,
		plan.SortOrder, plan.Active, plan.ID,
	)
	return err
}

// DeletePricingPlan removes a pricing plan by ID.
func DeletePricingPlan(ctx context.Context, pool *pgxpool.Pool, id int) error {
	_, err := pool.Exec(ctx, `DELETE FROM pricing_plans WHERE id = $1`, id)
	return err
}
