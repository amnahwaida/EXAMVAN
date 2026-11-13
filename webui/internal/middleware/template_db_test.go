package middleware

import (
	"context"
	"testing"

	"github.com/examvan/webui/internal/database"
)

// TestLoadSeoFooterSettings is DB-backed and SKIPS gracefully when
// TEST_DATABASE_URL is not configured (plain `go test ./...` without
// Postgres). It pins the behaviour of the single-query loader:
//   - custom saas_settings rows override the built-in defaults,
//   - empty/missing rows fall back to the defaults,
//   - the returned map carries exactly the SEO/footer keys (seo_index
//     normalised to bool by the caller, handled in TemplateData).
func TestLoadSeoFooterSettings(t *testing.T) {
	pool := database.NewPackageTestPool(t, "middleware")
	ctx := context.Background()

	// Empty table → pure defaults.
	got := loadSeoFooterSettings(ctx, pool)
	if got["seo_title"] != "EXAMVAN - Aplikasi Ujian Online Aman & Tertib" {
		t.Errorf("default seo_title missing: %v", got["seo_title"])
	}
	if _, ok := got["footer_text"]; !ok {
		t.Error("default footer_text missing")
	}

	// Custom rows win.
	for _, kv := range [][2]string{
		{"seo_title", "SMA Negeri 1 Contoh"},
		{"footer_text", "© SMA Negeri 1 Contoh"},
	} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO saas_settings (key, value) VALUES ($1, $2)
			 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, kv[0], kv[1]); err != nil {
			t.Fatalf("seed saas_settings: %v", err)
		}
	}
	got = loadSeoFooterSettings(ctx, pool)
	if got["seo_title"] != "SMA Negeri 1 Contoh" {
		t.Errorf("custom seo_title not loaded: %v", got["seo_title"])
	}
	if got["footer_text"] != "© SMA Negeri 1 Contoh" {
		t.Errorf("custom footer_text not loaded: %v", got["footer_text"])
	}
	// Untouched keys keep their defaults.
	if got["seo_description"] == "" {
		t.Error("seo_description default lost when only some keys are custom")
	}
}
