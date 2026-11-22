// Gerbang "izin mengulang" diuji MULUR: dari request-approval sampai izin
// diberikan pengawas. Test unit di package models hanya membuktikan
// query-nya benar; yang bisa rusak tanpa terdeteksi adalah GERBANGNYA
// tidak pernah menolak, atau menolak semua orang termasuk yang sedang
// mengerjakan.
package api

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/models"
)

// postRepeatGateway mengirim request-approval memakai helper yang sudah ada
// di request_approval_test.go, jadi tidak ada jalur kedua yang bisa melenceng.
func postRepeatGateway(t *testing.T, srv *httptest.Server, examID int, token string,
	payload map[string]interface{}) map[string]interface{} {
	t.Helper()
	payload["exam_id"] = examID
	code, out := postRequestApproval(t, srv, payload, token)
	if code != 200 {
		t.Fatalf("request-approval harus 200, dapat %d (%v)", code, out)
	}
	return out
}

// TestRepeatGateAllowsFirstAttempt adalah dasar yang paling penting:
// siswa yang belum pernah mengirim jawaban TIDAK BOLEH terblokir, walau
// auto-approve menyala.
func TestRepeatGateAllowsFirstAttempt(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	out := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:00:00:01",
		"student_name": "Andi", "exam_number": "N01", "student_class": "9A",
	})

	if out["status"] == "repeat_required" {
		t.Fatalf("siswa yang belum mengerjakan tidak boleh diblokir: %v", out)
	}
	if out["success"] != true {
		t.Fatalf("percobaan pertama harus berhasil: %v", out)
	}
}

// TestRepeatGateIgnoresTheEmptyMonitoringRow adalah regresi untuk jebakan
// paling berbahaya di fitur ini. Auto-approve membuat baris submissions
// KOSONG (EnsureFreshSubmissionOnApproval) supaya monitoring menampilkan
// siswa yang sedang mengerjakan. Kalau gerbang menghitung baris itu,
// polling kedua -- lima detik kemudian -- akan mengembalikan
// repeat_required dan mengunci siswa di tengah ujiannya sendiri.
func TestRepeatGateIgnoresTheEmptyMonitoringRow(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	// Polling PERTAMA: membuat baris kosong.
	first := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:00:00:02",
		"student_name": "Andi", "exam_number": "N02", "student_class": "9A",
	})
	if first["status"] == "repeat_required" {
		t.Fatalf("polling pertama tidak boleh diblokir: %v", first)
	}
	if first["status"] != "approved" {
		t.Fatalf("auto-approve harus menyetujui polling pertama: %v", first)
	}

	// Pastikan baris kosongnya benar-benar ada -- kalau tidak, test ini
	// lulus karena alasan yang salah.
	var emptyRows int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM submissions
		 WHERE exam_id = $1 AND exam_number = 'N02' AND answers_json IS NULL`,
		examID).Scan(&emptyRows); err != nil {
		t.Fatalf("cek baris kosong: %v", err)
	}
	if emptyRows == 0 {
		t.Fatal("test tidak benar-benar menguji apa pun: baris monitoring " +
			"yang kosong tidak terbentuk")
	}

	// Polling KEDUA: baris kosong sudah ada. Harus tetap lolos.
	second := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:00:00:02",
		"student_name": "Andi", "exam_number": "N02", "student_class": "9A",
	})
	if second["status"] == "repeat_required" {
		t.Fatalf("polling kedua pada ujian yang SEDANG BERLANGSUNG "+
			"tidak boleh diblokir; baris monitoring kosong bukan jawaban: %v",
			second)
	}
	if second["status"] != "approved" {
		t.Fatalf("polling kedua harus tetap disetujui: %v", second)
	}
}

// TestRepeatGateIgnoresEmptyJsonSubmission: auto-submit tanpa jawaban
// menyimpan answers_json = "{}", bukan NULL. Itu bukan attempt dan tidak
// boleh mengunci siswa yang tidak pernah menjawab.
func TestRepeatGateIgnoresEmptyJsonSubmission(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO submissions (exam_id, student_name, exam_number, answers_json)
		VALUES ($1, 'Andi', 'N09', '{}')`, examID); err != nil {
		t.Fatalf("insert submission: %v", err)
	}

	out := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:00:00:09",
		"student_name": "Andi", "exam_number": "N09", "student_class": "9A",
	})
	if out["status"] == "repeat_required" {
		t.Fatalf("submission {} bukan attempt; siswa tidak boleh diblokir: %v", out)
	}
}

