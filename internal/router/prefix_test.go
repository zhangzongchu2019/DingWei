package router

import "testing"

func TestMatchPrefixWildcardQuestion(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{text: "openAPI", want: true},
		{text: "openAPI1", want: true},
		{text: "openAPI12", want: true},
		{text: "openAPI123", want: true},
		{text: "openAPI1234", want: true},
		{text: "openAP", want: false},
	}
	for _, tc := range cases {
		if got := MatchPrefix("openAPI???", tc.text, true); got != tc.want {
			t.Fatalf("MatchPrefix(%q)=%v want %v", tc.text, got, tc.want)
		}
	}
}

func TestMatchPrefixLenStripsWildcardDeterministically(t *testing.T) {
	ok, n := MatchPrefixLen("bot???", "bot123 payload", true)
	if !ok || "bot123 payload"[n:] != " payload" {
		t.Fatalf("bot??? len ok=%v n=%d rest=%q", ok, n, "bot123 payload"[n:])
	}
	ok, n = MatchPrefixLen("open*", "open payload", true)
	if !ok || "open payload"[n:] != " payload" {
		t.Fatalf("open* len ok=%v n=%d rest=%q", ok, n, "open payload"[n:])
	}
}

func TestOverlapsWildcardMatrix(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{a: "openAPI???", b: "openAPI1", want: true},
		{a: "open*", b: "openAPI", want: true},
		{a: "codex;openAPI???", b: "home", want: false},
		{a: "a?b", b: "axc", want: false},
		{a: "a?b", b: "ax", want: true},
		{a: "Bot", b: "bot", want: false},
	}
	for _, tc := range cases {
		if got := Overlaps(tc.a, tc.b, true); got != tc.want {
			t.Fatalf("Overlaps(%q,%q)=%v want %v", tc.a, tc.b, got, tc.want)
		}
	}
	if !Overlaps("Bot", "bot", false) {
		t.Fatalf("case-insensitive overlap not detected")
	}
}
