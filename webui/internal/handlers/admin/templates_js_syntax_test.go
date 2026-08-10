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
//     /...) are skipped for this pass because those templates are not valid
//     JavaScript until rendered; the balance pass still covers them.
//
// Known limitations (crude static analysis, not a JS parser): regex literals
// containing delimiters (e.g. /\{/) or a literal "</script>" inside a JS
// string can produce false positives; no current template hits these. And the
// packages.html bug class (a statement in the middle of an expression) is only
// caught by the node pass, not the always-on balance pass.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	// Go's regexp has no lookahead, so match every <script...>...</script>
	// block and keep only the ones whose opening tag has no src= attribute.
	reScriptBlock = regexp.MustCompile(`(?s)<script([^>]*)>(.*?)</script>`)
	reGoTag       = regexp.MustCompile(`\{\{-?[\s\S]*?-?\}\}`)
	reGoControl   = regexp.MustCompile(`\{\{\s*-?\s*(if|range|with|template|block|end|define)`)
)

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

		// Balance pass (always): replace Go template tags with a neutral
		// expression first — they may legally contain parens (e.g. .Format
		// "2006...") and removing them entirely would leave artifacts like
		// "( / (1024*1024))" that skew both balance and node --check.
		noGo := reGoTag.ReplaceAllString(body, "0")
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
				t.Errorf("%s: unbalanced %q (open=%d close=%d) after stripping strings/comments/Go tags — a stray brace/paren here breaks the whole page script", block, string(open), oc, cc)
			}
		}

		// node --check pass (only when node exists and block has no Go
		// control flow, which would be invalid JS until rendered).
		if nodeOK && !reGoControl.MatchString(body) {
			tmp := filepath.Join(t.TempDir(), "inline.js")
			if err := os.WriteFile(tmp, []byte(noGo), 0o600); err != nil {
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
