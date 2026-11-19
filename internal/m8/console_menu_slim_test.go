package m8

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cf "github.com/zhangzongchu2019/dingwei/internal/changeflow"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
)

// S-R62-CONSOLE-SLIM（业主令 rulings ㊿-补129）：整个菜单只保留「Agent」与「平台管理」。
// 本格专守菜单形状：新平台管理角色恰为两项、普通角色恰为一项；两平台被摘项不得回潮。
// 删保护：把任一被摘项加回 handler（或换序、或多发一项）⇒ 本格红。
// 只摘入口：后端路由、表、数据、迁移不在本格判据内（直连旧 URL 的行为保持现状）。
// S-R62-OLDPLAT-SLIM（业主令 rulings ㊿-补133）：旧平台余 4 项（旧版后台 admin、变更证据
// change-evidence、openSpec 只读 openspec-read、仓库提交自定义项）亦摘；缺席循环扩到这些 id。

var consoleMenuSlimmedIDs = []string{"specs", "task-board", "pilot-questions", "coordinate-reviews", "openspec-confirmations",
	"admin", "change-evidence", "openspec-read"}

func assertConsoleMenuNoSlimmedIDs(t *testing.T, body string) {
	t.Helper()
	for _, gone := range consoleMenuSlimmedIDs {
		if strings.Contains(body, `"id":"`+gone+`"`) {
			t.Errorf("菜单不得再下发被摘项 %q: %s", gone, body)
		}
	}
}

func TestNewPlatformConsoleMenuSlimExactlyTwoEntries(t *testing.T) {
	v := newViewEnv(t)
	for _, u := range []identity.User{
		{UserID: "mgr", ShortName: "mg", Type: identity.UserManager, State: identity.UserActive},
		{UserID: "mem", ShortName: "me", Type: identity.UserMember, State: identity.UserActive},
	} {
		_ = v.svc.Store.CreateUser(v.ctx, u)
		_ = v.svc.Store.CreateSession(v.ctx, identity.Session{TokenHash: identity.HashSecret("tok-" + u.UserID), UserIDLC: u.UserID, ExpiresAt: v.svc.Now().Add(identity.SessionTTL)})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/console/platform", v.hub.RequireConsolePrincipal(v.hub.RequireConsolePlatformAction("read", v.hub.HandleConsolePlatform)))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	call := func(tok string) (int, string) {
		req, _ := http.NewRequest("GET", srv.URL+"/api/console/platform", nil)
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: tok})
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, strings.TrimSpace(string(b))
	}
	ids := func(body string) string {
		var resp struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, it := range resp.Items {
			out = append(out, it.ID)
		}
		return strings.Join(out, ",")
	}
	if st, body := call("tok-mgr"); st != 200 || ids(body) != "agent-groups,platform-admin" {
		t.Fatalf("管理角色菜单应恰为 agent-groups,platform-admin 两项: %d %s", st, body)
	} else {
		assertConsoleMenuNoSlimmedIDs(t, body)
	}
	if st, body := call("tok-mem"); st != 200 || ids(body) != "agent-groups" {
		t.Fatalf("普通角色菜单应恰为 agent-groups 一项: %d %s", st, body)
	} else {
		assertConsoleMenuNoSlimmedIDs(t, body)
	}
}

