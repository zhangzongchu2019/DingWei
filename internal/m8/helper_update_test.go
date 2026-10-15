package m8

import (
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

func TestHelperUpdateKeysAreNamespaced(t *testing.T) {
	sha := strings.Repeat("b", 64)
	if got, want := helperUpdateCatalogKey(strings.ToUpper(sha)), helperUpdateCatalogPrefix+sha; got != want {
		t.Fatalf("catalog key=%q want %q", got, want)
	}
	if got, want := helperUpdateJobKey("agent-1"), "helper_update.job.agent-1"; got != want {
		t.Fatalf("job key=%q want %q", got, want)
	}
}
