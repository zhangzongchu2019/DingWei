package board

import (
	"testing"
)

// ---- M5:治理扩展(张宗楚 07-23:看板=项目管理+调度者约束+结构化操作) ----

// 角色登记:guarded 写+事件;部分更新(空串不覆盖);台账读回。
func TestM5_RolesSetAndLedger(t *testing.T) {
	b, id, ctx := rig(t)
	oc, err := b.SetTaskRoles(ctx, "t1", Roles{
		Designer: "devo48", Implementer: "devo48", Tester: "tester", QAOwner: "shenceclaude",
	}, id)
	if err != nil {
		t.Fatal(err)
	}
	if oc != OutcomeApplied {
		t.Fatalf("roles=%s", oc)
	}
	// 部分更新:只改 tester,其余保留
	if oc, _ := b.SetTaskRoles(ctx, "t1", Roles{Tester: "tester2"}, id); oc != OutcomeApplied {
		t.Fatalf("partial=%s", oc)
	}
	e, err := b.TaskLedger(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Roles.Designer != "devo48" || e.Roles.Tester != "tester2" || e.Roles.QAOwner != "shenceclaude" {
		t.Fatalf("roles=%+v", e.Roles)
	}
	if e.Dispatcher != "orch1" {
		t.Fatalf("dispatcher=%s want orch1(问责主体)", e.Dispatcher)
	}
	// 僵尸身份改角色:拒(调度者约束)
	if oc, _ := b.SetTaskRoles(ctx, "t1", Roles{QAOwner: "x"}, Identity{Owner: "ghost", Epoch: 99}); oc != OutcomeCASMiss {
		t.Fatalf("僵尸身份应拒:%s", oc)
	}
}

// 台账起止时间:首 started/全终态才有 ended;进度计数。
func TestM5_LedgerTimesAndProgress(t *testing.T) {
	b, id, ctx := rig(t)
	toRunning(t, b, id, ctx, "rid-1") // n1 running(started_ts 落)
	e, _ := b.TaskLedger(ctx, "t1")
	if e.StartedTS == 0 {
		t.Fatal("started 应已落")
	}
	if e.EndedTS != 0 {
		t.Fatal("未全终态不得有 ended")
	}
	if e.NodesTotal != 2 || e.NodesDone != 0 {
		t.Fatalf("progress=%d/%d", e.NodesDone, e.NodesTotal)
	}
	// n1 done→n2 提升 ready(非终态)⇒仍无 ended;n2 走完⇒ended 落
	b.Terminal(ctx, "n1", 1, "rid-1", true, "", id)
	e, _ = b.TaskLedger(ctx, "t1")
	if e.EndedTS != 0 || e.NodesDone != 1 {
		t.Fatalf("中途:%+v", e)
	}
	b.Send(ctx, "n2", 1, "rid-2", "w1", id)
	b.Processing(ctx, "n2", 1, "rid-2", id)
	b.Terminal(ctx, "n2", 1, "rid-2", true, "", id)
	e, _ = b.TaskLedger(ctx, "t1")
	if e.EndedTS == 0 || e.NodesDone != 2 {
		t.Fatalf("收口后:%+v", e)
	}
}

// ★调度者约束核心:全链跑完后,board_event 零匿名(每条 agent_id 非空=谁做的写全可追责)。
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
	b.SetTaskRoles(ctx, "t1", Roles{Designer: "devo48"}, id)
	var anon int
	if err := b.db.QueryRow(`SELECT COUNT(*) FROM board_event WHERE agent_id IS NULL OR agent_id=''`).Scan(&anon); err != nil {
		t.Fatal(err)
	}
	if anon != 0 {
		t.Fatalf("匿名事件=%d want 0(结构化操作:每笔写可追责)", anon)
	}
	var total int
	b.db.QueryRow(`SELECT COUNT(*) FROM board_event`).Scan(&total)
	if total < 8 {
		t.Fatalf("事件太少(%d),链没跑全", total)
	}
}
