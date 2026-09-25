package store

// 阴性对照(复验席 opus50tc2k 补,组长 2026-09-25T16:40Z 裁定的两条之一)
//
// 判据形状按组长 16:25Z 那条:其余前置【全部摆成合法】,只让【疑点清单为空】这一项违规,
// 再断被拒,且断【具体的 sentinel】而不是"被拒了就算"。
// 两个子用例的**唯一差别就是清单空不空** —— 这就是排他性的来源。

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// 新 bundle 形态的验收标准:逐条齐 owner_ref / source / checked_by / checked_at / cases,
// 故 validSpecFinalAcceptance 与 legacy 旁路都不会成为被拒的原因。
const gateProbeAcceptance = `{"criteria":[{"id":"A1","text":"闸要挡住写错的 spec","owner_ref":"owner:先挡住","source":"owner","checked_by":"opus50tc2k","checked_at":"2026-09-25T16:25:00Z","cases":["console_spec_gate_empty_review_test.go"]}]}`

func gateProbeSpecInReview(t *testing.T, db *SQLite, specID string, now time.Time) {
	t.Helper()
	rev := specRevision(specID, 1, "# "+specID)
	rev.AcceptanceJSON = gateProbeAcceptance
	spec := model.ConsoleSpec{ID: specID, PrincipalID: "owner-a", Title: specID, EditCapabilityHash: specID + "-cap"}
	if err := db.CreateConsoleSpec(context.Background(), spec, rev, now); err != nil {
		t.Fatal(err)
	}
	if err := db.TransferConsoleSpec(context.Background(), specID, "owner-a", "丙", "review", now); err != nil {
		t.Fatal(err)
	}
	// 复核 Agent 归属人 owner-a ≠ 审结者 manager ⇒ ⑧ 那一项也是合法的
	if err := db.UpsertConsoleAgent(context.Background(), model.ConsoleAgent{
		ID: specID + "-reviewer", KeyID: specID + "-key", SessionName: specID, OwnerPrincipal: "owner-a", Active: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestConsoleSpecGateRejectsEmptyIssueListButAcceptsAnsweredOne(t *testing.T) {
	now := time.Date(2026, 9, 25, 16, 25, 0, 0, time.UTC)

	// 🔴 阴性:清单为空 ⇒ 「未逐条回复」无从谈起 ⇒ 必须挡住。
	// 业主原话是「先挡住」;零疑点与"根本没跑 AI 复核"在库里长得一模一样。
	for _, empty := range []struct{ name, issues string }{
		{"空数组", `[]`},
		{"null", `null`},
	} {
		t.Run("阴性_"+empty.name, func(t *testing.T) {
			db := newConsoleSpecStore(t)
			specID := "gate-empty-" + empty.name
			gateProbeSpecInReview(t, db, specID, now)
			saveErr := db.SaveConsoleSpecGateReview(context.Background(), specID, "manager",
				model.ConsoleSpecGateReview{ReviewAgentID: specID + "-reviewer", IssuesJSON: empty.issues}, now)
			finalErr := db.FinalizeConsoleSpec(context.Background(), specID, "manager", now)
			if saveErr == nil && finalErr == nil {
				t.Fatalf("issues=%s 竟然封板成功(save=%v finalize=%v) —— A5「全挡」被零疑点绕过", empty.issues, saveErr, finalErr)
			}
			if !errors.Is(saveErr, ErrConsoleSpecGate) && !errors.Is(finalErr, ErrConsoleSpecGate) {
				t.Fatalf("issues=%s 被拒了但不是 ErrConsoleSpecGate(save=%v finalize=%v) —— 拒的原因不对", empty.issues, saveErr, finalErr)
			}
			spec, err := db.ConsoleSpec(context.Background(), "owner-a", specID, false)
			if err != nil || spec == nil || spec.State == "final" {
				t.Fatalf("挡住了却仍变成 final:spec=%+v err=%v", spec, err)
			}
		})
	}

	// 🔵 阳性对照:同样的前置,只把清单换成【非空且逐条回复完整】⇒ 必须放行。
	// 没有这一格,上面那条可能只是"什么都挡"。
	t.Run("阳性_逐条回复完整", func(t *testing.T) {
		db := newConsoleSpecStore(t)
		specID := "gate-answered"
		gateProbeSpecInReview(t, db, specID, now)
		answered := `[{"id":"Q1","text":"owner_ref 是否逐条成立","response":"已补逐条引用","outcome":"true_problem"}]`
		if err := db.SaveConsoleSpecGateReview(context.Background(), specID, "manager",
			model.ConsoleSpecGateReview{ReviewAgentID: specID + "-reviewer", IssuesJSON: answered}, now); err != nil {
			t.Fatalf("回复完整的清单竟被拒:%v", err)
		}
		if err := db.FinalizeConsoleSpec(context.Background(), specID, "manager", now); err != nil {
			t.Fatalf("回复完整却封不了板:%v", err)
		}
	})
}
