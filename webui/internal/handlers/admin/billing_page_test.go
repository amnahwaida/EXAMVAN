package admin

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Billing page rendering test: the voucher-claim UI must NOT be rendered for
// operator-created sub-accounts.
// ---------------------------------------------------------------------------

// billingTemplates are exactly the templates billing.html pulls in (its own
// body plus the head/nav/svg partials). Loading precisely this set keeps the
// test focused on the real page without parsing every template in the repo
// (which would require the server's full funcMap).
var billingTemplates = []string{
	"admin/billing.html",
	"admin/partials/head.html",
	"admin/partials/nav.html",
	"admin/partials/svg-symbols.html",
}

// loadBillingTemplatesForTest registers the real billing templates on the gin
// engine with the minimal funcMap subset those templates actually use
// (`dict`, `default`, `displayRole` — verified against the template sources),
// mirroring the server's own loading (cmd/server/main.go:
// template.New("").Funcs(funcMap) + per-file Parse). The templates dir is
// resolved relative to the test package dir (webui/internal/handlers/admin →
// ../../../templates), which is where `go test` runs from.
func loadBillingTemplatesForTest(t *testing.T, r *gin.Engine) {
	t.Helper()
	templatesDir := "templates"
	if _, err := os.Stat(templatesDir); err != nil {
		templatesDir = filepath.Join("..", "..", "..", "templates")
	}
	if _, err := os.Stat(templatesDir); err != nil {
		t.Fatalf("resolve templates dir: %v", err)
	}

	tmpl := template.New("").Funcs(template.FuncMap{
		"dict": func(values ...interface{}) map[string]interface{} {
			m := make(map[string]interface{}, len(values)/2)
			for i := 0; i+1 < len(values); i += 2 {
				m[fmt.Sprintf("%v", values[i])] = values[i+1]
			}
			return m
		},
		"default": func(d, v interface{}) interface{} {
			if v == nil || v == "" {
				return d
			}
			return v
		},
		"displayRole": models.DisplayRoles,
		"contains":    strings.Contains,
	})
	for _, name := range billingTemplates {
		data, err := os.ReadFile(filepath.Join(templatesDir, name))
		if err != nil {
			t.Fatalf("read template %s: %v", name, err)
		}
		if _, err := tmpl.New(name).Parse(string(data)); err != nil {
			t.Fatalf("parse template %s: %v", name, err)
		}
	}
	r.SetHTMLTemplate(tmpl)
}

// newBillingPageTestRouter mirrors the production wiring for the billing page
// (AuthRequired → BillingPage, the one admin page a feature-locked account may
// still use) plus the /test/login/:id session seam shared by the other
// integration tests. Unlike newVoucherTestRouter — which stubs /admin/billing
// with a JSON flash probe — this router serves the REAL page with the REAL
// templates, so the rendered HTML can be asserted.
func newBillingPageTestRouter(t *testing.T, pool *pgxpool.Pool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-secret-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) { c.Set("db", pool) })

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

	// Real billing page behind AuthRequired, exactly like production.
	adminPages := r.Group("/admin", middleware.AuthRequired())
	adminPages.GET("/billing", BillingPage())

	loadBillingTemplatesForTest(t, r)
	return r
}

// getBillingPage fetches /admin/billing with the client's session and returns
// the HTTP status and the rendered HTML body.
func getBillingPage(t *testing.T, client *http.Client, srv *httptest.Server) (int, string) {
	t.Helper()
	resp, err := client.Get(srv.URL + "/admin/billing")
	if err != nil {
		t.Fatalf("GET /admin/billing: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /admin/billing body: %v", err)
	}
	return resp.StatusCode, string(body)
}

