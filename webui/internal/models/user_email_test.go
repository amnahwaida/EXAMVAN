package models

import (
	"strings"
	"testing"
)

// TestIsValidEmail locks the charset contract for user emails (review
// 14 Nov 2026): the email field is interpolated into SMTP headers and the
// envelope RCPT command downstream, so anything outside a strict mailbox
// shape — CR/LF/control characters, spaces, angle brackets — must be rejected
// at the entry points (register, CreateUser, EditUser). The old validation
// (`contains "@" && contains "."`) accepted `a@b.\r\nBcc: attacker@x` and
// let a registrant drive the school SMTP session.
func TestIsValidEmail(t *testing.T) {
	valid := []string{
		"budi@sekolah.sch.id",
		"a+b@gmail.com",            // plus-addressing
		"USER@EXAMPLE.COM",         // uppercase is fine (stored normalized later)
		"first.last@sub.x.example", // multi-dot domains
		"a_b-c@x.co",
	}
	for _, e := range valid {
		if !IsValidEmail(e) {
			t.Errorf("valid email rejected: %q", e)
		}
	}

	invalid := []string{
		"",
		"a@b",                               // no TLD dot
		"no-at-sign.example.com",            // no @
		"a@b.\r\nBcc: evil@x.com",           // CRLF header injection (the report case)
		"a@b.\nBcc: evil@x.com",             // bare LF injection
		"a@b.com\r",                         // stray CR
		"a@b.com\x00",                       // NUL
		"a b@c.com",                         // space in local part
		"a@b .com",                          // space in domain
		"a@b.<script>",                      // angle brackets
		"@no-local.com",                     // empty local part
		"no-tld@localhost",                  // dot-less domain
		"a@b..com",                          // empty domain label
		".dot@start.com",                    // local part starts with a dot
		strings.Repeat("a", 250) + "@x.com", // > 254 chars total
	}
	for _, e := range invalid {
		if IsValidEmail(e) {
			t.Errorf("invalid email accepted: %q", e)
		}
	}
}
