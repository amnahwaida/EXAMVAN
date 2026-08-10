package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// SaaS settings integration tests — default_max_storage_size wiring.
// ---------------------------------------------------------------------------
//
// These exercise the GET/POST /api/saas-settings roundtrip for the "Default
// Paket Pendaftaran" storage quota (default_max_storage_size). The saas_settings
// table is deliberately kept across test runs (see database/testdb.go), so the
// test snapshots the full settings object and re-POSTs it with only the storage
// field changed — every other setting is restored as-is and no sibling test in
// this package can be affected.

// newSaasSettingsTestRouter mirrors production wiring for the SaaS settings
// endpoints: AuthRequired → FeatureLockRequired → SuperAdminRequired, plus a
// /test/login/:id seam that opens a real session (same session keys AuthRequired
// reads) so a superadmin can drive GET and POST.
func newSaasSettingsTestRouter(pool *pgxpool.Pool, storagePath string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
		// Point the storage path at a writable temp dir so the disk-cap
		// validation in the handler measures real (positive) free space.
		c.Set("cfg", &config.Config{StoragePath: storagePath})
	})

	r.POST("/test/login/:id", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Param("id"))
		u, err := models.GetUserByID(c.Request.Context(), pool, id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"success": false})
			return
		}
		s := sessions.Default(c)
		s.Set(middleware.SessionKeyAdminID, u.ID)
		s.Set(middleware.SessionKeyUsername, u.Username)
		s.Set(middleware.SessionKeyName, u.Name)
		s.Set(middleware.SessionKeyRole, u.Role)
		s.Set(middleware.SessionKeyIsSuper, u.IsSuperAdmin())
		s.Set(middleware.SessionKeyInstansi, u.Instansi)
		_ = s.Save()
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	api := r.Group("/api")
	api.Use(middleware.AuthRequired())
	api.Use(middleware.FeatureLockRequired())
	api.Use(middleware.SuperAdminRequired())
	api.GET("/saas-settings", SaasSettings())
	api.POST("/saas-settings", SaasSettings())

	// Per-user storage cap: CreateUser/EditUser mirror production wiring
	// (AuthRequired → FeatureLockRequired → AdminManagementRequired).
	usersAPI := r.Group("/api")
	usersAPI.Use(middleware.AuthRequired())
	usersAPI.Use(middleware.FeatureLockRequired())
	usersAPI.Use(middleware.AdminManagementRequired())
	usersAPI.POST("/users", CreateUser())
	usersAPI.POST("/users/:user_id/edit", EditUser())

	// Voucher & package storage cap: production wiring is AuthRequired →
	// FeatureLockRequired → SuperAdminRequired (no CSRF in tests, mirroring
	// the voucher lifecycle router).
	superAPI := r.Group("/api")
	superAPI.Use(middleware.AuthRequired())
	superAPI.Use(middleware.FeatureLockRequired())
	superAPI.Use(middleware.SuperAdminRequired())
	superAPI.POST("/vouchers", CreateVoucherHandler())
	superAPI.POST("/vouchers/batch", CreateBatchVouchersHandler())
	superAPI.POST("/packages", SavePackageSettingsHandler())

	// Dashboard stats API: production wiring is AuthRequired →
	// FeatureLockRequired (Stats is available to every admin role, so no
	// SuperAdminRequired/AdminManagementRequired here). GET /admin/api/stats
	// reports the storage partition free space as server_disk_free_mb — the
	// JSON value behind the dashboard's "Sisa Disk Server" indicator.
	statsAPI := r.Group("/admin/api")
	statsAPI.Use(middleware.AuthRequired())
	statsAPI.Use(middleware.FeatureLockRequired())
	statsAPI.GET("/stats", Stats())
	return r
}