// TestBillingPageHidesRedeemFormForSubAccount locks in the UI half of the
// sub-account voucher policy: when the account was CREATED BY an operator
// (admin_users.operator_created = true), the rendered /admin/billing page must
// NOT contain the voucher-claim form (input #voucherCodeInput, button
// #btnRedeemVoucher) nor the "Paket yang Sudah Anda Klaim" list — instead it
// shows the explanatory sub-account notice card. A directly-created (non-sub)
// account still gets the full claim UI. The page is rendered through the REAL
// BillingPage handler with the REAL templates, so the test breaks the moment
// the template conditional regresses.
func TestBillingPageHidesRedeemFormForSubAccount(t *testing.T) {
	pool := setupVoucherITDB(t)
	ctx := context.Background()

	createSchoolVoucher(t, pool)
	op := createOperatorUser(t, pool, "op-bill", "SMK Billing Test", "pass-op-bill")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	if !models.HasRole(mustGetUser(t, pool, "op-bill").Role, models.RoleOperator) {
		t.Fatalf("op must hold the operator role after redeeming the school voucher")
	}

	// Sub-account created by the operator through the real CreateUser handler.
	if status, resp := tc.createUser(t, "sub1"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub1: status=%d resp=%+v", status, resp)
	}
	sub := mustGetUser(t, pool, "sub1")
	if !sub.OperatorCreated {
		t.Fatalf("fixture: sub1 must be operator_created")
	}

	// Direct (superadmin-created style) account: still gets the claim UI.
	direct, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: "direct-bill", Name: "Direct Bill",
		PasswordHash: "pass-direct-bill", Status: models.UserStatusActive,
		Instansi: "personal",
		Role:     models.SerializeRoles([]string{models.RoleGuru}),
		MaxExams: 3, MaxPDFSize: 1048576, MaxConcurrentExams: 2,
		MaxStorageSize: 50 * 1024 * 1024, Package: "free",
	})
	if err != nil {
		t.Fatalf("create direct account: %v", err)
	}
	if direct.OperatorCreated {
		t.Fatalf("fixture: direct account must NOT be operator_created")
	}

	br := newBillingPageTestRouter(t, pool)
	srv := httptest.NewServer(br)
	t.Cleanup(srv.Close)

	// --- Sub-account: the claim form must NOT be rendered -------------------
	// NOTE on markers: the page embeds a <script> block that is ALWAYS present
	// and whose strings reference the form (e.g. 'voucherCodeInput' and the
	// empty-state text "Punya Kode Voucher?"). So the absence assertions use
	// markers that exist ONLY in the template HTML of the rendered card
	// (id="btnRedeemVoucher", placeholder="EV-PROMO-2026", the claimed-packages
	// section title) — never in the static JS.
	subJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	subClient := &http.Client{Jar: subJar}
	if _, resp := postForm(t, subClient, srv, "/test/login/"+strconv.Itoa(sub.ID), nil); !resp.Success {
		t.Fatalf("login as sub1: %+v", resp)
	}
	status, body := getBillingPage(t, subClient, srv)
	if status != http.StatusOK {
		t.Fatalf("sub billing page: status=%d, want 200", status)
	}
	for _, marker := range []string{"id=\"btnRedeemVoucher\"", "placeholder=\"EV-PROMO-2026\"", "Paket yang Sudah Anda Klaim"} {
		if strings.Contains(body, marker) {
			t.Errorf("sub billing page must NOT render %q, but it was found in the HTML", marker)
		}
	}
	if !strings.Contains(body, "Akun Sub (Dibuat Operator)") {
		t.Error("sub billing page must show the sub-account notice card (\"Akun Sub (Dibuat Operator)\")")
	}

	// --- Direct account: the claim form IS rendered -------------------------
	directJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	directClient := &http.Client{Jar: directJar}
	if _, resp := postForm(t, directClient, srv, "/test/login/"+strconv.Itoa(direct.ID), nil); !resp.Success {
		t.Fatalf("login as direct-bill: %+v", resp)
	}
	status, body = getBillingPage(t, directClient, srv)
	if status != http.StatusOK {
		t.Fatalf("direct billing page: status=%d, want 200", status)
	}
	// Presence markers are the two strong ones (form element id + input
	// placeholder), both unique to the rendered card HTML.
	for _, marker := range []string{"id=\"btnRedeemVoucher\"", "placeholder=\"EV-PROMO-2026\""} {
		if !strings.Contains(body, marker) {
			t.Errorf("direct billing page must render %q, but it is missing from the HTML", marker)
		}
	}
	if strings.Contains(body, "Akun Sub (Dibuat Operator)") {
		t.Error("direct billing page must NOT show the sub-account notice card")
	}
}

