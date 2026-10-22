package m8

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
)

func TestPMSpecFlowDefaultOffAndCASConflictIsHTTP409(t *testing.T) {
	e := newShimEnv(t)
	body := `{"spec_id":"pm-api","title":"PM flow","content_markdown":"draft","acceptance_json":"{\"criteria\":[]}","input_anchors_json":"[]","source_refs_json":"[]","norm_anchors_json":"[]","expected_version":0,"expected_sha256":""}`
	r := httptest.NewRequest(http.MethodPost, "/api/console/pm-spec-flow/drafts", strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: e.tokZZC})
	r.Header.Set("Origin", npOrigin)
	w := httptest.NewRecorder()
	e.hub.RequireConsolePrincipal(e.hub.RequireConsolePlatformAction("submit", e.hub.HandlePMSpecWorkingDraft))(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("default-off status=%d body=%s", w.Code, w.Body.String())
	}
	e.hub.pmSpecFlowEnabled = true
	r = httptest.NewRequest(http.MethodPost, "/api/console/pm-spec-flow/drafts", strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: e.tokZZC})
	r.Header.Set("Origin", npOrigin)
	w = httptest.NewRecorder()
	e.hub.RequireConsolePrincipal(e.hub.RequireConsolePlatformAction("submit", e.hub.HandlePMSpecWorkingDraft))(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"version":1`) {
		t.Fatalf("first autosave status=%d body=%s", w.Code, w.Body.String())
	}
	if spec, err := e.db.ConsoleSpec(context.Background(), "zzc", "pm-api", false); err != nil || spec != nil {
		t.Fatalf("autosave created immutable console spec: %+v %v", spec, err)
	}
	conflictBody := strings.Replace(body, `"draft"`, `"stale client bytes"`, 1)
	r = httptest.NewRequest(http.MethodPost, "/api/console/pm-spec-flow/drafts", strings.NewReader(conflictBody))
	r.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: e.tokZZC})
	r.Header.Set("Origin", npOrigin)
	w = httptest.NewRecorder()
	e.hub.RequireConsolePrincipal(e.hub.RequireConsolePlatformAction("submit", e.hub.HandlePMSpecWorkingDraft))(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("stale autosave status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPMSpecFlowPageIsFeatureGatedAndBindsAuthenticatedPrincipal(t *testing.T) {
	e := newShimEnv(t)
	r := httptest.NewRequest(http.MethodGet, "/console/pm-specs", nil)
	r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "zzc"))
	w := httptest.NewRecorder()
	e.hub.HandlePMSpecFlowPage(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("disabled page status=%d", w.Code)
	}
	e.hub.pmSpecFlowEnabled = true
	r = httptest.NewRequest(http.MethodGet, "/console/pm-specs", nil)
	r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "zzc"))
	w = httptest.NewRecorder()
	e.hub.HandlePMSpecFlowPage(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `window.pmPrincipal="zzc"`) || !strings.Contains(w.Body.String(), "@media(max-width:600px)") {
		t.Fatalf("enabled page status=%d principal/mobile marker missing", w.Code)
	}
}
