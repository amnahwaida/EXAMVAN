package r2

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// bannedLiteralFragments are the pre-standardisation wording variants that
// must NEVER reappear in handler code outside the r2 package. The canonical
// wording lives only inside the exported constants defined in r2.go (which is
// why this scan skips the r2 directory itself).
var bannedLiteralFragments = []string{
	"Cloudflare R2 client tidak ditemukan",
	"Cloudflare R2 client tidak dikonfigurasi",
	"Gagal mengunggah file ke R2",
	"Gagal mengupload file ke Cloudflare R2",
	"Gagal men-generate URL soal dari Cloudflare R2",
	// Anti-revert: an R2 message constant must NEVER go through the plain
	// errorResponse helper (which omits error_code) or a gin.H without the
	// error_code key — both lose the machine-readable branch code.
	"errorResponse(c, http.StatusInternalServerError, r2client.ErrMsg",
	`"success": false, "message": r2client.ErrMsg`,
}

// requiredConsts maps each handler source file (relative to
// internal/handlers/) to the canonical constants it MUST use for its R2 error
// messages AND machine-readable error codes. A new handler surface that
// reports R2 errors should be added here (and start using the constants).
var requiredConsts = map[string][]string{"admin/exams.go": {"ErrMsgNotConfigured", "ErrCodeNotConfigured", "ErrMsgUploadFailed", "ErrCodeUploadFailed", "ErrMsgSignURLFailed", "ErrCodeSignURLFailed"},
	"admin/system_apps.go": {"ErrMsgNotConfigured", "ErrCodeNotConfigured", "ErrMsgUploadFailed", "ErrCodeUploadFailed"},
	"api/exams.go":         {"ErrMsgNotConfigured", "ErrCodeNotConfigured", "ErrMsgSignURLFailed", "ErrCodeSignURLFailed"},
	"public/download.go":   {"ErrMsgNotConfigured", "ErrCodeNotConfigured", "ErrMsgSignURLFailed", "ErrCodeSignURLFailed"},
}

// codeRe locks the error-code format: UPPER_SNAKE so clients can rely on a
// stable, parseable token (never a sentence).
var codeRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// TestCanonicalR2MessagesStatic guards the R2 error-message standardisation:
// every handler must report a missing/disabled backend (and failed uploads)
// with the exported constants — never a hand-written literal. The test scans
// the handler sources on disk (go test runs with cwd = the r2 package dir),
// so a stray literal or a removed constant usage fails the build immediately.
func TestCanonicalR2MessagesStatic(t *testing.T) {
	root := "../"

	// Sanity: the canonical message constants must themselves mention
	// Cloudflare R2 (guards against a typo turning them into a different
	// message), and the error-code constants must be stable UPPER_SNAKE
	// tokens clients can parse without string matching.
	if !strings.Contains(ErrMsgNotConfigured, "Cloudflare R2") {
		t.Errorf("ErrMsgNotConfigured = %q, harus menyebut 'Cloudflare R2'", ErrMsgNotConfigured)
	}
	if !strings.Contains(ErrMsgUploadFailed, "Cloudflare R2") {
		t.Errorf("ErrMsgUploadFailed = %q, harus menyebut 'Cloudflare R2'", ErrMsgUploadFailed)
	}
	for name, code := range map[string]string{
		"ErrCodeNotConfigured": ErrCodeNotConfigured,
		"ErrCodeUploadFailed":  ErrCodeUploadFailed,
		"ErrCodeSignURLFailed": ErrCodeSignURLFailed,
	} {
		if !codeRe.MatchString(code) {
			t.Errorf("%s = %q, harus format UPPER_SNAKE (mis. R2_NOT_CONFIGURED)", name, code)
		}
		if code == "" || strings.Contains(code, " ") {
			t.Errorf("%s = %q, kode tidak boleh kosong atau mengandung spasi", name, code)
		}
	}

	// Negative: no banned literal anywhere in handler production code.
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "r2" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, frag := range bannedLiteralFragments {
			if strings.Contains(string(src), frag) {
				t.Errorf("%s: literal/pola %q terpakai — gunakan konstanta kanonik r2client.ErrMsg*/ErrCode* (via errorResponseWithCode atau gin.H dengan error_code). Komentar pun sebaiknya menyebut nama konstanta, bukan menulis ulang kalimatnya.", path, frag)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk handler sources: %v", err)
	}

	// Positive: each known handler surface must reference its constants and
	// actually emit the machine-readable error_code JSON key.
	for rel, consts := range requiredConsts {
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("read %s: %v", rel, err)
			continue
		}
		for _, c := range consts {
			if !strings.Contains(string(src), c) {
				t.Errorf("%s: konstanta %q tidak terpakai untuk pesan error R2 (harus via r2client.ErrMsg* / ErrCode*)", rel, c)
			}
		}
		// The response must carry the machine-readable code: either via the
		// shared errorResponseWithCode helper or a direct gin.H{'error_code':
		// ...} (public/system_apps style).
		if !strings.Contains(string(src), `"error_code"`) && !strings.Contains(string(src), "errorResponseWithCode(") {
			t.Errorf("%s: respons error R2 harus memuat key JSON 'error_code' (via errorResponseWithCode atau gin.H{'error_code': ...})", rel)
		}
	}
}
