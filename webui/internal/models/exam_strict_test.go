package models

import "testing"

// security_level="high" adalah kontrak mode ketat. Baris exam lama / respons
// API lama bisa punya high + strict_mode=0; IsStrict harus tetap true supaya
// klien Android tidak jatuh ke perilaku MEDIUM (auto-submit saat keluar).
func TestIsStrictTreatsHighAsStrict(t *testing.T) {
	cases := []struct {
		name string
		exam Exam
		want bool
	}{
		{"flag on", Exam{StrictMode: 1, SecurityLevel: "medium"}, true},
		{"high without flag", Exam{StrictMode: 0, SecurityLevel: "high"}, true},
		{"high with spaces/case", Exam{StrictMode: 0, SecurityLevel: " HIGH "}, true},
		{"medium without flag", Exam{StrictMode: 0, SecurityLevel: "medium"}, false},
		{"low", Exam{StrictMode: 0, SecurityLevel: "low"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.exam.IsStrict(); got != tc.want {
				t.Fatalf("IsStrict() = %v, want %v", got, tc.want)
			}
		})
	}
}
