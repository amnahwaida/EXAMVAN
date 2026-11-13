package middleware

import (
	"os"
	"strings"
	"testing"
)

func osReadTemplateSrc() (string, error) {
	b, err := os.ReadFile("template.go")
	return string(b), err
}

// TestTemplateDataSingleSettingsQuery locks the single-query contract for the
// SEO/footer block of TemplateData (review 14 Nov 2026): every HTML render
// used to pay SIX sequential `SELECT value FROM saas_settings WHERE key = $1`
// round trips before the page could be sent. The loader now fetches all the
// keys it needs in ONE query; this guard fails if per-key queries creep back
// (a new key added with its own SELECT is the easy mistake).
func TestTemplateDataSingleSettingsQuery(t *testing.T) {
	src, err := osReadTemplateSrc()
	if err != nil {
		t.Fatalf("read template.go: %v", err)
	}
	const needle = "FROM saas_settings"
	if n := strings.Count(src, needle); n != 1 {
		t.Errorf("template.go contains %d %q occurrences, want exactly 1 (one combined query)", n, needle)
	}
	// The old per-key shape.
	if strings.Contains(src, `SELECT value FROM saas_settings WHERE key = $1`) {
		t.Error("per-key `SELECT value ... WHERE key = $1` query is back in template.go — use the combined loader")
	}
	// The loader must enumerate its keys in one slice literal so the guard
	// above stays meaningful when keys are added.
	if !strings.Contains(src, "seoFooterKeys") {
		t.Error("loader should enumerate the setting keys via a shared seoFooterKeys slice")
	}
}