// TestRepeatGateAutoApproveApprovesRepeatAndCreatesAttempt: saat auto-approve
// menyala, permintaan mengulang TIDAK masuk antrean — server langsung
// menyetujui dan membuat baris attempt baru. Setelah attempt itu selesai,
// repeat berikutnya juga langsung disetujui (satu baris per attempt).
func TestRepeatGateAutoApproveApprovesRepeatAndCreatesAttempt(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()
	ctx := context.Background()

	mac := "AA:BB:CC:00:00:03"
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, student_name, exam_number, answers_json)
		VALUES ($1, 'Andi', 'N03', '{"1":"A"}')`, examID); err != nil {
		t.Fatalf("insert submission: %v", err)
	}

	first := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": mac,
		"student_name": "Andi", "exam_number": "N03", "student_class": "9A",
		"reset": true,
	})
	if first["status"] != "approved" {
		t.Fatalf("auto-approve harus menyetujui repeat, dapat %v", first)
	}

	var open int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM submissions
		WHERE exam_id = $1 AND mac_address = $2
		  AND (answers_json IS NULL OR BTRIM(answers_json) = '')
		  AND BTRIM(COALESCE(start_time, '')) <> ''`, examID, mac).Scan(&open); err != nil {
		t.Fatalf("count open rows: %v", err)
	}
	if open != 1 {
		t.Fatalf("baris attempt repeat terbuka = %d, want 1", open)
	}

	// Selesaikan attempt kedua, lalu minta repeat lagi: tetap auto-approved.
	if _, err := pool.Exec(ctx, `
		UPDATE submissions SET answers_json = '{"1":"B"}'
		WHERE exam_id = $1 AND mac_address = $2
		  AND (answers_json IS NULL OR BTRIM(answers_json) = '')`, examID, mac); err != nil {
		t.Fatalf("submit repeat: %v", err)
	}
	second := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": mac,
		"student_name": "Andi", "exam_number": "N03", "student_class": "9A",
		"reset": false,
	})
	if second["status"] != "approved" {
		t.Fatalf("repeat berikutnya harus tetap auto-approved, dapat %v", second)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM submissions
		WHERE exam_id = $1 AND mac_address = $2
		  AND (answers_json IS NULL OR BTRIM(answers_json) = '')
		  AND BTRIM(COALESCE(start_time, '')) <> ''`, examID, mac).Scan(&open); err != nil {
		t.Fatalf("count open rows: %v", err)
	}
	if open != 1 {
		t.Fatalf("baris attempt repeat terbuka = %d, want 1", open)
	}
}

// TestRepeatGateQueuesRepeatWhenAutoApproveOff: saat auto-approve mati,
// repeat tetap masuk antrean pending. Grant hanya melepas gerbang repeat;
// keputusan perangkat tetap manual, dan grant dikonsumsi saat disetujui.
func TestRepeatGateQueuesRepeatWhenAutoApproveOff(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, false)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()
	ctx := context.Background()

	mac := "AA:BB:CC:00:00:04"
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, student_name, exam_number, answers_json)
		VALUES ($1, 'Andi', 'N04', '{"1":"A"}')`, examID); err != nil {
		t.Fatalf("insert submission: %v", err)
	}

	first := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": mac,
		"student_name": "Andi", "exam_number": "N04", "student_class": "9A",
		"reset": true,
	})
	if first["status"] != "pending" {
		t.Fatalf("repeat tanpa auto-approve harus pending, dapat %v", first)
	}

	if err := models.GrantRepeat(ctx, pool, examID, "n04", "1"); err != nil {
		t.Fatalf("grant: %v", err)
	}

	afterGrant := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": mac,
		"student_name": "Andi", "exam_number": "N04", "student_class": "9A",
		"reset": true,
	})
	if afterGrant["status"] != "pending" {
		t.Fatalf("grant tidak boleh auto-approve saat flag mati, dapat %v", afterGrant)
	}

	// Pengawas menyetujui perangkat: helper membuat attempt terbuka dan
	// mengonsumsi grant, lalu status di-set approved.
	if err := models.EnsureFreshSubmissionOnApproval(ctx, pool, examID, mac); err != nil {
		t.Fatalf("ensure fresh submission: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE exam_approvals SET status = 'approved' WHERE exam_id = $1 AND mac_address = $2`,
		examID, mac); err != nil {
		t.Fatalf("approve: %v", err)
	}

	allowed := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": mac,
		"student_name": "Andi", "exam_number": "N04", "student_class": "9A",
		"reset": true,
	})
	if allowed["status"] != "approved" {
		t.Fatalf("setelah disetujui pengawas harus approved, dapat %v", allowed)
	}

	var granted bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM exam_repeat_grants WHERE exam_id = $1 AND student_key = 'n04')`,
		examID).Scan(&granted); err != nil {
		t.Fatalf("check grant: %v", err)
	}
	if granted {
		t.Fatal("grant harus dikonsumsi setelah approval")
	}
}

// TestRepeatGateDemotesApprovedRowOnPoll: perangkat yang sudah approved dari
// attempt pertama harus turun ke pending saat siswa meminta mengulang, supaya
// permintaannya terlihat pengawas.
func TestRepeatGateDemotesApprovedRowOnPoll(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, false)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()
	ctx := context.Background()

	mac := "AA:BB:CC:00:00:0A"
	if _, err := pool.Exec(ctx, `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, student_class, status)
		VALUES ($1, $2, 'Andi', 'N10', '9A', 'approved')`, examID, mac); err != nil {
		t.Fatalf("insert approval: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, student_name, exam_number, answers_json)
		VALUES ($1, 'Andi', 'N10', '{"1":"A"}')`, examID); err != nil {
		t.Fatalf("insert submission: %v", err)
	}

	out := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": mac,
		"student_name": "Andi", "exam_number": "N10", "student_class": "9A",
		"reset": false,
	})
	if out["status"] != "pending" {
		t.Fatalf("poll mengulang harus pending, dapat %v", out)
	}

	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM exam_approvals WHERE exam_id = $1 AND mac_address = $2`,
		examID, mac).Scan(&status); err != nil {
		t.Fatalf("read approval: %v", err)
	}
	if status != "pending" {
		t.Fatalf("baris approval harus turun ke pending, dapat %q", status)
	}

	// Pengawas menyetujui: baris attempt repeat terbuka dibuat lebih dulu
	// (penanda approval per-attempt), lalu status approval di-set approved.
	if err := models.EnsureFreshSubmissionOnApproval(ctx, pool, examID, mac); err != nil {
		t.Fatalf("ensure fresh submission: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE exam_approvals SET status = 'approved' WHERE exam_id = $1 AND mac_address = $2`,
		examID, mac); err != nil {
		t.Fatalf("approve: %v", err)
	}

	after := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": mac,
		"student_name": "Andi", "exam_number": "N10", "student_class": "9A",
		"reset": false,
	})
	if after["status"] != "approved" {
		t.Fatalf("setelah izin, poll harus approved, dapat %v", after)
	}
}

