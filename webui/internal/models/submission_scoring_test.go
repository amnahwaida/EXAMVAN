package models

import (
	"context"
	"testing"
)

// TestShortAnswerNumericTolerance (review alur ujian 14 Nov 2026, P3):
// short_answer dulu dibandingkan sebagai string upper/trim murni, sehingga
// jawaban yang BENAR secara nilai bisa dinilai salah hanya karena format
// ketikan ("1.0" vs "1", "1,50" vs "1.5", "007" vs "7"). Normalisasi numerik
// kini diterapkan hanya pada tipe short_answer; non-numerik tetap
// dibandingkan string (case-insensitive) — perilaku lama untuk jawaban teks
// tidak berubah.
func TestShortAnswerNumericTolerance(t *testing.T) {
	cases := []struct {
		name       string
		student    string
		correct    string
		wantStatus string
	}{
		{"desimal nol", "1.0", "1", StatusCorrect},
		{"koma desimal", "1,5", "1.5", StatusCorrect},
		{"nol di depan", "007", "7", StatusCorrect},
		{"spasi ganda", "  1.50  ", "1.5", StatusCorrect},
		{"teks case-insensitive", "jakarta", "JAKARTA", StatusCorrect},
		{"bukan numerik -> string", "2.0.1", "2.0", StatusIncorrect},
		{"memang beda", "2.5", "3.5", StatusIncorrect},
		{"kosong vs isi", "", "1", StatusIncorrect},
	}
	for _, tc := range cases {
		questions := []Question{{
			Number: 1, Type: "short_answer", Weight: 2,
		}}
		questions[0].Key = tc.correct
		detail := EvaluateAnswersDetailed(
			map[string]interface{}{"1": tc.student}, questions)["1"]
		if detail.StatusClass != tc.wantStatus {
			t.Errorf("%s: status=%s want=%s (earned=%.2f)",
				tc.name, detail.StatusClass, tc.wantStatus, detail.Earned)
		}
	}
}

// TestMCPartialAndMatchingSanity mengunci formula partial yang sudah benar
// agar refactor normalisasi short_answer tidak menyentuhnya.
func TestMCPartialAndMatchingSanity(t *testing.T) {
	mc := []Question{{
		Number: 1, Type: "multiple_choice", Weight: 4, PartialScoring: true,
		Key: []interface{}{"A", "B"},
	}}
	// Formula partial dokumen: max(0, benar - salah) / jumlah_kunci x bobot.
	partial := EvaluateAnswersDetailed(map[string]interface{}{"1": []interface{}{"A"}}, mc)["1"]
	if partial.StatusClass != StatusPartial || partial.Earned != 2 {
		t.Errorf("MC partial: got %.2f/%s, want 2.00/%s (1 dari 2 kunci, tanpa salah)",
			partial.Earned, partial.StatusClass, StatusPartial)
	}
	exact := EvaluateAnswersDetailed(map[string]interface{}{"1": []interface{}{"B", "A"}}, mc)["1"]
	if exact.Earned != 4 || exact.StatusClass != StatusCorrect {
		t.Errorf("MC exact (urutan bebas): got %.2f/%s, want 4.00/%s",
			exact.Earned, exact.StatusClass, StatusCorrect)
	}

	match := []Question{{
		Number: 2, Type: "matching", Weight: 4, PartialScoring: true,
		Key: map[string]interface{}{"1": "A", "2": "B"},
	}}
	half := EvaluateAnswersDetailed(map[string]interface{}{"2": map[string]interface{}{"1": "A"}}, match)["2"]
	if half.Earned != 2 || half.StatusClass != StatusPartial {
		t.Errorf("matching partial: got %.2f/%s, want 2.00/%s",
			half.Earned, half.StatusClass, StatusPartial)
	}
}

// TestRecalculateSkipsPlaceholders (review pasca-ujian 14 Nov 2026):
// RecalculateAllScoresForExam dulu menulis score 0 ke placeholder siswa yang
// TIDAK pernah submit (baris monitoring dari approval) — nilai "belum" berubah
// jadi 0 di monitoring admin. Kontrak baru: baris placeholder (answers_json
// kosong) dilewati; baris yang benar-benar submit ikut dinilai ulang dengan
// kunci/bobot terbaru.
func TestRecalculateSkipsPlaceholders(t *testing.T) {
	pool := setupAuthTestDB(t)
	ctx := context.Background()

	owner, err := CreateUser(ctx, pool, &AdminUser{
		Username: "recalc-guru", Name: "Guru Recalc", PasswordHash: "x",
		Status: UserStatusActive, Role: SerializeRoles([]string{RoleGuru}),
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}

	questionsJSON := `[{"number":1,"type":"multiple_choice","label":"S","weight":2,"key":"jakarta","options":["Jakarta","Bogor"]}]`
	var examID int
	if err := pool.QueryRow(ctx, `
		INSERT INTO exams (name, file_path, size_bytes, token, active_token, status,
		                   security_level, questions_json, created_by, exam_started_at)
		VALUES ('Ujian Recalc', 'rc.pdf', 1024, $1, $1, 'inactive', 'medium', $2, $3, CURRENT_TIMESTAMP)
		RETURNING id`, "RC12345", questionsJSON, owner.ID).Scan(&examID); err != nil {
		t.Fatalf("insert exam: %v", err)
	}

	// Siswa A: sudah submit (jawaban lama dengan skor lama).
	// Siswa B: placeholder (tanpa jawaban, tanpa nilai).
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, student_name, exam_number, student_class,
		                         answers_json, score, mac_address, submitted_at)
		VALUES ($1, 'Siswa A', 'RA1', 'XII-A', '{"1":"salah-lama"}', 0, 'DEVICE:ra', CURRENT_TIMESTAMP)`, examID); err != nil {
		t.Fatalf("insert submitted row: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (exam_id, student_name, exam_number, student_class,
		                         mac_address, identity_data)
		VALUES ($1, 'Siswa B', 'RB1', 'XII-A', 'DEVICE:rb')`, examID); err != nil {
		t.Fatalf("insert placeholder row: %v", err)
	}

	if err := RecalculateAllScoresForExam(ctx, pool, examID); err != nil {
		t.Fatalf("recalculate: %v", err)
	}

	var scoreA, scoreB *float64
	if err := pool.QueryRow(ctx,
		`SELECT score FROM submissions WHERE exam_id=$1 AND mac_address='DEVICE:ra'`, examID).Scan(&scoreA); err != nil {
		t.Fatalf("read score A: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT score FROM submissions WHERE exam_id=$1 AND mac_address='DEVICE:rb'`, examID).Scan(&scoreB); err != nil {
		t.Fatalf("read score B: %v", err)
	}

	if scoreA == nil {
		t.Errorf("baris submitted harus dinilai ulang (bukan nil)")
	}
	if scoreB != nil {
		t.Errorf("placeholder tidak boleh ditulis score (dapat %v) — nilai 'belum' jangan jadi 0", *scoreB)
	}
}
