package hosted

// 板 ac33b1cb：容器起来 ≠ online。RunJob 停在 starting；Agent 以专属凭据连入（MarkOnline）才 online；
// 永不连入 ⇒ 清扫按 starting 5 分钟清成 start_timeout（契约第四节「进度」码）。

import (
	"context"
	"testing"
	"time"
)

func TestStartingUntilAgentConnectsElseTimeout(t *testing.T) {
	ctx := context.Background()
	o, idp, _, s := mkOrch(t)
	out, err := o.Create(ctx, CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "w1", NodeID: "auto",
		CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctOf(t, o.pool), Plan: "2c4g"})
	if err != nil {
		t.Fatal(err)
	}
	o.RunJob(ctx, out.AgentID)
	if jv, _ := o.JobProgress(ctx, out.AgentID); jv.Stage != "starting" || jv.AgentID != "" {
		t.Fatalf("after create_seat, before the agent connects: %+v, want starting", jv)
	}
	// 4 分钟：还在时限内，不清
	o.SweepTimeouts(ctx, time.Now().Add(4*time.Minute))
	if seat, _ := s.GetSeat(ctx, out.AgentID); seat.State != "starting" {
		t.Fatalf("swept too early: %+v", seat)
	}
	// 6 分钟：超 5 分钟，清成 start_timeout，Agent 吊销
	o.SweepTimeouts(ctx, time.Now().Add(6*time.Minute))
	if jv, _ := o.JobProgress(ctx, out.AgentID); jv.Stage != "failed" || jv.Reason != "start_timeout" {
		t.Fatalf("never connected: %+v, want failed/start_timeout", jv)
	}
	retired := false
	for _, id := range idp.retired {
		retired = retired || id == out.AgentID
	}
	if !retired {
		t.Fatal("timed-out seat must retire its agent")
	}
	// 超时之后才连入：不得把它拉回 online
	if err := o.MarkOnline(ctx, out.AgentID); err != nil {
		t.Fatal(err)
	}
	if seat, _ := s.GetSeat(ctx, out.AgentID); seat.State != "error" {
		t.Fatalf("late connect revived a timed-out seat: %+v", seat)
	}
}

// MarkOnline 只认 starting → online；别的状态（已 online、升级中、停用、不存在）一律不改、不报错。
func TestMarkOnlineOnlyFromStarting(t *testing.T) {
	ctx := context.Background()
	o, _, _, s := mkOrch(t)
	for _, state := range []string{"online", "busy", "disabled"} {
		id := "ag_m_" + state
		if err := s.CreateSeat(ctx, Seat{AgentID: id, NodeID: "nd_free", CLI: "codex", LLM: "gpt", CredKind: "token_file",
			Plan: "2c4g", ImageRef: "r", CreatedBy: "zzc"}); err != nil {
			t.Fatal(err)
		}
		if err := s.SetSeatState(ctx, id, state, "keep", "fixture", nil); err != nil {
			t.Fatal(err)
		}
		if err := o.MarkOnline(ctx, id); err != nil {
			t.Fatalf("%s: %v", state, err)
		}
		if seat, _ := s.GetSeat(ctx, id); seat.State != state || seat.StateDetail != "keep" {
			t.Errorf("%s seat changed by MarkOnline: %+v", state, seat)
		}
	}
	if err := o.MarkOnline(ctx, "ag_nosuch"); err != nil {
		t.Errorf("unknown seat: %v, want nil", err)
	}
}
