package admin

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// M6 — validateQuestionKeys harus menolak bentuk soal yang tidak bisa
// dihitung grader.
//
// Yang lolos dulu (dan TIDAK bisa direpresentasikan di
// `models.CalculateSubmissionScore`):
//
//   1. single_choice dengan key yang bukan salah satu `options`-nya
//   2. single_choice tanpa `options` sama sekali
//   3. `options` bukan list (object, string, angka)
//   4. `options` list berisi non-string / string kosong
//   5. multiple_choice dengan key angka
//   6. multiple_choice dengan key null
//   7. multiple_choice dengan key object (nested)
//   8. multiple_choice dengan key duplikat ("A","A")
//   9. matching dengan nilai non-string / string kosong
//  10. nomor soal negatif / nol / astronomis
//
// Semuanya akan tersimpan lalu dinilai 0 ATAU — lebih buruk — dipakai guru
// saat membuat kunci baru. Yang paling berbahaya adalah duplikat: siswa
// memilih satu, jawaban tercatat dua (atau vice versa) dan rekap nilai
// tidak bisa dipercaya.
// ---------------------------------------------------------------------------

func q(num interface{}, qtype string, key interface{}, extra map[string]interface{}) map[string]interface{} {
	m := map[string]interface{}{"number": num, "type": qtype, "key": key}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestValidateQuestionKeysRejectsUnrepresentableSingleChoice(t *testing.T) {
	cases := []struct {
		name    string
		opts    interface{}
		key     interface{}
		wantSub string
	}{
		{
			"key bukan salah satu options",
			[]interface{}{"A", "B"}, "C", "pilihan",
		},
		{
			"tanpa options sama sekali",
			nil, "A", "pilihan",
		},
		{
			"options kosong",
			[]interface{}{}, "A", "pilihan",
		},
		{
			"options bukan list (object)",
			map[string]interface{}{"a": "A"}, "A", "pilihan",
		},
		{
			"options bukan list (string)",
			"AB", "A", "pilihan",
		},
		{
			"options berisi angka",
			[]interface{}{"A", float64(2)}, "A", "pilihan",
		},
		{
			"options berisi string kosong",
			[]interface{}{"A", "   "}, "A", "pilihan",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			extra := map[string]interface{}{}
			if tc.opts != nil {
				extra["options"] = tc.opts
			}
			msg := validateQuestionKeys([]map[string]interface{}{
				q(1, "single_choice", tc.key, extra),
			})
			if msg == "" {
				t.Fatalf("diterima: options=%#v key=%#v", tc.opts, tc.key)
			}
			if !strings.Contains(msg, tc.wantSub) {
				t.Fatalf("pesan harus menyebut %q, dapat: %s", tc.wantSub, msg)
			}
		})
	}
}

func TestValidateQuestionKeysRejectsUnrepresentableMultipleChoice(t *testing.T) {
	opts := []interface{}{"A", "B", "C"}
	cases := []struct {
		name string
		key  interface{}
	}{
		{"key angka", []interface{}{float64(1)}},
		{"key null", []interface{}{nil}},
		{"key object", []interface{}{map[string]interface{}{"label": "A"}}},
		{"key duplikat", []interface{}{"A", "A"}},
		{"key duplikat berbeda huruf besar-kecil", []interface{}{"A", "a"}},
		{"key string kosong", []interface{}{""}},
		{"key string whitespace", []interface{}{"  "}},
		{"key non-list (object)", map[string]interface{}{"A": true}},
		{"key kosong tapi options ada", []interface{}{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := validateQuestionKeys([]map[string]interface{}{
				q(1, "multiple_choice", tc.key, map[string]interface{}{"options": opts}),
			})
			if msg == "" {
				t.Fatalf("diterima: key=%#v", tc.key)
			}
		})
	}
}

func TestValidateQuestionKeysMultipleChoiceKeyOutsideOptionsIsRejected(t *testing.T) {
	msg := validateQuestionKeys([]map[string]interface{}{
		q(1, "multiple_choice", []interface{}{"A", "Z"},
			map[string]interface{}{"options": []interface{}{"A", "B"}}),
	})
	if msg == "" {
		t.Fatal("kunci di luar options harus ditolak — tidak ada jawaban yang bisa benar")
	}
}

