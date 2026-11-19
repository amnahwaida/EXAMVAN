package api

// ---------------------------------------------------------------------------
// Review presence 14 Nov 2026 (ronde 2): AccessLog/CompleteExam menulis
// presence (heartbeat key + baris monitoring) atas MAC self-asserted. Gerbang
// lama menerima token MATCH saja — mode static-token berarti token dibagi
// sekelas, jadi siswa bisa membuat presence fantom untuk MAC arbitrer
// (phantom monitoring). Kontrak baru: presence menuntut approval 'approved'
// untuk MAC tersebut — entitlemen yang sama dengan toleransi submit.
// Pengawas tetap punya kontrol: Tolak per device memblokir presence-nya.
// ---------------------------------------------------------------------------

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/models"
)

func setupPresenceGateFixture(t *testing.T, pool *pgxpool.Pool, mac, approvalStatus string) (examID int, token string) {
	t.Helper()
	ctx := context.Background()

	owner, err := models.CreateUser(ctx, pool, &models.AdminUser{
		Username: fmt.Sprintf("pres-guru-%s-%s", mac, approvalStatus),
		Name:     "Guru Presence", PasswordHash: "x", Status: models.UserStatusActive,
		Role: models.SerializeRoles([]string{models.RoleGuru}),
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	token = fmt.Sprintf("P%07d", len(mac)*7919)
	var newExamID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
		                   security_level, created_by, exam_started_at)
		VALUES ('Ujian Presence', 'pres.pdf', 1024, $1, $1, 'active', 'medium', $2, CURRENT_TIMESTAMP)
		RETURNING id`, token, owner.ID).Scan(&newExamID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}
	examID = newExamID

	if approvalStatus != "" {
		if _, err := pool.Exec(ctx, `
			INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, status)
			VALUES ($1, $2, 'Siswa Presence', 'PR1', $3)`, examID, mac, approvalStatus); err != nil {
			t.Fatalf("insert approval: %v", err)
		}
	}
	return examID, token
}

func presencePost(t *testing.T, pool *pgxpool.Pool, path string, payload map[string]interface{}, token string) int {
	t.Helper()
	srv := httptest.NewServer(newExamFixesRouter(pool, nil))
	defer srv.Close()
	raw, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Exam-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestPresenceHeartbeatWithApprovedApprovalOK(t *testing.T) {
	pool := setupPresenceGatePool(t)
	examID, token := setupPresenceGateFixture(t, pool, "DEVICE:pres-ok", "approved")
	_ = examID

	status := presencePost(t, pool, fmt.Sprintf("/api/exams/%d/access-log", examID),
		map[string]interface{}{"event": "heartbeat", "mac_address": "DEVICE:pres-ok"}, token)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (device approved wajib bisa mengirim presence)", status)
	}
}

func TestPresenceHeartbeatPhantomMacBlocked(t *testing.T) {
	// Token VALID (dibagi sekelas di mode static) + MAC tanpa approval:
	// presence fantom wajib ditolak — approval row adalah entitlemen.
	pool := setupPresenceGatePool(t)
	examID, token := setupPresenceGateFixture(t, pool, "DEVICE:phantom", "")

	status := presencePost(t, pool, fmt.Sprintf("/api/exams/%d/access-log", examID),
		map[string]interface{}{"event": "heartbeat", "mac_address": "DEVICE:phantom"}, token)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 — presence fantom (mac tanpa approval) wajib ditolak", status)
	}
}

func TestPresenceLoginWithoutApprovalBlocked(t *testing.T) {
	pool := setupPresenceGatePool(t)
	examID, token := setupPresenceGateFixture(t, pool, "DEVICE:pres-noapp", "")

	status := presencePost(t, pool, fmt.Sprintf("/api/exams/%d/access-log", examID),
		map[string]interface{}{"event": "login", "mac_address": "DEVICE:pres-noapp",
			"student_name": "Siswa Fantom"}, token)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 — login tanpa approval wajib ditolak (baris akses tidak boleh dibuat)", status)
	}
}

func TestCompleteExamRequiresApprovalForMac(t *testing.T) {
	pool := setupPresenceGatePool(t)
	examID, token := setupPresenceGateFixture(t, pool, "DEVICE:comp-phantom", "")

	status := presencePost(t, pool, fmt.Sprintf("/api/exams/%d/complete", examID),
		map[string]interface{}{"mac_address": "DEVICE:comp-phantom", "token": token}, token)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 — complete untuk MAC tanpa approval wajib ditolak (phantom heartbeat delete)", status)
	}
}

func TestCompleteExamWithApprovalOK(t *testing.T) {
	pool := setupPresenceGatePool(t)
	examID, token := setupPresenceGateFixture(t, pool, "DEVICE:comp-ok", "approved")

	status := presencePost(t, pool, fmt.Sprintf("/api/exams/%d/complete", examID),
		map[string]interface{}{"mac_address": "DEVICE:comp-ok", "token": token}, token)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (device approved boleh menutup sesi sendiri)", status)
	}
}

func TestPresenceLogoutWithApprovalOK(t *testing.T) {
	pool := setupPresenceGatePool(t)
	examID, token := setupPresenceGateFixture(t, pool, "DEVICE:logout-ok", "approved")

	status := presencePost(t, pool, fmt.Sprintf("/api/exams/%d/access-log", examID),
		map[string]interface{}{"event": "logout", "mac_address": "DEVICE:logout-ok"}, token)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (logout device approved wajib lolos)", status)
	}

	var total int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM student_access_logs WHERE exam_id=$1 AND student_identifier='DEVICE:logout-ok' AND event='logout'`,
		examID).Scan(&total); err != nil {
		t.Fatalf("count access logs: %v", err)
	}
	if total != 1 {
		t.Errorf("logout rows = %d, want 1", total)
	}
}

func setupPresenceGatePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return database.NewPackageTestPool(t, "api")
}
