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

func TestEffectiveAndroidRequiredVersionFrom(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		apps       []SystemApp
		want       string
	}{
		// Nothing published → nothing to enforce (no deadlock: an outdated
		// client would be blocked with 426 but have no APK to download).
		{"no apps", "2.2.0", nil, ""},
		{"empty apps list", "2.2.0", []SystemApp{}, ""},
		{"only non-android apps", "2.2.0", []SystemApp{
			{Platform: "windows", Version: "9.9.9"},
			{Platform: "linux", Version: "1.0.0"},
		}, ""},

		// Configured is empty → the available APK becomes the requirement.
		{"configured empty falls back to available", "", []SystemApp{
			{Platform: "android", Version: "2.4.0"},
		}, "2.4.0"},

		// Configured higher than available → clamped to available (an admin who
		// raises android_version before uploading the APK must not lock clients
		// out beyond what is downloadable).
		{"configured above available clamps down", "2.5.0", []SystemApp{
			{Platform: "android", Version: "2.4.0"},
		}, "2.4.0"},
		{"configured far above available clamps down", "10.0.0", []SystemApp{
			{Platform: "android", Version: "2.4.0"},
		}, "2.4.0"},

		// Configured lower than available → the (stricter) configured minimum
		// wins; clients between configured and available still pass.
		{"configured below available is kept", "2.2.0", []SystemApp{
			{Platform: "android", Version: "2.4.0"},
		}, "2.2.0"},

		// Configured equal to the highest available → kept as-is.
		{"configured equals available", "2.4.0", []SystemApp{
			{Platform: "android", Version: "2.4.0"},
		}, "2.4.0"},

		// Highest-version Android entry wins the clamp target; non-android
		// entries and lower Android versions are ignored.
		{"highest available wins when configured above all", "2.10.0", []SystemApp{
			{Platform: "android", Version: "2.4.0"},
			{Platform: "android", Version: "2.10.0"},
			{Platform: "windows", Version: "9.9.9"},
		}, "2.10.0"},
		{"highest available wins when configured empty", "", []SystemApp{
			{Platform: "android", Version: "2.4.0"},
			{Platform: "android", Version: "2.4.5"},
		}, "2.4.5"},

		// Version padding: "2.4" == "2.4.0", so a configured "2.4" is NOT
		// above the available "2.4.0" and is kept (not clamped).
		{"configured partial equals available", "2.4", []SystemApp{
			{Platform: "android", Version: "2.4.0"},
		}, "2.4"},

		// Non-numeric configured segment parses as 0, so it is never above the
		// available version and is kept as the requirement.
		{"configured x segment is not clamped", "2.x", []SystemApp{
			{Platform: "android", Version: "2.4.0"},
		}, "2.x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveAndroidRequiredVersionFrom(tc.configured, tc.apps); got != tc.want {
				t.Errorf("EffectiveAndroidRequiredVersionFrom(%q, %+v) = %q, want %q",
					tc.configured, tc.apps, got, tc.want)
			}
		})
	}
}