func TestValidateQuestionKeysRejectsUnrepresentableMatching(t *testing.T) {
	cases := []struct {
		name string
		key  interface{}
	}{
		{"nilai angka", map[string]interface{}{"kiri": float64(1)}},
		{"nilai null", map[string]interface{}{"kiri": nil}},
		{"nilai string kosong", map[string]interface{}{"kiri": ""}},
		{"nilai whitespace", map[string]interface{}{"kiri": "   "}},
		{"nilai object", map[string]interface{}{"kiri": map[string]interface{}{"a": "b"}}},
		{"salah satu nilai rusak", map[string]interface{}{"kiri": "kanan", "kanan2": float64(2)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := validateQuestionKeys([]map[string]interface{}{
				q(1, "matching", tc.key, nil),
			})
			if msg == "" {
				t.Fatalf("diterima: key=%#v", tc.key)
			}
		})
	}
}

func TestValidateQuestionKeysRejectsImpossibleQuestionNumbers(t *testing.T) {
	cases := []struct {
		name string
		num  interface{}
	}{
		{"nol", float64(0)},
		{"negatif", float64(-1)},
		{"sangat besar", float64(1e15)},
		{"string negatif", "-3"},
		{"string nol", "0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := validateQuestionKeys([]map[string]interface{}{
				q(tc.num, "short_answer", "jawaban", nil),
			})
			if msg == "" {
				t.Fatalf("nomor %#v diterima", tc.num)
			}
		})
	}
}

func TestValidateQuestionKeysStillAcceptsValidConfigs(t *testing.T) {
	questions := []map[string]interface{}{
		q(1, "single_choice", "A", map[string]interface{}{"options": []interface{}{"A", "B"}}),
		q(2, "multiple_choice", []interface{}{"A", "C"},
			map[string]interface{}{"options": []interface{}{"A", "B", "C"}}),
		q(3, "true_false", "true", nil),
		q(4, "short_answer", "Jakarta", nil),
		q(5, "matching", map[string]interface{}{"a": "x", "b": "y"}, nil),
	}
	if msg := validateQuestionKeys(questions); msg != "" {
		t.Fatalf("config valid ditolak: %s", msg)
	}
}

func TestValidateQuestionKeysErrorNamesTheQuestion(t *testing.T) {
	questions := []map[string]interface{}{
		q(1, "short_answer", "Jakarta", nil),
		q(2, "single_choice", "C", map[string]interface{}{"options": []interface{}{"A", "B"}}),
	}
	msg := validateQuestionKeys(questions)
	if !strings.Contains(msg, "#2") {
		t.Fatalf("pesan harus menyebut soal ke-2, dapat: %s", msg)
	}
}

func TestValidateQuestionKeysErrorKeepsExistingWording(t *testing.T) {
	// Pesan yang sudah ada tidak boleh berubah — frontend/admin.js dan
	// test lain bergantung pada;subtest ini menjaga kontraknya.
	if msg := validateQuestionKeys([]map[string]interface{}{
		q(1, "esai", "apa saja", nil),
	}); !strings.Contains(msg, "tipe soal yang tidak valid") {
		t.Fatalf("pesan tipe soal berubah: %s", msg)
	}
	if msg := validateQuestionKeys([]map[string]interface{}{
		q(1, "short_answer", nil, nil),
	}); !strings.Contains(msg, "belum memiliki kunci jawaban") {
		t.Fatalf("pesan kunci kosong berubah: %s", msg)
	}
	if msg := validateQuestionKeys([]map[string]interface{}{
		q(1, "single_choice", "A", map[string]interface{}{"options": []interface{}{"A"}}),
		q(1, "short_answer", "x", nil),
	}); !strings.Contains(msg, "nomor soal yang sama") {
		t.Fatalf("pesan nomor kembar berubah: %s", msg)
	}
	if msg := validateQuestionKeys([]map[string]interface{}{
		q("abc", "short_answer", "x", nil),
	}); !strings.Contains(msg, "nomor soal yang valid") {
		t.Fatalf("pesan nomor tidak valid berubah: %s", msg)
	}
}
