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
	if _, _, err = store.Issue("owner/session", "f1", "GET", "", time.Minute); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatal("GET capability issued without a range")
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

func TestUploadTicketIsSeparateBoundAndSingleUse(t *testing.T) {
	now := time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC)
	clock := &FixedClock{Time: now}
	store := NewUploadTicketStore(clock, 2*time.Minute)
	token, grant, err := store.Issue("owner", "session", 16, time.Minute)
	if err != nil || grant.MaxBytes != 16 {
		t.Fatalf("issue grant=%+v err=%v", grant, err)
	}
	if _, err := store.VerifyUpload(token, "other", 8, now); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatal("ticket crossed session boundary")
	}
	if _, err := store.VerifyUpload(token, "session", 17, now); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatal("ticket exceeded size boundary")
	}
	if _, err := store.VerifyUpload(token, "session", 8, now); err != nil {
		t.Fatalf("valid ticket failed: %v", err)
	}
	if _, err := store.VerifyUpload(token, "session", 8, now); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatal("upload ticket replay succeeded")
	}
	expired, _, err := store.Issue("owner", "session", 16, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.VerifyUpload(expired, "session", 8, now.Add(time.Second)); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatal("expired upload ticket succeeded")
	}
}