func TestSaasSettingsDefaultMaxStorageSizeRoundtrip(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()

	su, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_saas_super", Name: "IT Saas Super",
		PasswordHash: "x", Status: models.UserStatusActive,
		Role:               models.SerializeRoles([]string{models.RoleSuperAdmin}),
		MaxExams:           3,
		MaxPDFSize:         1048576,
		MaxConcurrentExams: 2,
		MaxStorageSize:     50 * 1024 * 1024,
		Package:            "free",
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}

	storageDir, err := os.MkdirTemp("", "examvan-saas-it")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	jar, _ := cookiejar.New(nil)
	srv := httptest.NewServer(newSaasSettingsTestRouter(pool, storageDir))
	defer srv.Close()
	client := &http.Client{Jar: jar}

	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(su.ID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	getSettings := func() map[string]interface{} {
		t.Helper()
		resp, err := client.Get(srv.URL + "/api/saas-settings")
		if err != nil {
			t.Fatalf("GET saas-settings: %v", err)
		}
		defer resp.Body.Close()
		var out struct {
			Success  bool                   `json:"success"`
			Settings map[string]interface{} `json:"settings"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode GET response: %v", err)
		}
		if !out.Success {
			t.Fatalf("GET saas-settings not success")
		}
		return out.Settings
	}

	postSettings := func(settings map[string]interface{}) (bool, string) {
		t.Helper()
		body, err := json.Marshal(settings)
		if err != nil {
			t.Fatalf("marshal POST body: %v", err)
		}
		resp, err := client.Post(srv.URL+"/api/saas-settings", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST saas-settings: %v", err)
		}
		defer resp.Body.Close()
		var out struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode POST response: %v", err)
		}
		return out.Success, out.Message
	}

	storageMB := func(s map[string]interface{}) float64 {
		t.Helper()
		v, ok := s["default_max_storage_size_mb"].(float64)
		if !ok {
			t.Fatalf("default_max_storage_size_mb missing or not a number: %#v", s["default_max_storage_size_mb"])
		}
		return v
	}

	// 1) With the key missing, GET falls back to the 50 MB default — the same
	// default GetSaasSettingInt applies when new accounts register.
	if _, err := pool.Exec(ctx, `DELETE FROM saas_settings WHERE key = $1`, models.SettingDefaultMaxStorageSize); err != nil {
		t.Fatalf("delete storage key: %v", err)
	}
	if got := storageMB(getSettings()); got != 50 {
		t.Fatalf("default storage when unset = %v MB, want 50", got)
	}

	// 2) POST 250 MB → GET reflects it and the DB stores 250 * 1024 * 1024.
	settings := getSettings()
	settings["default_max_storage_size_mb"] = 250.0
	if ok, msg := postSettings(settings); !ok {
		t.Fatalf("POST 250 MB failed: %s", msg)
	}
	if got := storageMB(getSettings()); got != 250 {
		t.Fatalf("storage after POST 250 = %v MB, want 250", got)
	}
	var storedStr string
	if err := pool.QueryRow(ctx, `SELECT value FROM saas_settings WHERE key = $1`, models.SettingDefaultMaxStorageSize).Scan(&storedStr); err != nil {
		t.Fatalf("read stored storage: %v", err)
	}
	stored, err := strconv.ParseInt(storedStr, 10, 64)
	if err != nil {
		t.Fatalf("parse stored storage %q: %v", storedStr, err)
	}
	if stored != 250*1024*1024 {
		t.Fatalf("stored storage bytes = %d, want %d", stored, 250*1024*1024)
	}

	// 3) 0 = tidak terbatas (unlimited) roundtrips as 0 — the semantics of the
	// per-user quota editor (max_storage_size=0 means no enforcement).
	settings = getSettings()
	settings["default_max_storage_size_mb"] = 0.0
	if ok, msg := postSettings(settings); !ok {
		t.Fatalf("POST 0 MB failed: %s", msg)
	}
	if got := storageMB(getSettings()); got != 0 {
		t.Fatalf("storage after POST 0 = %v MB, want 0 (unlimited)", got)
	}
	if err := pool.QueryRow(ctx, `SELECT value FROM saas_settings WHERE key = $1`, models.SettingDefaultMaxStorageSize).Scan(&storedStr); err != nil {
		t.Fatalf("read stored storage (0): %v", err)
	}
	stored, err = strconv.ParseInt(storedStr, 10, 64)
	if err != nil {
		t.Fatalf("parse stored storage %q: %v", storedStr, err)
	}
	if stored != 0 {
		t.Fatalf("stored storage bytes = %d, want 0", stored)
	}

	// 4) Disk-cap enforcement: a value beyond the server disk free space is
	// rejected (400) WITHOUT writing anything — and 0 (unlimited) bypasses the
	// cap, so the huge value must fail while the previously stored 0 survives.
	settings = getSettings()
	settings["default_max_storage_size_mb"] = 1e12 // 1 EB — far beyond any disk
	if ok, msg := postSettings(settings); ok {
		t.Fatalf("POST 1e12 MB unexpectedly accepted")
	} else if !strings.Contains(msg, "melebihi sisa kapasitas disk") {
		t.Fatalf("unexpected rejection message: %q", msg)
	}
	assertStored := func(want int64, step string) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT value FROM saas_settings WHERE key = $1`, models.SettingDefaultMaxStorageSize).Scan(&storedStr); err != nil {
			t.Fatalf("read stored storage (%s): %v", step, err)
		}
		got, err := strconv.ParseInt(storedStr, 10, 64)
		if err != nil {
			t.Fatalf("parse stored storage (%s) %q: %v", step, storedStr, err)
		}
		if got != want {
			t.Fatalf("stored storage (%s) = %d, want %d", step, got, want)
		}
	}
	assertStored(0, "after rejected 1e12 MB")

	// 5) Negative values are rejected too (the UI clamps at min=0, but the API
	// must not accept them), and the DB stays untouched.
	settings = getSettings()
	settings["default_max_storage_size_mb"] = -5.0
	if ok, msg := postSettings(settings); ok {
		t.Fatalf("POST -5 MB unexpectedly accepted")
	} else if !strings.Contains(msg, "negatif") {
		t.Fatalf("unexpected rejection message: %q", msg)
	}
	assertStored(0, "after rejected -5 MB")

	// 6) GET also reports the storage partition free space for the UI cap.
	if free, ok := getSettings()["storage_free_mb"].(float64); !ok || free <= 0 {
		t.Fatalf("storage_free_mb missing or not positive: %#v", free)
	}

	// 7) Default PDF size: beyond disk → rejected, DB untouched (the key may
	// not exist yet — either way it must not hold the huge value).
	settings = getSettings()
	settings["default_max_pdf_size_mb"] = 1e12 // 1 EB — far beyond any disk
	if ok, msg := postSettings(settings); ok {
		t.Fatalf("POST pdf 1e12 MB unexpectedly accepted")
	} else if !strings.Contains(msg, "melebihi sisa kapasitas disk") {
		t.Fatalf("unexpected pdf rejection message: %q", msg)
	}
	var pdfStoredStr string
	if qerr := pool.QueryRow(ctx, `SELECT value FROM saas_settings WHERE key = $1`, models.SettingDefaultMaxPDFSize).Scan(&pdfStoredStr); qerr == nil {
		if v, perr := strconv.ParseInt(pdfStoredStr, 10, 64); perr == nil && v >= 1e12*1024*1024 {
			t.Fatalf("pdf 1e12 MB written despite rejection (stored=%d)", v)
		}
	}

	// 8) Default PDF size: negative → rejected.
	settings = getSettings()
	settings["default_max_pdf_size_mb"] = -5.0
	if ok, msg := postSettings(settings); ok {
		t.Fatalf("POST pdf -5 MB unexpectedly accepted")
	} else if !strings.Contains(msg, "negatif") {
		t.Fatalf("unexpected pdf rejection message: %q", msg)
	}

	// 9) Default PDF size: 10 MB valid → stored as bytes.
	settings = getSettings()
	settings["default_max_pdf_size_mb"] = 10.0
	if ok, msg := postSettings(settings); !ok {
		t.Fatalf("POST pdf 10 MB failed: %s", msg)
	}
	if err := pool.QueryRow(ctx, `SELECT value FROM saas_settings WHERE key = $1`, models.SettingDefaultMaxPDFSize).Scan(&pdfStoredStr); err != nil {
		t.Fatalf("read stored pdf (10): %v", err)
	}
	pdfStored, err := strconv.ParseInt(pdfStoredStr, 10, 64)
	if err != nil {
		t.Fatalf("parse stored pdf %q: %v", pdfStoredStr, err)
	}
	if pdfStored != 10*1024*1024 {
		t.Fatalf("stored pdf bytes = %d, want %d", pdfStored, 10*1024*1024)
	}
}

