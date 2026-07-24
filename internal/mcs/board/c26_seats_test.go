package board

import (
	"context"
	"strings"
	"testing"
)

// c26task 建自定义角色 task(board1 上;四角色 CLI/LLM 固定,只变 who)。
func c26task(t *testing.T, b *Board, ctx context.Context, id Identity, taskID, des, imp, tst, qa string) {
	t.Helper()
	rb := func(w string) RoleBinding { return RoleBinding{Who: w, CLI: "claude", LLM: "claude-opus-4-8"} }
	if err := b.CreateTask(ctx, taskID, "board1", taskID,
		RoleAssignments{Designer: rb(des), Implementer: rb(imp), Tester: rb(tst), QAOwner: rb(qa)}, id); err != nil {
		t.Fatalf("create %s: %v", taskID, err)
	}
}

func taskEvents(t *testing.T, b *Board, taskID, kind string) int {
	t.Helper()
	var n int
	if err := b.db.QueryRow(`SELECT COUNT(*) FROM board_event WHERE task_id=? AND kind=?`, taskID, kind).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func rolePromoted(t *testing.T, b *Board, taskID, role string) int {
	t.Helper()
	var p int
	if err := b.db.QueryRow(`SELECT promoted FROM task_role WHERE task_id=? AND role=?`, taskID, role).Scan(&p); err != nil {
		t.Fatal(err)
	}
	return p
}

// 放行例:轻角色不占席——四角色四个不同 who 建单成功(执行席只数 implementer+tester=2);
// implementer==tester 同人=1 席亦放行。
func TestC26_LightRolesDontOccupySeats(t *testing.T) {
	b, id, ctx := rig(t)
	c26task(t, b, ctx, id, "t-4who", "d1", "i1", "x1", "q1") // 4 distinct who,席位={i1,x1}=2 → 放行
	c26task(t, b, ctx, id, "t-1seat", "d1", "same", "same", "q1")
}

// ★拒例(criteria §3.2 C2.6):第 3 非豁免席升格 → rejected 零写入(不静默突破)。
// rig 角色:designer=devo48/impl=devo48/tester=tester/qa=shence;席位={devo48,tester},
// 升格 qa_owner(shence)=第 3 distinct who → 拒。
func TestC26_PromoteThirdSeatRejected(t *testing.T) {
	b, id, ctx := rig(t)
	oc, err := b.PromoteExecSeat(ctx, "t1", "qa_owner", id)
	if err != nil {
		t.Fatal(err)
	}
	mustOutcome(t, oc, OutcomeRejected, "第3席升格")
	if rolePromoted(t, b, "t1", "qa_owner") != 0 {
		t.Fatal("拒后 promoted 应保持 0(零写入)")
	}
	if taskEvents(t, b, "t1", "role_promoted") != 0 {
		t.Fatal("拒后不应有 role_promoted 事件")
	}
}

// 放行例:升格 who 已占席(designer=devo48 同 implementer)→ 席位数不变=2 → applied+事件 actor=调度者。
func TestC26_PromoteSameWhoAllowed(t *testing.T) {
	b, id, ctx := rig(t)
	oc, err := b.PromoteExecSeat(ctx, "t1", "designer", id)
	if err != nil {
		t.Fatal(err)
	}
	mustOutcome(t, oc, OutcomeApplied, "同 who 升格")
	if rolePromoted(t, b, "t1", "designer") != 1 {
		t.Fatal("升格后 promoted 应=1")
	}
	var actor, data string
	if err := b.db.QueryRow(`SELECT agent_id, data FROM board_event WHERE task_id='t1' AND kind='role_promoted'`).Scan(&actor, &data); err != nil {
		t.Fatal(err)
	}
	if actor != "orch1" {
		t.Fatalf("role_promoted actor 应=调度者 orch1,got %s", actor)
	}
	if !strings.Contains(data, `"role":"designer"`) || !strings.Contains(data, `"who":"devo48"`) {
		t.Fatalf("事件 data 应含 role/who,got %s", data)
	}
}

// 幂等:同角色二次升格 → applied no-op,事件仍恰 1 条。
func TestC26_PromoteIdempotent(t *testing.T) {
	b, id, ctx := rig(t)
	if oc, _ := b.PromoteExecSeat(ctx, "t1", "designer", id); oc != OutcomeApplied {
		t.Fatalf("首次升格应 applied,got %s", oc)
	}
	if oc, _ := b.PromoteExecSeat(ctx, "t1", "designer", id); oc != OutcomeApplied {
		t.Fatalf("重投应幂等 applied,got %s", oc)
	}
	if n := taskEvents(t, b, "t1", "role_promoted"); n != 1 {
		t.Fatalf("幂等重投不应二次落事件,role_promoted=%d want 1", n)
	}
}

// default-deny:执行席角色/未知角色名升格 → rejected(无升格语义)。
func TestC26_PromoteInvalidRoleRejected(t *testing.T) {
	b, id, ctx := rig(t)
	for _, role := range []string{"implementer", "tester", "hacker", ""} {
		if oc, _ := b.PromoteExecSeat(ctx, "t1", role, id); oc != OutcomeRejected {
			t.Fatalf("role=%q 应 default-deny rejected,got %s", role, oc)
		}
	}
}

// ★豁免路径:GrantRoleExemption(guarded+reason 必填+role_exemption 事件)后,第 3 席升格放行。
func TestC26_PromoteWithExemption(t *testing.T) {
	b, id, ctx := rig(t)
	// 拒:空 reason 豁免(不可审计)
	if oc, _ := b.GrantRoleExemption(ctx, "t1", "shence", "", id); oc != OutcomeRejected {
		t.Fatal("空 reason 豁免应拒")
	}
	oc, err := b.GrantRoleExemption(ctx, "t1", "shence", "评审需要第3席参与修复", id)
	if err != nil {
		t.Fatal(err)
	}
	mustOutcome(t, oc, OutcomeApplied, "豁免")
	var actor, data string
	if err := b.db.QueryRow(`SELECT agent_id, data FROM board_event WHERE task_id='t1' AND kind='role_exemption'`).Scan(&actor, &data); err != nil {
		t.Fatal(err)
	}
	if actor != "orch1" || !strings.Contains(data, `"who":"shence"`) || !strings.Contains(data, "reason") {
		t.Fatalf("role_exemption 事件应 actor=调度者+data 含 who/reason,got %s/%s", actor, data)
	}
	// 豁免后:shence 不计席 → 升格 qa_owner 放行
	if oc, _ := b.PromoteExecSeat(ctx, "t1", "qa_owner", id); oc != OutcomeApplied {
		t.Fatalf("豁免后升格应 applied,got %s", oc)
	}
	// 幂等:同 (task,who) 重授 → applied no-op,事件仍恰 1 条
	if oc, _ := b.GrantRoleExemption(ctx, "t1", "shence", "重复授权", id); oc != OutcomeApplied {
		t.Fatal("重授豁免应幂等 applied")
	}
	if n := taskEvents(t, b, "t1", "role_exemption"); n != 1 {
		t.Fatalf("豁免重授不应二次落事件,role_exemption=%d want 1", n)
	}
}

// ★guard 拒例:非调度者/旧 epoch 发升格·豁免 → cas_miss 零写入(不可匿名突破)。
func TestC26_GuardNonOrchestrator(t *testing.T) {
	b, id, ctx := rig(t)
	evil := Identity{Owner: "zzc-evil-1", Epoch: id.Epoch}
	if oc, _ := b.PromoteExecSeat(ctx, "t1", "designer", evil); oc != OutcomeCASMiss {
		t.Fatal("非调度者升格应 cas_miss")
	}
	if oc, _ := b.GrantRoleExemption(ctx, "t1", "shence", "越权豁免", evil); oc != OutcomeCASMiss {
		t.Fatal("非调度者豁免应 cas_miss")
	}
	stale := Identity{Owner: id.Owner, Epoch: id.Epoch + 99}
	if oc, _ := b.PromoteExecSeat(ctx, "t1", "designer", stale); oc != OutcomeCASMiss {
		t.Fatal("旧 epoch 升格应 cas_miss")
	}
	if rolePromoted(t, b, "t1", "designer") != 0 || taskEvents(t, b, "t1", "role_exemption") != 0 {
		t.Fatal("guard 拒后应零写入")
	}
}

// ★换席路径:第 3 席拒 → 调度者换席(tester 换成该 who)→ 升格放行(席位收敛回 2)。
func TestC26_SwapSeatThenPromote(t *testing.T) {
	b, id, ctx := rig(t)
	if oc, _ := b.PromoteExecSeat(ctx, "t1", "qa_owner", id); oc != OutcomeRejected {
		t.Fatal("换席前第3席应拒")
	}
	// 换席:tester who tester→shence(席位={devo48,shence})
	swap := RoleAssignments{Tester: RoleBinding{Who: "shence", CLI: "claude", LLM: "claude-opus-4-8"}}
	if oc, _ := b.SetTaskRoles(ctx, "t1", swap, id); oc != OutcomeApplied {
		t.Fatal("换席应 applied")
	}
	if oc, _ := b.PromoteExecSeat(ctx, "t1", "qa_owner", id); oc != OutcomeApplied {
		t.Fatal("换席后升格(who=shence 已占席)应 applied")
	}
}

// ★变更守门拒例:已升格轻角色换 who 会产生第 3 非豁免席 → SetTaskRoles rejected 零写入
// (promoted 不随 who 变更隐式释放=不可借换人绕 ≤2)。
func TestC26_RoleChangeCannotBreach(t *testing.T) {
	b, id, ctx := rig(t)
	// designer(devo48,与 impl 同人)升格:席位仍 {devo48,tester}=2
	if oc, _ := b.PromoteExecSeat(ctx, "t1", "designer", id); oc != OutcomeApplied {
		t.Fatal("同 who 升格应 applied")
	}
	// 换 designer.who→newguy:promoted 席位随人头变成 {devo48,tester,newguy}=3 → 拒
	ch := RoleAssignments{Designer: RoleBinding{Who: "newguy", CLI: "claude", LLM: "claude-opus-4-8"}}
	oc, err := b.SetTaskRoles(ctx, "t1", ch, id)
	if err != nil {
		t.Fatal(err)
	}
	mustOutcome(t, oc, OutcomeRejected, "变更产生第3席")
	var who string
	if err := b.db.QueryRow(`SELECT who FROM task_role WHERE task_id='t1' AND role='designer'`).Scan(&who); err != nil {
		t.Fatal(err)
	}
	if who != "devo48" {
		t.Fatalf("拒后 designer.who 应回滚为 devo48(零写入),got %s", who)
	}
	// 豁免 newguy 后同一变更放行
	if oc, _ := b.GrantRoleExemption(ctx, "t1", "newguy", "阶段性第3席", id); oc != OutcomeApplied {
		t.Fatal("豁免应 applied")
	}
	if oc, _ := b.SetTaskRoles(ctx, "t1", ch, id); oc != OutcomeApplied {
		t.Fatal("豁免后变更应 applied")
	}
}
