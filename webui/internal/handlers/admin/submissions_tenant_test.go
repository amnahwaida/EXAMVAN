package admin

// ---------------------------------------------------------------------------
// Tenant-isolation tests for the results path (operator role).
// ---------------------------------------------------------------------------
//
// TDD RED phase: these tests pin the cross-tenant leaks where the results
// path scopes by free-text `instansi` NAME while every other gate on the same
// exam is canonical (instansi_id-first):
//
//   - buildScopeConditions / fetchFilterExams / exportAllXLSX resolve the
//     operator's tenant with getInstansiForOperator (name only) and compare
//     with LOWER(instansi) = LOWER(name) plus a byte-exact != "personal"
//     bucket test.
//
// Because instansi.name has no UNIQUE constraint, two schools may share a
// name with different instansi_ids — and a 'Personal'-cased bucket value
// slips past the byte-exact test straight into the tenant branch.
//
// The twin-tenant fixture below models exactly that: two `instansi` rows
// with the SAME name but different ids.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// twinTenantFixture carries two tenants sharing one school NAME but holding
// different canonical instansi_ids — the state the dropped UNIQUE constraint
// on instansi.name makes legal.
type twinTenantFixture struct {
	OperatorAID int
	TeacherBID  int
	ExamAID     int
	ExamBID     int
	StudentA    string
	StudentB    string
}

func createTwinTenantFixture(t *testing.T, pool *pgxpool.Pool) twinTenantFixture {
	t.Helper()
	ctx := context.Background()
	uniq := fmt.Sprintf("%d", time.Now().UnixNano()%1000000)

	var instA, instB int
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Kembar', $1) RETURNING id`,
		"TWINA"+uniq).Scan(&instA); err != nil {
		t.Fatalf("seed instansi A: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Kembar', $1) RETURNING id`,
		"TWINB"+uniq).Scan(&instB); err != nil {
		t.Fatalf("seed instansi B: %v", err)
	}
	if instA == instB {
		t.Fatalf("twin instansi rows must have different ids, got %d twice", instA)
	}

	mk := func(username, name string, instID int, roles ...string) int {
		u, err := models.CreateUser(ctx, pool, &models.AdminUser{
			Username:     username,
			Name:         name,
			Instansi:     "SMA Kembar",
			InstansiID:   &instID,
			PasswordHash: "pass-" + username,
			Status:       models.UserStatusActive,
			Role:         models.SerializeRoles(roles),
		})
		if err != nil {
			t.Fatalf("create user %s: %v", username, err)
		}
		return u.ID
	}
	opA := mk("tw-op-a"+uniq, "Operator Twin A", instA, models.RoleOperator)
	guruB := mk("tw-guru-b"+uniq, "Guru Twin B", instB, models.RoleGuru)

	mkExam := func(ownerID int, examName, tokenSuffix, student string) int {
		token := fmt.Sprintf("TW%s%s", tokenSuffix, uniq)
		var examID int
		if err := pool.QueryRow(ctx, `
			INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
			VALUES ($1, 'tw.pdf', 1024, $2, $2, 'active', 'medium', $3)
			RETURNING id`, examName, token, ownerID).Scan(&examID); err != nil {
			t.Fatalf("seed exam %s: %v", examName, err)
		}
		startedAt := time.Now().Format(time.RFC3339)
		if _, err := pool.Exec(ctx, `
			INSERT INTO submissions (exam_id, mac_address, student_name, exam_number, student_class, answers_json, score, start_time, created_at)
			VALUES ($1, 'AA:BB:CC:DD:EE:FF', $2, '01', 'XII A', '{"1":"A"}', 80, $3, $4)`,
			examID, student, startedAt, time.Now()); err != nil {
			t.Fatalf("seed submission %s: %v", student, err)
		}
		return examID
	}
	examA := mkExam(opA, "Ujian Twin A", "A", "Siswa Twin A")
	examB := mkExam(guruB, "Ujian Twin B", "B", "Siswa Twin B")

	return twinTenantFixture{
		OperatorAID: opA,
		TeacherBID:  guruB,
		ExamAID:     examA,
		ExamBID:     examB,
		StudentA:    "Siswa Twin A",
		StudentB:    "Siswa Twin B",
	}
}

