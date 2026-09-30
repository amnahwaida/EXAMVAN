package models

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/database"
)

// seedRepeatExam membuat satu ujian tanpa created_by (kolomnya nullable).
func seedRepeatExam(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	token := fmt.Sprintf("RPT%05d", time.Now().UnixNano()%100000)
	var examID int
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token,
		                   status, security_level)
		VALUES ($1, 'repeat.pdf', 1024, $2, $2, 'active', 'medium')
		RETURNING id`, "Ujian "+token, token).Scan(&examID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}
	return examID
}

// insertRepeatSubmission membuat baris submission. answersJson kosong
// meniru baris yang dibuat EnsureFreshSubmissionOnApproval.
func insertRepeatSubmission(t *testing.T, pool *pgxpool.Pool, examID int,
	examNumber, studentName, answersJSON string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO submissions (exam_id, student_name, exam_number, answers_json)
		VALUES ($1, $2, $3, NULLIF($4, ''))`,
		examID, studentName, examNumber, answersJSON); err != nil {
		t.Fatalf("insert submission: %v", err)
	}
}

func TestRepeatGrantRoundTrip(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()

	examID := seedRepeatExam(t, pool)

	if err := GrantRepeat(ctx, pool, examID, "n01", "7"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	granted, err := HasRepeatGrant(ctx, pool, examID, "n01")
	if err != nil {
		t.Fatalf("has grant: %v", err)
	}
	if !granted {
		t.Fatal("izin harus berlaku setelah GrantRepeat")
	}

	// Idempoten: mengulang tidak boleh gagal dan tidak boleh menggandakan.
	if err := GrantRepeat(ctx, pool, examID, "n01", "7"); err != nil {
		t.Fatalf("grant kedua harus idempoten: %v", err)
	}
	list, err := ListRepeatGrants(ctx, pool, examID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("harus ada 1 izin, dapat %d", len(list))
	}

	if err := RevokeRepeat(ctx, pool, examID, "n01"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	granted, err = HasRepeatGrant(ctx, pool, examID, "n01")
	if err != nil {
		t.Fatalf("has grant after revoke: %v", err)
	}
	if granted {
		t.Fatal("izin harus hilang setelah RevokeRepeat")
	}

	// Mencabut izin yang memang tidak ada harus aman, bukan error.
	if err := RevokeRepeat(ctx, pool, examID, "n99"); err != nil {
		t.Fatalf("revoke yang tidak ada harus idempoten: %v", err)
	}
}

func TestHasRepeatGrantRejectsEmptyKey(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()
	examID := seedRepeatExam(t, pool)

	granted, err := HasRepeatGrant(ctx, pool, examID, "")
	if err != nil {
		t.Fatalf("key kosong tidak boleh error: %v", err)
	}
	if granted {
		t.Fatal("key kosong tidak boleh menghasilkan izin")
	}
	if err := GrantRepeat(ctx, pool, examID, "", "7"); err == nil {
		t.Fatal("GrantRepeat dengan key kosong harus error")
	}
}

func TestHasSubmissionIgnoresEmptyAnswerRows(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()
	examID := seedRepeatExam(t, pool)

	// Baris kosong persis yang dibuat EnsureFreshSubmissionOnApproval.
	insertRepeatSubmission(t, pool, examID, "N01", "Andi", "")

	submitted, err := HasSubmissionForStudentKey(ctx, pool, examID, "n01")
	if err != nil {
		t.Fatalf("check submission: %v", err)
	}
	if submitted {
		t.Fatal("baris tanpa jawaban BUKAN berarti sudah mengulang; kalau ikut " +
			"terhitung, polling kedua akan mengunci ujian yang sedang berjalan")
	}

	// Sekarang benar-benar mengirim jawaban.
	insertRepeatSubmission(t, pool, examID, "N01", "Andi", `{"1":"A"}`)

	submitted, err = HasSubmissionForStudentKey(ctx, pool, examID, "n01")
	if err != nil {
		t.Fatalf("check submission: %v", err)
	}
	if !submitted {
		t.Fatal("baris dengan jawaban harus berarti sudah mengulang")
	}
}

func TestHasSubmissionMatchesCaseInsensitively(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()
	examID := seedRepeatExam(t, pool)

	insertRepeatSubmission(t, pool, examID, "N01", "Andi", `{"1":"A"}`)

	// Client mengetik nomor dengan huruf besar-kecil berbeda; tetap siswa
	// yang sama, jadi harus terdeteksi.
	for _, key := range []string{"n01", "N01", " n01 "} {
		submitted, err := HasSubmissionForStudentKey(ctx, pool, examID, key)
		if err != nil {
			t.Fatalf("key %q: %v", key, err)
		}
		if !submitted {
			t.Fatalf("key %q harus cocok dengan N01", key)
		}
	}
	// Siswa lain tidak boleh ikut kena.
	submitted, err := HasSubmissionForStudentKey(ctx, pool, examID, "n02")
	if err != nil {
		t.Fatalf("n02: %v", err)
	}
	if submitted {
		t.Fatal("n02 tidak punya jawaban; tidak boleh terdeteksi sebagai n01")
	}
}

func TestHasSubmissionFallsBackToNameWhenNoNumber(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()
	examID := seedRepeatExam(t, pool)

	// Ujian yang memang tidak mengumpulkan nomor: semua baris punya
	// exam_number kosong, jadi kunci berarti nama.
	insertRepeatSubmission(t, pool, examID, "", "Andi", `{"1":"A"}`)

	submitted, err := HasSubmissionForStudentKey(ctx, pool, examID, "andi")
	if err != nil {
		t.Fatalf("fallback nama: %v", err)
	}
	if !submitted {
		t.Fatal("ujian tanpa nomor harus jatuh ke pencocokan nama")
	}
	submitted, err = HasSubmissionForStudentKey(ctx, pool, examID, "budi")
	if err != nil {
		t.Fatalf("nama lain: %v", err)
	}
	if submitted {
		t.Fatal("nama berbeda tidak boleh ikut terdeteksi")
	}
}

func TestHasSubmissionPrefersNumberWhenOthersHaveIt(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()
	examID := seedRepeatExam(t, pool)

	// Satu siswa punya nomor, satu lagi tidak. Kunci "andi" milik siswa
	// tanpa nomor, dan TIDAK boleh dianggap sudah submit karena ada
	// submission milik "Budi" yang kebetulan bernama sama... atau lebih
	// tepatnya: student yang punya nomor harus dicocokkan lewat nomor.
	insertRepeatSubmission(t, pool, examID, "N01", "Andi", `{"1":"A"}`)
	insertRepeatSubmission(t, pool, examID, "", "Andi", `{"1":"B"}`)

	// "n01" cocok lewat nomor.
	submitted, err := HasSubmissionForStudentKey(ctx, pool, examID, "n01")
	if err != nil {
		t.Fatalf("n01: %v", err)
	}
	if !submitted {
		t.Fatal("n01 harus terdeteksi lewat nomor ujian")
	}
}
