package trustedorigin

import "testing"

func TestParseListAndNormalize(t *testing.T) {
	got, err := ParseList(" HTTPS://DW123.CMJRT.COM:50443 , https://[2001:DB8::1]:8443 ")
	if err != nil || len(got) != 2 {
		t.Fatalf("ParseList() = %#v, %v", got, err)
	}
	if got[0] != (Origin{Scheme: "https", Host: "dw123.cmjrt.com", Port: "50443"}) || got[1] != (Origin{Scheme: "https", Host: "[2001:db8::1]", Port: "8443"}) {
		t.Fatalf("normalized origins = %#v", got)
	}
}

func TestParseListRejectsInvalidEntries(t *testing.T) {
	for _, raw := range []string{
		"   ", ",https://a.example:50443", "https://a.example:50443,", "https://a.example:50443, ", "https://a.example:50443,,https://b.example:50443",
		"https://a.example:50443,HTTPS://A.EXAMPLE:50443", "http://a.example:50443", "https://*.example:50443",
		"https://a.example:*", "https://a.example", "https://a.example:0443", "https://a.example:65536",
		"https://u@a.example:50443", "https://a.example:50443/path", "https://a.example:50443?q=x",
		"https://a.example:50443#x", "https://a.example:50443#", "https://a.example:0", "https://a.example:abc",
	} {
		if _, err := ParseList(raw); err == nil {
			t.Errorf("ParseList(%q) unexpectedly succeeded", raw)
		}
	}
	if got, err := ParseList(""); err != nil || len(got) != 0 {
		t.Fatalf("empty list = %#v, %v", got, err)
	}
}

func TestAllowsLegacyEmptyAndExactNormalizedList(t *testing.T) {
	list, err := ParseList("https://dw123.cmjrt.com:50443")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		raw    string
		list   []Origin
		legacy bool
		want   bool
	}{
		{"legacy exact", "https://dw123.cmjrt.com", nil, true, true},
		{"legacy no normalize", "HTTPS://DW123.CMJRT.COM", nil, false, false},
		{"legacy lenient predicate", "https://dw123.cmjrt.com:50444", nil, true, true},
		{"canonical in list", "https://dw123.cmjrt.com", list, true, true},
		{"allowlist hit", "HTTPS://DW123.CMJRT.COM:50443", list, false, true},
		{"different port", "https://dw123.cmjrt.com:50444", list, false, false},
		{"http", "http://dw123.cmjrt.com:50443", list, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Allows(tc.raw, "https://dw123.cmjrt.com", tc.list, tc.legacy, 1); got != tc.want {
				t.Fatalf("Allows(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
	if Allows("https://dw123.cmjrt.com:50443", "https://dw123.cmjrt.com", list, false, 2) {
		t.Fatal("multiple Origin header values accepted")
	}
	if Allows("https://dw123.cmjrt.com:50443,https://evil.example", "https://dw123.cmjrt.com", list, false, 1) {
		t.Fatal("comma-separated Origin accepted")
	}
}

func TestAuthorityMatchesPreservesLegacyAndNormalizesConfiguredHost(t *testing.T) {
	if AuthorityMatches("DW123.CMJRT.COM:50443", "dw123.cmjrt.com:50443", false) {
		t.Fatal("legacy comparison must remain exact")
	}
	if !AuthorityMatches("DW123.CMJRT.COM:50443", "dw123.cmjrt.com:50443", true) {
		t.Fatal("configured comparison should normalize host case")
	}
	if AuthorityMatches("dw123.cmjrt.com:50443", "dw123.cmjrt.com:50444", true) {
		t.Fatal("port mismatch accepted")
	}
}
