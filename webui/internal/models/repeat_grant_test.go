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

func TestHasSubmissionIgnoresEmptyJsonAnswers(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()
	examID := seedRepeatExam(t, pool)

	// Auto-submit tanpa jawaban menulis "{}" (bukan NULL). Itu bukan attempt
	// dan tidak boleh mengunci siswa yang belum menjawab apa pun.
	for _, empty := range []string{`{}`, `[]`, `null`} {
		insertRepeatSubmission(t, pool, examID, "N01", "Andi", empty)
		submitted, err := HasSubmissionForStudentKey(ctx, pool, examID, "n01")
		if err != nil {
			t.Fatalf("answers=%q: %v", empty, err)
		}
		if submitted {
			t.Fatalf("answers=%q bukan attempt berjawaban; tidak boleh memblokir", empty)
		}
	}

	insertRepeatSubmission(t, pool, examID, "N01", "Andi", `{"1":"A"}`)
	submitted, err := HasSubmissionForStudentKey(ctx, pool, examID, "n01")
	if err != nil {
		t.Fatalf("check submission: %v", err)
	}
	if !submitted {
		t.Fatal("jawaban nyata harus tetap terdeteksi")
	}
}

func TestSubmittedStudentKeysIgnoresEmptyAndPlaceholderRows(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()
	examID := seedRepeatExam(t, pool)

	// N01 punya "{}" (bukan attempt) dan N02 punya jawaban nyata + baris
	// placeholder monitoring.
	insertRepeatSubmission(t, pool, examID, "N01", "Andi", `{}`)
	insertRepeatSubmission(t, pool, examID, "N02", "Budi", `{"1":"B"}`)
	insertRepeatSubmission(t, pool, examID, "N02", "Budi", "")

	keys, err := SubmittedStudentKeys(ctx, pool, examID)
	if err != nil {
		t.Fatalf("submitted keys: %v", err)
	}
	if keys["n01"] {
		t.Fatal("baris {} tidak boleh masuk himpunan siswa yang sudah menjawab")
	}
	if !keys["n02"] {
		t.Fatal("baris berjawaban harus masuk himpunan")
	}
}

