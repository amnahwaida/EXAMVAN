package main

import (
	"os"
	"strings"
	"testing"
)

// extractNginxLocation returns the body of the `location <path> { ... }`
// block in nginx.conf (brace-matched from the opening line).
func extractNginxLocation(conf, path string) (string, bool) {
	needle := "location " + path
	idx := strings.Index(conf, needle)
	if idx == -1 {
		return "", false
	}
	open := strings.Index(conf[idx:], "{")
	if open == -1 {
		return "", false
	}
	depth := 0
	for j := idx + open; j < len(conf); j++ {
		switch conf[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return conf[idx : j+1], true
			}
		}
	}
	return "", false
}

// TestNginxSecurityHeadersOnStaticBlocks locks in the security headers on the
// /static/ location of nginx.conf.
//
// WHY THIS EXISTS: nginx's add_header inheritance is all-or-nothing per
// configuration level — a location block that declares ANY add_header of its
// own (here: Cache-Control for immutable static files) silently DROPS every
// server-level add_header. The /static/ location therefore served SVG/JS
// without X-Content-Type-Options, CSP, X-Frame-Options, etc. Found in the
// 2026-11-13 webui review.
func TestNginxSecurityHeadersOnStaticBlocks(t *testing.T) {
	conf, err := os.ReadFile("../../nginx/nginx.conf")
	if err != nil {
		t.Fatalf("read nginx.conf: %v", err)
	}
	text := string(conf)

	staticBlock, ok := extractNginxLocation(text, "/static/")
	if !ok {
		t.Fatal("location /static/ block not found in nginx.conf")
	}

	// Headers that the server-level block sets and /static/ must NOT lose.
	required := []string{
		"X-Frame-Options",
		"X-Content-Type-Options",
		"Referrer-Policy",
		"Content-Security-Policy",
		"Strict-Transport-Security",
		"Permissions-Policy",
	}
	for _, h := range required {
		if !strings.Contains(staticBlock, "add_header "+h) {
			t.Errorf("location /static/ missing security header %s — "+
				"nginx add_header inheritance is dropped when a block declares its own "+
				"add_header (Cache-Control); duplicate the security headers there", h)
		}
	}

	// The cache header that justifies the block's own add_header must stay.
	if !strings.Contains(staticBlock, "Cache-Control") {
		t.Error("location /static/ lost its Cache-Control — long-cache contract broken")
	}

	// Server-level block keeps the canonical set (everything before
	// /static/ — plain locations without their own add_header still inherit it).
	serverLevel := text[:strings.Index(text, "location /static/")]
	for _, h := range required {
		if !strings.Contains(serverLevel, "add_header "+h) {
			t.Errorf("server-level block missing security header %s", h)
		}
	}
}

// TestNginxWSCommentMatchesLoggerReality keeps the nginx.conf narrative in
// sync with cmd/server/main.go: the access-log leak the comment still warns
// about (gin.Logger appending query strings at origin) cannot happen in
// production — gin.Logger is only installed in development mode, with a
// formatter that strips the query string. Stale warnings read as live risks.
func TestNginxWSCommentMatchesLoggerReality(t *testing.T) {
	mainGo, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(mainGo), "gin.LoggerWithFormatter(logFormatterWithoutQueryString)") {
		t.Fatal("main.go no longer uses logFormatterWithoutQueryString — update this contract")
	}
	conf, err := os.ReadFile("../../nginx/nginx.conf")
	if err != nil {
		t.Fatalf("read nginx.conf: %v", err)
	}
	wsBlock, ok := extractNginxLocation(string(conf), "/ws/")
	if !ok {
		t.Fatal("location /ws/ block not found in nginx.conf")
	}
	if strings.Contains(wsBlock, "Yang masih menyisakan bocoran ada di sisi origin") {
		t.Error("stale comment: origin-side gin.Logger leak was fixed (dev-only, query-stripped) — " +
			"rewrite the /ws/ access_log comment to match reality")
	}
}
