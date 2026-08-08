package board

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/clock"
)

// ---- M5v2:治理扩展(张宗楚 07-23:建单即填全角色·角色绑定 CLI/LLM·可回溯优化) ----

// 建单硬约束:任一角色缺 who/cli/llm ⇒ 整单拒(ErrRolesIncomplete),零写入。
func TestM5_CreateTaskRequiresCompleteRoles(t *testing.T) {
	ctx, fc := context.Background(), &clock.Fake{T: time.Unix(10_000, 0)}
	b, err := Open(":memory:", fc)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	epoch, _ := b.Takeover(ctx, "board1", "orch1", 3600)
	id := Identity{Owner: "orch1", Epoch: epoch}
	bad := testRoles()
	bad.Tester.LLM = "" // 缺一项
	if err := b.CreateTask(ctx, "t-bad", "board1", "x", bad, id); !errors.Is(err, ErrRolesIncomplete) {
		t.Fatalf("缺项建单应拒:%v", err)
	}
	var n int
	b.db.QueryRow(`SELECT COUNT(*) FROM task WHERE task_id='t-bad'`).Scan(&n)
	if n != 0 {
		t.Fatal("拒单必须零写入")
	}
	// 全填→过,task_created 事件带 actor
	if err := b.CreateTask(ctx, "t-ok", "board1", "x", testRoles(), id); err != nil {
		t.Fatal(err)
	}
	var actor string
	b.db.QueryRow(`SELECT agent_id FROM board_event WHERE kind='task_created' AND task_id='t-ok'`).Scan(&actor)
	if actor != "orch1" {
		t.Fatalf("task_created actor=%q want orch1", actor)
	}
	// 僵尸身份建单:拒
	if err := b.CreateTask(ctx, "t-z", "board1", "x", testRoles(), Identity{Owner: "ghost", Epoch: 99}); err == nil {
		t.Fatal("僵尸身份建单应拒")
	}
}

// 台账:角色含 CLI/LLM;起止/进度;Dispatcher 问责主体;角色整组变更(半填拒)。
func TestM5_LedgerRolesWithCLIAndLLM(t *testing.T) {
	b, id, ctx := rig(t)
	e, err := b.TaskLedger(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Roles) != 4 {
		t.Fatalf("roles=%d want 4(建单即全)", len(e.Roles))
	}
	if rb := e.Roles["tester"]; rb.Who != "tester" || rb.CLI != "claude" || rb.LLM != "claude-opus-4-8" {
		t.Fatalf("tester 绑定=%+v", rb)
	}
	if e.Dispatcher != "orch1" {
		t.Fatalf("dispatcher=%s", e.Dispatcher)
	}
	// 变更:整组换 tester 绑定(cli/llm 一起换=回溯口径)
	oc, err := b.SetTaskRoles(ctx, "t1", RoleAssignments{
		Tester: RoleBinding{Who: "tester2", CLI: "deepseek", LLM: "deepseek-v4-pro"},
	}, id)
	if err != nil || oc != OutcomeApplied {
		t.Fatalf("set=%s err=%v", oc, err)
	}
	e, _ = b.TaskLedger(ctx, "t1")
	if rb := e.Roles["tester"]; rb.LLM != "deepseek-v4-pro" {
		t.Fatalf("变更未生效:%+v", rb)
	}
	if rb := e.Roles["designer"]; rb.Who != "devo48" {
		t.Fatalf("未动角色被扰:%+v", rb)
	}
	// 半填(有 who 无 llm)=拒
	if _, err := b.SetTaskRoles(ctx, "t1", RoleAssignments{QAOwner: RoleBinding{Who: "x", CLI: "claude"}}, id); !errors.Is(err, ErrRolesIncomplete) {
		t.Fatalf("半填应拒:%v", err)
	}
}

// 回溯优化聚合:RoleStats 按(role,cli,llm)统计任务参与/完成。
func TestM5_RoleStatsAggregation(t *testing.T) {
	b, id, ctx := rig(t)
	// 第二个 task:implementer 换 codex/gpt-5.6
	r2 := testRoles()
	r2.Implementer = RoleBinding{Who: "dev56", CLI: "codex", LLM: "gpt-5.6"}
	if err := b.CreateTask(ctx, "t2", "board1", "y", r2, id); err != nil {
		t.Fatal(err)
	}
	// t1 全链完成(task.status 仍 planning——task 级状态推进属上层;此处按 task.status 聚合,先置 done 验聚合口径)
	if _, err := b.db.Exec(`UPDATE task SET status='done' WHERE task_id='t1'`); err != nil {
		t.Fatal(err)
	}
	stats, err := b.RoleStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	find := func(role, cli, llm string) *RoleStat {
		for i := range stats {
			if stats[i].Role == role && stats[i].CLI == cli && stats[i].LLM == llm {
				return &stats[i]
			}
		}
		return nil
	}
	if s := find("implementer", "claude", "claude-opus-4-8"); s == nil || s.Tasks != 1 || s.DoneTasks != 1 {
		t.Fatalf("claude implementer stat=%+v", s)
	}
	if s := find("implementer", "codex", "gpt-5.6"); s == nil || s.Tasks != 1 || s.DoneTasks != 0 {
		t.Fatalf("codex implementer stat=%+v", s)
	}
	if s := find("designer", "claude", "claude-opus-4-8"); s == nil || s.Tasks != 2 {
		t.Fatalf("designer stat=%+v", s)
	}
}

// ★调度者约束核心:全链跑完 board_event 零匿名(每条 agent_id 非空=每笔写可追责)。
func TestM5_NoAnonymousEvents(t *testing.T) {
	b, id, ctx := rig(t)
	toRunning(t, b, id, ctx, "rid-1")
	b.SteerReserve(ctx, "n1", 1, "steer-1", 60, id)
	b.SteerFinalize(ctx, "steer-1", true, id)
	b.Terminal(ctx, "n1", 1, "rid-1", true, "ref", id)
	b.Send(ctx, "n2", 1, "rid-2", "w1", id)
	b.Redeliver(ctx, "n2", 1, "rid-2", id)
	b.DeliverFail(ctx, "n2", 1, "rid-2", false, id)
	b.Processing(ctx, "n2", 1, "rid-2", id)
	b.Terminal(ctx, "n2", 1, "rid-2", false, "", id)
	b.SetTaskRoles(ctx, "t1", RoleAssignments{Designer: RoleBinding{Who: "a", CLI: "b", LLM: "c"}}, id)
	var anon int
	if err := b.db.QueryRow(`SELECT COUNT(*) FROM board_event WHERE agent_id IS NULL OR agent_id=''`).Scan(&anon); err != nil {
		t.Fatal(err)
	}
	if anon != 0 {
		t.Fatalf("匿名事件=%d want 0(结构化操作:每笔写可追责)", anon)
	}
	var total int
	b.db.QueryRow(`SELECT COUNT(*) FROM board_event`).Scan(&total)
	if total < 9 {
		t.Fatalf("事件太少(%d),链没跑全", total)
	}
}