// max_approvals_per_exam — the per-exam approved-device cap behind server-side
// auto-approve (anti-spam) — must roundtrip through the SaaS settings API with
// the right semantics: default 500 when unset, 0 = unlimited preserved (a
// meaningful value, unlike an absent field), negatives clamped to 0.
func TestSaasSettingsMaxApprovalsPerExamRoundtrip(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()

	su, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_mape_super", Name: "IT Mape Super",
		PasswordHash: "x", Status: models.UserStatusActive,
		Role:               models.SerializeRoles([]string{models.RoleSuperAdmin}),
		MaxExams:           3,
		MaxPDFSize:         1048576,
		MaxConcurrentExams: 2,
		MaxStorageSize:     50 * 1024 * 1024,
		Package:            "free",
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}

	storageDir, err := os.MkdirTemp("", "examvan-mape-it")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	jar, _ := cookiejar.New(nil)
	srv := httptest.NewServer(newSaasSettingsTestRouter(pool, storageDir))
	defer srv.Close()
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(su.ID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	getCap := func() float64 {
		t.Helper()
		resp, err := client.Get(srv.URL + "/api/saas-settings")
		if err != nil {
			t.Fatalf("GET saas-settings: %v", err)
		}
		defer resp.Body.Close()
		var out struct {
			Success  bool                   `json:"success"`
			Settings map[string]interface{} `json:"settings"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode GET response: %v", err)
		}
		if !out.Success {
			t.Fatalf("GET saas-settings not success")
		}
		v, ok := out.Settings["max_approvals_per_exam"].(float64)
		if !ok {
			t.Fatalf("max_approvals_per_exam missing or not a number: %#v", out.Settings["max_approvals_per_exam"])
		}
		return v
	}

	postCap := func(cap float64) {
		t.Helper()
		// Snapshot + re-POST the full settings object (the handler writes every
		// field) with only max_approvals_per_exam changed — same pattern as the
		// storage roundtrip test, so no sibling setting is disturbed.
		resp, err := client.Get(srv.URL + "/api/saas-settings")
		if err != nil {
			t.Fatalf("GET saas-settings (snapshot): %v", err)
		}
		var snap struct {
			Settings map[string]interface{} `json:"settings"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
			t.Fatalf("decode snapshot: %v", err)
		}
		resp.Body.Close()
		snap.Settings["max_approvals_per_exam"] = cap
		body, err := json.Marshal(snap.Settings)
		if err != nil {
			t.Fatalf("marshal POST body: %v", err)
		}
		presp, err := client.Post(srv.URL+"/api/saas-settings", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST saas-settings: %v", err)
		}
		defer presp.Body.Close()
		var out struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}
		if err := json.NewDecoder(presp.Body).Decode(&out); err != nil {
			t.Fatalf("decode POST response: %v", err)
		}
		if !out.Success {
			t.Fatalf("POST max_approvals_per_exam=%v failed: %s", cap, out.Message)
		}
	}

	stored := func() string {
		t.Helper()
		var v string
		if err := pool.QueryRow(ctx, `SELECT value FROM saas_settings WHERE key = $1`, models.SettingMaxApprovalsPerExam).Scan(&v); err != nil {
			t.Fatalf("read stored max_approvals_per_exam: %v", err)
		}
		return v
	}

	// 1) Key missing → GET falls back to the 500 default (same default the
	// RequestApproval cap check uses when the row is absent).
	if _, err := pool.Exec(ctx, `DELETE FROM saas_settings WHERE key = $1`, models.SettingMaxApprovalsPerExam); err != nil {
		t.Fatalf("delete key: %v", err)
	}
	if got := getCap(); got != 500 {
		t.Fatalf("default max_approvals_per_exam = %v, want 500", got)
	}

	// 2) POST 250 → GET reflects it and the DB stores "250".
	postCap(250)
	if got := getCap(); got != 250 {
		t.Fatalf("cap after POST 250 = %v, want 250", got)
	}
	if s := stored(); s != "250" {
		t.Fatalf("stored cap = %q, want \"250\"", s)
	}

	// 3) 0 = tak terbatas (unlimited) — a meaningful value, roundtrips as 0.
	postCap(0)
	if got := getCap(); got != 0 {
		t.Fatalf("cap after POST 0 = %v, want 0 (unlimited)", got)
	}
	if s := stored(); s != "0" {
		t.Fatalf("stored cap (0) = %q, want \"0\"", s)
	}

	// 4) Negative → clamped to 0 (unlimited), not stored raw.
	postCap(-5)
	if got := getCap(); got != 0 {
		t.Fatalf("cap after POST -5 = %v, want 0 (clamped)", got)
	}
	if s := stored(); s != "0" {
		t.Fatalf("stored cap (-5) = %q, want \"0\" (clamped)", s)
	}
}

// approval-cleanup job tuning (interval minutes, ended-grace hours, inactive
// TTL hours) must roundtrip through the SaaS settings API: defaults 15/1/24
// when unset, custom values stored as-is, interval clamped to >= 1 (a 0 would
// make the background loop spin), and grace/TTL negatives clamped to 0 while a
// deliberate 0 (purge immediately) is preserved.
func TestSaasSettingsApprovalCleanupTuningRoundtrip(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()

	su, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_cleanup_super", Name: "IT Cleanup Super",
		PasswordHash: "x", Status: models.UserStatusActive,
		Role:               models.SerializeRoles([]string{models.RoleSuperAdmin}),
		MaxExams:           3,
		MaxPDFSize:         1048576,
		MaxConcurrentExams: 2,
		MaxStorageSize:     50 * 1024 * 1024,
		Package:            "free",
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}

	storageDir, err := os.MkdirTemp("", "examvan-cleanup-it")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	jar, _ := cookiejar.New(nil)
	srv := httptest.NewServer(newSaasSettingsTestRouter(pool, storageDir))
	defer srv.Close()
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(su.ID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	cleanupKeys := []string{
		models.SettingApprovalCleanupIntervalMinutes,
		models.SettingApprovalCleanupEndedGraceHours,
		models.SettingApprovalCleanupInactiveTTLHours,
	}
	// saas_settings survives TRUNCATE (deliberately, see testdb.go), so this
	// test cleans up after itself — the purge job's defaults must not be left
	// changed for sibling tests (e.g. approval cleanup integration tests).
	t.Cleanup(func() {
		for _, k := range cleanupKeys {
			_, _ = pool.Exec(context.Background(), `DELETE FROM saas_settings WHERE key = $1`, k)
		}
	})
	for _, k := range cleanupKeys {
		if _, err := pool.Exec(ctx, `DELETE FROM saas_settings WHERE key = $1`, k); err != nil {
			t.Fatalf("reset key %s: %v", k, err)
		}
	}

	getTuning := func() (interval, grace, ttl float64) {
		t.Helper()
		resp, err := client.Get(srv.URL + "/api/saas-settings")
		if err != nil {
			t.Fatalf("GET saas-settings: %v", err)
		}
		defer resp.Body.Close()
		var out struct {
			Settings map[string]interface{} `json:"settings"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode GET response: %v", err)
		}
		num := func(k string) float64 {
			v, ok := out.Settings[k].(float64)
			if !ok {
				t.Fatalf("%s missing or not a number: %#v", k, out.Settings[k])
			}
			return v
		}
		return num("approval_cleanup_interval_minutes"),
			num("approval_cleanup_ended_grace_hours"),
			num("approval_cleanup_inactive_ttl_hours")
	}

	postTuning := func(interval, grace, ttl float64) {
		t.Helper()
		resp, err := client.Get(srv.URL + "/api/saas-settings")
		if err != nil {
			t.Fatalf("GET snapshot: %v", err)
		}
		var snap struct {
			Settings map[string]interface{} `json:"settings"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
			t.Fatalf("decode snapshot: %v", err)
		}
		resp.Body.Close()
		snap.Settings["approval_cleanup_interval_minutes"] = interval
		snap.Settings["approval_cleanup_ended_grace_hours"] = grace
		snap.Settings["approval_cleanup_inactive_ttl_hours"] = ttl
		body, err := json.Marshal(snap.Settings)
		if err != nil {
			t.Fatalf("marshal POST body: %v", err)
		}
		presp, err := client.Post(srv.URL+"/api/saas-settings", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST saas-settings: %v", err)
		}
		defer presp.Body.Close()
		var out struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}
		if err := json.NewDecoder(presp.Body).Decode(&out); err != nil {
			t.Fatalf("decode POST response: %v", err)
		}
		if !out.Success {
			t.Fatalf("POST cleanup tuning failed: %s", out.Message)
		}
	}

	stored := func(key string) string {
		t.Helper()
		var v string
		if err := pool.QueryRow(ctx, `SELECT value FROM saas_settings WHERE key = $1`, key).Scan(&v); err != nil {
			t.Fatalf("read stored %s: %v", key, err)
		}
		return v
	}

	// 1) Keys missing → defaults 15 / 1 / 24 (same defaults the purge job
	// applies when the settings rows are absent).
	iv, gr, ttl := getTuning()
	if iv != 15 || gr != 1 || ttl != 24 {
		t.Fatalf("defaults = %v/%v/%v, want 15/1/24", iv, gr, ttl)
	}

	// 2) POST custom values → GET reflects and DB stores them verbatim.
	postTuning(30, 2, 48)
	iv, gr, ttl = getTuning()
	if iv != 30 || gr != 2 || ttl != 48 {
		t.Fatalf("after POST 30/2/48 = %v/%v/%v", iv, gr, ttl)
	}
	if s := stored(models.SettingApprovalCleanupIntervalMinutes); s != "30" {
		t.Fatalf("stored interval = %q, want \"30\"", s)
	}
	if s := stored(models.SettingApprovalCleanupEndedGraceHours); s != "2" {
		t.Fatalf("stored grace = %q, want \"2\"", s)
	}
	if s := stored(models.SettingApprovalCleanupInactiveTTLHours); s != "48" {
		t.Fatalf("stored ttl = %q, want \"48\"", s)
	}

	// 3) Interval 0 → clamped to 1 (a 0-minute cadence would busy-loop the
	// background job); grace/TTL 0 preserved as-is (deliberate "purge
	// immediately" choice).
	postTuning(0, 0, 0)
	iv, gr, ttl = getTuning()
	if iv != 1 {
		t.Fatalf("interval after POST 0 = %v, want 1 (clamped minimum)", iv)
	}
	if gr != 0 || ttl != 0 {
		t.Fatalf("grace/ttl after POST 0 = %v/%v, want 0/0 (preserved)", gr, ttl)
	}
	if s := stored(models.SettingApprovalCleanupIntervalMinutes); s != "1" {
		t.Fatalf("stored interval (0) = %q, want \"1\"", s)
	}

	// 4) Negatives → clamped: interval to 1, grace/TTL to 0 (never stored raw).
	postTuning(-10, -2, -3)
	iv, gr, ttl = getTuning()
	if iv != 1 || gr != 0 || ttl != 0 {
		t.Fatalf("after POST negatives = %v/%v/%v, want 1/0/0 (clamped)", iv, gr, ttl)
	}
	if s := stored(models.SettingApprovalCleanupInactiveTTLHours); s != "0" {
		t.Fatalf("stored ttl (-3) = %q, want \"0\" (clamped)", s)
	}
}

