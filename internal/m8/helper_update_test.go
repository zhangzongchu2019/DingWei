package m8

import (
	"context"
	"strings"
	"testing"
)

func TestHelperUpdateCatalogValidation(t *testing.T) {
	valid := strings.Repeat("a", 64)
	catalog := helperUpdateCatalog{SHA256: valid, LockSHA256: valid, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", Platform: "linux"}
	if err := validateHelperUpdateCatalog(catalog); err != nil {
		t.Fatalf("valid catalog: %v", err)
	}
	catalog.SHA256 = "not-a-digest"
	if err := validateHelperUpdateCatalog(catalog); err == nil {
		t.Fatal("invalid artifact digest accepted")
	}
	catalog.SHA256 = valid
	catalog.Platform = "darwin"
	if err := validateHelperUpdateCatalog(catalog); err == nil {
		t.Fatal("non-linux catalog accepted")
	}
}

func TestHelperUpdateCatalogAndJobAreDurableAndSingleUse(t *testing.T) {
	h, _, ctx := newTestHub(t)
	digest := strings.Repeat("c", 64)
	catalog := helperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", Platform: "linux"}
	if err := h.RegisterHelperUpdateCatalog(ctx, "ops", catalog); err != nil {
		t.Fatal(err)
	}
	if err := h.RegisterHelperUpdateCatalog(ctx, "ops", catalog); err == nil {
		t.Fatal("catalog registration replay accepted")
	}
	got, err := h.helperUpdateCatalog(ctx, digest)
	if err != nil || got == nil || got.Version != catalog.Version {
		t.Fatalf("catalog=%+v err=%v", got, err)
	}
	job, err := h.createHelperUpdateJob(context.Background(), *got, "agent-1", "owner", "agent", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if job.Stage != "offered" || job.Revision != 1 || job.ExpiresAt.IsZero() {
		t.Fatalf("job=%+v", job)
	}
	if _, err := h.createHelperUpdateJob(ctx, *got, "agent-1", "owner", "agent", "owner"); err == nil {
		t.Fatal("second active job accepted")
	}
	stored, _, err := h.helperUpdateJob(ctx, "agent-1")
	if err != nil || stored == nil || stored.JobID != job.JobID {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

func TestHelperUpdateKeysAreNamespaced(t *testing.T) {
	sha := strings.Repeat("b", 64)
	if got, want := helperUpdateCatalogKey(strings.ToUpper(sha)), helperUpdateCatalogPrefix+sha; got != want {
		t.Fatalf("catalog key=%q want %q", got, want)
	}
	if got, want := helperUpdateJobKey("agent-1"), "helper_update.job.agent-1"; got != want {
		t.Fatalf("job key=%q want %q", got, want)
	}
}
