package clock

import (
	"testing"
	"time"
)

func TestRealNow(t *testing.T) {
	r := Real{}
	before := time.Now()
	now := r.Now()
	after := time.Now()
	if now.Before(before) || now.After(after) {
		t.Fatalf("Real.Now()=%v, want between %v and %v", now, before, after)
	}
}

func TestFakeNow_ReturnsSetTime(t *testing.T) {
	ref := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	f := &Fake{T: ref}
	if got := f.Now(); !got.Equal(ref) {
		t.Fatalf("Fake.Now()=%v, want %v", got, ref)
	}
}

func TestFakeAdvance(t *testing.T) {
	ref := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	f := &Fake{T: ref}
	f.Advance(30 * time.Minute)
	if got := f.Now(); !got.Equal(ref.Add(30*time.Minute)) {
		t.Fatalf("after Advance = %v, want %v", got, ref.Add(30*time.Minute))
	}
}
