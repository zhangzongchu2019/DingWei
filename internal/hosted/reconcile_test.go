package hosted

// ⑤ 再修（板 3d0fc58f，第二十二条）：对账与 guard／Upgrade 的补格。
// TestZZ_Reconcile… 并入自复验件 REVIEW-G4-T50-REWORK-tosonnet55tc2k-20261001.md 附录 A.2，
// 断言原样；假件改用本包既有的 evLog／lcGw（原件的 zzLog／zzGw 未入仓）。

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func zzPendingSeat(t *testing.T, s *Store, id, nodeID string) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateSeat(ctx, Seat{AgentID: id, NodeID: nodeID, CLI: "codex", LLM: "gpt", CredKind: "token_file", Plan: "2c4g",
		ImageRef: "hub.local/dingwei/x@sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", CreatedBy: "zzc"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSeatState(ctx, id, "destroyed", "", "destroy", json.RawMessage(`{"backup":"x","ttl_days":14,"reconcile_pending":true}`)); err != nil {
		t.Fatal(err)
	}
}

// 命令必须是 destroy_seat；节点回失败不得标记完成；只清本节点的席位（复验 R3／R4／R5 三刀）。
func TestZZ_ReconcileSendsDestroyOnlyForThisNodeAndOnlyMarksDoneOnSuccess(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	o, _, _, s := mkOrch(t)
	zzPendingSeat(t, s, "ag_ra", "nd_free")
	zzPendingSeat(t, s, "ag_rb", "nd_full") // 另一个节点的待对账席位
	log := &evLog{}
	gw := &lcGw{log: log}
	orch := NewOrchestrator(s, o.pool, o.idp, gw, fakeCombos{m: map[string]bool{}}, fakeCatalog{})

	// ① 节点回失败：不得标记完成，仍待对账
	gw.failOn = "destroy_seat"
	orch.ReconcileNode(ctx, "nd_free")
	eqSeq(t, "commands on failure", log.get(), []string{"node:destroy_seat"}) // 命令名在这一步就钉住（复验注：原格 R3 红在别处）
	if ids, _ := s.SeatsPendingReconcile(ctx, "nd_free"); len(ids) != 1 {
		t.Fatalf("node answered failure but the seat is no longer pending: %v", ids)
	}
	// ② 节点回成功：只清本节点的席位，发的必须是 destroy_seat
	log.reset()
	gw.failOn = ""
	orch.ReconcileNode(ctx, "nd_free")
	eqSeq(t, "commands", log.get(), []string{"node:destroy_seat"})
	if ids, _ := s.SeatsPendingReconcile(ctx, "nd_free"); len(ids) != 0 {
		t.Fatalf("still pending after a successful reconcile: %v", ids)
	}
	if ids, _ := s.SeatsPendingReconcile(ctx, "nd_full"); len(ids) != 1 {
		t.Fatalf("the other node's seat must still be pending: %v", ids)
	}
}

// stallGw 第一个被问到的席位一直不回（直到 ctx 结束），其余立刻回 ok。
type stallGw struct {
	mu      sync.Mutex
	stalled string
	asked   []string
}

func (g *stallGw) Send(ctx context.Context, nodeID string, c NodeCommand) (NodeResult, error) {
	var p struct {
		SeatID string `json:"seat_id"`
	}
	_ = json.Unmarshal(c.Params, &p)
	g.mu.Lock()
	g.asked = append(g.asked, p.SeatID)
	if g.stalled == "" {
		g.stalled = p.SeatID
	}
	stall := g.stalled == p.SeatID
	g.mu.Unlock()
	if stall {
		<-ctx.Done()
		return NodeResult{}, ctx.Err()
	}
	return NodeResult{OK: true}, nil
}

// 每个席位带超时：一个席位不回，不得卡死整批；不回的那个仍待对账，其余照常完成。
func TestReconcileSeatTimeoutDoesNotStallBatch(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	o, _, _, s := mkOrch(t)
	zzPendingSeat(t, s, "ag_r1", "nd_free")
	zzPendingSeat(t, s, "ag_r2", "nd_free")
	saved := ReconcileSeatTimeout
	ReconcileSeatTimeout = 300 * time.Millisecond
	t.Cleanup(func() { ReconcileSeatTimeout = saved })
	gw := &stallGw{}
	orch := NewOrchestrator(s, o.pool, o.idp, gw, fakeCombos{m: map[string]bool{}}, fakeCatalog{})

	done := make(chan struct{})
	go func() { orch.ReconcileNode(ctx, "nd_free"); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ReconcileNode stalled on a seat that never answers")
	}
	ids, _ := s.SeatsPendingReconcile(ctx, "nd_free")
	if len(gw.asked) != 2 || len(ids) != 1 || ids[0] != gw.stalled {
		t.Fatalf("asked %v, still pending %v; want both asked and only the stalled %s pending", gw.asked, ids, gw.stalled)
	}
}

// created_by 为空串：归属不明，四操作与销毁一律 not_owner，管理用户也不豁免；什么都不发生。
func TestEmptyCreatedByRefused(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	lc, log, _, _, _, s, _ := mkLifecycle(t)
	if err := s.CreateSeat(ctx, Seat{AgentID: "ag_orphan", NodeID: "nd_free", CLI: "codex", LLM: "gpt", CredKind: "token_file",
		Plan: "2c4g", ImageRef: "r", CreatedBy: ""}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSeatState(ctx, "ag_orphan", "online", "", "online", nil); err != nil {
		t.Fatal(err)
	}
	log.reset()
	for _, actor := range []struct {
		lc    string
		admin bool
	}{{"zzc", false}, {"admin", true}} {
		for name, call := range map[string]func() error{
			"restart": func() error { return lc.Restart(ctx, "ag_orphan", actor.lc, actor.admin) },
			"upgrade": func() error { return lc.Upgrade(ctx, "ag_orphan", actor.lc, newerImage, actor.admin) },
			"disable": func() error { return lc.Disable(ctx, "ag_orphan", actor.lc, actor.admin, true) },
			"destroy": func() error { return lc.Destroy(ctx, "ag_orphan", actor.lc, actor.admin, true) },
		} {
			if err := call(); !errors.Is(err, ErrNotOwner) {
				t.Errorf("%s by %s (admin=%v): %v, want not_owner", name, actor.lc, actor.admin, err)
			}
		}
	}
	eqSeq(t, "orphan seat", log.get(), nil)
	if seat, _ := s.GetSeat(ctx, "ag_orphan"); seat.State != "online" {
		t.Fatalf("seat changed: %+v", seat)
	}
}

// Upgrade：席位套餐查不到 ⇒ params 错（handler 译 params_invalid），且在改状态、签令牌之前就停。
func TestUpgradeUnknownPlanNotSwallowed(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	lc, log, _, _, _, s, id := mkLifecycle(t)
	saved := plans["2c4g"]
	delete(plans, "2c4g")
	t.Cleanup(func() { plans["2c4g"] = saved })
	err := lc.Upgrade(ctx, id, "zzc", newerImage, false)
	if !errors.Is(err, ErrParams) || errors.Is(err, ErrRolledBack) {
		t.Fatalf("%v, want ErrParams (not rolled_back)", err)
	}
	eqSeq(t, "unknown plan", log.get(), nil) // 没签令牌、没发 upgrade_seat
	if seat, _ := s.GetSeat(ctx, id); seat.State != "online" {
		t.Fatalf("seat changed: %+v", seat)
	}
}
