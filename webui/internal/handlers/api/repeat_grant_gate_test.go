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

// TestRepeatGateBlocksAfterSubmitThenGrantUnblocks adalah alur utuh:
// submit -> ditolak -> pengawas memberi izin -> boleh masuk lagi.
func TestRepeatGateBlocksAfterSubmitThenGrantUnblocks(t *testing.T) {
	pool := database.NewPackageTestPool(t, "api")
	examID, token := createRequestApprovalFixture(t, pool, true, true, true)
	srv := httptest.NewServer(newRequestApprovalRouter(pool))
	defer srv.Close()
	ctx := context.Background()

	// Siswa mengirim jawaban.
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, student_name, exam_number, answers_json)
		VALUES ($1, 'Andi', 'N03', '{"1":"A"}')`, examID); err != nil {
		t.Fatalf("insert submission: %v", err)
	}

	// Tanpa izin: ditolak.
	blocked := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:00:00:03",
		"student_name": "Andi", "exam_number": "N03", "student_class": "9A",
		"reset": true,
	})
	if blocked["status"] != "repeat_required" {
		t.Fatalf("siswa yang sudah mengulang harus ditolak, dapat %v", blocked)
	}
	if blocked["success"] != false {
		t.Fatalf("repeat_required harus success=false: %v", blocked)
	}
	if blocked["student_key"] != "n03" {
		t.Fatalf("respons harus menyebut student_key, dapat %v", blocked["student_key"])
	}
	if _, ok := blocked["message"]; !ok {
		t.Fatal("respons harus punya message untuk ditampilkan ke siswa")
	}

	// Pengawas memberi izin.
	if err := models.GrantRepeat(ctx, pool, examID, "n03", "1"); err != nil {
		t.Fatalf("grant: %v", err)
	}

	// Dengan izin: boleh.
	allowed := postRepeatGateway(t, srv, examID, token, map[string]interface{}{
		"exam_id": examID, "mac_address": "AA:BB:CC:00:00:03",
		"student_name": "Andi", "exam_number": "N03", "student_class": "9A",
		"reset": true,
	})
	if allowed["status"] == "repeat_required" {
		t.Fatalf("izin sudah diberikan, siswa harus boleh masuk: %v", allowed)
	}
	if allowed["status"] != "approved" {
		t.Fatalf("auto-approve harus menyetujui setelah izin: %v", allowed)
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
