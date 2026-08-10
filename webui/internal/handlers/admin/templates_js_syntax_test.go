package admin

// TestTemplatesInlineScriptsParse is a static guard over the inline <script>
// blocks in the web templates (admin and public). It exists because two real
// production bugs were found only by accident during a manual browser
// session:
//
//  1. pengawas_detail.html carried a stray "}" that closed the nested init
//     IIFE early, leaving "loadDetail()" inside an unclosed paren — the whole
//     page script failed to parse (SyntaxError) and the page was dead.
//  2. packages.html had a "var pdfMaxMb = ..." statement inserted in the
//     middle of a "+" string-concatenation expression.
//
// Neither bug was caught by the textual fragment guards, because those only
// check that strings are present, not that the JavaScript parses. This test
// closes that gap:
//
//   - A pure-Go delimiter-balance pass (always runs, no external tools): it
//     strips Go template tags and JS strings/comments, then asserts (), {},
//     [] are balanced in every inline <script> block. This would have caught
//     the stray-brace regression.
//   - When the "node" binary is available (dev machines; skipped otherwise so
//     CI without node stays green), each block is additionally validated with
//     `node --check`. Blocks containing Go control-flow tags ({{if}}/{{range}}
//     /...) are first RENDERED with html/template (the server's funcMap
//     subset + scriptSampleData) because their raw source is not valid
//     JavaScript until rendered — so the guard covers 100% of inline script
//     blocks, not just the static ones.
//
// Known limitations (crude static analysis, not a JS parser): regex literals
// containing delimiters (e.g. /\{/) or a literal "</script>" inside a JS
// string can produce false positives; no current template hits these. And the
// packages.html bug class (a statement in the middle of an expression) is only
// caught by the node pass, not the always-on balance pass.

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var (
	// Go's regexp has no lookahead, so match every <script...>...</script>
	// block and keep only the ones whose opening tag has no src= attribute.
	reScriptBlock = regexp.MustCompile(`(?s)<script([^>]*)>(.*?)</script>`)
	reGoTag       = regexp.MustCompile(`\{\{-?[\s\S]*?-?\}\}`)
	reGoControl   = regexp.MustCompile(`\{\{\s*-?\s*(if|range|with|template|block|end|define)`)

	// renderFuncs is the funcMap subset needed to render script-block Go tags
	// (mirrors cmd/server/main.go). eq/or/and are builtins in html/template.
	renderFuncs = template.FuncMap{
		"hasRole": func(roleStr, role string) bool { return strings.Contains(roleStr, role) },
		"default": func(d, v interface{}) interface{} {
			if v == nil || v == "" {
				return d
			}
			return v
		},
		"dict": func(values ...interface{}) map[string]interface{} {
			m := make(map[string]interface{}, len(values)/2)
			for i := 0; i+1 < len(values); i += 2 {
				m[fmt.Sprintf("%v", values[i])] = values[i+1]
			}
			return m
		},
	}

	// scriptSampleData supplies every field referenced by Go tags inside the
	// templates' inline <script> blocks (verified by scanning all templates):
	// dashboard (admin_role/admin_id), pengawas_detail (.exam.*, token_mode),
	// users/packages (storage_free_mb, admin_instansi), login/register/forgot/
	// reset (turnstile_enabled), hasil (token, is_*), register_confirm
	// (username). When a future template adds a new script-block field, this
	// map must grow — the render error below names the missing field.
	scriptSampleData = map[string]interface{}{
		"exam": map[string]interface{}{
			"ID":                 int64(7),
			"TokenResetInterval": int64(15),
			"TokenLastResetAt":   timePtr(time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)),
		},
		"token_mode":        "dynamic",
		"csrf_token":        "guard-csrf-token",
		"admin_role":        "superadmin",
		"admin_id":          int64(1),
		"admin_instansi":    "SMA NEGERI 1",
		"storage_free_mb":   float64(2048.5),
		"turnstile_enabled": true,
		"token":             "GUARD-TOKEN",
		"is_logged_in":      true,
		"is_disabled":       false,
		"show_answers":      true,
		"username":          "guard_user",
		"SizeBytes":         int64(1048576),
		"android_app":       map[string]interface{}{"SizeBytes": int64(1048576)},
	}
)

func timePtr(t time.Time) *time.Time { return &t }

// renderScriptBlockForGuard renders an inline script body that contains Go
// control-flow tags so the resulting JavaScript can be node --check'd. The
// body is wrapped in <script> tags during parsing so html/template applies
// JS-context escaping exactly like the server's full-page render; the wrapper
// is stripped from the output.
func renderScriptBlockForGuard(body string) (string, error) {
	tmpl, err := template.New("guard-script").Funcs(renderFuncs).Parse("<script>" + body + "</script>")
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, scriptSampleData); err != nil {
		return "", err
	}
	out := buf.String()
	const openTag = "<script>"
	const closeTag = "</script>"
	if !strings.HasPrefix(out, openTag) || !strings.HasSuffix(out, closeTag) {
		return "", fmt.Errorf("rendered wrapper mismatch")
	}
	return out[len(openTag) : len(out)-len(closeTag)], nil
}

