// Package helpers provides shared utility functions for the EXAMVAN webui.
// These are pure utility functions with no framework dependency, ported from
// the Python server's helpers.py.
package helpers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// LocalizeDateString converts a UTC timestamp string to a localized string
// using a browser timezone offset in minutes.
func LocalizeDateString(utcStr string, tzOffsetMin *int) string {
	if utcStr == "" {
		return "—"
	}
	isoStr := strings.TrimSpace(utcStr)
	isoStr = strings.ReplaceAll(isoStr, " ", "T")
	if !strings.HasSuffix(isoStr, "Z") && !strings.HasSuffix(isoStr, "+00:00") {
		isoStr += "Z"
	}
	dt, err := time.Parse(time.RFC3339, isoStr)
	if err != nil {
		// Try parsing with nanoseconds.
		dt, err = time.Parse("2006-01-02T15:04:05Z", isoStr)
		if err != nil {
			return utcStr
		}
	}
	if tzOffsetMin != nil {
		localDT := dt.Add(-time.Duration(*tzOffsetMin) * time.Minute)
		return localDT.Format("2006-01-02 15:04:05")
	}
	return dt.Format("2006-01-02 15:04:05 UTC")
}

// FormatISOUTC converts a time.Time to ISO 8601 UTC format string.
func FormatISOUTC(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

// ParseISOUTC parses an ISO 8601 UTC string into a time.Time.
func ParseISOUTC(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "T")
	// Only append Z if the string doesn't already have a timezone suffix.
	if !hasTimezoneSuffix(s) {
		s += "Z"
	}
	return time.Parse(time.RFC3339, s)
}

// hasTimezoneSuffix checks if an ISO 8601 string already contains a timezone
// indicator: Z, +HH:MM, -HH:MM.
func hasTimezoneSuffix(s string) bool {
	if len(s) == 0 {
		return false
	}
	last := s[len(s)-1]
	if last == 'Z' {
		return true
	}
	// Check for ±HH:MM offset (last 6 chars like +07:00 or -05:30).
	if len(s) >= 6 && (last == '0' || last == '5' || last == '9') &&
		s[len(s)-3] == ':' &&
		(s[len(s)-6] == '+' || s[len(s)-6] == '-') {
		return true
	}
	return false
}

// SanitizeStudentInput strips leading/trailing whitespace and collapses
// internal whitespace for student-provided text.
func SanitizeStudentInput(input string) string {
	return strings.Join(strings.Fields(input), " ")
}

// GenerateToken creates an alphanumeric token of the specified length
// using cryptographically secure random bytes.
func GenerateToken(length int) (string, error) {
	if length < 1 {
		length = 8
	}
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", fmt.Errorf("generate token: %w", err)
		}
		result[i] = charset[n.Int64()]
	}
	return string(result), nil
}

// GenerateCSRFToken creates a 64-character hex CSRF token from 32 random bytes.
func GenerateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback to SHA256 of current time if crypto/rand fails (extremely rare).
		h := sha256.Sum256([]byte(time.Now().String()))
		return hex.EncodeToString(h[:])
	}
	return hex.EncodeToString(b)
}

// ParseRoles parses a role string into a slice of role names.
// It handles the hardcoded 'superadmin' value and JSON array formats.
func ParseRoles(roleStr string) []string {
	return parseRolesInternal(roleStr)
}

// HasRole checks if a role string includes the target role.
func HasRole(roleStr, target string) bool {
	for _, r := range ParseRoles(roleStr) {
		if r == target {
			return true
		}
	}
	return false
}

// SerializeRoles serializes a slice of roles to a JSON string for storage.
func SerializeRoles(roles []string) string {
	return serializeRolesInternal(roles)
}

// DisplayRoles returns human-readable role labels from a role string.
func DisplayRoles(roleStr string) string {
	roleMap := map[string]string{
		"superadmin": "Super Admin",
		"guru":       "Guru",
		"pengawas":   "Pengawas",
		"operator":   "Operator",
	}
	roles := ParseRoles(roleStr)
	labels := make([]string, 0, len(roles))
	for _, r := range roles {
		if label, ok := roleMap[r]; ok {
			labels = append(labels, label)
		} else {
			labels = append(labels, r)
		}
	}
	if len(labels) == 0 {
		return "Guru"
	}
	return strings.Join(labels, ", ")
}

// ---------------------------------------------------------------------------
// Internal implementations (no external JSON dependency — kept simple)
// ---------------------------------------------------------------------------

func parseRolesInternal(roleStr string) []string {
	if roleStr == "" {
		return []string{"guru"}
	}
	if roleStr == "superadmin" {
		return []string{"superadmin"}
	}
	// Detect JSON array: starts with '['.
	trimmed := strings.TrimSpace(roleStr)
	if strings.HasPrefix(trimmed, "[") {
		return parseJSONRoles(trimmed)
	}
	return []string{roleStr}
}

// parseJSONRoles does a minimal JSON string array parse without importing encoding/json.
func parseJSONRoles(s string) []string {
	// Remove surrounding brackets and whitespace.
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return []string{"guru"}
	}
	var roles []string
	for {
		inner = strings.TrimSpace(inner)
		if inner == "" {
			break
		}
		if !strings.HasPrefix(inner, "\"") {
			break
		}
		// Find closing quote (simple parse, no escaping support for brevity).
		quoteEnd := strings.IndexByte(inner[1:], '"')
		if quoteEnd < 0 {
			break
		}
		role := inner[1 : 1+quoteEnd]
		roles = append(roles, role)
		inner = inner[1+quoteEnd+1:]
		// Skip comma.
		inner = strings.TrimLeft(inner, ",")
	}
	if len(roles) == 0 {
		return []string{"guru"}
	}
	return roles
}

func serializeRolesInternal(roles []string) string {
	if len(roles) == 0 {
		return `["guru"]`
	}
	parts := make([]string, len(roles))
	for i, r := range roles {
		parts[i] = `"` + r + `"`
	}
	return "[" + strings.Join(parts, ",") + "]"
}
