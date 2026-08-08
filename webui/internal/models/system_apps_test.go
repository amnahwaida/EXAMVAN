package models

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		name string
		a    string
		b    string
		want int
	}{
		// Basic numeric ordering
		{"equal", "2.4.0", "2.4.0", 0},
		{"newer major", "3.0.0", "2.9.9", 1},
		{"older major", "2.9.9", "3.0.0", -1},
		{"newer minor", "2.5.0", "2.4.9", 1},
		{"older minor", "2.4.9", "2.5.0", -1},
		{"newer patch", "2.4.1", "2.4.0", 1},
		{"older patch", "2.4.0", "2.4.1", -1},

		// Missing trailing parts are treated as 0 (Android padding parity).
		{"missing patch equals", "2.4", "2.4.0", 0},
		{"missing patch equals reverse", "2.4.0", "2.4", 0},
		{"missing patch older", "2.4", "2.4.1", -1},
		{"client partial equal required", "2.4", "2.4.0", 0},
		{"client one-part equal", "2", "2.0.0", 0},

		// Non-numeric suffixes are ignored.
		{"beta suffix", "2.4.1-beta", "2.4.1", 0},
		{"non-numeric minor", "2.x.1", "2.0.1", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CompareVersions(tc.a, tc.b); got != tc.want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestBestAndroidAppVersion(t *testing.T) {
	apps := []SystemApp{
		{Platform: "windows", Version: "9.9.9"},
		{Platform: "android", Version: "2.4.0"},
		{Platform: "android", Version: "2.4.5"},
		{Platform: "android", Version: "2.10.0"},
	}
	// Highest android version wins; non-android entries ignored.
	if got := BestAndroidAppVersion(apps); got != "2.10.0" {
		t.Errorf("BestAndroidAppVersion = %q, want %q", got, "2.10.0")
	}

	// 2.4 vs 2.4.0 must compare equal → the later-in-list 2.4.0 wins purely by
	// position, and 2.4.1 must still beat both.
	if got := CompareVersions("2.4", "2.4.0"); got != 0 {
		t.Errorf("padding equality broken: CompareVersions(2.4, 2.4.0) = %d, want 0", got)
	}
}
