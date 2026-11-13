// Package helpers provides shared utility functions for the EXAMVAN webui.
// These are pure utility functions with no framework dependency, ported from
// the Python server's helpers.py.
package helpers

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
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

// GenerateExamToken creates an 8-character uppercase alphanumeric token (A-Z, 0-9).
// Uses crypto/rand.Int for unbiased distribution.
func GenerateExamToken() (string, error) {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 8)
	for i := range b {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			// Fail-closed (review 14 Nov 2026): dulu fallback time-based
			// (UnixNano % 99999999, ≤32 bit, prediktabel) mengeluarkan token
			// ujian lemah saat crypto/rand gagal — token adalah kredensial
			// masuk siswa, jadi error dibalikkan ke caller alih-alih dicetak.
			return "", fmt.Errorf("generate exam token: crypto/rand unavailable: %w", err)
		}
		b[i] = chars[idx.Int64()]
	}
	return string(b), nil
}

// SafeStoragePath resolves a relative file path against the storage
// directory and verifies the result does not escape via directory traversal.
// The prefix check requires the separator (fix G.2): a bare strings.HasPrefix
// accepted sibling directories like /data/uploads-evil for base /data/uploads.
func SafeStoragePath(baseDir, relPath string) (string, error) {
	cleanBase := filepath.Clean(baseDir)
	full := filepath.Join(cleanBase, filepath.Clean(relPath))
	if full != cleanBase && !strings.HasPrefix(full, cleanBase+string(os.PathSeparator)) {
		return "", fmt.Errorf("path traversal detected: %s", relPath)
	}
	return full, nil
}

// ParseIdentityFields unmarshals the identity_fields JSON column.
// Falls back to the provided default fields.
func ParseIdentityFields(raw *string, defaultFields []map[string]interface{}) []map[string]interface{} {
	if raw == nil || *raw == "" {
		return defaultFields
	}
	var fields []map[string]interface{}
	if err := json.Unmarshal([]byte(*raw), &fields); err != nil || len(fields) == 0 {
		return defaultFields
	}
	return fields
}