// TestBillingPageShowsSchoolPoolQuotaForSubAccount locks in the quota-display
// half of the sub-account policy: an operator-created account's REAL limits
// come from the school pool (the instansi's operator's active redemption
// snapshot — the same source that gates exam/PDF/storage/concurrent at upload
// time), NOT from its own admin_users columns, which only hold the forced
// 'free' defaults (CreateUser forces package='free' and default quotas). The
// rendered /admin/billing page must therefore show the school package values
// (PDF 50 MB, storage 500 MB, concurrent 3 from the IT-SEKOLAH voucher) and
// the school package label instead of "free" / 1 MB / 2 / 50 MB. Rendered
// through the REAL BillingPage handler with the REAL templates, so the test
// breaks the moment the display override regresses.
func TestBillingPageShowsSchoolPoolQuotaForSubAccount(t *testing.T) {
	pool := setupVoucherITDB(t)

	createSchoolVoucher(t, pool) // sekolah-test: 3 exams, 50MB PDF, 3 concurrent, 500MB storage
	op := createOperatorUser(t, pool, "op-bill-quota", "SMK Quota Test", "pass-op-bill-quota")
	tc := newVoucherTestClient(t, pool)
	tc.login(t, op.ID)
	tc.redeem(t, "IT-SEKOLAH")
	if !models.HasRole(mustGetUser(t, pool, "op-bill-quota").Role, models.RoleOperator) {
		t.Fatalf("op must hold the operator role after redeeming the school voucher")
	}

	// Sub-account created by the operator through the real CreateUser handler:
	// its row carries the forced 'free' package and default quotas.
	if status, resp := tc.createUser(t, "sub-quota"); status != http.StatusOK || !resp.Success {
		t.Fatalf("create sub-quota: status=%d resp=%+v", status, resp)
	}
	sub := mustGetUser(t, pool, "sub-quota")
	if !sub.OperatorCreated {
		t.Fatalf("fixture: sub-quota must be operator_created")
	}
	if sub.Package != "free" || sub.MaxPDFSize != 1048576 || sub.MaxStorageSize != 50*1024*1024 || sub.MaxConcurrentExams != 2 {
		t.Fatalf("fixture: sub-quota row must hold the forced free defaults, got package=%q pdf=%d storage=%d concurrent=%d",
			sub.Package, sub.MaxPDFSize, sub.MaxStorageSize, sub.MaxConcurrentExams)
	}

	br := newBillingPageTestRouter(t, pool)
	srv := httptest.NewServer(br)
	t.Cleanup(srv.Close)

	subJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	subClient := &http.Client{Jar: subJar}
	if _, resp := postForm(t, subClient, srv, "/test/login/"+strconv.Itoa(sub.ID), nil); !resp.Success {
		t.Fatalf("login as sub-quota: %+v", resp)
	}
	status, body := getBillingPage(t, subClient, srv)
	if status != http.StatusOK {
		t.Fatalf("sub billing page: status=%d, want 200", status)
	}

	// The quota cards show the SCHOOL POOL values (from the operator's active
	// redemption), not the sub-account's forced free defaults: PDF 50 MB (not
	// 1 MB), storage 500 MB (not 50 MB), concurrent 3 (not 2).
	for _, marker := range []string{">50 MB<", ">500 MB<", ">3<"} {
		if !strings.Contains(body, marker) {
			t.Errorf("sub billing page must render the school pool quota %q, but it is missing", marker)
		}
	}
	for _, marker := range []string{">1 MB<"} {
		if strings.Contains(body, marker) {
			t.Errorf("sub billing page must NOT render the per-account default quota %q, but it was found", marker)
		}
	}

	// "Paket Saat Ini" shows the school package label (sekolah-test →
	// "Sekolah-Test" via packageDisplayName), never the forced 'free' row.
	if !strings.Contains(body, "id=\"currentPkgName\">Sekolah-Test<") {
		t.Errorf("sub billing page must render the school package label in currentPkgName, got:\n%s", body)
	}
	if strings.Contains(body, "id=\"currentPkgName\">free<") {
		t.Error("sub billing page must NOT render the forced 'free' package label")
	}

	// The sub-account notice card is still present (claim UI stays hidden).
	if !strings.Contains(body, "Akun Sub (Dibuat Operator)") {
		t.Error("sub billing page must keep the sub-account notice card")
	}
}