// TestSaasSettingsMaxApprovalsPerExamUIMarkup pins the SaaS-panel field in
// users.html and its admin.js wiring (save reads the input, load fills it), so
// the SuperAdmin-editable cap cannot silently vanish from the UI.
func TestSaasSettingsMaxApprovalsPerExamUIMarkup(t *testing.T) {
	templatesDir := "templates"
	if _, err := os.Stat(templatesDir); err != nil {
		templatesDir = filepath.Join("..", "..", "..", "templates")
	}
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(templatesDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return string(data)
	}

	users := read("admin/users.html")
	for _, frag := range []string{
		`id="maxApprovalsPerExamInput"`,
		"Maks Perangkat Disetujui per Ujian",
		// Approval-cleanup job tuning fields (SuperAdmin-tunable purge cadence).
		`id="approvalCleanupIntervalMinutesInput"`,
		`id="approvalCleanupEndedGraceHoursInput"`,
		`id="approvalCleanupInactiveTTLHoursInput"`,
		"Pembersihan Otomatis Antrean Persetujuan",
	} {
		if !strings.Contains(users, frag) {
			t.Errorf("users.html must contain %q (approval-cap/cleanup field markup)", frag)
		}
	}

	js, err := os.ReadFile("static/js/admin.js")
	if err != nil {
		js, err = os.ReadFile(filepath.Join("..", "..", "..", "static/js/admin.js"))
	}
	if err != nil {
		t.Fatalf("read admin.js: %v", err)
	}
	jsStr := string(js)
	for _, frag := range []string{
		"maxApprovalsPerExamInput",
		"max_approvals_per_exam",
		"approvalCleanupIntervalMinutesInput",
		"approval_cleanup_interval_minutes",
		"approvalCleanupEndedGraceHoursInput",
		"approval_cleanup_ended_grace_hours",
		"approvalCleanupInactiveTTLHoursInput",
		"approval_cleanup_inactive_ttl_hours",
	} {
		if !strings.Contains(jsStr, frag) {
			t.Errorf("admin.js must contain %q (approval-cap/cleanup wiring)", frag)
		}
	}
}

