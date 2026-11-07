package m8

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	td "github.com/zhangzongchu2019/dingwei/internal/taskdispatch"
	"github.com/zhangzongchu2019/dingwei/internal/trustedorigin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestR10ClosedAndUntaggedHaveNoDependencyAccess(t *testing.T) {
	for _, flag := range []string{"", "0", "true", " 1", "1"} {
		t.Run("flag="+flag, func(t *testing.T) {
			t.Setenv("DW_TASK_DISPATCH_LOCK_ENABLED", flag)
			h := &Hub{}
			for _, meta := range []map[string]any{nil, {"kind": "task_assigned"}, {"kind": "legacy_control"}, {"kind": "conversation"}} {
				if err := h.taskDispatchEnvelopeAllowed(context.Background(), model.Envelope{Meta: meta}); err != nil {
					t.Fatal("R10_UNTAGGED_BLOCKED", err)
				}
			}
			if flag != "1" {
				if h.taskDispatchService() != nil {
					t.Fatal("R10_CLOSED_SERVICE_CREATED")
				}
				h.StartTaskDispatchLockMonitor(context.Background())
				w := httptest.NewRecorder()
				h.HandleTaskDispatchLock(w, httptest.NewRequest("POST", "/api/console/task-dispatch-locks", strings.NewReader(`{}`)))
				if w.Code != 404 {
					t.Fatal("R10_CLOSED_ENDPOINT", w.Code)
				}
			}
		})
	}
}
func TestR10ForgedNoticeCannotEnterExistingRoute(t *testing.T) {
	t.Setenv("DW_TASK_DISPATCH_LOCK_ENABLED", "1")
	h := &Hub{}
	if err := h.RouteEnvelope(context.Background(), model.Envelope{ID: "forged", Meta: map[string]any{"kind": "r10_task_notice", "r10_notice_id": "forged"}}); !errors.Is(err, td.ErrForbidden) {
		t.Fatal("R10_FORGED_NOTICE_ROUTED", err)
	}
}

func TestR10HTTPRejectsForgedActorsCSRFAndAmbiguousJSON(t *testing.T) {
	e, db, _, m := r10NoticeEnv(t)
	request := td.Request{OperationID: "http_valid_operation_0001", Kind: "grant", Key: m.Key, HolderAgentID: m.HolderAgentID, Group: m.AuthorizationGroup, Commit: m.CommitSHA, MonitorRef: "main"}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, method, body, origin, content string
		agent                               bool
		status                              int
	}{
		{"unauthenticated_console", "POST", string(body), "https://example.test", "application/json", false, 403},
		{"unauthenticated_agent", "POST", string(body), "", "application/json", true, 403},
		{"cross_origin", "POST", string(body), "https://other.invalid", "application/json", false, 403},
		{"missing_origin", "POST", string(body), "", "application/json", false, 403},
		{"body_user", "POST", `{"actor_user_id_lc":"zzc"}`, "https://example.test", "application/json", false, 400},
		{"body_agent", "POST", `{"agent_id":"forged"}`, "", "application/json", true, 400},
		{"second_json", "POST", `{}` + `{}`, "https://example.test", "application/json", false, 400},
		{"content_type", "POST", string(body), "https://example.test", "text/plain", false, 400},
		{"wrong_method", "PUT", string(body), "https://example.test", "application/json", false, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "https://example.test/api/console/task-dispatch-locks", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.content)
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			if tc.agent {
				e.hub.HandleAgentTaskDispatchLock(w, r)
			} else {
				e.hub.HandleTaskDispatchLock(w, r)
			}
			if w.Code != tc.status {
				t.Fatal("R10_HTTP_AUTH_GUARD", tc.name, w.Code, w.Body.String())
			}
		})
	}
	r := httptest.NewRequest(http.MethodPost, "https://example.test/api/console/task-dispatch-locks/batch", strings.NewReader(`{"operations":[],"actor":"zzc"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://example.test")
	w := httptest.NewRecorder()
	e.hub.HandleTaskDispatchBatch(w, r)
	if w.Code != 400 {
		t.Fatal("R10_BATCH_FORGED_ACTOR", w.Code)
	}
	l, err := db.TaskDispatchLock(e.ctx, m.Key)
	if err != nil || l != nil {
		t.Fatal("R10_HTTP_DENIAL_CREATED_LOCK", l, err)
	}
}

func TestR52TaskDispatchLockAndBatchTrustedOriginMatrix(t *testing.T) {
	e, _, _, _ := r10NoticeEnv(t)
	h := e.hub
	h.PublicOrigin = "https://dw123.cmjrt.com"
	trusted, err := trustedorigin.ParseList("https://dw123.cmjrt.com:50443")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, source, target string
		configured           bool
		want                 int
	}{
		{"empty canonical", "https://dw123.cmjrt.com", "https://dw123.cmjrt.com", false, http.StatusBadRequest},
		{"empty extra port", "https://dw123.cmjrt.com:50443", "https://dw123.cmjrt.com:50443", false, http.StatusForbidden},
		{"empty case variation", "HTTPS://DW123.CMJRT.COM:50443", "https://DW123.CMJRT.COM:50443", false, http.StatusForbidden},
		{"configured canonical", "https://dw123.cmjrt.com", "https://dw123.cmjrt.com", true, http.StatusBadRequest},
		{"configured normalized hit", "HTTPS://DW123.CMJRT.COM:50443", "https://dw123.cmjrt.com:50443", true, http.StatusBadRequest},
		{"configured different port", "https://dw123.cmjrt.com:50444", "https://dw123.cmjrt.com:50444", true, http.StatusForbidden},
		{"configured HTTP impostor", "http://dw123.cmjrt.com:50443", "http://dw123.cmjrt.com:50443", true, http.StatusForbidden},
		{"configured missing origin", "", "https://dw123.cmjrt.com:50443", true, http.StatusForbidden},
	}
	for _, endpoint := range []struct {
		name, path string
		handler    http.HandlerFunc
	}{
		{"lock", "/api/console/task-dispatch-locks", h.HandleTaskDispatchLock},
		{"batch", "/api/console/task-dispatch-locks/batch", h.HandleTaskDispatchBatch},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					if tc.configured {
						h.TrustedOrigins = trusted
					} else {
						h.TrustedOrigins = nil
					}
					r := httptest.NewRequest(http.MethodPost, tc.target+endpoint.path, strings.NewReader("{"))
					r.Header.Set("Content-Type", "application/json")
					if tc.source != "" {
						r.Header.Set("Origin", tc.source)
					}
					w := httptest.NewRecorder()
					endpoint.handler(w, r)
					if w.Code != tc.want {
						t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
					}
				})
			}
		})
	}
}
