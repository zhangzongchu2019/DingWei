package hubdriver

import (
	"strings"
	"testing"
)

// M3-TC4:error_class 值域。
func TestReceiptErrorClass(t *testing.T) {
	base := `{"kind":"mcs_receipt_v1","node_id":"n1","rid":"r1","phase":"done","ok":false,"from":"w1"`
	for _, tc := range []struct {
		name    string
		extra   string
		wantErr bool
		fatal   bool
	}{
		{"retryable", `,"error_class":"retryable"}`, false, false},
		{"fatal", `,"error_class":"fatal"}`, false, true},
		{"bogus", `,"error_class":"bogus"}`, true, false},
		{"absent", ``, false, false},
	} {
		r, err := ParseReceipt([]byte(base + tc.extra + `}`))
		if tc.wantErr && err == nil {
			t.Fatalf("%s: bogus error_class accepted", tc.name)
		}
		if !tc.wantErr {
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if tc.fatal && r.RetryableFailure() {
				t.Fatalf("%s: fatal must not retry", tc.name)
			}
			if !tc.fatal && !r.RetryableFailure() {
				t.Fatalf("%s: should be retryable", tc.name)
			}
		}
	}
}

// M3-TC5:ParseReceiptStrict 严格模式。
func TestParseReceiptStrict(t *testing.T) {
	good := `{"kind":"mcs_receipt_v1","node_id":"n1","rid":"r1","phase":"done","ok":true,"from":"w1"}`
	if _, err := ParseReceiptStrict([]byte(good)); err != nil {
		t.Fatalf("clean receipt rejected: %v", err)
	}
	dirty := good + "\n2026-07-27 timestamp junk"
	if _, err := ParseReceipt([]byte(dirty)); err != nil {
		t.Fatalf("prod-tolerant must accept trailing junk: %v", err)
	}
	if _, err := ParseReceiptStrict([]byte(dirty)); err == nil {
		t.Fatalf("strict must reject trailing junk")
	}
	if _, err := ParseReceiptStrict([]byte(`{"kind":`)); err == nil {
		t.Fatalf("strict must reject broken first object")
	}
	if _, err := ParseReceiptStrict([]byte(good + `{"more":1}`)); err == nil {
		t.Fatalf("strict must reject trailing second object")
	}
	if _, err := ParseReceiptStrict([]byte(good + "\n")); err != nil {
		t.Fatalf("strict must accept trailing whitespace: %v", err)
	}
	_ = strings.TrimSpace("")
}
