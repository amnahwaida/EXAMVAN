package models

import "testing"

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
