package fileservice

import (
	"errors"
	"testing"
	"time"
)

func TestSchemeABearerContract(t *testing.T) {
	now := time.Date(2026, 9, 10, 4, 0, 0, 0, time.UTC)
	clock := &FixedClock{Time: now}
	store := NewGrantStore(clock, 2*time.Minute)
	head, _, err := store.Issue("owner/session", "f1", "HEAD", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Verify(head, "GET", "f1", "", now); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatal("HEAD token upgraded to GET")
	}
	get, _, err := store.Issue("owner/session", "不得记录这个原始值", "GET", "bytes=0-15", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// Verification has no host/device input: a copied token succeeds during TTL.
	if _, err = store.Verify(get, "GET", "不得记录这个原始值", "bytes=0-15", now); err != nil {
		t.Fatalf("copied bearer should work in scheme A: %v", err)
	}
	if _, err = store.Verify(get, "GET", "other", "bytes=0-15", now); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatal("token crossed object boundary")
	}
	if _, err = store.Verify(get, "GET", "不得记录这个原始值", "bytes=0-15", now.Add(time.Minute)); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatal("expired token accepted")
	}
	store.Revoke(head)
	if _, err = store.Verify(head, "HEAD", "f1", "", now); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatal("revoked token accepted")
	}
}
