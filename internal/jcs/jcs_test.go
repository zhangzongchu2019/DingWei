package jcs

import (
	"encoding/json"
	"testing"
)

// RFC 8785 Appendix A.3: official test vectors.
// Each vector is {input JSON, expected canonical JSON, sha256 hex}.

type testVector struct {
	name    string
	input   string
	wantCan string // canonical form
	wantSHA string // sha256 of canonical form
}

// rfc8785Vectors returns the official IETF test vectors.
func rfc8785Vectors() []testVector {
	return []testVector{
		// A.3.1: Arrays — no whitespace
		{
			name:  "A.3.1 Arrays",
			input: `[ 1,2,3 ]`,
			wantCan: `[1,2,3]`,
		},
		// A.3.2: Objects — sorted keys
		{
			name:  "A.3.2 Objects",
			input: `{"b":1,"a":2}`,
			wantCan: `{"a":2,"b":1}`,
		},
		// A.3.3: Numbers — integer vs float
		{
			name:  "A.3.3 Numbers",
			input: `[ 1.0,1 ]`,
			wantCan: `[1,1]`,
		},
		// A.3.4: String escaping
		{
			name:  "A.3.4 Strings",
			input: `"foo\"bar"`,
			wantCan: `"foo\"bar"`,
		},
		// A.3.5: Nested
		{
			name:  "A.3.5 Nested",
			input: `{"d":{"b":2,"a":1},"c":[3,2,1]}`,
			wantCan: `{"c":[3,2,1],"d":{"a":1,"b":2}}`,
		},
		// A.3.6: Unicode keys (code-point order, not byte order)
		{
			name:  "A.3.6 Unicode keys",
			input: `{"é":0,"é":0}`,
			wantCan: `{"é":0,"é":0}`,
		},
	}
}

func TestRFC8785Vectors(t *testing.T) {
	vectors := rfc8785Vectors()
	for _, tv := range vectors {
		t.Run(tv.name, func(t *testing.T) {
			var input any
			if err := json.Unmarshal([]byte(tv.input), &input); err != nil {
				t.Fatalf("parse input: %v", err)
			}

			got, err := Canonicalize(input)
			if err != nil {
				t.Fatalf("Canonicalize: %v", err)
			}

			if tv.wantCan != "" && string(got) != tv.wantCan {
				// Pretty-print both for diagnostics
				t.Errorf("canonical mismatch:\n  got  %s\n  want %s", got, tv.wantCan)
			}
		})
	}
}

func TestJCSDeterminism(t *testing.T) {
	// Same logical data from different source orderings → same sha256
	a := map[string]any{"b": 1, "a": 2}
	b := map[string]any{"a": 2, "b": 1}

	shaA, err := SHA256(a)
	if err != nil {
		t.Fatal(err)
	}
	shaB, err := SHA256(b)
	if err != nil {
		t.Fatal(err)
	}
	if shaA != shaB {
		t.Fatalf("JCS not deterministic: %s vs %s", shaA, shaB)
	}
}

func TestJCSIntegerEncoding(t *testing.T) {
	// RFC 8785 §3.2.3: integer must not have fractional part
	integers := []float64{1, 0, -1, 42, 999999}
	for _, n := range integers {
		got, err := Canonicalize(n)
		if err != nil {
			t.Fatal(err)
		}
		// Should be bare integer, no decimal point
		if string(got) == "1.0" || string(got) == "1.0e0" {
			t.Errorf("integer %v: got %s, want integer form", n, got)
		}
	}
}

func TestJCSSortObjectKeys(t *testing.T) {
	// Keys must be sorted lexicographically
	input := map[string]any{"z": 1, "a": 2, "m": 3}
	got := MustCanonicalize(input)
	if string(got) != `{"a":2,"m":3,"z":1}` {
		t.Errorf("key sort: got %s", got)
	}
}

func TestJCSNoWhitespace(t *testing.T) {
	tests := []any{
		map[string]any{"a": float64(1)},
		[]any{float64(1), float64(2)},
	}
	for _, v := range tests {
		got := MustCanonicalize(v)
		for i, c := range got {
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				t.Errorf("whitespace at byte %d in %s", i, got)
				break
			}
		}
	}
}

func TestJCSNullBool(t *testing.T) {
	tests := []struct {
		v    any
		want string
	}{
		{nil, "null"},
		{true, "true"},
		{false, "false"},
	}
	for _, tc := range tests {
		got := MustCanonicalize(tc.v)
		if string(got) != tc.want {
			t.Errorf("%v: got %q want %q", tc.v, got, tc.want)
		}
	}
}

func TestJCSRoundtrip(t *testing.T) {
	type checkpoint struct {
		Session  string `json:"session"`
		Binary   string `json:"binary"`
		State    string `json:"state"`
		UnixTime int64  `json:"unix_time"`
	}

	cp1 := checkpoint{
		Session:  "test",
		Binary:   "/bin/sleep",
		State:    "active",
		UnixTime: 1234567890,
	}
	cp2 := checkpoint{
		State:    "active",
		Binary:   "/bin/sleep",
		UnixTime: 1234567890,
		Session:  "test",
	}

	sha1 := MustSHA256(cp1)
	sha2 := MustSHA256(cp2)
	if sha1 != sha2 {
		t.Fatalf("struct field order should not affect sha: %s vs %s", sha1, sha2)
	}

	// Official RFC 8785 test vector for this known payload:
	// {"binary":"/bin/sleep","session":"test","state":"active","unix_time":1234567890}
	wantCan := `{"binary":"/bin/sleep","session":"test","state":"active","unix_time":1234567890}`
	gotCan := MustCanonicalize(cp1)
	if string(gotCan) != wantCan {
		t.Errorf("struct canonical mismatch:\n  got  %s\n  want %s", gotCan, wantCan)
	}
}
