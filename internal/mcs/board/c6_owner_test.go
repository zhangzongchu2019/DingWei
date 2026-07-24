package board

import (
	"context"
	"testing"
)

// c6rig:rig(orchestrator epoch=1/node n1 ready)+启用 C6+两业主(测跨 principal 守卫)。
func c6rig(t *testing.T) (*Board, context.Context) {
	b, _, ctx := rig(t)
	b.SetEmergencyEnabled(true)
	b.SetOwnerPrincipals(map[string]bool{"zzc-owner-2642": true, "zzc-owner2-2642": true})
	return b, ctx
}

// ★C6.3 过渡期硬禁用:emergencyEnabled=false(默认)→两原语 disabled、不执行(fail-closed)。
func TestC6_TransitionHardDisabled(t *testing.T) {
	b, _, ctx := rig(t)
	b.SetOwnerPrincipals(map[string]bool{"zzc-owner-2642": true}) // 未 SetEmergencyEnabled → 默认 false
	if oc, _ := b.OwnerTakeover(ctx, "board1", OwnerTakeoverReq{OwnerPrincipal: "zzc-owner-2642", ToOwner: "zzc-neworch-2642", ExpectedEpoch: 1, IdempotencyID: "d1"}); oc != OwnerDisabled {
		t.Fatalf("过渡期 takeover 应 disabled,got %s", oc)
	}
	if oc, _ := b.OwnerOverride(ctx, OwnerOverrideReq{OwnerPrincipal: "zzc-owner-2642", Target: "n1", Directive: "pause", ExpectedEpoch: 1, ExpectedState: "ready", IdempotencyID: "d2"}); oc != OwnerDisabled {
		t.Fatalf("过渡期 override 应 disabled,got %s", oc)
	}
	// 硬禁用=不执行:epoch/node 未动
	var ep int64
	b.db.QueryRow(`SELECT epoch FROM orchestrator WHERE board_id='board1'`).Scan(&ep)
	if ep != 1 {
		t.Fatalf("过渡期硬禁用不应改 epoch,got %d", ep)
	}
}

// OwnerTakeover:非业主拒/cas_miss/applied+actor=业主/幂等 no-op/★shence E 跨principal拒。
func TestC6_OwnerTakeover(t *testing.T) {
	b, ctx := c6rig(t)
	// 非业主拒
	if oc, _ := b.OwnerTakeover(ctx, "board1", OwnerTakeoverReq{OwnerPrincipal: "zzc-dev55-2642", ToOwner: "zzc-neworch-2642", ExpectedEpoch: 1, IdempotencyID: "nonown"}); oc != OwnerRejected {
		t.Fatalf("非业主应拒,got %s", oc)
	}
	// schema:to_owner 非法 zzc-* 拒
	if oc, _ := b.OwnerTakeover(ctx, "board1", OwnerTakeoverReq{OwnerPrincipal: "zzc-owner-2642", ToOwner: "bad name", ExpectedEpoch: 1, IdempotencyID: "badto"}); oc != OwnerRejected {
		t.Fatalf("to_owner 非法应拒,got %s", oc)
	}
	// cas_miss:陈旧 expected_epoch(不跨代/不自动重取)
	if oc, _ := b.OwnerTakeover(ctx, "board1", OwnerTakeoverReq{OwnerPrincipal: "zzc-owner-2642", ToOwner: "zzc-neworch-2642", ExpectedEpoch: 99, IdempotencyID: "stale"}); oc != OwnerCASMiss {
		t.Fatalf("陈旧 epoch 应 cas_miss,got %s", oc)
	}
	// applied:epoch 1→2,owner=to_owner
	if oc, _ := b.OwnerTakeover(ctx, "board1", OwnerTakeoverReq{OwnerPrincipal: "zzc-owner-2642", ToOwner: "zzc-neworch-2642", ExpectedEpoch: 1, IdempotencyID: "ok", Reason: "整体故障"}); oc != OwnerApplied {
		t.Fatalf("应 applied,got %s", oc)
	}
	var ep int64
	var ow string
	b.db.QueryRow(`SELECT epoch, owner FROM orchestrator WHERE board_id='board1'`).Scan(&ep, &ow)
	if ep != 2 || ow != "zzc-neworch-2642" {
		t.Fatalf("epoch/owner 应 2/zzc-neworch-2642,got %d/%s", ep, ow)
	}
	// ★事件 actor=业主 principal(不冒充调度者 Identity)
	var actor string
	b.db.QueryRow(`SELECT agent_id FROM board_event WHERE kind='owner_takeover'`).Scan(&actor)
	if actor != "zzc-owner-2642" {
		t.Fatalf("事件 actor 应=业主 principal,got %s", actor)
	}
	// 幂等 no-op:同 id+principal 重投→返原 applied,不二次夺权
	if oc, _ := b.OwnerTakeover(ctx, "board1", OwnerTakeoverReq{OwnerPrincipal: "zzc-owner-2642", ToOwner: "zzc-neworch-2642", ExpectedEpoch: 1, IdempotencyID: "ok"}); oc != OwnerApplied {
		t.Fatalf("幂等重投应返原 applied,got %s", oc)
	}
	b.db.QueryRow(`SELECT epoch FROM orchestrator WHERE board_id='board1'`).Scan(&ep)
	if ep != 2 {
		t.Fatalf("幂等重投不应二次夺权,epoch=%d want 2", ep)
	}
	// ★shence E:另一业主复用同 idempotency_id → 拒(不返他人 outcome)
	if oc, _ := b.OwnerTakeover(ctx, "board1", OwnerTakeoverReq{OwnerPrincipal: "zzc-owner2-2642", ToOwner: "zzc-x-2642", ExpectedEpoch: 2, IdempotencyID: "ok"}); oc != OwnerRejected {
		t.Fatalf("★shence E 跨 principal 复用同 id 应拒,got %s", oc)
	}
}

