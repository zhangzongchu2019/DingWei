package m8

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	viewservice "github.com/zhangzongchu2019/dingwei/view-service"
)

func TestConsoleSpecAssetsArePinnedAndRouteIsAllowlisted(t *testing.T) {
	wants := map[string]string{
		"easymde.min.js":  "2c06bddfd0c89176db08ccf9d42e2beaa9b4f1a4ff8fb2ec0bf1bed25ce08e05",
		"easymde.min.css": "6eee36340432776d682e7372ec4a7eb29be4fdb3ffada32c0bfa5e31a5ef34a2",
		"LICENSE":         "98eee44a1cdad5ad0bb0ea976791ab57b5eef7ac4cafeb162f30e3808564a564",
	}
	for name, want := range wants {
		req := httptest.NewRequest(http.MethodGet, "/console/spec-assets/"+name, nil)
		req.SetPathValue("name", name)
		rr := httptest.NewRecorder()
		new(Hub).HandleConsoleSpecAsset(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("asset %s code=%d", name, rr.Code)
		}
		sum := sha256.Sum256(rr.Body.Bytes())
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Fatalf("asset %s sha=%s want=%s", name, got, want)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/console/spec-assets/not-allowlisted.js", nil)
	req.SetPathValue("name", "not-allowlisted.js")
	rr := httptest.NewRecorder()
	new(Hub).HandleConsoleSpecAsset(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("non-allowlisted asset code=%d want=404", rr.Code)
	}
}

func TestConsoleSpecEmbeddedMarkdownCoreIsPinned(t *testing.T) {
	sum := sha256.Sum256([]byte(viewservice.MarkdownRenderCore))
	const want = "0f03c242f05e359d94ff1dbe0453d77e83ec7f50d0dec2add4bb6e70b7eb146a"
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("embedded md-render-core sha=%s want=%s", got, want)
	}
	if !strings.Contains(renderedConsoleSpecsPage(), "<script>"+viewservice.MarkdownRenderCore+"</script>") {
		t.Fatal("served spec page does not contain the pinned shared renderer bytes")
	}
}
