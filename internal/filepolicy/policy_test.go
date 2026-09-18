package filepolicy

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestUploadLimitMatchesNginx(t *testing.T) {
	if MaxUploadBytes != 52_428_800 {
		t.Fatalf("MaxUploadBytes=%d", MaxUploadBytes)
	}
	raw, err := os.ReadFile("../../deploy/nginx-dingwei.conf")
	if err != nil {
		t.Fatal(err)
	}
	const directive = "client_max_body_size 50m;"
	if count := strings.Count(string(raw), directive); count != 1 {
		t.Fatalf("%q count=%d", directive, count)
	}
}

func TestDirectLinkExtensionAllowListsExcludeActiveContent(t *testing.T) {
	nginx := mustRead(t, "../../deploy/nginx-dingwei.conf")
	store := mustRead(t, "../fileservice/local_store.go")
	assertForbiddenExtensionsAbsent(t, "nginx file_ext", captureExtensions(t, nginx, `\(\?<file_ext>([^)]+)\)`))
	assertForbiddenExtensionsAbsent(t, "directObjectKey", captureExtensions(t, store, `directObjectKey\s*=.*?\\\.\(\?:([^)]*)\)`))
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func captureExtensions(t *testing.T, raw, pattern string) []string {
	t.Helper()
	match := regexp.MustCompile(pattern).FindStringSubmatch(raw)
	if len(match) != 2 {
		t.Fatalf("extension allowlist not found with %q", pattern)
	}
	return strings.Split(match[1], "|")
}

func assertForbiddenExtensionsAbsent(t *testing.T, source string, extensions []string) {
	t.Helper()
	forbidden := map[string]bool{"svg": true, "svgz": true, "html": true, "htm": true, "xhtml": true, "xml": true}
	for _, extension := range extensions {
		if forbidden[extension] {
			t.Fatalf("%s permits active-content extension %q", source, extension)
		}
	}
}

func TestDefaultRetentionMatchesContract(t *testing.T) {
	if DefaultRetention != 7*24*time.Hour {
		t.Fatalf("DefaultRetention=%s", DefaultRetention)
	}
}

func TestDeploymentContractMatchesFilePolicy(t *testing.T) {
	contract := mustRead(t, "../../deploy/H-FILE-SERVICE-DEPLOYMENT.md")
	for _, setting := range []string{
		"FS_RETENTION=168h",
		"FS_MAX_FILE_BYTES=52428800",
	} {
		if count := strings.Count(contract, setting); count != 1 {
			t.Fatalf("deployment setting %q count=%d", setting, count)
		}
	}
}
