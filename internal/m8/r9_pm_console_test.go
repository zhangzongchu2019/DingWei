package m8

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func TestR9PMConsoleAcceptsOnlyThreeExplicitDecisions(t *testing.T) {
	for _, tc := range []struct {
		text string
	}{{"同意"}, {"不同意"}, {"需修改"}} {
		t.Run(tc.text, func(t *testing.T) {
			h, db, ctx := newTestHub(t)
			t.Setenv("DW_PM_DECISION_REQUEST_ENABLED", "1")
			mem := identity.NewMemStore()
			if err := mem.CreateUser(ctx, identity.User{UserID: "pm", ShortName: "pm", Type: identity.UserMember, State: identity.UserActive}); err != nil {
				t.Fatal(err)
			}
			if err := mem.CreateSession(ctx, identity.Session{TokenHash: identity.HashSecret("pm-console"), UserIDLC: "pm", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			h.Identity = identity.NewService(mem, nil, nil)
			key := store.R9PMKey{Provider: "github", Instance: "github.com", RepositoryID: "1", ChangeDirectory: "demo", PointID: "R9-PM01"}
			if _, err := db.SetR9PMResponsibility(ctx, key, "pm", "admin"); err != nil {
				t.Fatal(err)
			}
			if err := db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: "admin", Provider: "feishu", ProviderIssuer: "test", ProviderSubject: "admin", DisplayName: "admin", Active: true}); err != nil {
				t.Fatal(err)
			}
			if err := db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: "pm", Provider: "feishu", ProviderIssuer: "bot", ProviderSubject: "ou_pm", DisplayName: "pm", Active: true}); err != nil {
				t.Fatal(err)
			}
			if err := db.SetSpecReadBinding(ctx, "admin", specread.Binding{Principal: "pm", Provider: "github", Instance: "github.com", AccountID: "1", Login: "pm"}); err != nil {
				t.Fatal(err)
			}
			proposal := "## 待 PM 决策\n- R9-PM01: 首期记录边界\n  - 问题：平台如何记录？\n  - A. 只记录平台\n  - B. 自动写MR\n  - 建议：A\n"
			h.SpecReader = &specread.Service{Bindings: db, Providers: map[string]specread.Provider{"github:github.com": &r9ProposalProvider{body: proposal}}, Repositories: map[string]bool{"github:github.com:1": true}}
			if _, created, err := db.CreateR9PMRequest(ctx, store.R9PMCreateInput{RequestID: "console-r9", R9PMKey: key, CommitSHA: strings.Repeat("a", 40), ContentSHA256: "fingerprint", PMUserIDLC: "pm", ConsoleRecipient: "pm", FeishuRecipient: "ou_pm", SummaryJSON: `{"question":"Q","options":["A","B"],"recommendation":"A"}`}); err != nil || !created {
				t.Fatalf("create request created=%v err=%v", created, err)
			}
			body := `{"generation":1,"decision":"` + tc.text + `","comment":"verified"}`
			r := httptest.NewRequest(http.MethodPost, "https://console.fixture.invalid/api/console/openspec-pm-requests/console-r9/response", strings.NewReader(body))
			r.SetPathValue("requestID", "console-r9")
			r.Header.Set("Origin", "https://console.fixture.invalid")
			r.Header.Set("Content-Type", "application/json")
			r = r.WithContext(context.WithValue(r.Context(), consolePrincipalContextKey{}, "pm"))
			w := httptest.NewRecorder()
			h.HandleR9PMConsoleResponse(w, r)
			if w.Code != http.StatusNoContent {
				t.Fatalf("explicit Console decision %q status=%d body=%s", tc.text, w.Code, w.Body.String())
			}
			if tasks, err := db.ListR9PMConsoleTasks(ctx, "pm"); err != nil || len(tasks) != 0 {
				t.Fatalf("accepted Console decision did not close its task: %+v err=%v", tasks, err)
			}
		})
	}
}
