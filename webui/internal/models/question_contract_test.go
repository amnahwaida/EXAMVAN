package models

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readContractFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "contracts", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return raw
}

func TestCanonicalizeQuestionsMatchesSharedFixture(t *testing.T) {
	var legacy []map[string]interface{}
	if err := json.Unmarshal(readContractFixture(t, "questions_v1_legacy.json"), &legacy); err != nil {
		t.Fatalf("decode legacy fixture: %v", err)
	}

	var want []map[string]interface{}
	if err := json.Unmarshal(readContractFixture(t, "questions_v1_canonical.json"), &want); err != nil {
		t.Fatalf("decode canonical fixture: %v", err)
	}

	got := CanonicalizeQuestions(legacy)
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal canonical: %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal expected: %v", err)
	}
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("canonical mismatch\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

func TestCanonicalizeQuestionsValueAcceptsJSONDecodedArray(t *testing.T) {
	var raw interface{}
	if err := json.Unmarshal(readContractFixture(t, "questions_v1_legacy.json"), &raw); err != nil {
		t.Fatalf("decode legacy fixture: %v", err)
	}
	got, ok := CanonicalizeQuestionsValue(raw)
	if !ok || len(got) != 4 {
		t.Fatalf("CanonicalizeQuestionsValue = %d questions, ok=%v", len(got), ok)
	}
	if got[0]["number"] != 1 || got[1]["number"] != 2 || got[2]["number"] != 3 {
		t.Fatalf("fallback numbering wrong: %#v", []interface{}{got[0]["number"], got[1]["number"], got[2]["number"]})
	}
}

func TestQuestionNumberRuleSharedWithAndroid(t *testing.T) {
	cases := []struct {
		name string
		in   interface{}
		want int
		ok   bool
	}{
		{"double truncates", 3.9, 3, true},
		{"numeric string", " 10 ", 10, true},
		{"decimal string rejected", "3.5", 0, false},
		{"non numeric rejected", "3A", 0, false},
		{"nil rejected", nil, 0, false},
		{"bool rejected", true, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := questionNumberInt(tc.in)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("questionNumberInt(%#v) = (%d, %v), want (%d, %v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestCanonicalizeQuestionsPartialScoringDefaults(t *testing.T) {
	legacy := []map[string]interface{}{
		{"number": 1, "type": "multiple_choice", "choices": []interface{}{"A", "B"}, "key": []interface{}{"A"}},
		{"number": 2, "type": "matching", "left_items": []interface{}{"Satu"}, "right_items": []interface{}{"A"}, "key": map[string]interface{}{"Satu": "A"}},
		{"number": 3, "type": "true_false", "statements": []interface{}{"Pernyataan"}, "key": []interface{}{"TRUE"}},
		{"number": 4, "type": "multiple_choice", "partial_scoring": false, "choices": []interface{}{"A", "B"}, "key": []interface{}{"A"}},
		{"number": 5, "type": "single_choice", "choices": []interface{}{"A", "B"}, "key": "A"},
	}
	got := CanonicalizeQuestions(legacy)
	for _, index := range []int{0, 1, 2} {
		if got[index]["partial_scoring"] != true {
			t.Fatalf("question %d should default partial_scoring=true, got %#v", index+1, got[index]["partial_scoring"])
		}
	}
	if got[3]["partial_scoring"] != false {
		t.Fatalf("explicit partial_scoring=false lost: %#v", got[3]["partial_scoring"])
	}
	if got[4]["partial_scoring"] != false {
		t.Fatalf("single_choice should not default to partial: %#v", got[4]["partial_scoring"])
	}
}

func TestCanonicalizeQuestionsShortAnswerMulti(t *testing.T) {
	legacy := []map[string]interface{}{
		{
			"number":       1,
			"type":         "short_answer",
			"statements":   []interface{}{" Ibu kota ", "Hasil"},
			"key":          []interface{}{" Jakarta ", "7"},
			"answer_order": "ANY",
		},
		{
			"number":    3,
			"type":      "short_answer",
			"key":       []interface{}{"A", "B"},
			"numbering": "range",
		},
	}
	got := CanonicalizeQuestions(legacy)
	if got[0]["partial_scoring"] != true {
		t.Fatalf("short_answer harus default partial: %#v", got[0]["partial_scoring"])
	}
	statements := questionStringList(got[0]["statements"])
	if len(statements) != 2 || statements[0] != "Ibu kota" || statements[1] != "Hasil" {
		t.Fatalf("statements short_answer salah: %#v", statements)
	}
	keys := questionStringList(got[0]["key"])
	if len(keys) != 2 || keys[0] != "Jakarta" || keys[1] != "7" {
		t.Fatalf("key short_answer salah: %#v", keys)
	}
	if got[0]["answer_order"] != "any" {
		t.Fatalf("answer_order short_answer hilang: %#v", got[0]["answer_order"])
	}
	if _, present := got[1]["answer_order"]; present {
		t.Fatalf("answer_order default ordered tidak boleh ditulis: %#v", got[1]["answer_order"])
	}
	if got[1]["numbering"] != "range" {
		t.Fatalf("numbering short_answer hilang: %#v", got[1]["numbering"])
	}
	generated := questionStringList(got[1]["statements"])
	if len(generated) != 2 || generated[0] != "Isian 1" || generated[1] != "Isian 2" {
		t.Fatalf("statements otomatis salah: %#v", generated)
	}
}

func TestCanonicalizeQuestionsTrueFalseLabels(t *testing.T) {
	legacy := []map[string]interface{}{
		{
			"number":      1,
			"type":        "true_false",
			"true_label":  " Sesuai ",
			"false_label": "Tidak Sesuai",
		},
	}
	got := CanonicalizeQuestions(legacy)
	if got[0]["true_label"] != "Sesuai" || got[0]["false_label"] != "Tidak Sesuai" {
		t.Fatalf("true/false labels not preserved: %#v", got[0])
	}
}

func TestCanonicalizeQuestionsMatchingNumbering(t *testing.T) {
	legacy := []map[string]interface{}{
		{
			"number":      1,
			"type":        "matching",
			"left_items":  []interface{}{"Satu", "Dua"},
			"right_items": []interface{}{"A", "B"},
			"key":         map[string]interface{}{"Satu": "A", "Dua": "B"},
		},
		{
			"number":      3,
			"type":        "matching",
			"left_items":  []interface{}{"Tiga"},
			"right_items": []interface{}{"C"},
			"key":         map[string]interface{}{"Tiga": "C"},
			"numbering":   "range",
		},
	}
	got := CanonicalizeQuestions(legacy)
	if got[0]["numbering"] != "single" {
		t.Fatalf("missing numbering should default to single, got %#v", got[0]["numbering"])
	}
	if got[1]["numbering"] != "range" {
		t.Fatalf("explicit range numbering lost, got %#v", got[1]["numbering"])
	}
}