// stripJSStringsAndComments removes string literals and comments from a
// JavaScript snippet so delimiter counts only see real syntax. Crude but
// adequate for balance checking (template literals with interpolation are
// collapsed as opaque strings, which is conservative).
func stripJSStringsAndComments(js string) string {
	var b strings.Builder
	inLine := false
	inBlock := false
	inStr := byte(0)
	n := len(js)
	for i := 0; i < n; i++ {
		c := js[i]
		switch {
		case inLine:
			if c == '\n' {
				inLine = false
				b.WriteByte(c)
			}
			continue
		case inBlock:
			if c == '*' && i+1 < n && js[i+1] == '/' {
				inBlock = false
				i++
			}
			continue
		case inStr != 0:
			if c == '\\' && i+1 < n {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		if c == '/' && i+1 < n {
			if js[i+1] == '/' {
				inLine = true
				i++
				continue
			}
			if js[i+1] == '*' {
				inBlock = true
				i++
				continue
			}
		}
		if c == '\'' || c == '"' || c == '`' {
			inStr = c
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func TestTemplatesInlineScriptsParse(t *testing.T) {
	templatesDir := "templates"
	if _, err := os.Stat(templatesDir); err != nil {
		templatesDir = filepath.Join("..", "..", "..", "templates")
	}

	nodeOK := false
	if _, err := exec.LookPath("node"); err == nil {
		nodeOK = true
	}

	// Cover every template area recursively (admin incl. partials, public,
	// and any future area under templates/) so no page script escapes the
	// guard.
	err := filepath.WalkDir(templatesDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".html") {
			return nil
		}
		rel, err := filepath.Rel(templatesDir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		checkInlineScripts(t, rel, string(data), nodeOK)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", templatesDir, err)
	}
}

// checkInlineScripts runs both guard passes over every inline <script> block
// in one rendered-HTML file. name is the path used in error messages.
func checkInlineScripts(t *testing.T, name, html string, nodeOK bool) {
	t.Helper()
	blocks := reScriptBlock.FindAllStringSubmatch(html, -1)
	for i, m := range blocks {
		if strings.Contains(m[1], "src=") {
			continue // external script, not inline JS
		}
		body := m[2]
		if strings.TrimSpace(body) == "" {
			continue
		}
		block := name + " block " + itoa(i)

		// Neutralized source for the always-on balance pass: Go tags become a
		// literal 0 (they may legally contain parens, e.g. .Format "2006...",
		// and removing them entirely would leave artifacts like
		// "( / (1024*1024))" that skew the checks). Neutralizing — not
		// rendering — keeps BOTH branches of {{if}}/{{else}} present so a
		// stray brace in an unrendered branch is still caught.
		noGo := reGoTag.ReplaceAllString(body, "0")

		// node --check source: plain blocks use the neutralized source, but
		// control-flow blocks must be RENDERED first (their raw Go tags are
		// not valid JavaScript), so the actual JS the browser runs is checked.
		jsForNode := noGo
		if reGoControl.MatchString(body) {
			rendered, err := renderScriptBlockForGuard(body)
			if err != nil {
				t.Errorf("%s: cannot render control-flow script block for node --check: %v — add the missing field to scriptSampleData (or func to renderFuncs) in this test file", block, err)
				continue
			}
			jsForNode = rendered
		}

		// Balance pass (always): strings/comments stripped, delimiters must
		// pair up. Runs on the neutralized source so both control-flow
		// branches are inspected.
		clean := stripJSStringsAndComments(noGo)
		for _, open := range []byte{'(', '{', '['} {
			close := map[byte]byte{'(': ')', '{': '}', '[': ']'}[open]
			oc, cc := 0, 0
			for j := 0; j < len(clean); j++ {
				if clean[j] == open {
					oc++
				}
				if clean[j] == close {
					cc++
				}
			}
			if oc != cc {
				t.Errorf("%s: unbalanced %q (open=%d close=%d) after stripping strings/comments — a stray brace/paren here breaks the whole page script", block, string(open), oc, cc)
			}
		}

		// node --check pass (only when node exists).
		if nodeOK {
			tmp := filepath.Join(t.TempDir(), "inline.js")
			if err := os.WriteFile(tmp, []byte(jsForNode), 0o600); err != nil {
				t.Fatalf("write temp js: %v", err)
			}
			out, err := exec.Command("node", "--check", tmp).CombinedOutput()
			if err != nil {
				t.Errorf("%s: node --check failed: %v\n%s", block, err, firstLine(string(out)))
			}
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
