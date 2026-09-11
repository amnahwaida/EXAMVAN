package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Android version-gate tests (H1) for the three in-handler checks
// ---------------------------------------------------------------------------
//
// H1: join (ExamByToken), submit (SubmitExam) and access-log (AccessLog) read
// the RAW saas_setting "android_version" (default const 2.5.0) instead of the
// effective version used by middleware.AndroidVersionCheck and /api/health
// (models.EffectiveAndroidRequiredVersion). Two failure modes:
//
//   (a) CLAMP — the configured version is higher than the published APK
//       (e.g. admin publishes 2.5.0 but leaves android_version=3.0.0).
//       The in-handler gates demand 3.0.0 which /download/apk cannot serve
//       → clients locked out of submit/access-log with nothing to install.
//
//   (b) NO APK — no Android system_app exists at all. The effective required
//       version is "" (nothing downloadable → nothing to enforce), but the
//       in-handler gates still enforce the raw setting/default → an old
//       client gets 426 everywhere while /download/apk has nothing to offer
//       (deadlock: it cannot update to pass the gate).
//
// These tests pin the intended contract — all three sites behave exactly
// like middleware.AndroidVersionCheck:
//
//	effective required version = clamp(configured, available APK)
//	"" (no Android APK)       → enforcement skipped
//
// Post-gate NOT-426 signals are deterministic:
//
//	join          → bogus token      → 404 "Token tidak valid"
//	submit        → nonexistent exam → 404 "Ujian tidak ditemukan"
//	access-log    → nonexistent exam → 404 "Ujian tidak ditemukan"
//
// Redis is NOT injected: checkRateLimit is open-access with a nil client, so
// every per-token/per-exam rate limit is skipped and cannot mask results.

// newVersionGateRouter wires the three gate-bearing endpoints like main.go
// does. The join route also carries middleware.AndroidVersionCheck so the
// middleware and the in-handler check stay consistent in production wiring.
func newVersionGateRouter(pool *pgxpool.Pool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
	})
	api := r.Group("/api")
	api.GET("/exams/token/:token", middleware.AndroidVersionCheck(), ExamByToken())
	api.POST("/exams/:exam_id/submit", SubmitExam())
	api.POST("/exams/:exam_id/access-log", AccessLog())
	return r
}

// versionGateFixture isolates the environment for one scenario:
//   - saas_settings "android_version" is deleted first (saas_settings survives
//     TRUNCATE across runs, so a leftover value from another test must not
//     leak in), and re-deleted on cleanup so this test does not leak out.
//   - any pre-existing android system_apps rows are removed (defensive).
//   - opt-in: seed an APK row and/or a configured android_version.
type versionGateFixture struct {
	pool *pgxpool.Pool
}

func newVersionGateFixture(t *testing.T) *versionGateFixture {
	t.Helper()
	pool := database.NewPackageTestPool(t, "api")
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `DELETE FROM saas_settings WHERE key = $1`,
		models.SettingAndroidVersion); err != nil {
		t.Fatalf("clear android_version setting: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM system_apps WHERE platform = 'android'`); err != nil {
		t.Fatalf("clear android system_apps: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := pool.Exec(ctx, `DELETE FROM saas_settings WHERE key = $1`,
			models.SettingAndroidVersion); err != nil {
			t.Logf("cleanup android_version setting: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM system_apps WHERE platform = 'android'`); err != nil {
			t.Logf("cleanup android system_apps: %v", err)
		}
	})
	return &versionGateFixture{pool: pool}
}

