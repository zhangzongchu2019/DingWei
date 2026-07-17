package redact

import (
	"strings"
	"testing"
)

func TestContentRedactsSecretsAndPhone(t *testing.T) {
	in := `{"token":"abc123","password":"pw","api_key":"sk-1234567890abcd","text":"Bearer abc.def.ghi phone 13800138000"}`
	out := Content(in)
	for _, forbidden := range []string{"abc123", `"pw"`, "sk-1234567890abcd", "abc.def.ghi", "13800138000"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("redaction leaked %q in %s", forbidden, out)
		}
	}
	for _, want := range []string{"token", "***", "Bearer ***", "1**********"} {
		if !strings.Contains(out, want) {
			t.Fatalf("redaction missing %q in %s", want, out)
		}
	}
}
