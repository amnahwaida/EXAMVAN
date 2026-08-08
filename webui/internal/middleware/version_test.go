package middleware

import "testing"

func TestIsVersionCompatible(t *testing.T) {
	cases := []struct {
		name     string
		required string
		client   string
		want     bool
	}{
		{"same version", "2.4.0", "2.4.0", true},
		{"client newer", "2.4.0", "2.5.0", true},
		{"client older", "2.4.0", "2.3.9", false},
		{"older major", "2.4.0", "1.9.9", false},
		// Padding: the Android client reports "2.4" when the required is "2.4.0"
		// — must be compatible (2.4 == 2.4.0), matching UpdateManager.
		{"client partial equal", "2.4.0", "2.4", true},
		{"required partial equal", "2.4", "2.4.0", true},
		{"one-part equal", "2.0.0", "2", true},
		{"one-part newer", "2.0.0", "3", true},
		{"one-part older", "2.0.0", "1", false},
		// Non-numeric segments treated as 0 in both, so 2.x.1 == 2.0.1.
		{"alpha client", "2.0.0", "2.0.0-alpha", true},
		{"x minor client", "2.0.1", "2.x.1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isVersionCompatible(tc.required, tc.client); got != tc.want {
				t.Errorf("isVersionCompatible(req=%q, client=%q) = %v, want %v",
					tc.required, tc.client, got, tc.want)
			}
		})
	}
}