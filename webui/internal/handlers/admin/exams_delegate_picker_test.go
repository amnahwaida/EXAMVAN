package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Delegate picker tenant scoping (GET /admin/api/exams/:exam_id/delegate-data)
// ---------------------------------------------------------------------------
//
// The guru/pengawas picker feeds the delegation modal; it must list ONLY the
// exam owner's tenant. The original queries matched admin_users by instansi
// NAME (`LOWER(instansi) = LOWER($1)`), so two schools sharing a name leaked
// each other's staff, and a sub-account whose text label drifted dropped out
// of its own school's picker. The canonical query matches by instansi_id (with
// a legacy name fallback), pinned by this test: a same-NAME other school must
// never appear, while a same-id row with a drifted label must.

func newDelegatePickerRouter(pool *pgxpool.Pool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("examvan-it-delegate-0123456789abcdef0123456789abcdef"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, MaxAge: 86400 * 30, SameSite: http.SameSiteLaxMode})
	r.Use(sessions.Sessions("examvan_session", store))
	r.Use(func(c *gin.Context) {
		c.Set("db", pool)
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
	lockedAPI.GET("/exams/:exam_id/delegate-data", DelegateData())
	return r
}

// delegatePickerFixture carries the two tenants under test. Both schools are
// named "SMA Sama" but carry DIFFERENT instansi_id — the exact case a
// name-based picker gets wrong.
type delegatePickerFixture struct {
	OperatorID  int
	GuruOwnerID int
	GuruSameID  int // same tenant (id A), drifted text label
	GuruOtherID int // same NAME, other tenant (id B)
	PengawasAID int // same tenant (id A)
	PengawasBID int // same NAME, other tenant (id B)
	ExamID      int
}

func createDelegatePickerFixture(t *testing.T, pool *pgxpool.Pool) delegatePickerFixture {
	t.Helper()
	ctx := context.Background()

	// FK target: admin_users.instansi_id REFERENCES instansi(id). Two rows with
	// the SAME display name but distinct canonical ids.
	var tenantA, tenantB int
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Sama', 'IT-A') RETURNING id`).Scan(&tenantA); err != nil {
		t.Fatalf("seed instansi A: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO instansi (name, code) VALUES ('SMA Sama', 'IT-B') RETURNING id`).Scan(&tenantB); err != nil {
		t.Fatalf("seed instansi B: %v", err)
	}

	mk := func(username, name, instansi string, instansiID int, roles ...string) int {
		id := instansiID
		u, err := models.CreateUser(ctx, pool, &models.AdminUser{
			Username:     username,
			Name:         name,
			Instansi:     instansi,
			InstansiID:   &id,
			PasswordHash: "pass-" + username,
			Status:       models.UserStatusActive,
			Role:         models.SerializeRoles(roles),
		})
		if err != nil {
			t.Fatalf("create user %s: %v", username, err)
		}
		return u.ID
	}

	operatorID := mk("dp-op", "Operator A", "SMA Sama", tenantA, models.RoleOperator)
	guruOwnerID := mk("dp-guru-owner", "Guru Owner A", "SMA Sama", tenantA, models.RoleGuru)
	guruSameID := mk("dp-guru-same", "Guru Drift", "SMA Drift", tenantA, models.RoleGuru)
	guruOtherID := mk("dp-guru-other", "Guru B", "SMA Sama", tenantB, models.RoleGuru)
	pengawasAID := mk("dp-pengawas-a", "Pengawas A", "SMA Sama", tenantA, models.RolePengawas)
	pengawasBID := mk("dp-pengawas-b", "Pengawas B", "SMA Sama", tenantB, models.RolePengawas)

	token := "DP" + strconv.FormatInt(time.Now().UnixNano()%1000000, 10)
	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status, security_level, created_by)
		VALUES ('Ujian Delegasi', 'dp.pdf', 1024, $1, $1, 'active', 'medium', $2)
		RETURNING id`, token, guruOwnerID).Scan(&examID); err != nil {
		t.Fatalf("seed exam: %v", err)
	}

	return delegatePickerFixture{
		OperatorID:  operatorID,
		GuruOwnerID: guruOwnerID,
		GuruSameID:  guruSameID,
		GuruOtherID: guruOtherID,
		PengawasAID: pengawasAID,
		PengawasBID: pengawasBID,
		ExamID:      examID,
	}
}

type delegatePickerClient struct {
	t      *testing.T
	base   string
	client *http.Client
}

func newDelegatePickerClient(t *testing.T, base string) *delegatePickerClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &delegatePickerClient{t: t, base: base, client: &http.Client{Jar: jar}}
}

func (dc *delegatePickerClient) login(userID int) {
	dc.t.Helper()
	resp, err := dc.client.Post(dc.base+"/test/login/"+strconv.Itoa(userID), "", nil)
	if err != nil {
		dc.t.Fatalf("login %d: %v", userID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		dc.t.Fatalf("login %d: status=%d", userID, resp.StatusCode)
	}
}

type delegatePickerResponse struct {
	Success bool `json:"success"`
	Data    struct {
		AvailableGurus    []struct{ ID int `json:"id"`; Username string `json:"username"` } `json:"available_gurus"`
		AvailablePengawas []struct{ ID int `json:"id"`; Username string `json:"username"` } `json:"available_pengawas"`
	} `json:"data"`
}

func (dc *delegatePickerClient) delegateData(examID int) (int, delegatePickerResponse) {
	dc.t.Helper()
	req, err := http.NewRequest(http.MethodGet,
		dc.base+"/admin/api/exams/"+strconv.Itoa(examID)+"/delegate-data", nil)
	if err != nil {
		dc.t.Fatalf("build request: %v", err)
	}
	resp, err := dc.client.Do(req)
	if err != nil {
		dc.t.Fatalf("GET delegate-data: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		dc.t.Fatalf("read body: %v", err)
	}
	var parsed delegatePickerResponse
	_ = json.Unmarshal(body, &parsed)
	return resp.StatusCode, parsed
}

func pickerHasUsername(list []struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}, username string) bool {
	for _, u := range list {
		if u.Username == username {
			return true
		}
	}
	return false
}

// TestDelegatePickerScopesByCanonicalInstansiID pins the fix: the picker shows
// only the exam owner's tenant, even when another school shares the same
// display name, and it still lists a same-id row whose text label drifted.
func TestDelegatePickerScopesByCanonicalInstansiID(t *testing.T) {
	pool := database.NewPackageTestPool(t, "admin")
	fix := createDelegatePickerFixture(t, pool)

	r := newDelegatePickerRouter(pool)
	srv := httptest.NewServer(r)
	defer srv.Close()

	dc := newDelegatePickerClient(t, srv.URL)
	dc.login(fix.OperatorID)

	status, resp := dc.delegateData(fix.ExamID)
	if status != http.StatusOK {
		t.Fatalf("delegate-data: status=%d, want 200", status)
	}
	if !resp.Success {
		t.Fatalf("delegate-data: success=false, body=%+v", resp)
	}

	// Same tenant (id A) with a drifted label is listed...
	if !pickerHasUsername(resp.Data.AvailableGurus, "dp-guru-same") {
		t.Errorf("same-tenant guru with drifted label missing from available_gurus")
	}
	// ...but the same-NAMED other school (id B) is not.
	if pickerHasUsername(resp.Data.AvailableGurus, "dp-guru-other") {
		t.Errorf("cross-tenant guru leaked into available_gurus (same name, different instansi_id)")
	}
	// The exam creator is excluded from the guru picker.
	if pickerHasUsername(resp.Data.AvailableGurus, "dp-guru-owner") {
		t.Errorf("exam creator must be excluded from available_gurus")
	}

	if !pickerHasUsername(resp.Data.AvailablePengawas, "dp-pengawas-a") {
		t.Errorf("same-tenant pengawas missing from available_pengawas")
	}
	if pickerHasUsername(resp.Data.AvailablePengawas, "dp-pengawas-b") {
		t.Errorf("cross-tenant pengawas leaked into available_pengawas (same name, different instansi_id)")
	}
}
