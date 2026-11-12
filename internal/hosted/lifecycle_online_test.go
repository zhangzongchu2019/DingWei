package hosted

// 板 79498c60：启用、重启与创建同口径——节点回 ok 后席位停在 starting，Agent 以专属凭据连入（MarkOnline）才 online；
// 永不连入 ⇒ 5 分钟 start_timeout。与新建不同：既有席位超时只记错，不吊销 Agent、不解绑账号，之后仍可重启。

import (
	"context"
	"testing"
	"time"
)

func TestEnableRestartWaitForAgentConnect(t *testing.T) {
	parallelHostedTest(t)
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
			if seat, _ := s.GetSeat(ctx, id); seat.State != "starting" || seat.StateDetail != op {
				t.Fatalf("after %s, before the agent connects: %s/%s, want starting/%s", op, seat.State, seat.StateDetail, op)
			}
			if err := o.MarkOnline(ctx, id); err != nil {
				t.Fatal(err)
			}
			if seat, _ := s.GetSeat(ctx, id); seat.State != "online" {
				t.Fatalf("after the agent connects: %s, want online", seat.State)
			}
		})
	}
}

// 重启后 Agent 永不连入：5 分钟记 start_timeout；但这是用户在用的既有席位 —— 不吊销、不解绑，且仍可再重启。
func TestRestartTimeoutKeepsExistingSeat(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	lc, _, _, _, _, s, id := mkLifecycle(t)
	idp := lc.idp.(*lcIdp).IdentityIssuer.(*fakeIdentity)
	o := NewOrchestrator(s, lc.pool, lc.idp, lc.gw, fakeCombos{m: map[string]bool{}}, fakeCatalog{})
	before, _ := s.GetSeat(ctx, id)
	if err := lc.Restart(ctx, id, "zzc", false); err != nil {
		t.Fatal(err)
	}
	o.SweepTimeouts(ctx, time.Now().Add(4*time.Minute))
	if seat, _ := s.GetSeat(ctx, id); seat.State != "starting" {
		t.Fatalf("swept too early: %+v", seat)
	}
	retiredBefore := len(idp.retired)
	o.SweepTimeouts(ctx, time.Now().Add(6*time.Minute))
	seat, _ := s.GetSeat(ctx, id)
	if seat.State != "error" || seat.StateDetail != "start_timeout" {
		t.Fatalf("restart never connected: %s/%s, want error/start_timeout", seat.State, seat.StateDetail)
	}
	if len(idp.retired) != retiredBefore {
		t.Fatalf("existing seat's agent must not be retired on restart timeout: %v", idp.retired)
	}
	// 0.2.1.101：占用以席位 account_ref 为准——既有席位超时后仍引用原账号（不释放）
	if ref, err := s.SeatAccountOf(ctx, id); err != nil || ref != before.AccountRef.String {
		t.Fatalf("existing seat's account reference must stay: %q %v", ref, err)
	}
	// 仍可再重启（guard 放行 error 态）
	if err := lc.Restart(ctx, id, "zzc", false); err != nil {
		t.Fatalf("restart again after timeout: %v", err)
	}
	if seat, _ := s.GetSeat(ctx, id); seat.State != "starting" {
		t.Fatalf("second restart: %+v", seat)
	}
}

// 对照：新建席位的 starting 超时仍整个回收（吊销＋解绑）——区分只看 state_detail，不能把新建也放过。
func TestCreateTimeoutStillRetires(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	o, idp, _, s := mkOrch(t)
	out, err := o.Create(ctx, CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "w1", NodeID: "auto",
		CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctOf(t, o.pool), Plan: "2c4g"})
	if err != nil {
		t.Fatal(err)
	}
	o.RunJob(ctx, out.AgentID)
	o.SweepTimeouts(ctx, time.Now().Add(6*time.Minute))
	retired := false
	for _, r := range idp.retired {
		retired = retired || r == out.AgentID
	}
	if !retired {
		t.Fatal("a create that never connects must still be retired")
	}
	if seat, _ := s.GetSeat(ctx, out.AgentID); seat.State != "error" || seat.StateDetail != "start_timeout" {
		t.Fatalf("create timeout: %+v", seat)
	}
}
