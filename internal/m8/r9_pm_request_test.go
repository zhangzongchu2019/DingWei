package m8

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"github.com/zhangzongchu2019/dingwei/internal/trustedorigin"
	"net/http"
	"net/http/httptest"
)

type r9ProposalProvider struct{ body string }

func (*r9ProposalProvider) Repository(context.Context, string) (specread.Repository, error) {
	return specread.Repository{ID: "1", FullName: "owner/repo"}, nil
}
func (*r9ProposalProvider) Authorize(context.Context, specread.Repository, specread.Binding) error {
	return nil
}
func (*r9ProposalProvider) Resolve(context.Context, specread.Repository, string) (string, error) {
	return strings.Repeat("a", 40), nil
}
func (p *r9ProposalProvider) Tree(context.Context, specread.Repository, string) ([]specread.Entry, error) {
	return []specread.Entry{{Path: "openspec/changes/demo/proposal.md", SHA: strings.Repeat("b", 40), Mode: "100644", Type: "blob", Size: int64(len(p.body))}}, nil
}
func (p *r9ProposalProvider) Blob(context.Context, specread.Repository, specread.Entry) ([]byte, error) {
	return []byte(p.body), nil
}
func (*r9ProposalProvider) Source(_ specread.Repository, sha, path string) string {
	return "https://github.test/owner/repo/blob/" + sha + "/" + path
}