// seedAndroidAPK inserts one downloadable Android system_app.
func (f *versionGateFixture) seedAndroidAPK(t *testing.T, version string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO system_apps (name, platform, version, file_path, size_bytes)
		VALUES ('EXAMVAN Android', 'android', $1, 'apk/examvan.apk', 1024)
		ON CONFLICT (name, platform, version) DO NOTHING`, version); err != nil {
		t.Fatalf("seed android apk %s: %v", version, err)
	}
}

// configureRequiredVersion seeds saas_settings android_version.
func (f *versionGateFixture) configureRequiredVersion(t *testing.T, version string) {
	t.Helper()
	if err := models.SetSaasSetting(context.Background(), f.pool,
		models.SettingAndroidVersion, version); err != nil {
		t.Fatalf("set android_version=%s: %v", version, err)
	}
}

// gateProbes issues the three requests with X-App-Version and reports whether
// each endpoint answered 426 (blocked) or moved past the version gate.
type gateProbeResult struct {
	JoinStatus       int
	SubmitStatus     int
	AccessLogStatus  int
}

// probe sends one request per endpoint, all with the given client version.
func probeVersionGates(t *testing.T, r http.Handler, clientVersion string) gateProbeResult {
	t.Helper()
	srv := httptest.NewServer(r)
	defer srv.Close()
	base := srv.URL

	do := func(method, path, body string) int {
		t.Helper()
		req, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatalf("build %s %s: %v", method, path, err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-App-Version", clientVersion)
		// SubmitExam rejects an empty X-Exam-Token with 401 BEFORE the
		// GetExamByID 404 these probes rely on; join and access-log ignore
		// the header, so setting it unconditionally is safe everywhere.
		req.Header.Set("X-Exam-Token", "it-version-gate-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	// Join: bogus token → 404 "Token tidak valid" once past the gate.
	join := do(http.MethodGet, "/api/exams/token/BOGUSTOKEN", "")
	// Submit: valid body + token header + nonexistent exam → 404.
	submit := do(http.MethodPost, "/api/exams/999999/submit",
		`{"student_name":"Siswa","exam_number":"01","mac_address":"AA:BB:CC:DD:EE:FF"}`)
	// AccessLog: valid body + nonexistent exam → 404.
	access := do(http.MethodPost, "/api/exams/999999/access-log",
		`{"event":"login","mac_address":"AA:BB:CC:DD:EE:FF"}`)

	return gateProbeResult{JoinStatus: join, SubmitStatus: submit, AccessLogStatus: access}
}

// assertNotUpgradeRequired verifies every endpoint moved past the version
// gate (i.e. answered its post-gate 404, not 426).
func assertNotUpgradeRequired(t *testing.T, label string, got gateProbeResult) {
	t.Helper()
	if got.JoinStatus == http.StatusUpgradeRequired ||
		got.SubmitStatus == http.StatusUpgradeRequired ||
		got.AccessLogStatus == http.StatusUpgradeRequired {
		t.Errorf("%s: version gate still blocks — join=%d submit=%d access-log=%d (want none 426)",
			label, got.JoinStatus, got.SubmitStatus, got.AccessLogStatus)
	}
	if got.JoinStatus != http.StatusNotFound {
		t.Errorf("%s: join status=%d, want 404 (past gate, bogus token)", label, got.JoinStatus)
	}
	if got.SubmitStatus != http.StatusNotFound {
		t.Errorf("%s: submit status=%d, want 404 (past gate, nonexistent exam)", label, got.SubmitStatus)
	}
	if got.AccessLogStatus != http.StatusNotFound {
		t.Errorf("%s: access-log status=%d, want 404 (past gate, nonexistent exam)", label, got.AccessLogStatus)
	}
}

// TestVersionGateClampToPublishedAPK pins case (a): the required version must
// be clamped to the published APK, so a stale configured value cannot lock
// clients beyond what /download/apk can serve. With the raw setting enforced,
// header 2.5.0 (== published APK) is rejected by demanding 3.0.0.
func TestVersionGateClampToPublishedAPK(t *testing.T) {
	f := newVersionGateFixture(t)
	f.seedAndroidAPK(t, "2.5.0")
	f.configureRequiredVersion(t, "3.0.0")

	got := probeVersionGates(t, newVersionGateRouter(f.pool), "2.5.0")
	assertNotUpgradeRequired(t, "clamp (APK 2.5.0, configured 3.0.0, header 2.5.0)", got)
}

// TestVersionGateNoAPKSkipsEnforcement pins case (b): with no Android APK
// published there is nothing to enforce (and nothing an outdated client could
// install), so an old client header must pass the gates instead of hitting a
// 426 deadlock. With the raw default (2.5.0) enforced, header 2.0.0 is 426.
func TestVersionGateNoAPKSkipsEnforcement(t *testing.T) {
	f := newVersionGateFixture(t)
	// no APK seeded, no android_version setting

	got := probeVersionGates(t, newVersionGateRouter(f.pool), "2.0.0")
	assertNotUpgradeRequired(t, "no APK (header 2.0.0)", got)
}

// TestVersionGateStillBlocksOutdatedClient is the regression guard: when an
// APK is published and the client header is older, all three sites must keep
// answering 426 (join via middleware, submit/access-log via the in-handler
// gates).
func TestVersionGateStillBlocksOutdatedClient(t *testing.T) {
	f := newVersionGateFixture(t)
	f.seedAndroidAPK(t, "2.5.0")
	f.configureRequiredVersion(t, "2.5.0")

	got := probeVersionGates(t, newVersionGateRouter(f.pool), "2.4.0")
	if got.JoinStatus != http.StatusUpgradeRequired {
		t.Errorf("join status=%d, want 426 (outdated client)", got.JoinStatus)
	}
	if got.SubmitStatus != http.StatusUpgradeRequired {
		t.Errorf("submit status=%d, want 426 (outdated client)", got.SubmitStatus)
	}
	if got.AccessLogStatus != http.StatusUpgradeRequired {
		t.Errorf("access-log status=%d, want 426 (outdated client)", got.AccessLogStatus)
	}
}

// TestVersionGateConfiguredBelowAPK pins the normal path: a configured
// requirement below the published APK is honoured as-is (no clamping upward),
// so a client at the configured version passes.
func TestVersionGateConfiguredBelowAPK(t *testing.T) {
	f := newVersionGateFixture(t)
	f.seedAndroidAPK(t, "2.5.0")
	f.configureRequiredVersion(t, "2.2.0")

	got := probeVersionGates(t, newVersionGateRouter(f.pool), "2.2.0")
	assertNotUpgradeRequired(t, "configured 2.2.0 < APK 2.5.0 (header 2.2.0)", got)
}
