package hosted

import (
	"context"
	"testing"
	"time"
)

// R-V2-0102-GROUP-FIXES A：新建链的三处失败（落库、执行链、超时清扫）一律 AbortHostedAgent（吊销＋回收本次新建的空组），
// 不用只吊销的 RetireHostedAgent；新组名走 IssueHostedAgentWithNewGroup。
// 断言面＝Orchestrator 对 IdentityIssuer 的调用；实现承诺＝上述分流。落库失败那处由 cmd/workpulse 的真存储格覆盖。
func TestCreateFailurePathsAbortTheIssue(t *testing.T) {
	parallelHostedTest(t)
	ctx := context.Background()
	contains := func(list []string, id string) bool {
		for _, v := range list {
			if v == id {
				return true
			}
		}
		return false
	}
	t.Run("new group name goes through IssueHostedAgentWithNewGroup", func(t *testing.T) {
		o, idp, _, _ := mkOrch(t)
		if _, err := o.Create(ctx, CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "w1", NodeID: "auto", NewGroupName: "新组",
			CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctOf(t, o.pool), Plan: "2c4g"}); err != nil {
			t.Fatal(err)
		}
		if len(idp.newGroups) != 1 || idp.newGroups[0] != "新组" {
			t.Fatalf("newGroups=%v", idp.newGroups)
		}
	})
	t.Run("run job failure aborts", func(t *testing.T) {
		o, idp, gw, _ := mkOrch(t)
		gw.failOn = "create_seat"
		out, err := o.Create(ctx, CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "w2", NodeID: "auto",
			CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctOf(t, o.pool), Plan: "2c4g"})
		if err != nil {
			t.Fatal(err)
		}
		o.RunJob(ctx, out.AgentID)
		if !contains(idp.aborted, out.AgentID) {
			t.Fatalf("RunJob failure must abort the issue: aborted=%v", idp.aborted)
		}
	})
	t.Run("create timeout aborts", func(t *testing.T) {
		o, idp, _, _ := mkOrch(t)
		out, err := o.Create(ctx, CreateRequest{OwnerLC: "zzc", By: "zzc", Name: "w3", NodeID: "auto",
			CLI: "codex", LLM: "gpt", CredKind: "token_file", AccountID: acctOf(t, o.pool), Plan: "2c4g"})
		if err != nil {
			t.Fatal(err)
		}
		o.SweepTimeouts(ctx, time.Now().Add(11*time.Minute))
		if !contains(idp.aborted, out.AgentID) {
			t.Fatalf("create timeout must abort the issue: aborted=%v", idp.aborted)
		}
	})
}
