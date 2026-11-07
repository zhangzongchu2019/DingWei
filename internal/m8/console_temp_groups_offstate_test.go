package m8

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/tempgroup"
)

func TestConsoleTempGroupsCapabilityFollowsHub(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		h := &Hub{}
		want := `id=tempGroups data-enabled="false" hidden`
		if enabled {
			h.TempGroups = &tempgroup.Service{}
			want = `id=tempGroups data-enabled="true" hidden`
		}
		rr := httptest.NewRecorder()
		h.HandleConsolePage(rr, httptest.NewRequest(http.MethodGet, "/console", nil))
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), want) || strings.Contains(rr.Body.String(), "{{FEATURE_TEMP_GROUPS}}") {
			t.Fatalf("enabled=%v: capability not rendered from Hub", enabled)
		}
	}
}
