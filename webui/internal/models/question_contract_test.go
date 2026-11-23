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
