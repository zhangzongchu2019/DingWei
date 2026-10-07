package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// R-V2-0102-GROUP-FIXES A：建席表单「新建编组…」真的建组、新席为组长；与入网审批同码；没建成的席位不留空组。
// 断言面＝托管建席 HTTP 路由（真 identity 适配器＋真 hosted PG 存储＋假节点网关）；
// 实现承诺＝handleCreate 读 new_group_name、二选一、编排器走 IssueHostedAgentWithNewGroup，
// 新建链失败一律 AbortHostedAgent（吊销＋删随本次新建、无其他成员的组）。

type ngEnv struct {
	t      *testing.T
	ctx    context.Context
	mux    *http.ServeMux
	st     *hosted.Store
	svc    *identity.Service
	gw     *fakeGW
	pool   *repo.Pool
	acctID string
}

func newNGEnv(t *testing.T) *ngEnv {
	t.Helper()
	ctx := context.Background()
	pool := pgtest.Pool(t)
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
	idStore := identity.NewMemStore()
	svc := identity.NewService(idStore, nil, nil)
	if err := idStore.CreateUser(ctx, identity.User{UserID: "zzc", ShortName: "zz", Type: identity.UserMember, State: identity.UserActive, CreatedAt: svc.Now()}); err != nil {
		t.Fatal(err)
	}
	idp := newHostedIdentity[hosted.EnrollToken, hosted.IssuedAgent](svc, hosted.ErrGroupNotFound, hosted.ErrGroupClosed)
	gw := &fakeGW{}
	orch := hosted.NewOrchestrator(st, poolSvc, idp, gw, fakeCombosRT{}, fakeImagesRT{})
	life := hosted.NewLifecycle(st, poolSvc, idp, idp, gw, fakeImagesRT{}, notReadyBackups{})
	gw.onCreated = func(seatID string) { _ = orch.MarkOnline(context.Background(), seatID) }
	if err := st.UpsertNode(ctx, hosted.Node{ID: "nd_ng", Name: "ng", Labels: json.RawMessage(`{}`), DockerVersion: "29", ProtocolVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetNodeOnline(ctx, "nd_ng", json.RawMessage(`{"cpu_total":64,"mem_free_mb":262144,"disk_free_mb":1048576}`)); err != nil {
		t.Fatal(err)
	}
	if err := st.PutQuota(ctx, hosted.Quota{UserLC: "zzc", AgentsLimit: 20, VCPULimit: 64, MemMBLimit: 262144, DiskMBLimit: 1 << 30}, "admin"); err != nil {
		t.Fatal(err)
	}
	acct, err := poolSvc.RegisterOwned(ctx, "zzc", "team-codex", "token_file", "codex", "gpt", `{"access_token":"sk-x"}`)
	if err != nil {
		t.Fatal(err)
	}
	deps := hostedDeps{
		orch: orch, life: life, qs: hosted.NewQuotaService(st, idp), pool: poolSvc, publicOrigin: testRouteOrigin,
		actor:     func(r *http.Request) (string, error) { return r.Header.Get("X-Test-Actor"), nil },
		ownsAgent: func(r *http.Request, userLC, agentID string) bool { return userLC == "zzc" },
		isManager: func(r *http.Request, userLC string) bool { return false },
		listUsers: func(r *http.Request) ([]hostedUser, error) {
			return []hostedUser{{ID: "zzc", IDLC: "zzc", ShortName: "zz"}}, nil
		},
	}
	mux := http.NewServeMux()
	registerHostedRoutes(mux, deps)
	return &ngEnv{t: t, ctx: ctx, mux: mux, st: st, svc: svc, gw: gw, pool: pool, acctID: acct.ID}
}

// create 发一次建席；extra 是拼进请求体的编组字段（如 `,"new_group_name":"甲"`）。
func (e *ngEnv) create(name, extra string) (int, map[string]string) {
	e.t.Helper()
	body := `{"name":"` + name + `","node_id":"auto","cli":"codex","llm":"gpt","account_id":"` + e.acctID + `","plan_id":"2c4g"` + extra + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/console/hosted/agents", strings.NewReader(body))
	req.Header.Set("X-Test-Actor", "zzc")
	req.Header.Set("Origin", testRouteOrigin)
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	var out map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func (e *ngEnv) group(shortName string) *identity.Group {
	e.t.Helper()
	gs, err := e.svc.Store.ListGroups(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	for i := range gs {
		if gs[i].ShortName == shortName {
			return &gs[i]
		}
	}
	return nil
}

func (e *ngEnv) groupCount() int {
	gs, err := e.svc.Store.ListGroups(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	return len(gs)
}

func (e *ngEnv) currentAgents() int {
	ags, err := e.svc.Store.ListAgents(e.ctx, "zzc")
	if err != nil {
		e.t.Fatal(err)
	}
	n := 0
	for _, a := range ags {
		if a.Current() {
			n++
		}
	}
	return n
}

// failSeatInsert 在 g4_hosted_seat 上装一个 BEFORE INSERT 触发器（真存储里的真失败）；返回卸载函数。
func (e *ngEnv) failSeatInsert() func() {
	e.t.Helper()
	tc, _ := repo.NewAccessCtx("test", "group-fixes-seat-fail")
	for _, q := range []string{
		`CREATE OR REPLACE FUNCTION ng_fail_seat() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'injected seat insert failure'; END $$ LANGUAGE plpgsql`,
		`CREATE TRIGGER ng_fail_seat BEFORE INSERT ON g4_hosted_seat FOR EACH ROW EXECUTE FUNCTION ng_fail_seat()`,
	} {
		if _, err := e.pool.Exec(e.ctx, tc, q); err != nil {
			e.t.Fatal(err)
		}
	}
	return func() {
		if _, err := e.pool.Exec(e.ctx, tc, `DROP TRIGGER IF EXISTS ng_fail_seat ON g4_hosted_seat`); err != nil {
			e.t.Fatal(err)
		}
	}
}

func TestHostedCreateWithNewGroup(t *testing.T) {
	e := newNGEnv(t)

	// 格 A1：合法新组名 ⇒ 202，组真的建出来、新席是组长、Agent 入该组。
	t.Run("new group is created with the new seat as leader", func(t *testing.T) {
		st, out := e.create("Walker", `,"new_group_name":"新组甲"`)
		if st != 202 || out["job_id"] == "" {
			t.Fatalf("create: %d %v", st, out)
		}
		g := e.group("新组甲")
		if g == nil || g.LeaderAgentID != out["job_id"] || g.CreatorLC != "zzc" {
			t.Fatalf("group=%+v agent=%s", g, out["job_id"])
		}
		ag, err := e.svc.Store.GetAgent(e.ctx, out["job_id"])
		if err != nil || ag.GroupID != g.Code {
			t.Fatalf("agent %+v err=%v, want group %s", ag, err, g.Code)
		}
		ngWaitSeat(t, e.st, out["job_id"], "online")
		if e.group("新组甲") == nil {
			t.Fatal("建成后组不得被回收")
		}
	})

	// 格 A2：group_id 与 new_group_name 同给 ⇒ 422 group_ambiguous，不建席不建组。
	t.Run("both group_id and new_group_name is ambiguous", func(t *testing.T) {
		code := e.group("新组甲").Code
		groups, agents := e.groupCount(), e.currentAgents()
		st, out := e.create("Amb", `,"group_id":"`+code+`","new_group_name":"新组乙"`)
		if st != 422 || out["error"] != "group_ambiguous" {
			t.Fatalf("%d %v", st, out)
		}
		if e.groupCount() != groups || e.currentAgents() != agents {
			t.Fatalf("不得建组或建席：groups %d→%d agents %d→%d", groups, e.groupCount(), agents, e.currentAgents())
		}
	})

	// 格 A3：名字不合规（连字符、9 字、只有空白）⇒ 422 group_name_invalid，不建席不建组。
	t.Run("invalid new group names are refused with group_name_invalid", func(t *testing.T) {
		for _, name := range []string{"test-1", "九个字九个字九个字", "   "} {
			groups, agents := e.groupCount(), e.currentAgents()
			st, out := e.create("Bad", `,"new_group_name":"`+name+`"`)
			if st != 422 || out["error"] != "group_name_invalid" {
				t.Fatalf("%q: %d %v", name, st, out)
			}
			if e.groupCount() != groups || e.currentAgents() != agents {
				t.Fatalf("%q: 不得建组或建席", name)
			}
		}
	})

	// 格 A4：组名已被占用 ⇒ 409 group_name_taken，不建席；该 Agent 名字没被占住，换个组名即可建成。
	t.Run("taken group name is 409 and leaves the agent name free", func(t *testing.T) {
		agents := e.currentAgents()
		st, out := e.create("Taken", `,"new_group_name":"新组甲"`)
		if st != 409 || out["error"] != "group_name_taken" {
			t.Fatalf("%d %v", st, out)
		}
		if e.currentAgents() != agents {
			t.Fatal("不得建席")
		}
		if st, out := e.create("Taken", `,"new_group_name":"新组丙"`); st != 202 {
			t.Fatalf("同名重建应成功：%d %v", st, out)
		}
		ngWaitSeat(t, e.st, e.group("新组丙").LeaderAgentID, "online")
	})

	// 格 A6：席位落库失败 ⇒ 没有残留的新组；Agent 已吊销（名字可再用）。
	t.Run("seat insert failure leaves no new group", func(t *testing.T) {
		groups := e.groupCount()
		undo := e.failSeatInsert()
		st, out := e.create("Doomed", `,"new_group_name":"残留组"`)
		undo()
		if st < 400 {
			t.Fatalf("注入失败下建席应失败：%d %v", st, out)
		}
		if g := e.group("残留组"); g != nil || e.groupCount() != groups {
			t.Fatalf("残留新组：%+v（groups %d→%d）", g, groups, e.groupCount())
		}
		if st, out := e.create("Doomed", `,"new_group_name":"残留组"`); st != 202 {
			t.Fatalf("回收后同名同组应可重建：%d %v", st, out)
		}
		ngWaitSeat(t, e.st, e.group("残留组").LeaderAgentID, "online")
	})

	// 格 A7：选既有编组时席位落库失败 ⇒ 既有编组原样保留。
	t.Run("seat insert failure with an existing group keeps that group", func(t *testing.T) {
		g := e.group("新组甲")
		undo := e.failSeatInsert()
		st, _ := e.create("Joiner", `,"group_id":"`+g.Code+`"`)
		undo()
		if st < 400 {
			t.Fatalf("注入失败下建席应失败：%d", st)
		}
		if after := e.group("新组甲"); after == nil || after.Code != g.Code {
			t.Fatalf("既有编组被删：%+v", after)
		}
	})

	// 格 A8：异步执行链失败（节点建容器失败）⇒ 新组也被回收。
	t.Run("async create failure also reclaims the new group", func(t *testing.T) {
		e.gw.failOn = "create_seat"
		defer func() { e.gw.failOn = "" }()
		st, out := e.create("Late", `,"new_group_name":"晚败组"`)
		if st != 202 {
			t.Fatalf("%d %v", st, out)
		}
		ngWaitSeat(t, e.st, out["job_id"], "error")
		deadline := time.Now().Add(3 * time.Second)
		for e.group("晚败组") != nil {
			if time.Now().After(deadline) {
				t.Fatal("执行链失败后新组未回收")
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
}

func ngWaitSeat(t *testing.T, st *hosted.Store, agentID, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		seat, err := st.GetSeat(context.Background(), agentID)
		if err == nil && seat.State == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("seat %s state=%q err=%v, want %s", agentID, seat.State, err, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