// TestUserStorageQuotaDiskCap verifies the same disk-cap is enforced on the
// per-user Maks Storage quota in Tambah User (CreateUser) and Atur Limit
// (EditUser): values beyond the server disk free space — and negative values —
// are rejected, while 0 (unlimited) and modest values pass.
func TestUserStorageQuotaDiskCap(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()

	su, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_cap_super", Name: "IT Cap Super",
		PasswordHash: "x", Status: models.UserStatusActive,
		Role:               models.SerializeRoles([]string{models.RoleSuperAdmin}),
		MaxExams:           3,
		MaxPDFSize:         1048576,
		MaxConcurrentExams: 2,
		MaxStorageSize:     50 * 1024 * 1024,
		Package:            "free",
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}

	storageDir, err := os.MkdirTemp("", "examvan-cap-it")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	jar, _ := cookiejar.New(nil)
	srv := httptest.NewServer(newSaasSettingsTestRouter(pool, storageDir))
	defer srv.Close()
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(su.ID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	postJSON := func(path string, payload interface{}) (int, string) {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		resp, err := client.Post(srv.URL+path, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		defer resp.Body.Close()
		var out struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return resp.StatusCode, out.Message
	}

	// 1) CreateUser: storage beyond disk → rejected, no user created.
	code, msg := postJSON("/api/users", map[string]interface{}{
		"username": "it_cap_big", "password": "test1234",
		"max_storage_size_mb": 1e12,
	})
	if code != http.StatusBadRequest || !strings.Contains(msg, "melebihi sisa kapasitas disk") {
		t.Fatalf("CreateUser huge storage: code=%d msg=%q", code, msg)
	}

	// 2) CreateUser: negative → rejected.
	code, msg = postJSON("/api/users", map[string]interface{}{
		"username": "it_cap_neg", "password": "test1234",
		"max_storage_size_mb": -5,
	})
	if code != http.StatusBadRequest || !strings.Contains(msg, "negatif") {
		t.Fatalf("CreateUser negative storage: code=%d msg=%q", code, msg)
	}

	// 3) CreateUser: 250 MB (under disk) → accepted.
	code, msg = postJSON("/api/users", map[string]interface{}{
		"username": "it_cap_ok", "password": "test1234",
		"max_storage_size_mb": 250,
	})
	if code != http.StatusOK {
		t.Fatalf("CreateUser 250 MB: code=%d msg=%q", code, msg)
	}

	created, err := models.GetUserByUsername(ctx, pool, "it_cap_ok")
	if err != nil {
		t.Fatalf("get created user: %v", err)
	}
	if created.MaxStorageSize != 250*1024*1024 {
		t.Fatalf("created user storage = %d, want %d", created.MaxStorageSize, 250*1024*1024)
	}

	// 3b) CreateUser: PDF beyond disk → rejected, no user created.
	code, msg = postJSON("/api/users", map[string]interface{}{
		"username": "it_cap_pdf_big", "password": "test1234",
		"max_pdf_size_mb": 1e12,
	})
	if code != http.StatusBadRequest || !strings.Contains(msg, "melebihi sisa kapasitas disk") {
		t.Fatalf("CreateUser huge pdf: code=%d msg=%q", code, msg)
	}
	var pdfBigCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM admin_users WHERE username = 'it_cap_pdf_big'`).Scan(&pdfBigCount); err != nil {
		t.Fatalf("count pdf-big user: %v", err)
	}
	if pdfBigCount != 0 {
		t.Fatalf("pdf-big user created unexpectedly (count=%d)", pdfBigCount)
	}

	// 3c) CreateUser: PDF negative → rejected.
	code, msg = postJSON("/api/users", map[string]interface{}{
		"username": "it_cap_pdf_neg", "password": "test1234",
		"max_pdf_size_mb": -5,
	})
	if code != http.StatusBadRequest || !strings.Contains(msg, "negatif") {
		t.Fatalf("CreateUser negative pdf: code=%d msg=%q", code, msg)
	}

	// 3d) CreateUser: PDF 10 MB → accepted and stored as bytes.
	code, msg = postJSON("/api/users", map[string]interface{}{
		"username": "it_cap_pdf_ok", "password": "test1234",
		"max_pdf_size_mb": 10,
	})
	if code != http.StatusOK {
		t.Fatalf("CreateUser pdf 10 MB: code=%d msg=%q", code, msg)
	}
	pdfUser, err := models.GetUserByUsername(ctx, pool, "it_cap_pdf_ok")
	if err != nil {
		t.Fatalf("get pdf user: %v", err)
	}
	if pdfUser.MaxPDFSize != 10*1024*1024 {
		t.Fatalf("created user pdf = %d, want %d", pdfUser.MaxPDFSize, 10*1024*1024)
	}

	// 4) EditUser: storage beyond disk → rejected, other fields untouched.
	code, msg = postJSON("/api/users/"+strconv.Itoa(created.ID)+"/edit", map[string]interface{}{
		"name": "Changed Name", "max_storage_size_mb": 1e12,
	})
	if code != http.StatusBadRequest || !strings.Contains(msg, "melebihi sisa kapasitas disk") {
		t.Fatalf("EditUser huge storage: code=%d msg=%q", code, msg)
	}

	// 5) EditUser: 0 (unlimited) → accepted.
	code, msg = postJSON("/api/users/"+strconv.Itoa(created.ID)+"/edit", map[string]interface{}{
		"max_storage_size_mb": 0,
	})
	if code != http.StatusOK {
		t.Fatalf("EditUser 0 MB: code=%d msg=%q", code, msg)
	}
	refreshed, err := models.GetUserByID(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("get edited user: %v", err)
	}
	if refreshed.MaxStorageSize != 0 {
		t.Fatalf("edited user storage = %d, want 0 (unlimited)", refreshed.MaxStorageSize)
	}

	// 6) EditUser: PDF beyond disk → rejected, other fields untouched.
	code, msg = postJSON("/api/users/"+strconv.Itoa(created.ID)+"/edit", map[string]interface{}{
		"name": "Changed Again", "max_pdf_size_mb": 1e12,
	})
	if code != http.StatusBadRequest || !strings.Contains(msg, "melebihi sisa kapasitas disk") {
		t.Fatalf("EditUser huge pdf: code=%d msg=%q", code, msg)
	}
	nameAfter, err := models.GetUserByID(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("get user after pdf reject: %v", err)
	}
	if nameAfter.Name == "Changed Again" {
		t.Fatalf("EditUser huge pdf partially applied (name changed)")
	}

	// 7) EditUser: PDF 20 MB → accepted and stored as bytes.
	code, msg = postJSON("/api/users/"+strconv.Itoa(created.ID)+"/edit", map[string]interface{}{
		"max_pdf_size_mb": 20,
	})
	if code != http.StatusOK {
		t.Fatalf("EditUser pdf 20 MB: code=%d msg=%q", code, msg)
	}
	refreshed2, err := models.GetUserByID(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("get edited user: %v", err)
	}
	if refreshed2.MaxPDFSize != 20*1024*1024 {
		t.Fatalf("edited user pdf = %d, want %d", refreshed2.MaxPDFSize, 20*1024*1024)
	}
}

// TestVoucherPackageStorageDiskCap verifies the same disk-cap is enforced when
// a custom voucher's storage quota is defined (single/batch create both go
// through parseCustomVoucherInto) and when package storage quotas are saved
// (Pengaturan Paket).
func TestVoucherPackageStorageDiskCap(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()

	su, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_vp_super", Name: "IT VoucherPkg Super",
		PasswordHash: "x", Status: models.UserStatusActive,
		Role:               models.SerializeRoles([]string{models.RoleSuperAdmin}),
		MaxExams:           3,
		MaxPDFSize:         1048576,
		MaxConcurrentExams: 2,
		MaxStorageSize:     50 * 1024 * 1024,
		Package:            "free",
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}

	storageDir, err := os.MkdirTemp("", "examvan-vp-it")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	jar, _ := cookiejar.New(nil)
	srv := httptest.NewServer(newSaasSettingsTestRouter(pool, storageDir))
	defer srv.Close()
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(su.ID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	postForm := func(path string, form url.Values) (int, string) {
		t.Helper()
		resp, err := client.PostForm(srv.URL+path, form)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		defer resp.Body.Close()
		var out struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return resp.StatusCode, out.Message
	}

	postJSON := func(path string, payload interface{}) (int, string) {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		resp, err := client.Post(srv.URL+path, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		defer resp.Body.Close()
		var out struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return resp.StatusCode, out.Message
	}

	baseVoucher := url.Values{
		"code":          {"IT-VP-001"},
		"package":       {"custom"},
		"duration_type": {"bulanan"},
	}

	// 1) Voucher custom: storage beyond disk → rejected (nothing created).
	form := url.Values{}
	for k, v := range baseVoucher {
		form[k] = v
	}
	form.Set("custom_max_storage_size_mb", "999999999")
	code, msg := postForm("/api/vouchers", form)
	if code != http.StatusBadRequest || !strings.Contains(msg, "melebihi sisa kapasitas disk") {
		t.Fatalf("voucher huge storage: code=%d msg=%q", code, msg)
	}

	// 2) Voucher custom valid (250 MB) → accepted and stored as bytes.
	form = url.Values{}
	for k, v := range baseVoucher {
		form[k] = v
	}
	form.Set("code", "IT-VP-002")
	form.Set("custom_max_storage_size_mb", "250")
	code, msg = postForm("/api/vouchers", form)
	if code != http.StatusOK {
		t.Fatalf("voucher 250 MB: code=%d msg=%q", code, msg)
	}
	var stored int64
	if err := pool.QueryRow(ctx, `SELECT custom_max_storage_size FROM vouchers WHERE code = 'IT-VP-002'`).Scan(&stored); err != nil {
		t.Fatalf("read voucher storage: %v", err)
	}
	if stored != 250*1024*1024 {
		t.Fatalf("voucher stored storage = %d, want %d", stored, 250*1024*1024)
	}

	// 2b) Voucher custom: negative storage → rejected (semantic parity with the
	// other storage editors).
	form = url.Values{}
	for k, v := range baseVoucher {
		form[k] = v
	}
	form.Set("code", "IT-VP-NEG")
	form.Set("custom_max_storage_size_mb", "-5")
	code, msg = postForm("/api/vouchers", form)
	if code != http.StatusBadRequest || !strings.Contains(msg, "tidak boleh bernilai negatif") {
		t.Fatalf("voucher negative storage: code=%d msg=%q", code, msg)
	}
	var negCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM vouchers WHERE code = 'IT-VP-NEG'`).Scan(&negCount); err != nil {
		t.Fatalf("count voucher negative: %v", err)
	}
	if negCount != 0 {
		t.Fatalf("negative voucher created unexpectedly (count=%d)", negCount)
	}

	// 2c) Voucher batch: huge storage → rejected for the whole batch.
	batch := url.Values{
		"prefix":                     {"IT-VP-B"},
		"package":                    {"custom"},
		"duration_type":              {"bulanan"},
		"count":                      {"3"},
		"custom_max_storage_size_mb": {"999999999"},
	}
	code, msg = postForm("/api/vouchers/batch", batch)
	if code != http.StatusBadRequest || !strings.Contains(msg, "melebihi sisa kapasitas disk") {
		t.Fatalf("voucher batch huge storage: code=%d msg=%q", code, msg)
	}
	var batchCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM vouchers WHERE code LIKE 'IT-VP-B%'`).Scan(&batchCount); err != nil {
		t.Fatalf("count batch vouchers: %v", err)
	}
	if batchCount != 0 {
		t.Fatalf("batch vouchers created unexpectedly (count=%d)", batchCount)
	}

	// 2d) Voucher custom: PDF size beyond disk → rejected (nothing created).
	form = url.Values{}
	for k, v := range baseVoucher {
		form[k] = v
	}
	form.Set("code", "IT-VP-PDF-HUGE")
	form.Set("custom_max_pdf_size_mb", "999999999")
	code, msg = postForm("/api/vouchers", form)
	if code != http.StatusBadRequest || !strings.Contains(msg, "melebihi sisa kapasitas disk") {
		t.Fatalf("voucher huge pdf: code=%d msg=%q", code, msg)
	}

	// 2e) Voucher custom: PDF size negative → rejected explicitly.
	form = url.Values{}
	for k, v := range baseVoucher {
		form[k] = v
	}
	form.Set("code", "IT-VP-PDF-NEG")
	form.Set("custom_max_pdf_size_mb", "-5")
	code, msg = postForm("/api/vouchers", form)
	if code != http.StatusBadRequest || !strings.Contains(msg, "tidak boleh bernilai negatif") {
		t.Fatalf("voucher negative pdf: code=%d msg=%q", code, msg)
	}
	var pdfNegCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM vouchers WHERE code = 'IT-VP-PDF-NEG'`).Scan(&pdfNegCount); err != nil {
		t.Fatalf("count voucher pdf negative: %v", err)
	}
	if pdfNegCount != 0 {
		t.Fatalf("negative-pdf voucher created unexpectedly (count=%d)", pdfNegCount)
	}

	// 2f) Voucher custom: PDF size valid (10 MB) → accepted and stored as bytes.
	form = url.Values{}
	for k, v := range baseVoucher {
		form[k] = v
	}
	form.Set("code", "IT-VP-PDF-OK")
	form.Set("custom_max_pdf_size_mb", "10")
	code, msg = postForm("/api/vouchers", form)
	if code != http.StatusOK {
		t.Fatalf("voucher 10 MB pdf: code=%d msg=%q", code, msg)
	}
	var pdfStored int64
	if err := pool.QueryRow(ctx, `SELECT custom_max_pdf_size FROM vouchers WHERE code = 'IT-VP-PDF-OK'`).Scan(&pdfStored); err != nil {
		t.Fatalf("read voucher pdf: %v", err)
	}
	if pdfStored != 10*1024*1024 {
		t.Fatalf("voucher stored pdf = %d, want %d", pdfStored, 10*1024*1024)
	}

	// 2g) Voucher batch: PDF size huge → rejected for the whole batch.
	batchPDF := url.Values{
		"prefix":                 {"IT-VP-PB"},
		"package":                {"custom"},
		"duration_type":          {"bulanan"},
		"count":                  {"3"},
		"custom_max_pdf_size_mb": {"999999999"},
	}
	code, msg = postForm("/api/vouchers/batch", batchPDF)
	if code != http.StatusBadRequest || !strings.Contains(msg, "melebihi sisa kapasitas disk") {
		t.Fatalf("voucher batch huge pdf: code=%d msg=%q", code, msg)
	}
	var batchPdfCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM vouchers WHERE code LIKE 'IT-VP-PB%'`).Scan(&batchPdfCount); err != nil {
		t.Fatalf("count batch pdf vouchers: %v", err)
	}
	if batchPdfCount != 0 {
		t.Fatalf("batch pdf vouchers created unexpectedly (count=%d)", batchPdfCount)
	}

	pkgPayload := func(storageMB, pdfMB float64) map[string]interface{} {
		return map[string]interface{}{
			"packages": []map[string]interface{}{
				{
					"key": "guru", "label": "Paket Guru",
					"max_exams": 1, "max_concurrent_exams": 1,
					"max_pdf_size_mb": pdfMB, "max_storage_mb": storageMB,
					"max_users": 0, "role": "guru",
				},
			},
		}
	}

	// 3) Package storage beyond disk → rejected (transaction rolled back).
	code, msg = postJSON("/api/packages", pkgPayload(1e12, 10))
	if code != http.StatusBadRequest || !strings.Contains(msg, "melebihi sisa kapasitas disk") {
		t.Fatalf("package huge storage: code=%d msg=%q", code, msg)
	}

	// 3b) Package storage negative → rejected explicitly (not clamped to 1 MB).
	code, msg = postJSON("/api/packages", pkgPayload(-5, 10))
	if code != http.StatusBadRequest || !strings.Contains(msg, "tidak boleh bernilai negatif") {
		t.Fatalf("package negative storage: code=%d msg=%q", code, msg)
	}

	// 3c) Package PDF size beyond disk → rejected.
	code, msg = postJSON("/api/packages", pkgPayload(250, 1e12))
	if code != http.StatusBadRequest || !strings.Contains(msg, "melebihi sisa kapasitas disk") {
		t.Fatalf("package huge pdf: code=%d msg=%q", code, msg)
	}

	// 3d) Package PDF size negative → rejected explicitly (not clamped to 1 MB).
	code, msg = postJSON("/api/packages", pkgPayload(250, -5))
	if code != http.StatusBadRequest || !strings.Contains(msg, "tidak boleh bernilai negatif") {
		t.Fatalf("package negative pdf: code=%d msg=%q", code, msg)
	}

	// 4) Package storage valid (250 MB) → accepted.
	code, msg = postJSON("/api/packages", pkgPayload(250, 10))
	if code != http.StatusOK {
		t.Fatalf("package 250 MB: code=%d msg=%q", code, msg)
	}
	var pkgStored int64
	if err := pool.QueryRow(ctx, `SELECT max_storage_size FROM package_settings WHERE pkg_key = 'guru'`).Scan(&pkgStored); err != nil {
		t.Fatalf("read package storage: %v", err)
	}
	if pkgStored != 250*1024*1024 {
		t.Fatalf("package stored storage = %d, want %d", pkgStored, 250*1024*1024)
	}

	// 4b) Package PDF size valid (20 MB) → accepted and stored as bytes.
	code, msg = postJSON("/api/packages", pkgPayload(250, 20))
	if code != http.StatusOK {
		t.Fatalf("package 20 MB pdf: code=%d msg=%q", code, msg)
	}
	var pkgPdfStored int64
	if err := pool.QueryRow(ctx, `SELECT max_pdf_size FROM package_settings WHERE pkg_key = 'guru'`).Scan(&pkgPdfStored); err != nil {
		t.Fatalf("read package pdf: %v", err)
	}
	if pkgPdfStored != 20*1024*1024 {
		t.Fatalf("package stored pdf = %d, want %d", pkgPdfStored, 20*1024*1024)
	}

	// 5) Multi-package payload with one invalid value → whole payload rejected,
	// no partial save (transaction rolled back, first package untouched).
	rollbackPayload := map[string]interface{}{
		"packages": []map[string]interface{}{
			{
				"key": "guru", "label": "Paket Guru",
				"max_exams": 1, "max_concurrent_exams": 1,
				"max_pdf_size_mb": 10, "max_storage_mb": 300,
				"max_users": 0, "role": "guru",
			},
			{
				"key": "free", "label": "Free / Trial",
				"max_exams": 1, "max_concurrent_exams": 1,
				"max_pdf_size_mb": 1, "max_storage_mb": 1e12,
				"max_users": 0, "role": "",
			},
		},
	}
	code, msg = postJSON("/api/packages", rollbackPayload)
	if code != http.StatusBadRequest || !strings.Contains(msg, "melebihi sisa kapasitas disk") {
		t.Fatalf("multi-package rollback: code=%d msg=%q", code, msg)
	}
	var guruAfter int64
	if err := pool.QueryRow(ctx, `SELECT max_storage_size FROM package_settings WHERE pkg_key = 'guru'`).Scan(&guruAfter); err != nil {
		t.Fatalf("read guru after rollback: %v", err)
	}
	if guruAfter != 250*1024*1024 {
		t.Fatalf("guru storage changed despite rollback = %d, want %d", guruAfter, 250*1024*1024)
	}
}

// TestStatsServerDiskFree verifies GET /admin/api/stats — the JSON source of
// the dashboard's "Sisa Disk Server" indicator — reports the free space of
// the server storage partition (server_disk_free_mb, > 0 when the storage
// path is a real writable directory) alongside the exam aggregates. The
// storage path is pointed at a temp dir, so the handler's statfs measures
// real (positive) free space, exactly like the disk-cap tests above. Also
// checks the endpoint is auth-gated (401 without a session).
func TestStatsServerDiskFree(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()

	su, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "it_stats_super", Name: "IT Stats Super",
		PasswordHash: "x", Status: models.UserStatusActive,
		Role:               models.SerializeRoles([]string{models.RoleSuperAdmin}),
		MaxExams:           3,
		MaxPDFSize:         1048576,
		MaxConcurrentExams: 2,
		MaxStorageSize:     50 * 1024 * 1024,
		Package:            "free",
	})
	if err != nil {
		t.Fatalf("create superadmin: %v", err)
	}

	// Seed one active exam (5 MB) so total/active/storage_mb are non-trivial
	// and the aggregate assertions below actually exercise the queries.
	if _, err := pool.Exec(ctx, `INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, created_by)
		VALUES ('IT Stats Exam', '/tmp/it-stats.pdf', 5*1024*1024, 'ITSTATS1', 'ITSTATS1', 'active', $1)`, su.ID); err != nil {
		t.Fatalf("seed exam: %v", err)
	}

	storageDir, err := os.MkdirTemp("", "examvan-stats-it")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	srv := httptest.NewServer(newSaasSettingsTestRouter(pool, storageDir))
	defer srv.Close()

	// 1) Without a session the endpoint is auth-gated: an API-style request
	// (Accept: application/json) gets a 401 JSON, not a redirect.
	anonReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/admin/api/stats", nil)
	anonReq.Header.Set("Accept", "application/json")
	anonResp, err := http.DefaultClient.Do(anonReq)
	if err != nil {
		t.Fatalf("GET stats unauthenticated: %v", err)
	}
	defer anonResp.Body.Close()
	if anonResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stats without session: status=%d, want 401", anonResp.StatusCode)
	}

	// 2) As superadmin: 200 with success=true, server_disk_free_mb > 0, and
	// the aggregates reflecting the seeded exam.
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(su.ID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("test login: status=%v err=%v", resp, err)
	}

	resp, err := client.Get(srv.URL + "/admin/api/stats")
	if err != nil {
		t.Fatalf("GET stats: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET stats: status=%d", resp.StatusCode)
	}
	var out struct {
		Success bool `json:"success"`
		Data    struct {
			Total            int     `json:"total"`
			Active           int     `json:"active"`
			Inactive         int     `json:"inactive"`
			StorageMB        float64 `json:"storage_mb"`
			ServerDiskFreeMB float64 `json:"server_disk_free_mb"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode stats response: %v", err)
	}
	if !out.Success {
		t.Fatalf("stats success=false")
	}
	if out.Data.ServerDiskFreeMB <= 0 {
		t.Fatalf("server_disk_free_mb = %v, want > 0 (temp storage dir on a real disk)", out.Data.ServerDiskFreeMB)
	}
	if out.Data.Total != 1 || out.Data.Active != 1 || out.Data.Inactive != 0 {
		t.Fatalf("stats aggregates = total:%d active:%d inactive:%d, want 1/1/0", out.Data.Total, out.Data.Active, out.Data.Inactive)
	}
	if out.Data.StorageMB != 5 {
		t.Fatalf("storage_mb = %v, want 5 (the seeded 5 MB exam)", out.Data.StorageMB)
	}

	// 3) server_disk_free_mb agrees with the free-space helper used by the
	// storage quota editors — same statfs, same rounding. A small tolerance
	// (1 MB) absorbs any background write on the shared filesystem between
	// the handler's statfs and this call; the file's disk-cap tests use the
	// same > 0 / bounded style to stay immune to that noise.
	wantFree := roundTo(getFreeDiskSpace(storageDir)/(1024*1024), 2)
	if diff := out.Data.ServerDiskFreeMB - wantFree; diff > 1 || diff < -1 {
		t.Fatalf("server_disk_free_mb = %v, want ≈ %v (getFreeDiskSpace on the same storage dir, ±1 MB)", out.Data.ServerDiskFreeMB, wantFree)
	}
}

