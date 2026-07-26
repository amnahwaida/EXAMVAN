package models

import "testing"

func TestEmailDomainAllowed(t *testing.T) {
	const wl = "gmail.com, sch.id, ac.id"

	cases := []struct {
		name  string
		wl    string
		email string
		want  bool
	}{
		{"empty whitelist allows anything", "", "anyone@whatever.io", true},
		{"exact match", wl, "budi@gmail.com", true},
		{"exact match case-insensitive", wl, "Budi@GMail.Com", true},
		{"subdomain of sch.id allowed", wl, "guru@smanegeri1.sch.id", true},
		{"deep subdomain allowed", wl, "a@mail.kampus.ac.id", true},
		{"not in whitelist rejected", wl, "user@outlook.com", false},
		// Security: suffix must be anchored on a dot — these must NOT match.
		{"lookalike suffix rejected", wl, "user@notgmail.com", false},
		{"lookalike tld rejected", wl, "user@evilsch.id", false},
		{"bare entry not matched as suffix", wl, "user@id", false},
		{"no @ rejected", wl, "notanemail", false},
		{"empty domain rejected", wl, "user@", false},
		{"whitespace/format-tolerant whitelist", "  GMAIL.COM ;\n @yahoo.com , *.sch.id ", "x@yahoo.com", true},
		{"wildcard-style entry matches subdomain", "*.sch.id", "x@a.sch.id", true},
	}
	for _, c := range cases {
		if got := EmailDomainAllowed(c.wl, c.email); got != c.want {
			t.Errorf("%s: EmailDomainAllowed(%q, %q) = %v, want %v", c.name, c.wl, c.email, got, c.want)
		}
	}
}

func TestParseDomainList(t *testing.T) {
	got := ParseDomainList("Gmail.com, @Yahoo.com;\n *.sch.id\t, , ac.id.")
	want := []string{"gmail.com", "yahoo.com", "sch.id", "ac.id"}
	if len(got) != len(want) {
		t.Fatalf("ParseDomainList len = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ParseDomainList[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