// newRepeatGrantsRouter mirrors the production route stack for
// GET /admin/api/pengawas/exams/:exam_id/repeat-grants.
func newRepeatGrantsRouter(t *testing.T, pool *pgxpool.Pool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-rgnt-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
		c.Set("cfg", &config.Config{Version: "test", StoragePath: t.TempDir()})
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
	adminAPI := r.Group("/admin/api", middleware.AuthRequired())
	lockedAPI := adminAPI.Group("", middleware.FeatureLockRequired())
	lockedAPI.GET("/pengawas/exams/:exam_id/repeat-grants", ListStudentRepeats())
	return r
}

// repeatGrantsClient is one logged-in browser hitting the repeat-grants list.
type repeatGrantsClient struct {
	t      *testing.T
	base   string
	client *http.Client
}

func newRepeatGrantsClient(t *testing.T, base string) *repeatGrantsClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &repeatGrantsClient{t: t, base: base, client: &http.Client{Jar: jar}}
}

func (rc *repeatGrantsClient) login(userID int) {
	rc.t.Helper()
	resp, err := rc.client.Post(rc.base+"/test/login/"+strconv.Itoa(userID), "", nil)
	if err != nil {
		rc.t.Fatalf("login %d: %v", userID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		rc.t.Fatalf("login %d: status=%d", userID, resp.StatusCode)
	}
}

func (rc *repeatGrantsClient) list(examID int) (int, string) {
	rc.t.Helper()
	req, err := http.NewRequest(http.MethodGet,
		rc.base+"/admin/api/pengawas/exams/"+strconv.Itoa(examID)+"/repeat-grants", nil)
	if err != nil {
		rc.t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := rc.client.Do(req)
	if err != nil {
		rc.t.Fatalf("GET repeat-grants: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		rc.t.Fatalf("read repeat-grants body: %v", err)
	}
	return resp.StatusCode, string(body)
}

// TestListStudentRepeatsRequiresExamAccess: the grants list must be gated by
// the same exam-access predicate as grant/revoke. RED before the fix: any
// authenticated account — including a twin-tenant operator — can enumerate
// another exam's student keys.
func TestListStudentRepeatsRequiresExamAccess(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fix := createTwinTenantFixture(t, pool)
	ctx := context.Background()

	// A live grant on the twin tenant's exam: the concrete PII that leaks.
	if err := models.GrantRepeat(ctx, pool, fix.ExamBID,
		"01|siswa twin b|xii a", strconv.Itoa(fix.TeacherBID)); err != nil {
		t.Fatalf("seed repeat grant: %v", err)
	}

	r := newRepeatGrantsRouter(t, pool)
	srv := httptest.NewServer(r)
	defer srv.Close()

	// Owner (teacher B): allowed, sees the grant.
	owner := newRepeatGrantsClient(t, srv.URL)
	owner.login(fix.TeacherBID)
	if status, body := owner.list(fix.ExamBID); status != http.StatusOK {
		t.Errorf("owner list: status=%d, want 200", status)
	} else if !strings.Contains(body, "siswa twin b") {
		t.Errorf("owner list must contain the granted student key, got: %.200s", body)
	}

	// Twin-tenant operator: must be denied.
	xo := newRepeatGrantsClient(t, srv.URL)
	xo.login(fix.OperatorAID)
	if status, body := xo.list(fix.ExamBID); status != http.StatusForbidden {
		t.Errorf("twin-tenant list: status=%d, want 403", status)
	} else if strings.Contains(body, "siswa twin b") {
		t.Errorf("LEAK: twin tenant enumerated the grant list: %.200s", body)
	}

	// Unknown exam: 404, not an oracle-shaped 403/500.
	if status, _ := owner.list(2147483647); status != http.StatusNotFound {
		t.Errorf("unknown exam list: status=%d, want 404", status)
	}
}

// newUsersScopeRouter mirrors the production route stack for
// GET /admin/api/users and GET /admin/api/users/:user_id.
func newUsersScopeRouter(t *testing.T, pool *pgxpool.Pool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-usrs-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
		c.Set("cfg", &config.Config{Version: "test", StoragePath: t.TempDir()})
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
	adminAPI := r.Group("/admin/api", middleware.AuthRequired())
	lockedAPI := adminAPI.Group("", middleware.FeatureLockRequired())
	mgmt := lockedAPI.Group("", middleware.AdminManagementRequired())
	mgmt.GET("/users", ListUsers())
	mgmt.GET("/users/:user_id", GetUser())
	return r
}

// usersScopeClient is one logged-in browser hitting the user endpoints.
type usersScopeClient struct {
	t      *testing.T
	base   string
	client *http.Client
}

func newUsersScopeClient(t *testing.T, base string) *usersScopeClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &usersScopeClient{t: t, base: base, client: &http.Client{Jar: jar}}
}

func (uc *usersScopeClient) login(userID int) {
	uc.t.Helper()
	resp, err := uc.client.Post(uc.base+"/test/login/"+strconv.Itoa(userID), "", nil)
	if err != nil {
		uc.t.Fatalf("login %d: %v", userID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		uc.t.Fatalf("login %d: status=%d", userID, resp.StatusCode)
	}
}

func (uc *usersScopeClient) get(path string) (int, string) {
	uc.t.Helper()
	req, err := http.NewRequest(http.MethodGet, uc.base+path, nil)
	if err != nil {
		uc.t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := uc.client.Do(req)
	if err != nil {
		uc.t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		uc.t.Fatalf("read %s body: %v", path, err)
	}
	return resp.StatusCode, string(body)
}

// personalBucketFixture seeds a personal-bucket operator, its own
// sub-account, and an unrelated personal account (another school-less guru).
type personalBucketFixture struct {
	OperatorID int
	OwnSubID   int
	OtherID    int
}

func createPersonalBucketFixture(t *testing.T, pool *pgxpool.Pool) personalBucketFixture {
	t.Helper()
	ctx := context.Background()
	uniq := fmt.Sprintf("%d", time.Now().UnixNano()%1000000)

	mk := func(username, name, instansi string, roles ...string) int {
		u, err := models.CreateUser(ctx, pool, &models.AdminUser{
			Username:     username,
			Name:         name,
			Instansi:     instansi,
			PasswordHash: "pass-" + username,
			Status:       models.UserStatusActive,
			Role:         models.SerializeRoles(roles),
		})
		if err != nil {
			t.Fatalf("create user %s: %v", username, err)
		}
		return u.ID
	}
	opID := mk("pb-op"+uniq, "PB Operator", "personal", models.RoleOperator)
	otherID := mk("pb-other"+uniq, "PB Other", "personal", models.RoleGuru)

	// The operator's own sub-account (created_by = operator), still carrying
	// the transitional 'personal' label.
	sub, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username:     "pb-sub" + uniq,
		Name:         "PB Sub",
		Instansi:     "personal",
		PasswordHash: "pass-pb-sub" + uniq,
		Status:       models.UserStatusActive,
		Role:         models.SerializeRoles([]string{models.RoleGuru}),
		CreatedBy:    &opID,
	})
	if err != nil {
		t.Fatalf("create own sub: %v", err)
	}
	return personalBucketFixture{OperatorID: opID, OwnSubID: sub.ID, OtherID: otherID}
}

// TestPersonalOperatorUserListIsBucketed: a personal-bucket operator must NOT
// receive the whole platform's personal bucket from GET /users. The bucket
// is not a tenant — fail closed, exactly like an empty instansi. RED before
// the fix: 200 with every personal account listed.
func TestPersonalOperatorUserListIsBucketed(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fix := createPersonalBucketFixture(t, pool)

	r := newUsersScopeRouter(t, pool)
	srv := httptest.NewServer(r)
	defer srv.Close()

	oc := newUsersScopeClient(t, srv.URL)
	oc.login(fix.OperatorID)
	status, body := oc.get("/admin/api/users")
	if status != http.StatusInternalServerError {
		t.Errorf("personal-bucket operator list: status=%d, want 500 (fail closed)", status)
	}
	if strings.Contains(body, "pb-other") {
		t.Errorf("LEAK: personal-bucket operator lists unrelated personal account: %.300s", body)
	}
}

// TestPersonalOperatorCannotReadForeignPersonalUser: same contract for the
// single-account detail endpoint. RED before the fix: 200 with the foreign
// account's username/email/whatsapp.
func TestPersonalOperatorCannotReadForeignPersonalUser(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fix := createPersonalBucketFixture(t, pool)

	r := newUsersScopeRouter(t, pool)
	srv := httptest.NewServer(r)
	defer srv.Close()

	oc := newUsersScopeClient(t, srv.URL)
	oc.login(fix.OperatorID)
	status, body := oc.get("/admin/api/users/" + strconv.Itoa(fix.OtherID))
	if status != http.StatusInternalServerError {
		t.Errorf("personal-bucket operator detail: status=%d, want 500 (fail closed)", status)
	}
	if strings.Contains(body, "pb-other") {
		t.Errorf("LEAK: personal-bucket operator reads unrelated personal account: %.300s", body)
	}
}

// TestPersonalCaseVariantCannotAccessExam pins the bucket guard inside
// InstansiMatchSelfSQL: an id-less operator whose instansi reads 'Personal'
// must NOT satisfy the same-tenant predicate against another id-less
// 'personal' account. RED before the fix: the byte-exact
// `NOT IN ('', 'personal')` passes 'Personal', LOWER() then matches, and the
// operator can export the foreign exam (200). Exercised through the
// specific-exam export gate (UserCanAccessExam, canonical).
func TestPersonalCaseVariantCannotAccessExam(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()
	uniq := fmt.Sprintf("%d", time.Now().UnixNano()%1000000)

	mk := func(username, name, instansi string, roles ...string) int {
		u, err := models.CreateUser(ctx, pool, &models.AdminUser{
			Username:     username,
			Name:         name,
			Instansi:     instansi,
			PasswordHash: "pass-" + username,
			Status:       models.UserStatusActive,
			Role:         models.SerializeRoles(roles),
		})
		if err != nil {
			t.Fatalf("create user %s: %v", username, err)
		}
		return u.ID
	}
	// Id-less legacy rows — the boot backfill cannot byte-match them, so no
	// instansi_id is ever assigned.
	opID := mk("pc-op"+uniq, "PC Operator", "Personal", models.RoleOperator)
	guruID := mk("pc-guru"+uniq, "PC Guru", "personal", models.RoleGuru)

	token := fmt.Sprintf("PC%s", uniq)
	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
		VALUES ('Ujian PC', 'pc.pdf', 1024, $1, $1, 'active', 'medium', $2)
		RETURNING id`, token, guruID).Scan(&examID); err != nil {
		t.Fatalf("seed exam: %v", err)
	}
	startedAt := time.Now().Format(time.RFC3339)
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, mac_address, student_name, exam_number, student_class, answers_json, score, start_time, created_at)
		VALUES ($1, 'AA:BB:CC:DD:EE:FF', 'Siswa PC', '01', 'XII A', '{"1":"A"}', 80, $2, $3)`,
		examID, startedAt, time.Now()); err != nil {
		t.Fatalf("seed submission: %v", err)
	}

	r := newSubmissionsExportRouter(pool)
	srv := httptest.NewServer(r)
	defer srv.Close()

	// Sanity: the owning guru can still export its own exam.
	gc := newExportScopeClient(t, srv.URL)
	gc.login(guruID)
	if status, _ := gc.exportExam(examID); status != http.StatusOK {
		t.Fatalf("owner export: status=%d, want 200", status)
	}

	// The 'Personal'-cased operator must be denied.
	oc := newExportScopeClient(t, srv.URL)
	oc.login(opID)
	if status, body := oc.exportExam(examID); status != http.StatusForbidden {
		t.Errorf("'Personal'-cased operator export: status=%d, want 403", status)
	} else if strings.Contains(string(body), "Siswa PC") {
		t.Errorf("LEAK: 'Personal'-cased operator exported the foreign exam: %.200s", body)
	}
}

// TestAdminPagesAreNotCached pins that server-rendered admin pages (which embed
// their JavaScript inline) must never be served cacheable. A cached copy keeps
// running the PREVIOUS inline script after a deploy — so a supervisor who
// opened the page before a fix keeps experiencing the fixed bug, including the
// "Memuat data..." wedge that the watchdog was added to break. The public
// hasil page already sends no-store; admin pages must match.
func TestAdminPagesAreNotCached(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fix := createScopeFixture(t, pool)

	r := newSubmissionsScopeRouter(t, pool)
	srv := httptest.NewServer(r)
	defer srv.Close()

	gc := newScopeClient(t, srv.URL)
	gc.login(fix.GuruID)
	req, err := http.NewRequest(http.MethodGet,
		srv.URL+"/admin/submissions?exam_id="+strconv.Itoa(fix.ExamID), nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Accept", "text/html")
	resp, err := gc.client.Do(req)
	if err != nil {
		t.Fatalf("GET submissions page: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("page status=%d, want 200", resp.StatusCode)
	}
	cc := resp.Header.Get("Cache-Control")
	if !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store — without it browsers keep "+
			"running the previous inline script after a deploy", cc)
	}
}

// TestSubmissionsPageTwinTenantIsolation: an operator must not see another
// tenant's students on the unfiltered Hasil Ujian list, even when both
// tenants share the same school NAME. RED before the canonical-id fix:
// the name-only predicate matches the twin tenant's rows.
func TestSubmissionsPageTwinTenantIsolation(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fix := createTwinTenantFixture(t, pool)

	r := newSubmissionsScopeRouter(t, pool)
	srv := httptest.NewServer(r)
	defer srv.Close()

	oc := newScopeClient(t, srv.URL)
	oc.login(fix.OperatorAID)
	status, body := oc.listAny("")
	if status != http.StatusOK {
		t.Fatalf("operator page: status=%d, want 200", status)
	}
	if !strings.Contains(body, fix.StudentA) {
		t.Errorf("operator must still see own tenant's student %q", fix.StudentA)
	}
	if strings.Contains(body, fix.StudentB) {
		t.Errorf("LEAK: operator sees twin tenant's student %q on Hasil Ujian", fix.StudentB)
	}
}

// TestSubmissionsExportAllTwinTenantIsolation: same contract for the
// all-exams export ("Semua Hasil" sheet). RED before the fix.
func TestSubmissionsExportAllTwinTenantIsolation(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fix := createTwinTenantFixture(t, pool)

	r := newSubmissionsExportRouter(pool)
	srv := httptest.NewServer(r)
	defer srv.Close()

	ec := newExportScopeClient(t, srv.URL)
	ec.login(fix.OperatorAID)
	status, body := ec.exportAll()
	if status != http.StatusOK {
		t.Fatalf("operator export all: status=%d, want 200", status)
	}
	if !exportContainsStudent(t, body, fix.StudentA) {
		t.Errorf("operator export must still contain own tenant's student %q", fix.StudentA)
	}
	if exportContainsStudent(t, body, fix.StudentB) {
		t.Errorf("LEAK: operator export contains twin tenant's student %q", fix.StudentB)
	}
}

// TestSubmissionsDropdownListsOperatorExams: the exam-filter dropdown must
// actually render the operator's exams. RED before the fix: the unqualified
// `created_by` reference is ambiguous (both exams and admin_users expose it),
// so the query always errors and the dropdown is permanently empty.
func TestSubmissionsDropdownListsOperatorExams(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fix := createTwinTenantFixture(t, pool)

	r := newSubmissionsScopeRouter(t, pool)
	srv := httptest.NewServer(r)
	defer srv.Close()

	oc := newScopeClient(t, srv.URL)
	oc.login(fix.OperatorAID)
	status, body := oc.listAny("")
	if status != http.StatusOK {
		t.Fatalf("operator page: status=%d, want 200", status)
	}
	ownOption := fmt.Sprintf(`<option value="%d"`, fix.ExamAID)
	if !strings.Contains(body, ownOption) {
		t.Errorf("dropdown must list the operator's own exam (want %s)", ownOption)
	}
	otherOption := fmt.Sprintf(`<option value="%d"`, fix.ExamBID)
	if strings.Contains(body, otherOption) {
		t.Errorf("LEAK: dropdown lists the twin tenant's exam (want no %s)", otherOption)
	}
}

// TestPersonalCaseVariantOperatorIsBucketed: an operator whose instansi reads
// 'Personal' (capital P — e.g. a legacy/padded value the byte-exact
// `!= "personal"` test misses) must fall back to own-created scope, not match
// the whole platform's personal bucket. RED before the fix.
func TestPersonalCaseVariantOperatorIsBucketed(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	ctx := context.Background()
	uniq := fmt.Sprintf("%d", time.Now().UnixNano()%1000000)

	mk := func(username, name, instansi string, roles ...string) int {
		u, err := models.CreateUser(ctx, pool, &models.AdminUser{
			Username:     username,
			Name:         name,
			Instansi:     instansi,
			PasswordHash: "pass-" + username,
			Status:       models.UserStatusActive,
			Role:         models.SerializeRoles(roles),
		})
		if err != nil {
			t.Fatalf("create user %s: %v", username, err)
		}
		return u.ID
	}
	// Id-less legacy rows, exactly what the boot backfill leaves behind for
	// case variants it cannot match byte-exactly.
	opID := mk("tw-op-pers"+uniq, "Operator Personal", "Personal", models.RoleOperator)
	guruID := mk("tw-guru-pers"+uniq, "Guru Personal", "personal", models.RoleGuru)

	token := fmt.Sprintf("TP%s", uniq)
	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
		VALUES ('Ujian Personal', 'tp.pdf', 1024, $1, $1, 'active', 'medium', $2)
		RETURNING id`, token, guruID).Scan(&examID); err != nil {
		t.Fatalf("seed exam: %v", err)
	}
	student := "Siswa Personal Bucket"
	startedAt := time.Now().Format(time.RFC3339)
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, mac_address, student_name, exam_number, student_class, answers_json, score, start_time, created_at)
		VALUES ($1, 'AA:BB:CC:DD:EE:FF', $2, '01', 'XII A', '{"1":"A"}', 80, $3, $4)`,
		examID, student, startedAt, time.Now()); err != nil {
		t.Fatalf("seed submission: %v", err)
	}

	// Page list.
	r := newSubmissionsScopeRouter(t, pool)
	srv := httptest.NewServer(r)
	defer srv.Close()
	oc := newScopeClient(t, srv.URL)
	oc.login(opID)
	if status, body := oc.listAny(""); status != http.StatusOK {
		t.Fatalf("operator page: status=%d, want 200", status)
	} else if strings.Contains(body, student) {
		t.Errorf("LEAK: 'Personal'-cased operator sees the personal bucket's student on Hasil Ujian")
	}

	// All-exams export.
	er := newSubmissionsExportRouter(pool)
	esrv := httptest.NewServer(er)
	defer esrv.Close()
	ec := newExportScopeClient(t, esrv.URL)
	ec.login(opID)
	if status, body := ec.exportAll(); status != http.StatusOK {
		t.Fatalf("operator export all: status=%d, want 200", status)
	} else if exportContainsStudent(t, body, student) {
		t.Errorf("LEAK: 'Personal'-cased operator exports the personal bucket's student")
	}
}

// TestSpecificExamExportTwinTenantDenied locks the already-correct behaviour:
// the specific-exam export gate (UserCanAccessExam, canonical) must keep
// denying the twin tenant. Guards against regressions while the list query
// is being re-scoped.
func TestSpecificExamExportTwinTenantDenied(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fix := createTwinTenantFixture(t, pool)

	r := newSubmissionsExportRouter(pool)
	srv := httptest.NewServer(r)
	defer srv.Close()

	ec := newExportScopeClient(t, srv.URL)
	ec.login(fix.OperatorAID)
	status, _ := ec.exportExam(fix.ExamBID)
	if status != http.StatusForbidden {
		t.Errorf("twin-tenant specific-exam export: status=%d, want 403", status)
	}
	status, body := ec.exportExam(fix.ExamAID)
	if status != http.StatusOK {
		t.Fatalf("own-exam export: status=%d, want 200", status)
	}
	if !exportSheetContainsStudent(t, body, "Rekapitulasi", fix.StudentA) {
		t.Errorf("own-exam export missing own student %q", fix.StudentA)
	}
}