func TestSubmittedStudentKeysFallsBackToNameWhenNoNumber(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()
	examID := seedRepeatExam(t, pool)

	insertRepeatSubmission(t, pool, examID, "", "Andi", `{"1":"A"}`)

	keys, err := SubmittedStudentKeys(ctx, pool, examID)
	if err != nil {
		t.Fatalf("submitted keys: %v", err)
	}
	if !keys["andi"] {
		t.Fatal("ujian tanpa nomor harus memakai nama sebagai kunci")
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

func TestHasOpenRepeatAttemptAfterCompletedAttempt(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()
	examID := seedRepeatExam(t, pool)

	// Attempt pertama selesai.
	insertRepeatSubmission(t, pool, examID, "N01", "Andi", `{"1":"A"}`)

	// Belum ada attempt terbuka: repeat harus menunggu approval.
	open, err := HasOpenRepeatAttempt(ctx, pool, examID, "n01")
	if err != nil {
		t.Fatalf("open repeat: %v", err)
	}
	if open {
		t.Fatal("tanpa baris terbuka, repeat belum disetujui")
	}

	// Pengawas menyetujui: baris attempt terbuka setelah attempt selesai.
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, student_name, exam_number, answers_json, start_time, created_at)
		VALUES ($1, 'Andi', 'N01', NULL, '2026-01-01T00:00:00Z', CURRENT_TIMESTAMP + INTERVAL '1 second')`,
		examID); err != nil {
		t.Fatalf("insert open row: %v", err)
	}

	open, err = HasOpenRepeatAttempt(ctx, pool, examID, "n01")
	if err != nil {
		t.Fatalf("open repeat: %v", err)
	}
	if !open {
		t.Fatal("baris terbuka setelah attempt selesai = repeat sudah disetujui")
	}

	// Setelah repeat disubmit, tidak ada lagi baris terbuka setelah attempt
	// terakhir: repeat berikutnya harus menunggu approval lagi.
	if _, err := pool.Exec(ctx,
		`UPDATE submissions SET answers_json = '{"1":"B"}'
		 WHERE exam_id = $1 AND exam_number = 'N01' AND answers_json IS NULL`,
		examID); err != nil {
		t.Fatalf("submit repeat: %v", err)
	}
	open, err = HasOpenRepeatAttempt(ctx, pool, examID, "n01")
	if err != nil {
		t.Fatalf("open repeat: %v", err)
	}
	if open {
		t.Fatal("setelah repeat disubmit, repeat berikutnya harus pending")
	}
}

func TestHasOpenRepeatAttemptIgnoresStaleOpenRow(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()
	examID := seedRepeatExam(t, pool)

	// Baris terbuka lama (mis. sisa attempt pertama) TIDAK boleh dianggap
	// sebagai approval repeat untuk attempt yang selesai sesudahnya.
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, student_name, exam_number, answers_json, start_time, created_at)
		VALUES ($1, 'Andi', 'N01', NULL, '2026-01-01T00:00:00Z', CURRENT_TIMESTAMP - INTERVAL '1 minute')`,
		examID); err != nil {
		t.Fatalf("insert stale open row: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, student_name, exam_number, answers_json, created_at)
		VALUES ($1, 'Andi', 'N01', '{"1":"A"}', CURRENT_TIMESTAMP)`,
		examID); err != nil {
		t.Fatalf("insert completed row: %v", err)
	}

	open, err := HasOpenRepeatAttempt(ctx, pool, examID, "n01")
	if err != nil {
		t.Fatalf("open repeat: %v", err)
	}
	if open {
		t.Fatal("baris terbuka SEBELUM attempt selesai terakhir bukan approval repeat")
	}
}

func TestEnsureFreshSubmissionConsumesGrantOnRepeat(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()
	examID := seedRepeatExam(t, pool)
	mac := "AA:BB:CC:00:00:30"

	insertRepeatSubmission(t, pool, examID, "N30", "Andi", `{"1":"A"}`)
	if err := GrantRepeat(ctx, pool, examID, "n30", "1"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, student_class, status)
		VALUES ($1, $2, 'Andi', 'N30', '9A', 'approved')`, examID, mac); err != nil {
		t.Fatalf("insert approval: %v", err)
	}

	if err := EnsureFreshSubmissionOnApproval(ctx, pool, examID, mac); err != nil {
		t.Fatalf("ensure fresh submission: %v", err)
	}

	open, err := HasOpenRepeatAttempt(ctx, pool, examID, "n30")
	if err != nil {
		t.Fatalf("open repeat: %v", err)
	}
	if !open {
		t.Fatal("approval repeat harus membuat baris attempt terbuka")
	}
	granted, err := HasRepeatGrant(ctx, pool, examID, "n30")
	if err != nil {
		t.Fatalf("has grant: %v", err)
	}
	if granted {
		t.Fatal("izin eksplisit harus habis setelah dipakai satu attempt")
	}
}

func TestEnsureFreshSubmissionKeepsGrantOnFirstAttempt(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()
	examID := seedRepeatExam(t, pool)
	mac := "AA:BB:CC:00:00:31"

	// Izin diberikan sebelum attempt pertama: bukan untuk attempt ini.
	if err := GrantRepeat(ctx, pool, examID, "n31", "1"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, student_class, status)
		VALUES ($1, $2, 'Andi', 'N31', '9A', 'approved')`, examID, mac); err != nil {
		t.Fatalf("insert approval: %v", err)
	}

	if err := EnsureFreshSubmissionOnApproval(ctx, pool, examID, mac); err != nil {
		t.Fatalf("ensure first attempt: %v", err)
	}
	granted, err := HasRepeatGrant(ctx, pool, examID, "n31")
	if err != nil {
		t.Fatalf("has grant: %v", err)
	}
	if !granted {
		t.Fatal("attempt pertama tidak boleh menghabiskan izin repeat")
	}

	// Attempt pertama selesai; repeat berikutnya memakai izin tersebut.
	if _, err := pool.Exec(ctx,
		`UPDATE submissions SET answers_json = '{"1":"A"}'
		 WHERE exam_id = $1 AND mac_address = $2 AND answers_json IS NULL`,
		examID, mac); err != nil {
		t.Fatalf("submit first attempt: %v", err)
	}
	if err := EnsureFreshSubmissionOnApproval(ctx, pool, examID, mac); err != nil {
		t.Fatalf("ensure repeat: %v", err)
	}
	open, err := HasOpenRepeatAttempt(ctx, pool, examID, "n31")
	if err != nil {
		t.Fatalf("open repeat: %v", err)
	}
	if !open {
		t.Fatal("repeat dengan izin harus membuka attempt")
	}
	granted, err = HasRepeatGrant(ctx, pool, examID, "n31")
	if err != nil {
		t.Fatalf("has grant after repeat: %v", err)
	}
	if granted {
		t.Fatal("izin harus habis setelah repeat dibuka")
	}
}

func TestEnsureFreshSubmissionKeepsGrantForNextRepeatWhileOpen(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()
	examID := seedRepeatExam(t, pool)
	mac := "AA:BB:CC:00:00:32"

	insertRepeatSubmission(t, pool, examID, "N32", "Andi", `{"1":"A"}`)
	if _, err := pool.Exec(ctx, `
		INSERT INTO exam_approvals (exam_id, mac_address, student_name, exam_number, student_class, status)
		VALUES ($1, $2, 'Andi', 'N32', '9A', 'approved')`, examID, mac); err != nil {
		t.Fatalf("insert approval: %v", err)
	}

	// Repeat pertama disetujui: baris terbuka dibuat.
	if err := EnsureFreshSubmissionOnApproval(ctx, pool, examID, mac); err != nil {
		t.Fatalf("ensure first repeat: %v", err)
	}

	// Pengawas memberi izin untuk repeat BERIKUTNYA saat attempt ini masih
	// berjalan. Izin tidak boleh langsung habis.
	if err := GrantRepeat(ctx, pool, examID, "n32", "1"); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if err := EnsureFreshSubmissionOnApproval(ctx, pool, examID, mac); err != nil {
		t.Fatalf("ensure while open: %v", err)
	}
	granted, err := HasRepeatGrant(ctx, pool, examID, "n32")
	if err != nil {
		t.Fatalf("has grant: %v", err)
	}
	if !granted {
		t.Fatal("izin untuk repeat berikutnya tidak boleh habis saat attempt lama masih terbuka")
	}

	// Attempt berjalan selesai; repeat berikutnya memakai izin tersebut.
	if _, err := pool.Exec(ctx,
		`UPDATE submissions SET answers_json = '{"1":"B"}'
		 WHERE exam_id = $1 AND mac_address = $2 AND answers_json IS NULL`,
		examID, mac); err != nil {
		t.Fatalf("submit repeat: %v", err)
	}
	if err := EnsureFreshSubmissionOnApproval(ctx, pool, examID, mac); err != nil {
		t.Fatalf("ensure next repeat: %v", err)
	}
	granted, err = HasRepeatGrant(ctx, pool, examID, "n32")
	if err != nil {
		t.Fatalf("has grant after next repeat: %v", err)
	}
	if granted {
		t.Fatal("izin harus habis setelah dipakai repeat berikutnya")
	}
}

func TestHasOpenRepeatAttemptFallsBackToName(t *testing.T) {
	pool := database.NewPackageTestPool(t, "models")
	ctx := context.Background()
	examID := seedRepeatExam(t, pool)

	// Ujian tanpa nomor: kunci berarti nama.
	insertRepeatSubmission(t, pool, examID, "", "Andi", `{"1":"A"}`)
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, student_name, exam_number, answers_json, start_time, created_at)
		VALUES ($1, 'Andi', '', NULL, '2026-01-01T00:00:00Z', CURRENT_TIMESTAMP + INTERVAL '1 second')`,
		examID); err != nil {
		t.Fatalf("insert open row: %v", err)
	}

	open, err := HasOpenRepeatAttempt(ctx, pool, examID, "andi")
	if err != nil {
		t.Fatalf("open repeat: %v", err)
	}
	if !open {
		t.Fatal("ujian tanpa nomor harus memakai nama untuk mendeteksi repeat")
	}

	open, err = HasOpenRepeatAttempt(ctx, pool, examID, "budi")
	if err != nil {
		t.Fatalf("open repeat budi: %v", err)
	}
	if open {
		t.Fatal("nama berbeda tidak boleh ikut terdeteksi")
	}
}
