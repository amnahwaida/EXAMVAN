package api

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Transaction-scoped advisory locks and the no-duplicate-row guarantee in
// this package are only exercised by TestRequestApprovalAutoApproveConcurrentNoDuplicate,
// which fires 8 simultaneous requests. That concurrency is what makes a
// connection-pool deadlock possible and visible.
//
// The deadlock it used to hit: RequestApproval called pool.Begin (holding one
// pooled connection for the whole transaction) and then, still inside the
// transaction, called models.GetSaasSettingInt(ctx, pool, ...) — a SECOND
// connection from the same pool. With 8 concurrent requests and pgxpool's
// default MaxConns of max(4, numCPU) = 4, every holder of a connection was
// waiting for a connection that could never be released. Nothing progressed,
// nothing finished, and the package timeout killed the run after 10 minutes.
//
// It never surfaced as a slow query. The earlier attempt to "fix" it by
// removing the client's timeout mistook the resulting "context deadline
// exceeded" for latency, which converted a fast diagnosable failure into a
// silent 10-minute hang. The actual fix was to read the cap BEFORE Begin.
//
// Go cannot catch this at compile time: pool.Begin returns a pgx.Tx and the
// handler simply keeps using the pool it already had. So this test reads the
// source of the package's handlers and asserts the invariant directly.
//
// If it fires, the fix is to move the pool call above the Begin, or to pass
// tx into the model function instead of pool.
func TestNoPoolCallWhileTransactionOpen(t *testing.T) {
	beginRE := regexp.MustCompile(`\b(\w+)\s*,\s*\w+\s*:?=\s*pool\.Begin\(`)
	commitRE := func(v string) *regexp.Regexp {
		return regexp.MustCompile(`\b` + regexp.QuoteMeta(v) + `\.Commit\(`)
	}
	// Matches pool.Method(...) and a bare pool passed as an argument
	// (e.g. GetSaasSettingInt(ctx, pool, ...)), which is the exact shape
	// that caused the original deadlock.
	poolUseRE := regexp.MustCompile(`\bpool\s*(?:\.[A-Za-z]+)?\s*[,)]`)

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	var violations []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		lines := strings.Split(string(src), "\n")

		for i, line := range lines {
			m := beginRE.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			txVar := m[1]
			commit := commitRE(txVar)

			// Only tx.Commit closes the transaction. Do NOT stop at
			// tx.Rollback: `defer func() { _ = tx.Rollback(ctx) }()`
			// only *registers* the rollback — the transaction stays open
			// (and keeps its connection) until Commit or function exit.
			for j := i + 1; j < len(lines) && j < i+200; j++ {
				if commit.MatchString(lines[j]) {
					break
				}
				trimmed := strings.TrimSpace(lines[j])
				if trimmed == "" || strings.HasPrefix(trimmed, "//") {
					continue
				}
				for _, use := range poolUseRE.FindAllStringIndex(lines[j], -1) {
					// "tx := pool.Begin" is how the transaction started,
					// not a second acquisition.
					if beginRE.MatchString(lines[j][:use[0]]) {
						continue
					}
					violations = append(violations, fmt.Sprintf(
						"%s:%d uses pool while %s (opened at line %d) is still open: %s",
						name, j+1, txVar, i+1, trimmed))
				}
			}
		}
	}

	if len(violations) > 0 {
		t.Errorf("connection-pool deadlock risk — a handler holds a transaction (one pooled "+
			"connection) while also asking the same pool for another connection. Under "+
			"enough concurrency this deadlocks permanently instead of returning an error.\n"+
			"Move the pool call above pool.Begin, or pass tx to the model function:\n  %s",
			strings.Join(violations, "\n  "))
	}
}