// OwnerOverride:白名单外拒/applied+node变更+actor=业主/cas_miss(expected_state)/幂等。
func TestC6_OwnerOverride(t *testing.T) {
	b, ctx := c6rig(t)
	// directive 白名单外拒
	if oc, _ := b.OwnerOverride(ctx, OwnerOverrideReq{OwnerPrincipal: "zzc-owner-2642", Target: "n1", Directive: "delete", ExpectedEpoch: 1, ExpectedState: "ready", IdempotencyID: "wl"}); oc != OwnerRejected {
		t.Fatalf("白名单外 directive 应拒,got %s", oc)
	}
	// cas_miss:expected_state 不符(n1 是 ready,给 running)
	if oc, _ := b.OwnerOverride(ctx, OwnerOverrideReq{OwnerPrincipal: "zzc-owner-2642", Target: "n1", Directive: "pause", ExpectedEpoch: 1, ExpectedState: "running", IdempotencyID: "cs"}); oc != OwnerCASMiss {
		t.Fatalf("expected_state 不符应 cas_miss,got %s", oc)
	}
	// applied:pause n1(ready→blocked)
	if oc, _ := b.OwnerOverride(ctx, OwnerOverrideReq{OwnerPrincipal: "zzc-owner-2642", Target: "n1", Directive: "pause", ExpectedEpoch: 1, ExpectedState: "ready", IdempotencyID: "ov-ok", Reason: "急停"}); oc != OwnerApplied {
		t.Fatalf("应 applied,got %s", oc)
	}
	var st string
	b.db.QueryRow(`SELECT status FROM node WHERE node_id='n1'`).Scan(&st)
	if st != "blocked" {
		t.Fatalf("pause 后 n1 应 blocked,got %s", st)
	}
	var actor string
	b.db.QueryRow(`SELECT agent_id FROM board_event WHERE kind='owner_override'`).Scan(&actor)
	if actor != "zzc-owner-2642" {
		t.Fatalf("override 事件 actor 应=业主,got %s", actor)
	}
	// 幂等 no-op:同 id 重投→返原 applied(不再变更)
	if oc, _ := b.OwnerOverride(ctx, OwnerOverrideReq{OwnerPrincipal: "zzc-owner-2642", Target: "n1", Directive: "pause", ExpectedEpoch: 1, ExpectedState: "ready", IdempotencyID: "ov-ok"}); oc != OwnerApplied {
		t.Fatalf("override 幂等重投应返原 applied,got %s", oc)
	}
}
