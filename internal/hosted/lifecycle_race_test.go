package hosted

// 组长 lead-enable-restart-race（板 49fec245）：
// ① Restart／Enable 先置 starting（detail）再发节点命令 —— Agent 在节点回 ok 之前连入也不丢；失败按原语义落
//    （Restart ⇒ error/start_failed；Enable ⇒ 回 disabled 并撤回 Resume）。
// ② 既有席位 error/start_timeout 之后 Agent 才连入 ⇒ MarkOnline 照样置 online；新建超时已回收，不涉及。

import (
	"context"
	"errors"
	"testing"
	"time"
)

// 复验方 tosonnet55tc2k 补格（REVIEW-G4-ENABLE-RESTART-ONLINE 附录，原样并入）：
// 节点命令执行成功的当下立刻 MarkOnline，模拟「容器里的 Agent 秒连入」。
type zzHookGw struct {
	NodeGateway
	hook func(cmdType string)
}

func (g *zzHookGw) Send(ctx context.Context, nodeID string, c NodeCommand) (NodeResult, error) {
	res, err := g.NodeGateway.Send(ctx, nodeID, c)
	if err == nil && res.OK && g.hook != nil {
		g.hook(c.Type) // 节点已执行命令；容器里的 Agent 立刻连入 ⇒ Hub 回调 MarkOnline
	}
	return res, err
}

func TestZZ_AgentConnectingBeforeStartingIsNotLost(t *testing.T) {
	ctx := context.Background()
	for _, op := range []string{"restart", "enable"} {
		t.Run(op, func(t *testing.T) {
			lc, _, _, _, _, s, id := mkLifecycle(t)
			o := NewOrchestrator(s, lc.pool, lc.idp, lc.gw, fakeCombos{m: map[string]bool{}}, fakeCatalog{})
			if op == "enable" {
				if err := lc.Disable(ctx, id, "zzc", false, true); err != nil {
					t.Fatal(err)
				}
			}
			want := map[string]string{"restart": "restart_seat", "enable": "start_seat"}[op]
			lc.gw = &zzHookGw{NodeGateway: lc.gw, hook: func(ct string) {
				if ct == want {
					_ = o.MarkOnline(ctx, id)
				}
			}}
			var err error
			if op == "enable" {
				err = lc.Enable(ctx, id, "zzc", false)
			} else {
				err = lc.Restart(ctx, id, "zzc", false)
			}
			if err != nil {
				t.Fatal(err)
			}
			seat, _ := s.GetSeat(ctx, id)
			t.Logf("ZZ %s: Agent connected during the node command; seat is now %s/%s", op, seat.State, seat.StateDetail)
			if seat.State != "online" {
				t.Fatalf("the Agent already connected but the seat is %s/%s (it will be timed out to error in 5 minutes)", seat.State, seat.StateDetail)
			}
		})
	}
}

// ① 失败按原语义落：节点报起不来时，重启 ⇒ error/start_failed；启用 ⇒ 回 disabled、凭据重新暂停。
// 节点不通 ⇒ 两者都回原态、凭据回原样（第 7 条不变）。
func TestEnableRestartFailureAfterStarting(t *testing.T) {
	ctx := context.Background()
	t.Run("restart node reports failure", func(t *testing.T) {
		lc, _, _, gw, _, s, id := mkLifecycle(t)
		gw.failOn = "restart_seat"
		if err := lc.Restart(ctx, id, "zzc", false); !errors.Is(err, ErrStartFailed) {
			t.Fatalf("%v, want start_failed", err)
		}
		if seat, _ := s.GetSeat(ctx, id); seat.State != "error" || seat.StateDetail != "start_failed" {
			t.Fatalf("restart failed: %s/%s, want error/start_failed", seat.State, seat.StateDetail)
		}
	})
	t.Run("restart node unreachable", func(t *testing.T) {
		lc, _, _, gw, _, s, id := mkLifecycle(t)
		gw.downOn = "restart_seat"
		if err := lc.Restart(ctx, id, "zzc", false); !errors.Is(err, ErrNodeOffline) {
			t.Fatalf("%v, want node_offline", err)
		}
		if seat, _ := s.GetSeat(ctx, id); seat.State != "online" || seat.StateDetail != "" {
			t.Fatalf("restart node offline: %s/%s, want online/ unchanged", seat.State, seat.StateDetail)
		}
	})
	for _, c := range []struct {
		name    string
		down    bool
		wantErr error
	}{{"enable node reports failure", false, ErrStartFailed}, {"enable node unreachable", true, ErrNodeOffline}} {
		t.Run(c.name, func(t *testing.T) {
			lc, _, susp, gw, _, s, id := mkLifecycle(t)
			if err := lc.Disable(ctx, id, "zzc", false, true); err != nil {
				t.Fatal(err)
			}
			if c.down {
				gw.downOn = "start_seat"
			} else {
				gw.failOn = "start_seat"
			}
			if err := lc.Enable(ctx, id, "zzc", false); !errors.Is(err, c.wantErr) {
				t.Fatalf("%v, want %v", err, c.wantErr)
			}
			if seat, _ := s.GetSeat(ctx, id); seat.State != "disabled" {
				t.Fatalf("enable failed: %s/%s, want disabled", seat.State, seat.StateDetail)
			}
			if !susp.suspended {
				t.Fatal("enable failed but the credential stayed resumed (Resume must be undone)")
			}
		})
	}
}

