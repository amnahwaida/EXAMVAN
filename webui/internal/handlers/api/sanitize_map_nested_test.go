package api

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// sanitizeMap hanya menyalin nilai string di level atas apa adanya; map
// bersarang dan slice lewat tanpa dibersihkan. Jadi
//
//	{"a": {"b": "‮evil‬"}}
//
// tersimpan apa adanya ke exam_submissions.identity_data dan dirender di
// halaman hasil — kebocoran yang sama seperti pada sanitize() (item di atas),
// hanya lewat satu tingkat nesting.
//
// Yang disyaratkan tes: SEMUA string di kedalaman APUN harus dibersihkan, dan
// nilai non-string (angka, bool, nil) harus tetap utuh supaya tipe JSON tidak
// berubah.
// ---------------------------------------------------------------------------

func TestSanitizeMapRecursesIntoNestedMaps(t *testing.T) {
	in := map[string]interface{}{
		"a": map[string]interface{}{
			"b": "‮evil‬",
		},
	}
	out := sanitizeMap(in)

	nested, ok := out["a"].(map[string]interface{})
	if !ok {
		t.Fatalf("out[\"a\"] = %T, want map[string]interface{} (tipe harus dipertahankan)", out["a"])
	}
	if got := nested["b"]; got != "evil" {
		t.Fatalf("out[\"a\"][\"b\"] = %q, want %q — map bersarang tidak boleh dilewati", got, "evil")
	}
}

// TestSanitizeMapRecursesIntoSlices: slice string harus ikut dibersihkan,
// termasuk slice di dalam map bersarang.
func TestSanitizeMapRecursesIntoSlices(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]interface{}
		want interface{}
	}{
		{
			name: "slice di level atas",
			in:   map[string]interface{}{"a": []interface{}{"‮evil‬"}},
			want: []interface{}{"evil"},
		},
		{
			name: "slice di dalam map",
			in:   map[string]interface{}{"a": map[string]interface{}{"xs": []interface{}{"‮evil‬"}}},
			want: map[string]interface{}{"xs": []interface{}{"evil"}},
		},
		{
			name: "slice dalam slice",
			in:   map[string]interface{}{"a": []interface{}{[]interface{}{"‮evil‬"}}},
			want: []interface{}{[]interface{}{"evil"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeMap(tc.in)
			if !deepEqual(got["a"], tc.want) {
				t.Fatalf("out[\"a\"] = %#v, want %#v", got["a"], tc.want)
			}
		})
	}
}

// TestSanitizeMapKeepsNonStringValues: angka, bool dan nil harus tetap dengan
// tipe aslinya — membersihkan tidak boleh mengubah bentuk JSON yang disimpan.
func TestSanitizeMapKeepsNonStringValues(t *testing.T) {
	in := map[string]interface{}{
		"num":  42,
		"fl":   1.5,
		"ok":   true,
		"nil":  nil,
		"nul":  []interface{}{1, nil, "‮Andi‬"},
		"deep": map[string]interface{}{"n": 7},
	}
	out := sanitizeMap(in)

	if out["num"] != 42 {
		t.Fatalf("num = %#v, want 42 (int)", out["num"])
	}
	if out["fl"] != 1.5 {
		t.Fatalf("fl = %#v, want 1.5", out["fl"])
	}
	if out["ok"] != true {
		t.Fatalf("ok = %#v, want true", out["ok"])
	}
	if out["nil"] != nil {
		t.Fatalf("nil = %#v, want nil", out["nil"])
	}
	xs, ok := out["nul"].([]interface{})
	if !ok || len(xs) != 3 || xs[0] != 1 || xs[1] != nil || xs[2] != "Andi" {
		t.Fatalf("nul = %#v, want [1 nil \"Andi\"]", out["nul"])
	}
	deep, ok := out["deep"].(map[string]interface{})
	if !ok || deep["n"] != 7 {
		t.Fatalf("deep = %#v, want map dengan n=7", out["deep"])
	}
}

// TestSanitizeMapTruncatesNestedStrings: pemotongan 200 rune harus berlaku di
// semua kedalaman, bukan hanya di level atas.
func TestSanitizeMapTruncatesNestedStrings(t *testing.T) {
	long := strings.Repeat("a", 250)
	in := map[string]interface{}{
		"top":    long,
		"nested": map[string]interface{}{"s": long},
		"sliced": []interface{}{long},
	}
	out := sanitizeMap(in)

	if got := len([]rune(out["top"].(string))); got != 200 {
		t.Fatalf("top = %d rune, want 200", got)
	}
	nested := out["nested"].(map[string]interface{})
	if got := len([]rune(nested["s"].(string))); got != 200 {
		t.Fatalf("nested.s = %d rune, want 200", got)
	}
	xs := out["sliced"].([]interface{})
	if got := len([]rune(xs[0].(string))); got != 200 {
		t.Fatalf("sliced[0] = %d rune, want 200", got)
	}
}

// deepEqual membandingkan nilai yang sudah disanitasi dengan []interface{}
// dan map[string]interface{} di dalamnya.
func deepEqual(got, want interface{}) bool {
	switch w := want.(type) {
	case string:
		g, ok := got.(string)
		return ok && g == w
	case []interface{}:
		g, ok := got.([]interface{})
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !deepEqual(g[i], w[i]) {
				return false
			}
		}
		return true
	case map[string]interface{}:
		g, ok := got.(map[string]interface{})
		if !ok || len(g) != len(w) {
			return false
		}
		for k, v := range w {
			if !deepEqual(g[k], v) {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}