// TestStatsScopeByRole verifies the per-role scoping of GET /admin/api/stats:
// a superadmin sees every exam, an operator only the exams created by accounts
// of their own instansi, a plain guru only their own (created or delegated)
// exams, and a pengawas-only account only the exams assigned via exam_pengawas.
// server_disk_free_mb (the dashboard's "Sisa Disk Server" indicator) is
// reported ONLY to the superadmin — every other role gets 0/absent, so the
// server disk capacity is never exposed to non-super roles.
func TestStatsScopeByRole(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()

	mkUser := func(username, instansi string, roles ...string) models.AdminUser {
		t.Helper()
		u, err := models.CreateUser(ctx, pool, &models.AdminUser{
			Username: username, Name: username,
			PasswordHash: "x", Status: models.UserStatusActive,
			Instansi: instansi,
			Role:     models.SerializeRoles(roles),
			MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
			MaxStorageSize: 50 * 1024 * 1024, Package: "free",
		})
		if err != nil {
			t.Fatalf("create %s: %v", username, err)
		}
		return *u
	}

	su := mkUser("it_scope_su", "", models.RoleSuperAdmin)
	op := mkUser("it_scope_op", "SMA Test", models.RoleGuru, models.RoleOperator)
	guruA := mkUser("it_scope_guru_a", "SMA Test", models.RoleGuru)
	guruB := mkUser("it_scope_guru_b", "SMP Test", models.RoleGuru)
	pw := mkUser("it_scope_pw", "SMA Test", models.RolePengawas)
	// guru+pengawas combined: exercises the handler's 4th branch (union of
	// created/delegated/assigned) — sees E2 as pengawas AND E5 as creator.
	gp := mkUser("it_scope_gp", "SMA Test", models.RoleGuru, models.RolePengawas)

	// Seed exams with distinct ownership so each role's window is provably
	// different:
	//   E1 guruA (5 MB, active)      — own for guruA, SMA instansi for op, all for su
	//   E2 guruB (3 MB, active)      — own for guruB, SMP instansi (hidden from op);
	//                                  also assigned to gp as pengawas
	//   E3 guruA → delegated guruB (2 MB, inactive) — own for guruA, delegated for guruB
	//   E4 guruA (4 MB, active) + pengawas pw        — the pengawas-only window
	//   E5 gp (6 MB, active)         — own for gp (created_by), SMA instansi for op
	seedExam := func(name, token string, sizeMB int, createdBy int, delegatedTo *int, status string) int {
		t.Helper()
		var id int
		if err := pool.QueryRow(ctx, `INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, created_by, delegated_to)
			VALUES ($1, $2, $3, $4, $4, $5, $6, $7) RETURNING id`,
			name, "/tmp/"+token+".pdf", sizeMB*1024*1024, token, status, createdBy, delegatedTo).Scan(&id); err != nil {
			t.Fatalf("seed exam %s: %v", token, err)
		}
		return id
	}

	_ = seedExam("E1", "ITSCOPE1", 5, guruA.ID, nil, "active")
	e2 := seedExam("E2", "ITSCOPE2", 3, guruB.ID, nil, "active")
	_ = seedExam("E3", "ITSCOPE3", 2, guruA.ID, &guruB.ID, "inactive")
	e4 := seedExam("E4", "ITSCOPE4", 4, guruA.ID, nil, "active")
	_ = seedExam("E5", "ITSCOPE5", 6, gp.ID, nil, "active")
	if _, err := pool.Exec(ctx, `INSERT INTO exam_pengawas (exam_id, user_id) VALUES ($1, $2), ($3, $4)`, e4, pw.ID, e2, gp.ID); err != nil {
		t.Fatalf("assign pengawas: %v", err)
	}

	storageDir, err := os.MkdirTemp("", "examvan-scope-it")
	if err != nil {
		t.Fatalf("make temp storage dir: %v", err)
	}
	defer os.RemoveAll(storageDir)

	srv := httptest.NewServer(newSaasSettingsTestRouter(pool, storageDir))
	defer srv.Close()

	type statsResp struct {
		Success bool `json:"success"`
		Data    struct {
			Total            int     `json:"total"`
			Active           int     `json:"active"`
			Inactive         int     `json:"inactive"`
			StorageMB        float64 `json:"storage_mb"`
			ServerDiskFreeMB float64 `json:"server_disk_free_mb"`
		} `json:"data"`
	}

	// fetchStats logs in as the given user and returns the parsed stats
	// payload, failing on any auth/status/decode error (every role must be
	// able to reach the endpoint — Stats is NOT super-admin-gated). The
	// server_disk_free_mb assertion is per-case (superadmin only).
	fetchStats := func(as models.AdminUser) statsResp {
		t.Helper()
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar}
		if resp, err := client.Post(srv.URL+"/test/login/"+strconv.Itoa(as.ID), "application/json", nil); err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("login as %s: status=%v err=%v", as.Username, resp, err)
		}
		resp, err := client.Get(srv.URL + "/admin/api/stats")
		if err != nil {
			t.Fatalf("GET stats as %s: %v", as.Username, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET stats as %s: status=%d", as.Username, resp.StatusCode)
		}
		var out statsResp
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode stats as %s: %v", as.Username, err)
		}
		if !out.Success {
			t.Fatalf("stats as %s: success=false", as.Username)
		}
		return out
	}

	cases := []struct {
		label         string
		user          models.AdminUser
		wantTotal     int
		wantActive    int
		wantStorageMB float64
		wantDiskFree  bool // server_disk_free_mb > 0 only for superadmin
	}{
		{"superadmin sees all 5 exams", su, 5, 4, 20, true},
		{"operator scoped to own instansi (SMA)", op, 4, 3, 17, false},
		{"guruA sees own exams (E1, E3, E4)", guruA, 3, 2, 11, false},
		{"guruB sees own + delegated (E2, E3)", guruB, 2, 1, 5, false},
		{"pengawas sees assigned exam only (E4)", pw, 1, 1, 4, false},
		{"guru+pengawas sees union (E2 assigned, E5 created)", gp, 2, 2, 9, false},
	}
	for _, tc := range cases {
		got := fetchStats(tc.user)
		if got.Data.Total != tc.wantTotal || got.Data.Active != tc.wantActive || got.Data.Inactive != tc.wantTotal-tc.wantActive {
			t.Errorf("%s: total=%d active=%d inactive=%d, want %d/%d/%d",
				tc.label, got.Data.Total, got.Data.Active, got.Data.Inactive,
				tc.wantTotal, tc.wantActive, tc.wantTotal-tc.wantActive)
		}
		if got.Data.StorageMB != tc.wantStorageMB {
			t.Errorf("%s: storage_mb=%v, want %v", tc.label, got.Data.StorageMB, tc.wantStorageMB)
		}
		if tc.wantDiskFree {
			if got.Data.ServerDiskFreeMB <= 0 {
				t.Errorf("%s: server_disk_free_mb=%v, want > 0 (superadmin)", tc.label, got.Data.ServerDiskFreeMB)
			}
		} else if got.Data.ServerDiskFreeMB != 0 {
			t.Errorf("%s: server_disk_free_mb=%v, want 0/absent (only superadmin sees the server disk free space)", tc.label, got.Data.ServerDiskFreeMB)
		}
	}
}