// ① 失败回写是条件迁移：命令途中 Agent 已连入（online）后节点才报失败／不通，不得把 online 盖回去、不得暂停它的凭据。
func TestFailureAfterAgentConnectedKeepsOnline(t *testing.T) {
	ctx := context.Background()
	lc, _, susp, gw, _, s, id := mkLifecycle(t)
	o := NewOrchestrator(s, lc.pool, lc.idp, lc.gw, fakeCombos{m: map[string]bool{}}, fakeCatalog{})
	if err := lc.Disable(ctx, id, "zzc", false, true); err != nil {
		t.Fatal(err)
	}
	gw.downOn = "start_seat"
	lc.gw = &zzConnectThenGw{NodeGateway: gw, connect: func() { _ = o.MarkOnline(ctx, id) }}
	if err := lc.Enable(ctx, id, "zzc", false); !errors.Is(err, ErrNodeOffline) {
		t.Fatalf("%v, want node_offline", err)
	}
	if seat, _ := s.GetSeat(ctx, id); seat.State != "online" {
		t.Fatalf("agent connected during the command: %s/%s, want online kept", seat.State, seat.StateDetail)
	}
	if susp.suspended {
		t.Fatal("agent is online; its credential must not be suspended by the failure rollback")
	}
}

// zzConnectThenGw 发命令前先让 Agent 连入（无论结果如何）。
type zzConnectThenGw struct {
	NodeGateway
	connect func()
}

func (g *zzConnectThenGw) Send(ctx context.Context, nodeID string, c NodeCommand) (NodeResult, error) {
	g.connect()
	return g.NodeGateway.Send(ctx, nodeID, c)
}

// ② 既有席位超时后晚连入 ⇒ online（重启、启用两条）。
func TestLateConnectAfterExistingTimeoutGoesOnline(t *testing.T) {
	ctx := context.Background()
	for _, op := range []string{"restart", "enable"} {
		t.Run(op, func(t *testing.T) {
			lc, _, _, _, _, s, id := mkLifecycle(t)
			o := NewOrchestrator(s, lc.pool, lc.idp, lc.gw, fakeCombos{m: map[string]bool{}}, fakeCatalog{})
			if op == "enable" {
				if err := lc.Disable(ctx, id, "zzc", false, true); err != nil {
					t.Fatal(err)
				}
				if err := lc.Enable(ctx, id, "zzc", false); err != nil {
					t.Fatal(err)
				}
			} else if err := lc.Restart(ctx, id, "zzc", false); err != nil {
				t.Fatal(err)
			}
			o.SweepTimeouts(ctx, time.Now().Add(6*time.Minute))
			if seat, _ := s.GetSeat(ctx, id); seat.State != "error" || seat.StateDetail != "start_timeout" {
				t.Fatalf("precondition: %s/%s, want error/start_timeout", seat.State, seat.StateDetail)
			}
			if err := o.MarkOnline(ctx, id); err != nil {
				t.Fatal(err)
			}
			if seat, _ := s.GetSeat(ctx, id); seat.State != "online" || seat.StateDetail != "" {
				t.Fatalf("late connect after %s timeout: %s/%s, want online", op, seat.State, seat.StateDetail)
			}
		})
	}
}

// ② 反面：只认「既有席位超时」——别的 error（重启起不来 start_failed、新建超时）MarkOnline 不动。
func TestLateConnectOnlyForExistingTimeout(t *testing.T) {
	ctx := context.Background()
	t.Run("start_failed stays error", func(t *testing.T) {
		lc, _, _, gw, _, s, id := mkLifecycle(t)
		o := NewOrchestrator(s, lc.pool, lc.idp, lc.gw, fakeCombos{m: map[string]bool{}}, fakeCatalog{})
		gw.failOn = "restart_seat"
		_ = lc.Restart(ctx, id, "zzc", false)
		if err := o.MarkOnline(ctx, id); err != nil {
			t.Fatal(err)
		}
		if seat, _ := s.GetSeat(ctx, id); seat.State != "error" || seat.StateDetail != "start_failed" {
			t.Fatalf("start_failed + connect: %s/%s, want error/start_failed unchanged", seat.State, seat.StateDetail)
		}
	})
	t.Run("create timeout stays error", func(t *testing.T) {
		o, _, _, s := mkOrch(t)
		out, err := o.Create(ctx, CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "w1", NodeID: "auto",
			CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctOf(t, o.pool), Plan: "2c4g"})
		if err != nil {
			t.Fatal(err)
		}
		o.RunJob(ctx, out.AgentID)
		o.SweepTimeouts(ctx, time.Now().Add(6*time.Minute))
		if err := o.MarkOnline(ctx, out.AgentID); err != nil {
			t.Fatal(err)
		}
		if seat, _ := s.GetSeat(ctx, out.AgentID); seat.State != "error" || seat.StateDetail != "start_timeout" {
			t.Fatalf("create timeout + connect: %s/%s, want error/start_timeout unchanged", seat.State, seat.StateDetail)
		}
	})
}