// TestRepeatGateApprovedAttemptAllowsOnceThenNextRepeatQueues: approval
// berlaku untuk SATU attempt. Baris attempt terbuka adalah penandanya; setelah
// siswa submit, repeat berikutnya harus masuk antrean pending lagi.
func TestRepeatGateApprovedAttemptAllowsOnceThenNextRepeatQueues(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, false)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()
	ctx := context.Background()

	mac := "AA:BB:CC:00:00:0C"
	if _, err := pool.Exec(ctx, `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, student_class, status)
		VALUES ($1, $2, 'Andi', 'N12', '9A', 'approved')`, examID, mac); err != nil {
		t.Fatalf("insert approval: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, student_name, exam_number, answers_json)
		VALUES ($1, 'Andi', 'N12', '{"1":"A"}')`, examID); err != nil {
		t.Fatalf("insert submission: %v", err)
	}
	// Pengawas menyetujui repeat: buat baris attempt terbuka.
	if err := models.EnsureFreshSubmissionOnApproval(ctx, pool, examID, mac); err != nil {
		t.Fatalf("ensure fresh submission: %v", err)
	}

	// Attempt repeat yang sedang berjalan boleh (baris terbuka ada).
	during := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": mac,
		"student_name": "Andi", "exam_number": "N12", "student_class": "9A",
		"reset": false,
	})
	if during["status"] != "approved" {
		t.Fatalf("attempt repeat yang sudah disetujui harus approved, dapat %v", during)
	}

	// Siswa menyelesaikan repeat: baris terbuka menjadi jawaban.
	if _, err := pool.Exec(ctx,
		`UPDATE submissions SET answers_json = '{"1":"B"}'
		 WHERE exam_id = $1 AND mac_address = $2 AND answers_json IS NULL`,
		examID, mac); err != nil {
		t.Fatalf("submit repeat: %v", err)
	}

	// Repeat BERIKUTNYA harus pending: approval hanya untuk satu attempt.
	next := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": mac,
		"student_name": "Andi", "exam_number": "N12", "student_class": "9A",
		"reset": false,
	})
	if next["status"] != "pending" {
		t.Fatalf("repeat berikutnya harus pending, dapat %v", next)
	}
}

// TestRepeatGateKeepsRejectedDecisionOnPoll: polling tidak boleh membatalkan
// keputusan pengawas yang menolak permintaan mengulang.
func TestRepeatGateKeepsRejectedDecisionOnPoll(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()
	ctx := context.Background()

	mac := "AA:BB:CC:00:00:0B"
	if _, err := pool.Exec(ctx, `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, student_class, status)
		VALUES ($1, $2, 'Andi', 'N11', '9A', 'rejected')`, examID, mac); err != nil {
		t.Fatalf("insert approval: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, student_name, exam_number, answers_json)
		VALUES ($1, 'Andi', 'N11', '{"1":"A"}')`, examID); err != nil {
		t.Fatalf("insert submission: %v", err)
	}

	out := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": mac,
		"student_name": "Andi", "exam_number": "N11", "student_class": "9A",
		"reset": false,
	})
	if out["status"] != "rejected" {
		t.Fatalf("keputusan rejected harus bertahan, dapat %v", out)
	}

	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM exam_approvals WHERE exam_id = $1 AND mac_address = $2`,
		examID, mac).Scan(&status); err != nil {
		t.Fatalf("read approval: %v", err)
	}
	if status != "rejected" {
		t.Fatalf("baris approval harus tetap rejected, dapat %q", status)
	}
}

// TestRepeatGateDoesNotBlockOtherStudents: izin dan blokir per siswa, bukan
// per ujian. Kalau tidak, satu siswa yang punya izin membuka pintu untuk
// semua orang.
func TestRepeatGateDoesNotBlockOtherStudents(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, student_name, exam_number, answers_json)
		VALUES ($1, 'Andi', 'N04', '{"1":"A"}')`, examID); err != nil {
		t.Fatalf("insert submission: %v", err)
	}
	if err := models.GrantRepeat(ctx, pool, examID, "n04", "1"); err != nil {
		t.Fatalf("grant: %v", err)
	}

	// Siswa BERBEDA tanpa izin: tetap boleh (belum pernah mengerjakan).
	other := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:00:00:05",
		"student_name": "Budi", "exam_number": "N05", "student_class": "9A",
		"reset": true,
	})
	if other["status"] == "repeat_required" {
		t.Fatalf("siswa lain tidak boleh ikut terblokir: %v", other)
	}
}

// TestRepeatGateIgnoresMissingIdentity: identitas kosong tidak boleh
// memblokir. Mengunci orang yang tidak bisa diidentifikasi hanya menjebak
// mereka dengan tidak ada tombol yang bisa melepas.
func TestRepeatGateIgnoresMissingIdentity(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()

	out := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:00:00:06",
		"student_name": "", "exam_number": "", "student_class": "",
		"reset": true,
	})
	if out["status"] == "repeat_required" {
		t.Fatalf("tanpa identitas, jangan memblokir: %v", out)
	}
}
