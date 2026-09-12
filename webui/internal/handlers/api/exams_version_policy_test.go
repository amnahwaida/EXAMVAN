package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// M3 + M4 — policy & comparator unification for the in-handler version gates
// ---------------------------------------------------------------------------
//
// M3 (policy): ExamByToken and middleware.AndroidVersionCheck SKIP the check
// when the X-App-Version header is absent (web clients, old builds), but
// SubmitExam and AccessLog treated a missing header as a hard failure
// (parseVersionParts("") → [] → "too old" → 426). A client without the header
// could list exams and join a token yet was rejected at submit — three
// different policies on the same version gate. The unified contract: an
// ABSENT header skips the gate at every site (present-but-outdated still 426).
//
// M4 (comparator): isVersionAtLeast had divergent semantics — missing
// trailing segments REJECTED ("2.5" vs required "2.5.0" → false) and
// non-numeric segments SKIPPED instead of counting 0, while
// middleware.isVersionCompatible and models.CompareVersions pad with 0
// (matching the Android client's UpdateManager). A client reporting "2.5"
// passed the middleware on /api/exams/token/* but hit 426 at submit. The
// unified contract: ONE comparator (models.CompareVersions) everywhere.
//
// The fixtures come from the H1 suite (exams_version_gate_test.go, same
// package): newVersionGateRouter mounts middleware.AndroidVersionCheck on the
// join route, and the post-gate NOT-426 signal is the deterministic 404.