// 夹具接线 ChangeEvidence／SpecReader：两条件项的 builtin 闸（h.ChangeEvidence／h.SpecReader
// 非空）在现网为真；不接线则「缺席」对这两项无判别力（加回也观测不到）。
func consolePlatformItemsForWithServices(t *testing.T, principalID string) (string, []model.ConsolePlatformItem) {
	t.Helper()
	hub := newConsolePlatformTestHub(t)
	hub.SpecReader = &specread.Service{Bindings: hub.Repo.(specread.Bindings)}
	hub.ChangeEvidence = &cf.Service{Store: hub.Repo.(cf.Store), Provider: &uiChangeProvider{f: uiFacts(), allowed: true}}
	rr := callConsolePlatform(t, hub, principalID, consolePlatformActionRead, http.MethodGet,
		"/api/console/platform", nil, hub.HandleConsolePlatform)
	if rr.Code != http.StatusOK {
		t.Fatalf("%s list code=%d body=%q", principalID, rr.Code, rr.Body.String())
	}
	var listed struct {
		Role  string                      `json:"role"`
		Items []model.ConsolePlatformItem `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	return listed.Role, listed.Items
}

func TestLegacyConsoleMenuSlimRemovedEntriesAbsent(t *testing.T) {
	for _, principalID := range []string{"manager", "owner"} {
		role, items := consolePlatformItemsForWithServices(t, principalID)
		var ids []string
		for _, it := range items {
			ids = append(ids, it.ID)
		}
		body := strings.Join(ids, ",")
		for _, gone := range consoleMenuSlimmedIDs {
			if strings.Contains(","+body+",", ","+gone+",") {
				t.Errorf("%s 菜单不得再下发被摘项 %q: %s", principalID, gone, body)
			}
		}
		// S-R62-OLDPLAT-SLIM：旧平台菜单最终恰为 Agent＋平台管理（按角色），多发一项即红。
		want := "agent-groups"
		if role == consolePlatformRoleManagement {
			want = "agent-groups,platform-admin"
		}
		if body != want {
			t.Errorf("%s(role=%s) 菜单应恰为 %q,实为 %q", principalID, role, want, body)
		}
	}
}

// S-R62-OLDPLAT-SLIM 白名单口径（组长 2026-11-19T00:20:38Z 裁定）：库内存在已批准／待审
// 自定义项时，旧平台菜单仍只有 builtin 两项（按角色）。夹具自建项（不碰现网库）；
// 「仓库提交」即此类自定义项，id 不定亦不影响判据（白名单不按键过滤）。
// 删保护：恢复「把库内自定义项拼进菜单」⇒ 本格必红。
func TestLegacyConsoleMenuSlimCustomItemsNeverEmitted(t *testing.T) {
	hub := newConsolePlatformTestHub(t)
	hub.SpecReader = &specread.Service{Bindings: hub.Repo.(specread.Bindings)}
	hub.ChangeEvidence = &cf.Service{Store: hub.Repo.(cf.Store), Provider: &uiChangeProvider{f: uiFacts(), allowed: true}}
	repo := hub.Repo.(consolePlatformRepository)
	for _, item := range []model.ConsolePlatformItem{
		{ID: "repo-commits", Kind: "link", Title: "仓库提交", Href: "https://git.example.invalid/repo/commits", Status: "approved", SubmittedBy: "owner", Active: true},
		{ID: "pending-extra", Kind: "link", Title: "待审自定义项", Href: "/console/pending-extra", Status: "pending", SubmittedBy: "owner", Active: true},
	} {
		if err := repo.CreateConsolePlatformItem(context.Background(), item, hub.now()); err != nil {
			t.Fatalf("夹具自建自定义项失败:%v", err)
		}
	}
	for _, principalID := range []string{"manager", "owner"} {
		rr := callConsolePlatform(t, hub, principalID, consolePlatformActionRead, http.MethodGet,
			"/api/console/platform", nil, hub.HandleConsolePlatform)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s list code=%d body=%q", principalID, rr.Code, rr.Body.String())
		}
		var listed struct {
			Role  string                      `json:"role"`
			Items []model.ConsolePlatformItem `json:"items"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &listed); err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, it := range listed.Items {
			ids = append(ids, it.ID)
		}
		body := strings.Join(ids, ",")
		want := "agent-groups"
		if listed.Role == consolePlatformRoleManagement {
			want = "agent-groups,platform-admin"
		}
		if body != want {
			t.Errorf("%s(role=%s) 库内有自定义项时菜单仍应恰为 %q,实为 %q（恢复拼接自定义项必红在这里）",
				principalID, listed.Role, want, body)
		}
	}
}
