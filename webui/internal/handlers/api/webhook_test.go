package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/models"
)

// newRegisterStatusRouter mirrors the production wiring of the public
// registration-status endpoint (no auth, "db" injected into the context).
func newRegisterStatusRouter(pool *pgxpool.Pool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("db", pool) })
	r.GET("/register/status", GetRegisterStatus())
	return r
}

// TestGetRegisterStatusHidesExactState locks in the non-oracle contract: the
// public registration-status endpoint only ever reveals whether registration
// COMPLETED ('active'). Every other state — pending_otp, suspended, and even
// non-existent usernames — collapses into the same generic 'inactive' answer,
// so an unauthenticated caller can neither enumerate usernames (existence was
// previously a 404 vs 200 signal) nor learn an account's exact state.
func TestGetRegisterStatusHidesExactState(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	ctx := context.Background()

	for _, u := range []*models.AdminUser{
		{Username: "status-pending", Name: "Pending", PasswordHash: "x", Status: models.UserStatusPendingOTP},
		{Username: "status-suspended", Name: "Suspended", PasswordHash: "x", Status: models.UserStatusSuspended},
		{Username: "status-active", Name: "Active", PasswordHash: "x", Status: models.UserStatusActive},
	} {
		if _, err := models.CreateUser(ctx, pool, u); err != nil {
			t.Fatalf("create user %s: %v", u.Username, err)
		}
	}

	srv := httptest.NewServer(newRegisterStatusRouter(pool))
	defer srv.Close()

	get := func(username string) (int, string) {
		t.Helper()
		resp, err := http.Get(srv.URL + "/register/status?username=" + username)
		if err != nil {
			t.Fatalf("GET status %s: %v", username, err)
		}
		defer resp.Body.Close()
		var out struct {
			Success bool   `json:"success"`
			Status  string `json:"status"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode %s: %v", username, err)
		}
		return resp.StatusCode, out.Status
	}

	// pending_otp, suspended, and a non-existent username are all
	// indistinguishable: 200 + the generic "inactive".
	for _, name := range []string{"status-pending", "status-suspended", "tidak-ada-user"} {
		if code, s := get(name); code != http.StatusOK || s != "inactive" {
			t.Fatalf("%s: code=%d status=%q, want 200/inactive", name, code, s)
		}
	}

	// Only a completed registration reveals itself.
	if code, s := get("status-active"); code != http.StatusOK || s != "active" {
		t.Fatalf("active: code=%d status=%q, want 200/active", code, s)
	}
}