// probeGateWithoutHeader sends one request WITHOUT X-App-Version and returns
// the status, to pin the skip-when-absent policy at each site.
func probeGateWithoutHeader(t *testing.T, r http.Handler, method, path, body string) int {
	t.Helper()
	srv := httptest.NewServer(r)
	defer srv.Close()

	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Exam-Token", "it-version-gate-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// TestSubmitGateMissingHeaderSkips pins M3 for submit: without the version
// header the request moves PAST the gate (post-gate 404 for a nonexistent
// exam), not 426. Enforcement still applies the moment a header IS present.
func TestSubmitGateMissingHeaderSkips(t *testing.T) {
	f := newVersionGateFixture(t)
	f.seedAndroidAPK(t, "2.5.0")
	f.configureRequiredVersion(t, "2.5.0")

	got := probeGateWithoutHeader(t, newVersionGateRouter(f.pool), http.MethodPost,
		"/api/exams/999999/submit",
		`{"student_name":"Siswa","exam_number":"01","mac_address":"AA:BB:CC:DD:EE:FF"}`)
	if got == http.StatusUpgradeRequired {
		t.Fatalf("submit without X-App-Version: 426 (M3: absent header must skip the gate like the middleware and ExamByToken do)")
	}
	if got != http.StatusNotFound {
		t.Fatalf("submit without header: status=%d, want 404 (past gate)", got)
	}
}

// TestAccessLogGateMissingHeaderSkips pins M3 for access-log.
func TestAccessLogGateMissingHeaderSkips(t *testing.T) {
	f := newVersionGateFixture(t)
	f.seedAndroidAPK(t, "2.5.0")
	f.configureRequiredVersion(t, "2.5.0")

	got := probeGateWithoutHeader(t, newVersionGateRouter(f.pool), http.MethodPost,
		"/api/exams/999999/access-log",
		`{"event":"login","mac_address":"AA:BB:CC:DD:EE:FF"}`)
	if got == http.StatusUpgradeRequired {
		t.Fatalf("access-log without X-App-Version: 426 (M3: absent header must skip the gate like the middleware and ExamByToken do)")
	}
	if got != http.StatusNotFound {
		t.Fatalf("access-log without header: status=%d, want 404 (past gate)", got)
	}
}

// TestJoinGateMissingHeaderSkips pins the join side of the unified policy
// (middleware + in-handler): no header → past the gate even when a strict
// version is configured and published.
func TestJoinGateMissingHeaderSkips(t *testing.T) {
	f := newVersionGateFixture(t)
	f.seedAndroidAPK(t, "2.5.0")
	f.configureRequiredVersion(t, "2.5.0")

	got := probeGateWithoutHeader(t, newVersionGateRouter(f.pool), http.MethodGet,
		"/api/exams/token/BOGUSTOKEN", "")
	if got != http.StatusNotFound {
		t.Fatalf("join without header: status=%d, want 404 (past gate, bogus token)", got)
	}
}

// TestSubmitGatePartialVersionHeader pins M4: the Android client may report a
// short version ("2.5") while the required one is "2.5.0" — missing trailing
// parts count as 0, so this client is AT the required version and must pass
// (the middleware on the join route already lets it through).
func TestSubmitGatePartialVersionHeader(t *testing.T) {
	f := newVersionGateFixture(t)
	f.seedAndroidAPK(t, "2.5.0")
	f.configureRequiredVersion(t, "2.5.0")

	srv := httptest.NewServer(newVersionGateRouter(f.pool))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/exams/999999/submit",
		strings.NewReader(`{"student_name":"Siswa","exam_number":"01","mac_address":"AA:BB:CC:DD:EE:FF"}`))
	if err != nil {
		t.Fatalf("build submit: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-App-Version", "2.5")
	req.Header.Set("X-Exam-Token", "it-version-gate-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUpgradeRequired {
		t.Fatalf("submit with header 2.5 vs required 2.5.0: 426 (M4: missing trailing parts count as 0 — the middleware already passes this client)")
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("submit with header 2.5: status=%d, want 404 (past gate)", resp.StatusCode)
	}
}

// TestSubmitGateNonNumericSegment pins M4's second divergence: a non-numeric
// segment contributes 0 (never SKIPPED), so "2.5.x" == "2.5.0" and passes a
// required "2.5.0" — mirroring models.CompareVersions / UpdateManager.
func TestSubmitGateNonNumericSegment(t *testing.T) {
	f := newVersionGateFixture(t)
	f.seedAndroidAPK(t, "2.5.0")
	f.configureRequiredVersion(t, "2.5.0")

	srv := httptest.NewServer(newVersionGateRouter(f.pool))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/exams/999999/submit",
		strings.NewReader(`{"student_name":"Siswa","exam_number":"01","mac_address":"AA:BB:CC:DD:EE:FF"}`))
	if err != nil {
		t.Fatalf("build submit: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-App-Version", "2.5.x")
	req.Header.Set("X-Exam-Token", "it-version-gate-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUpgradeRequired {
		t.Fatalf("submit with header 2.5.x vs required 2.5.0: 426 (M4: non-numeric segment counts as 0, not skipped)")
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("submit with header 2.5.x: status=%d, want 404 (past gate)", resp.StatusCode)
	}
}

// TestSubmitGateStillBlocksOutdatedHeader guards the unified gate against
// over-correction: a PRESENT header that is genuinely older must still 426.
func TestSubmitGateStillBlocksOutdatedHeader(t *testing.T) {
	f := newVersionGateFixture(t)
	f.seedAndroidAPK(t, "2.5.0")
	f.configureRequiredVersion(t, "2.5.0")

	srv := httptest.NewServer(newVersionGateRouter(f.pool))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/exams/999999/submit",
		strings.NewReader(`{"student_name":"Siswa","exam_number":"01","mac_address":"AA:BB:CC:DD:EE:FF"}`))
	if err != nil {
		t.Fatalf("build submit: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-App-Version", "2.4.0")
	req.Header.Set("X-Exam-Token", "it-version-gate-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUpgradeRequired {
		t.Fatalf("submit with outdated header 2.4.0: status=%d, want 426 (gate must keep blocking)", resp.StatusCode)
	}
}

// TestCompareVersionsUnifiedSemantics pins the comparator contract all gate
// sites now share (models.CompareVersions): missing trailing parts pad with 0
// and non-numeric segments count 0.
func TestCompareVersionsUnifiedSemantics(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.5", "2.5.0", 0},   // padding
		{"2.5.0", "2.5", 0},   // padding (symmetric)
		{"2.5.x", "2.5.0", 0}, // non-numeric = 0
		{"2.5", "2.4.9", 1},   // newer
		{"2.4", "2.5.0", -1},  // older
		{"2.5.1", "2.5", 1},   // patch above padded
	}
	for _, tc := range cases {
		if got := models.CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
	var _ = context.Background
	var _ = gin.Mode
}