func TestRequestR9PMDecisionRequiresMappedPMAndCreatesBoundRequest(t *testing.T) {
	h, db, ctx := newTestHub(t)
	t.Setenv("DW_PM_DECISION_REQUEST_ENABLED", "1")
	const commit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	proposal := "## 待 PM 决策\n- R9-PM01: 首期如何记录？\n  - 问题：是否先写平台？\n  - A. 平台记录、MR后置\n  - B. 立即写MR\n  - 建议：A\n"
	if err := db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: "alice", Provider: "feishu", ProviderIssuer: "test", ProviderSubject: "alice", DisplayName: "alice", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: "pm", Provider: "feishu", ProviderIssuer: "bot", ProviderSubject: "ou_pm", DisplayName: "pm", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSpecReadBinding(ctx, "alice", specread.Binding{Principal: "alice", Provider: "github", Instance: "github.com", AccountID: "1", Login: "alice"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSpecReadBinding(ctx, "pm", specread.Binding{Principal: "pm", Provider: "github", Instance: "github.com", AccountID: "1", Login: "alice"}); err != nil {
		t.Fatal(err)
	}
	h.SpecReader = &specread.Service{Bindings: db, Providers: map[string]specread.Provider{"github:github.com": &r9ProposalProvider{body: proposal}}, Repositories: map[string]bool{"github:github.com:1": true}}
	mem := identity.NewMemStore()
	if err := mem.CreateUser(ctx, identity.User{UserID: "pm", ShortName: "pm", State: identity.UserActive, Type: identity.UserMember}); err != nil {
		t.Fatal(err)
	}
	if err := mem.CreateUser(ctx, identity.User{UserID: "intruder", ShortName: "intruder", State: identity.UserActive, Type: identity.UserMember}); err != nil {
		t.Fatal(err)
	}
	if err := mem.CreateRegistrationCode(ctx, "pm", "bind", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.RedeemRegistrationCode(ctx, "bind", identity.Identity{Provider: "feishu", Issuer: "bot", Subject: "ou_pm"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := mem.CreateRegistrationCode(ctx, "intruder", "intruder-bind", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.RedeemRegistrationCode(ctx, "intruder-bind", identity.Identity{Provider: "feishu", Issuer: "bot", Subject: "ou_intruder"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	h.Identity = identity.NewService(mem, nil, nil)
	c := specread.Coordinate{Provider: "github", Instance: "github.com", Repository: "1", Commit: commit}
	if _, err := h.RequestR9PMDecision(ctx, "alice", c, "demo", "R9-PM01"); err != ErrR9PMNotConfigured {
		t.Fatalf("unmapped point did not stop: %v", err)
	}
	k := store.R9PMKey{Provider: "github", Instance: "github.com", RepositoryID: "1", ChangeDirectory: "demo", PointID: "R9-PM01"}
	if _, err := db.SetR9PMResponsibility(ctx, k, "pm", "root"); err != nil {
		t.Fatal(err)
	}
	result, err := h.RequestR9PMDecision(ctx, "alice", c, "demo", "R9-PM01")
	if err != nil || !result.Created || result.Request.Status != "open" {
		t.Fatalf("request=%+v err=%v", result, err)
	}
	tasks, err := db.ListR9PMConsoleTasks(ctx, "pm")
	if err != nil || len(tasks) != 1 || tasks[0].CommitSHA != commit || tasks[0].Generation != 1 || tasks[0].MRWritebackStatus != "postponed" {
		t.Fatalf("console task=%+v err=%v", tasks, err)
	}
	// A verified personal reply must bind the exact persisted Feishu message and current PM read ACL.
	h.ClarificationReplyAuthorities = map[string]ClarificationReplyAuthority{"bot": {AppID: "app", Tenant: "tenant"}}
	h.R9PMQuestionSender = func(context.Context, string, string, string, string) (string, error) { return "om_r9_request", nil }
	h.dispatchR9PMDeliveries(ctx, db)
	payload := []byte("reply")
	sum := sha256.Sum256(payload)
	msg := model.Message{BotChannelID: "bot", SenderOpenID: "ou_pm", ChatType: model.ChatPersonal, IngressProvenance: model.IngressFeishuWSAuthenticated, FeishuReply: &model.FeishuReplyMetadata{Authenticated: true, MessageID: "om_r9_reply", EventID: "ev-r9-reply", ParentID: "om_r9_request", RootID: "om_r9_request", MessageType: "text", Tenant: "tenant", SenderTenant: "tenant", AppID: "app", SenderType: "user", ContentSHA: hex.EncodeToString(sum[:])}}
	for name, bad := range map[string]model.Message{
		"untrusted transport": func() model.Message { m := msg; m.IngressProvenance = model.IngressProvenance("untrusted"); return m }(),
		"group chat":          func() model.Message { m := msg; m.ChatType = model.ChatGroup; return m }(),
		"wrong PM":            func() model.Message { m := msg; m.SenderOpenID = "ou_intruder"; return m }(),
	} {
		_, handled, e := h.dispatchR9PMReply(ctx, bad, "同意")
		if e != nil || name == "wrong PM" && !handled || name != "wrong PM" && handled {
			t.Errorf("%s Feishu input handled=%v err=%v", name, handled, e)
		}
		pending, e := db.ListR9PMConsoleTasks(ctx, "pm")
		if e != nil || len(pending) != 1 {
			t.Fatalf("%s input changed open request: tasks=%+v err=%v", name, pending, e)
		}
	}
	if err := db.DeleteSpecReadBinding(ctx, "root", "pm", "github", "github.com"); err != nil {
		t.Fatal(err)
	}
	if _, handled, e := h.dispatchR9PMReply(ctx, msg, "同意"); e != nil || !handled {
		t.Fatalf("revoked repository read was not rejected: handled=%v err=%v", handled, e)
	}
	pending, err := db.ListR9PMConsoleTasks(ctx, "pm")
	if err != nil || len(pending) != 1 {
		t.Fatalf("revoked repository read changed request: tasks=%+v err=%v", pending, err)
	}
	if err := db.SetSpecReadBinding(ctx, "root", specread.Binding{Principal: "pm", Provider: "github", Instance: "github.com", AccountID: "1", Login: "alice"}); err != nil {
		t.Fatal(err)
	}
	first, err := h.Dispatch(ctx, msg, "同意")
	if err != nil || !first.Matched || first.Reply != "已记录你的 PM 决定。" {
		t.Fatalf("trusted reply=%+v err=%v", first, err)
	}
	late, err := h.Dispatch(ctx, msg, "同意")
	if err != nil || !late.Matched || late.Reply == first.Reply {
		t.Fatalf("duplicate reply not late: %+v err=%v", late, err)
	}
	tasks, err = db.ListR9PMConsoleTasks(ctx, "pm")
	if err != nil || len(tasks) != 0 {
		t.Fatalf("answered PM still has console todo: %+v err=%v", tasks, err)
	}
}

func TestR9PMRequestTrustedOriginMatrix(t *testing.T) {
	t.Setenv("DW_PM_DECISION_REQUEST_ENABLED", "1")
	h, _, _ := newTestHub(t)
	h.PublicOrigin = "https://dw123.cmjrt.com"
	list, err := trustedorigin.ParseList("https://dw123.cmjrt.com:50443")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, source, target string
		configured           bool
		want                 int
	}{
		{"empty canonical", "https://dw123.cmjrt.com", "https://dw123.cmjrt.com/api/console/r9-pm-requests", false, http.StatusBadRequest},
		{"empty list excludes extra port", "https://dw123.cmjrt.com:50443", "https://dw123.cmjrt.com:50443/api/console/r9-pm-requests", false, http.StatusForbidden},
		{"empty list exact case behavior", "HTTPS://DW123.CMJRT.COM:50443", "HTTPS://DW123.CMJRT.COM:50443/api/console/r9-pm-requests", false, http.StatusForbidden},
		{"configured canonical", "https://dw123.cmjrt.com", "https://dw123.cmjrt.com/api/console/r9-pm-requests", true, http.StatusBadRequest},
		{"configured allowlist case normalized", "HTTPS://DW123.CMJRT.COM:50443", "https://dw123.cmjrt.com:50443/api/console/r9-pm-requests", true, http.StatusBadRequest},
		{"configured different port", "https://dw123.cmjrt.com:50444", "https://dw123.cmjrt.com:50444/api/console/r9-pm-requests", true, http.StatusForbidden},
		{"configured HTTP impersonation", "http://dw123.cmjrt.com:50443", "http://dw123.cmjrt.com:50443/api/console/r9-pm-requests", true, http.StatusForbidden},
		{"configured missing origin", "", "https://dw123.cmjrt.com:50443/api/console/r9-pm-requests", true, http.StatusForbidden},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.configured {
				h.TrustedOrigins = list
			} else {
				h.TrustedOrigins = nil
			}
			r := httptest.NewRequest(http.MethodPost, tc.target, strings.NewReader("{"))
			if tc.source != "" {
				r.Header.Set("Origin", tc.source)
			}
			r.Header.Set("Content-Type", "application/json")
			r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "manager"))
			w := httptest.NewRecorder()
			h.HandleR9PMRequest(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}
