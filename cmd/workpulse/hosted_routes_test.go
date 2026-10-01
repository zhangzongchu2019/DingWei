package main

// hosted 路由接线用例：桩 actor/归属/角色＋pgtest 真库＋假网关——机器码与状态码逐格。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// ---------- 路由级假件（hosted 包的测试假件不导出，这里自带最小集） ----------

type fakeIDP struct {
	ids     []string
	retired map[string]bool
}

func (f *fakeIDP) IssueHostedAgent(ctx context.Context, ownerLC, name, groupCode, by string) (hosted.IssuedAgent, hosted.EnrollToken, error) {
	// 与 identity.ValidAgentName 同形状的名称校验（B6：拒绝须以契约机器码透传）
	ok := len(name) > 0
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			ok = false
		}
	}
	if !ok {
		// 与真装配同一条路：G3 适配器把 identity.ErrNameInvalid 原样透传上来（其文本是 Go 错误文本，不是机器码），
		// 由 handler 译成 name_invalid。原先这里直接返回 "name_invalid" 文本，恰好掩盖了联调第 3 条。
		return hosted.IssuedAgent{}, hosted.EnrollToken{}, identity.ErrNameInvalid
	}
	id := fmt.Sprintf("ag_rt%d", len(f.ids)+1)
	f.ids = append(f.ids, id)
	return hosted.IssuedAgent{ID: id, FullName: name + ".zzc"},
		hosted.EnrollToken{ApplicationID: "app1", PollToken: "tok", ExpiresAt: time.Now().Add(2 * time.Hour)}, nil
}
func (f *fakeIDP) RetireHostedAgent(ctx context.Context, agentID, by string) (bool, error) {
	if f.retired == nil {
		f.retired = map[string]bool{}
	}
	f.retired[agentID] = true
	return true, nil
}
func (f *fakeIDP) ListHostedAgentIDsByOwner(ctx context.Context, ownerLC string) ([]string, error) {
	var out []string // 与 identity 语义对齐：已吊销（Retire）的不在名下
	for _, id := range f.ids {
		if !f.retired[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

type fakeGW struct {
	sent   []hosted.NodeCommand
	failOn string // 命中该 Type 即失败（故障注入）
	downOn string // 命中该 Type 即发不到节点（传输错误）
}

func (g *fakeGW) Send(ctx context.Context, nodeID string, cmd hosted.NodeCommand) (hosted.NodeResult, error) {
	g.sent = append(g.sent, cmd)
	if cmd.Type == g.downOn {
		return hosted.NodeResult{}, errors.New("injected: node unreachable")
	}
	if cmd.Type == g.failOn {
		return hosted.NodeResult{OK: false, Error: "injected"}, nil
	}
	return hosted.NodeResult{OK: true}, nil
}

type fakeCombosRT struct{}

func (fakeCombosRT) Lookup(cli, llm, kind string) (bool, bool) {
	if cli == "codex" && llm == "gpt" && kind == "token_file" {
		return true, true
	}
	return false, false
}
func (fakeCombosRT) List() []hosted.Combo {
	return []hosted.Combo{{CLI: "codex", LLM: "gpt", CredKind: "token_file", Verified: true}}
}

type fakeImagesRT struct{}

func (fakeImagesRT) Latest(cli string) (string, bool) {
	return "hub.local/dingwei/base-codex@sha256:" + strings.Repeat("ab", 32), true
}

// 返回里的 fakeGW 供集成套件注入故障／核对指令日志（T-G4-34 B2/B3）。
func mkHostedDeps(t *testing.T) (hostedDeps, *hosted.Store, *fakeIDP, *fakeGW, *hosted.PoolService) {
	t.Helper()
	pool := pgtest.Pool(t)
	ctx := context.Background()
	st := hosted.NewStore(pool)
	if err := st.DropSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.ApplySchema(ctx); err != nil {
		t.Fatal(err)
	}
	mk := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(mk, []byte(strings.Repeat("k", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	poolSvc, err := hosted.NewPoolService(st, mk)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIDP{}
	gw := &fakeGW{}
	orch := hosted.NewOrchestrator(st, poolSvc, idp, gw, fakeCombosRT{}, fakeImagesRT{})
	life := hosted.NewLifecycle(st, poolSvc, idp, idpSusp{}, gw, fakeImagesRT{}, notReadyBackups{})
	// 节点一枚（有余量）＋配额＋账号一枚授权给 zzc
	if err := st.UpsertNode(ctx, hosted.Node{ID: "nd_rt", Name: "rt", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetNodeOnline(ctx, "nd_rt", json.RawMessage(`{"cpu_total":16,"mem_free_mb":16384,"disk_free_mb":102400}`)); err != nil {
		t.Fatal(err)
	}
	if err := st.PutQuota(ctx, hosted.Quota{UserLC: "zzc", AgentsLimit: 2, VCPULimit: 4, MemMBLimit: 8192}, "admin"); err != nil {
		t.Fatal(err)
	}
	a, err := poolSvc.Register(ctx, "admin", true, "team-codex", "token_file", "codex", "gpt", "sk-x")
	if err != nil {
		t.Fatal(err)
	}
	if err := poolSvc.Grant(ctx, "admin", true, a.ID, "zzc"); err != nil {
		t.Fatal(err)
	}
	deps := hostedDeps{
		orch: orch, life: life, qs: hosted.NewQuotaService(st, idp),
		actor: func(r *http.Request) (string, error) {
			a := r.Header.Get("X-Test-Actor")
			if a == "" {
				return "", errors.New("unauthorized")
			}
			if a == "must-change" { // N3.Actor 对必须改密的会话返回 identity.ErrMustChange
				return "", identity.ErrMustChange
			}
			return a, nil
		},
		ownsAgent: func(r *http.Request, userLC, agentID string) bool { return userLC == "zzc" },
		isManager: func(r *http.Request, userLC string) bool { return userLC == "admin" },
		listUsers: func(r *http.Request) ([]hostedUser, error) {
			return []hostedUser{{ID: "Zzc", IDLC: "zzc", ShortName: "zz"}}, nil
		},
	}
	return deps, st, idp, gw, poolSvc
}

type idpSusp struct{ fakeIDP }

func (idpSusp) SuspendHostedAgent(ctx context.Context, agentID, by string) (bool, error) {
	return true, nil
}
func (idpSusp) ResumeHostedAgent(ctx context.Context, agentID, by string) (bool, error) {
	return true, nil
}
func (idpSusp) ReissueHostedToken(ctx context.Context, agentID, by string) (hosted.EnrollToken, error) {
	return hosted.EnrollToken{}, nil
}

func TestHostedRoutesContractCodes(t *testing.T) {
	deps, st, _, _, _ := mkHostedDeps(t)
	mux := http.NewServeMux()
	registerHostedRoutes(mux, deps)
	as := func(actor, method, path, body string) *httptest.ResponseRecorder {
		var rdr *strings.Reader
		if body != "" {
			rdr = strings.NewReader(body)
		} else {
			rdr = strings.NewReader("")
		}
		req := httptest.NewRequest(method, path, rdr)
		req.Header.Set("X-Test-Actor", actor)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	codeOf := func(rec *httptest.ResponseRecorder) string {
		var v map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &v)
		s, _ := v["error"].(string)
		return s
	}

	// options：未登录 401；正常 200 带五段
	if rec := as("", "GET", "/api/console/hosted/options", ""); rec.Code != 401 {
		t.Fatalf("未登录应 401: %d", rec.Code)
	}
	rec := as("zzc", "GET", "/api/console/hosted/options", "")
	if rec.Code != 200 {
		t.Fatalf("options: %d %s", rec.Code, rec.Body)
	}
	var ov map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &ov)
	for _, k := range []string{"nodes", "combos", "accounts", "plans", "quota"} {
		if _, ok := ov[k]; !ok {
			t.Fatalf("options 缺 %s: %v", k, ov)
		}
	}

	// create：不兼容组合 ⇒ combo_incompatible（用真实账号：kind 由账号解析后组合不在表）；
	// 合法 ⇒ 202＋job_id；不存在账号 ⇒ account_not_allowed
	acctID := ""
	{
		accts, _ := st.ListUsableAccounts(context.Background(), "zzc", "", "")
		acctID = accts[0].ID
	}
	if rec := as("zzc", "POST", "/api/console/hosted/agents", `{"name":"w1","node_id":"auto","cli":"claude","llm":"gpt","account_id":"`+acctID+`","plan_id":"2c4g"}`); codeOf(rec) != "combo_incompatible" {
		t.Fatalf("不兼容组合: %d %s", rec.Code, rec.Body)
	}
	if rec := as("zzc", "POST", "/api/console/hosted/agents", `{"name":"w1","node_id":"auto","cli":"codex","llm":"gpt","account_id":"acct_nope","plan_id":"2c4g"}`); codeOf(rec) != "account_not_allowed" {
		t.Fatalf("不存在账号: %d %s", rec.Code, rec.Body)
	}
	rec = as("zzc", "POST", "/api/console/hosted/agents", `{"name":"w1","node_id":"auto","cli":"codex","llm":"gpt","account_id":"`+acctID+`","plan_id":"2c4g"}`)
	if rec.Code != 202 {
		t.Fatalf("create 应 202: %d %s", rec.Code, rec.Body)
	}
	var created map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created["job_id"] == "" {
		t.Fatal("缺 job_id")
	}

	// jobs：非本人 ⇒ not_owner
	if rec := as("eve", "GET", "/api/console/hosted/jobs/"+created["job_id"], ""); rec.Code != 403 || codeOf(rec) != "not_owner" {
		t.Fatalf("非本人应 not_owner: %d %s", rec.Code, rec.Body)
	}

	// 生命周期：disable 裸调 ⇒ confirm_required（D8 真判据＝服务端拒绝）
	if rec := as("zzc", "POST", "/api/console/hosted/agents/ag_rt1/disable", `{}`); codeOf(rec) != "confirm_required" {
		t.Fatalf("裸调停用: %d %s", rec.Code, rec.Body)
	}
	// 非本人操作 ⇒ not_owner
	if rec := as("eve", "POST", "/api/console/hosted/agents/ag_rt1/restart", `{}`); codeOf(rec) != "not_owner" {
		t.Fatalf("非本人操作: %d %s", rec.Code, rec.Body)
	}

	// 配额：非管理 ⇒ forbidden_role；管理 ⇒ GET 200／PUT 缺键 name_invalid／四键齐 200
	if rec := as("zzc", "GET", "/api/console/admin/quotas", ""); codeOf(rec) != "forbidden_role" {
		t.Fatalf("普通用户读配额: %d %s", rec.Code, rec.Body)
	}
	if rec := as("admin", "GET", "/api/console/admin/quotas", ""); rec.Code != 200 {
		t.Fatalf("管理读配额: %d %s", rec.Code, rec.Body)
	}
	if rec := as("admin", "PUT", "/api/console/admin/quotas/zzc", `{"agents_limit":4}`); codeOf(rec) != "name_invalid" {
		t.Fatalf("缺键: %d %s", rec.Code, rec.Body)
	}
	if rec := as("admin", "PUT", "/api/console/admin/quotas/zzc", `{"agents_limit":4,"vcpu_limit":8,"mem_gb_limit":16,"disk_gb_limit":64}`); rec.Code != 200 {
		t.Fatalf("四键齐: %d %s", rec.Code, rec.Body)
	}
}
